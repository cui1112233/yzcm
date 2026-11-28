package yzcmusage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

func testSessionTracker(t *testing.T) (*Tracker, sqlmock.Sqlmock) {
	t.Helper()
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		db.Close()
	})
	tr := &Tracker{Store: Store{DB: db}, ReceiptDir: t.TempDir()}
	cacheReceiptTestBinding(t, tr)
	return tr, m
}
func expectSessionBinding(m sqlmock.Sqlmock, b *receiptBinding, physical string) {
	m.ExpectQuery("FROM yzcm_usage_receipt_bindings").WithArgs(receiptBindingSlot).WillReturnRows(sqlmock.NewRows([]string{"path", "store", "state", "origin", "filesystem", "levels"}).AddRow(b.pathDigest, b.id, "active", "new", physical, 1))
}

func testBoundSessionRequest(t *testing.T, ctx context.Context, subject, team, actor, id string, claim func(context.Context) error) *http.Request {
	t.Helper()
	ctx = WithSessionAttempt(WithAttribution(ctx, subject, team, actor), id, subject, team, actor, Fingerprint("shared"), claim)
	req, err := http.NewRequestWithContext(ctx, "POST", SessionChatEndpoint, strings.NewReader("private content"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer shared")
	return req
}

func TestSessionClaimAtTransportBoundary(t *testing.T) {
	tracker, m := testSessionTracker(t)
	expectSessionBinding(m, tracker.receiptBound, tracker.receiptBound.objects.encode())
	expectSessionBinding(m, tracker.receiptBound, tracker.receiptBound.objects.encode())
	var calls atomic.Int32
	tr := &trackedTransport{t: tracker, session: true, base: transportFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, context.DeadlineExceeded
	})}
	var claimed atomic.Bool
	claim := func(context.Context) error {
		if !claimed.CompareAndSwap(false, true) {
			return errors.New("already claimed")
		}
		return nil
	}
	req := testBoundSessionRequest(t, context.Background(), "member", "team", "founder", "unique", claim)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := tr.RoundTrip(req.Clone(req.Context()))
			if !errors.Is(err, ErrSessionReceiptUnknown) && !errors.Is(err, ErrSessionNotSent) {
				t.Errorf("unexpected result: %v", err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("sent %d times", calls.Load())
	}
}

func TestSessionTransportRefusesBeforeSending(t *testing.T) {
	tracker, m := testSessionTracker(t)
	expectSessionBinding(m, tracker.receiptBound, tracker.receiptBound.objects.encode())
	base := transportFunc(func(*http.Request) (*http.Response, error) { t.Fatal("unexpected network call"); return nil, nil })
	tr := &trackedTransport{t: tracker, session: true, base: base}
	denied := func(context.Context) error { return errors.New("revoked") }
	req := testBoundSessionRequest(t, context.Background(), "m", "t", "a", "id", denied)
	for _, tc := range []struct {
		name   string
		change func(*http.Request) *http.Request
	}{
		{"revoked", func(r *http.Request) *http.Request { return r }},
		{"missing binding", func(r *http.Request) *http.Request { return r.WithContext(context.Background()) }},
		{"forged actor", func(r *http.Request) *http.Request {
			return r.WithContext(WithAttribution(r.Context(), "m", "t", "admin"))
		}},
		{"unsafe endpoint", func(r *http.Request) *http.Request { r.URL.Host = "example.invalid"; return r }},
		{"old transport", func(r *http.Request) *http.Request { return r }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt := http.RoundTripper(tr)
			if tc.name == "old transport" {
				rt = tracker.Wrap(base)
			}
			_, err := rt.RoundTrip(tc.change(req.Clone(req.Context())))
			if !errors.Is(err, ErrSessionNotSent) {
				t.Fatal(err)
			}
		})
	}
}

func expectSessionRow(m sqlmock.Sqlmock, r receipt, state string, linked any) {
	m.ExpectQuery("SELECT subject,team_id,actor,credential_hash,state,provider_id").WithArgs(r.AttemptID).WillReturnRows(sqlmock.NewRows([]string{"subject", "team", "actor", "hash", "state", "provider"}).AddRow(r.Request.Owner, r.Request.TeamID, r.Request.ActorUsername, r.CredentialHash, state, linked))
}
func expectSessionPost(m sqlmock.Sqlmock, r receipt) {
	m.ExpectBegin()
	expectSessionRow(m, r, "unknown", nil)
	m.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs(r.Request.ID, r.Request.Owner, r.Request.TeamID, r.Request.ActorUsername, r.Request.Feature, r.CredentialHash, sqlmock.AnyArg(), 200).WillReturnResult(sqlmock.NewResult(1, 1))
	m.ExpectExec("UPDATE screenplay_session_usage_attempts SET state='captured'").WithArgs(r.Request.ID, r.AttemptID).WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
}

func TestSessionSharedKeyLateReceiptsUseFrozenAttribution(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: t.TempDir()}
	cacheReceiptTestBinding(t, tracker)
	for _, subject := range []string{"member-a", "member-b"} {
		r := receipt{AttemptID: "attempt-" + subject, Request: Request{ID: "provider-" + subject, Owner: subject, TeamID: "team-" + subject, ActorUsername: "founder", Feature: "model"}, CredentialHash: Fingerprint("shared"), Status: 200, At: time.Now()}
		expectSessionBinding(m, tracker.receiptBound, tracker.receiptBound.objects.encode())
		expectSessionPost(m, r)
		ctx, cancel := context.WithCancel(context.Background())
		req := testBoundSessionRequest(t, ctx, subject, r.Request.TeamID, "founder", r.AttemptID, func(context.Context) error { return nil })
		tr := &trackedTransport{session: true, t: tracker, base: transportFunc(func(req *http.Request) (*http.Response, error) {
			cancel()
			for _, v := range req.Header {
				for _, h := range v {
					if strings.Contains(h, r.AttemptID) || strings.Contains(h, "founder") {
						t.Fatal("exposed internal attribution")
					}
				}
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"X-Oneapi-Request-Id": {r.Request.ID}}, Body: io.NopCloser(strings.NewReader("answer"))}, nil
		})}
		resp, err := tr.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionMissingHeaderNeverUsesTraceOrToken(t *testing.T) {
	tracker, m := testSessionTracker(t)
	expectSessionBinding(m, tracker.receiptBound, tracker.receiptBound.objects.encode())
	req := testBoundSessionRequest(t, context.Background(), "m", "t", "a", "unique", func(context.Context) error { return nil })
	tr := &trackedTransport{t: tracker, session: true, base: transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Request-Id": {"trace"}}, Body: http.NoBody}, nil
	})}
	if _, err := tr.RoundTrip(req); !errors.Is(err, ErrSessionReceiptUnknown) {
		t.Fatal(err)
	}
	if len(receiptFiles(t, tracker.ReceiptDir)) != 0 {
		t.Fatal("invented provider ID")
	}
}

func TestSessionRecoveryFileAndUnboundCollision(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tr := &Tracker{Store: Store{DB: db}, ReceiptDir: t.TempDir()}
	cacheReceiptTestBinding(t, tr)
	r := receipt{AttemptID: "a", At: time.Now(), CredentialHash: Fingerprint("shared"), Status: 200, Request: Request{ID: "provider", Owner: "subject", TeamID: "team", ActorUsername: "founder", Feature: "assistant-session"}}
	m.ExpectBegin().WillReturnError(errors.New("offline"))
	if err := tr.recordBoundReceipt(context.Background(), tr.receiptBound, r); !errors.Is(err, ErrSessionReceiptOnDisk) {
		t.Fatal(err)
	}
	other := r
	other.AttemptID = "other"
	if err := saveReceiptAt(tr.ReceiptDir, other); !errors.Is(err, ErrReceiptAttributionConflict) {
		t.Fatal(err)
	}
	old := r
	old.AttemptID = ""
	if err := saveReceiptAt(tr.ReceiptDir, old); !errors.Is(err, ErrReceiptAttributionConflict) {
		t.Fatal(err)
	}
	expectSessionPost(m, r)
	tr.flushReceipts(context.Background())
	if len(receiptFiles(t, tr.ReceiptDir)) != 0 {
		t.Fatal("replay did not drain")
	}
	m.ExpectBegin()
	expectSessionRow(m, r, "unknown", nil)
	m.ExpectExec("INSERT INTO yzcm_usage_requests").WillReturnError(&mysql.MySQLError{Number: 1062})
	m.ExpectRollback()
	dbOnly := &Tracker{Store: Store{DB: db}, DBOnly: true}
	if err := dbOnly.recordBoundReceipt(context.Background(), nil, r); !errors.Is(err, ErrReceiptAttributionConflict) {
		t.Fatal(err)
	}
	expectSessionPost(m, r)
	if err := dbOnly.recordBoundReceipt(context.Background(), nil, r); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSessionRejectsLegacyBindingWithMatchingMarkers(t *testing.T) {
	tr, m := testSessionTracker(t)
	expectSessionBinding(m, tr.receiptBound, "")
	req := testBoundSessionRequest(t, context.Background(), "m", "t", "a", "id", func(context.Context) error { t.Fatal("legacy storage claim"); return nil })
	rt := &trackedTransport{t: tr, session: true, base: transportFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("paid call despite legacy binding")
		return nil, nil
	})}
	if _, err := rt.RoundTrip(req); !errors.Is(err, ErrSessionNotSent) {
		t.Fatal(err)
	}
}

func TestSessionDBOnlyReplayAndFailure(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tr := &Tracker{Store: Store{DB: db}, DBOnly: true}
	r := receipt{AttemptID: "attempt-a", At: time.Now(), CredentialHash: Fingerprint("k"), Status: 200, Request: Request{ID: "real-a", Owner: "member", TeamID: "team", ActorUsername: "operator", Feature: "assistant-session"}}
	m.ExpectBegin().WillReturnError(errors.New("database unavailable"))
	if err := tr.recordBoundReceipt(context.Background(), nil, r); !errors.Is(err, ErrReceiptPersistenceUnavailable) {
		t.Fatal(err)
	}
	m.ExpectBegin()
	expectSessionRow(m, r, "captured", "other")
	m.ExpectRollback()
	if err := tr.recordBoundReceipt(context.Background(), nil, r); !errors.Is(err, ErrReceiptAttributionConflict) {
		t.Fatal(err)
	}
	m.ExpectBegin()
	expectSessionRow(m, r, "captured", r.Request.ID)
	m.ExpectQuery("SELECT owner,COALESCE").WithArgs(r.Request.ID).WillReturnRows(sqlmock.NewRows([]string{"owner", "team", "actor", "hash"}).AddRow("member", "team", "operator", r.CredentialHash))
	m.ExpectCommit()
	if err := tr.recordBoundReceipt(context.Background(), nil, r); err != nil {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
