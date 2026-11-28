package screenplay

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
)

type preparationRepo struct {
	a         AssistantTextAttachment
	events    *[]string
	duplicate bool
	ready     int
}

func (r *preparationRepo) BeginIntent(_ context.Context, s AssistantReferenceScope, a AssistantTextAttachment, g AssistantAttachmentGuard) (AssistantTextAttachment, string, error) {
	*r.events = append(*r.events, "intent-committed")
	r.a = a
	if r.duplicate {
		return a, "", nil
	}
	return a, "token", nil
}
func (r *preparationRepo) InspectOwnIntentByID(context.Context, AssistantReferenceScope, string, AssistantAttachmentGuard) (AssistantAttachmentPreparationEntry, error) {
	return AssistantAttachmentPreparationEntry{Attachment: r.a}, nil
}
func (r *preparationRepo) ClaimRetry(context.Context, AssistantReferenceScope, string, string, int64, AssistantAttachmentGuard) (AssistantTextAttachment, string, error) {
	*r.events = append(*r.events, "claim-committed")
	return r.a, "token", nil
}
func (r *preparationRepo) MarkReady(ctx context.Context, _ AssistantReferenceScope, _ string, _ string, _ string, _ AssistantAttachmentGuard) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	r.ready++
	*r.events = append(*r.events, "ready-committed")
	return nil
}

type preparationObjects struct {
	events    *[]string
	missing   bool
	fail      error
	onPut     func(context.Context)
	deadlines []time.Time
}

func (o *preparationObjects) NamespaceID() string                { return strings.Repeat("a", 64) }
func (o *preparationObjects) CheckPrivacy(context.Context) error { return nil }
func (o *preparationObjects) PutVerified(ctx context.Context, a AssistantTextAttachment, raw []byte) error {
	d, ok := ctx.Deadline()
	if !ok {
		panic("deadline absent")
	}
	o.deadlines = append(o.deadlines, d)
	*o.events = append(*o.events, "put-verified")
	if o.onPut != nil {
		o.onPut(ctx)
	}
	return o.fail
}
func (o *preparationObjects) OpenVerified(ctx context.Context, a AssistantTextAttachment) ([]byte, error) {
	d, _ := ctx.Deadline()
	o.deadlines = append(o.deadlines, d)
	*o.events = append(*o.events, "open-verified")
	if o.fail != nil {
		return nil, o.fail
	}
	if o.missing {
		return nil, ErrAssistantAttachmentObjectNotFound
	}
	return []byte(a.Text), nil
}
func TestAssistantTextAttachmentPreparationUpload(t *testing.T) {
	events := []string{}
	r := &preparationRepo{events: &events}
	o := &preparationObjects{events: &events}
	svc := AssistantTextAttachmentPreparation{Repository: r, Objects: o}
	verify := func(context.Context) error { events = append(events, "verify"); return nil }
	guard := func(context.Context, *sql.Tx) error { return nil }
	a, e := svc.Upload(context.Background(), textScope(t, "actual"), "x.md", "stable-request-key", []byte("hello"), verify, guard)
	if e != nil || a.Status != "ready" || r.ready != 1 || strings.Join(events, ",") != "verify,intent-committed,put-verified,verify,ready-committed" {
		t.Fatal(a, e, events)
	}
	events = nil
	r.duplicate = true
	if _, e = svc.Upload(context.Background(), textScope(t, "actual"), "x.md", "stable-request-key", []byte("hello"), verify, guard); e != nil || strings.Contains(strings.Join(events, ","), "put") {
		t.Fatal(e, events)
	}
	events = nil
	r.duplicate = false
	o.onPut = func(context.Context) {}
	ctx, cancel := context.WithCancel(context.Background())
	o.onPut = func(context.Context) { cancel() }
	if _, e = svc.Upload(ctx, textScope(t, "actual"), "x.md", "stable-request-key", []byte("hello"), verify, guard); !errors.Is(e, context.Canceled) || r.ready != 1 {
		t.Fatal("late ready", e, r.ready)
	}
}
func TestAssistantTextAttachmentPreparationRetry(t *testing.T) {
	for _, missing := range []bool{false, true} {
		events := []string{}
		r := &preparationRepo{events: &events}
		a, _ := PrepareAssistantTextAttachment(textScope(t, "actual"), "id", "x.md", strings.Repeat("a", 64), []byte("hello"))
		a.RequestKeyHash = contentDigest("stable-request-key")
		a.ObjectKey = "assistant-text/id"
		r.a = a
		o := &preparationObjects{events: &events, missing: missing}
		svc := AssistantTextAttachmentPreparation{Repository: r, Objects: o}
		guard := func(context.Context, *sql.Tx) error { return nil }
		_, e := svc.Retry(context.Background(), textScope(t, "actual"), a.ID, "stable-request-key", a.StateRevision, []byte("hello"), func(context.Context) error { return nil }, guard)
		if e != nil || r.ready != 1 {
			t.Fatal(e, events)
		}
		if missing && (len(o.deadlines) != 2 || !o.deadlines[0].Equal(o.deadlines[1])) {
			t.Fatal("retry extended deadline", o.deadlines)
		}
		if !missing && strings.Contains(strings.Join(events, ","), "put") {
			t.Fatal("second object write", events)
		}
	}
}

func TestAssistantTextAttachmentPreparationRetryUnknownAndMissing(t *testing.T) {
	for _, scenario := range []string{"unknown", "missing_without_file", "wrong_file", "wrong_key"} {
		t.Run(scenario, func(t *testing.T) {
			events := []string{}
			r := &preparationRepo{events: &events}
			a, _ := PrepareAssistantTextAttachment(textScope(t, "actual"), "id", "x.md", strings.Repeat("a", 64), []byte("hello"))
			a.RequestKeyHash = contentDigest("stable-request-key")
			a.ObjectKey = "assistant-text/id"
			r.a = a
			o := &preparationObjects{events: &events, missing: scenario == "missing_without_file"}
			if scenario == "unknown" {
				o.fail = errors.New("PRIVATE_SDK_URL")
			}
			svc := AssistantTextAttachmentPreparation{Repository: r, Objects: o}
			key := "stable-request-key"
			var raw []byte
			if scenario == "wrong_file" {
				raw = []byte("wrong")
			}
			if scenario == "wrong_key" {
				key = "another-stable-key"
			}
			_, e := svc.Retry(context.Background(), textScope(t, "actual"), a.ID, key, a.StateRevision, raw, func(context.Context) error { return nil }, func(context.Context, *sql.Tx) error { return nil })
			if e == nil || r.ready != 0 || strings.Contains(strings.Join(events, ","), "put") {
				t.Fatal(e, events)
			}
			if scenario == "missing_without_file" && !errors.Is(e, ErrAssistantAttachmentOriginalRequired) {
				t.Fatal(e)
			}
		})
	}
}
func TestAssistantTextAttachmentPreparationBudgetBeforeIntent(t *testing.T) {
	events := []string{}
	r := &preparationRepo{events: &events}
	o := &preparationObjects{events: &events}
	svc := AssistantTextAttachmentPreparation{Repository: r, Objects: o}
	_, e := svc.Upload(context.Background(), textScope(t, "actual"), "x.md", "stable-request-key", []byte(strings.Repeat("<", 10000)), func(context.Context) error { return nil }, func(context.Context, *sql.Tx) error { return nil })
	if e == nil || len(events) != 0 {
		t.Fatal("oversize created intent or TOS write", e, events)
	}
}
func TestAssistantTextAttachmentPreparationRevokedAfterNetwork(t *testing.T) {
	events := []string{}
	r := &preparationRepo{events: &events}
	o := &preparationObjects{events: &events}
	svc := AssistantTextAttachmentPreparation{Repository: r, Objects: o}
	calls := 0
	_, e := svc.Upload(context.Background(), textScope(t, "actual"), "x.md", "stable-request-key", []byte("hello"), func(context.Context) error {
		calls++
		if calls > 1 {
			return ErrAccessDenied
		}
		return nil
	}, func(context.Context, *sql.Tx) error { return nil })
	if !errors.Is(e, ErrAccessDenied) || r.ready != 0 {
		t.Fatal("revoked ready", e)
	}
}
