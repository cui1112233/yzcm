package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"qiantie/backend/internal/novelfetchworkshop"
)

type rulesBodyFaultStore struct {
	*novelfetchworkshop.MemoryStore
	readErrors, writeErrors map[string]error
	documentError           error
}

func (s *rulesBodyFaultStore) GetBody(c context.Context, o, b, v string) (novelfetchworkshop.BodyRecord, error) {
	if e := s.readErrors[v]; e != nil {
		return novelfetchworkshop.BodyRecord{}, e
	}
	return s.MemoryStore.GetBody(c, o, b, v)
}
func (s *rulesBodyFaultStore) PutBody(c context.Context, o string, b novelfetchworkshop.BodyRecord) (novelfetchworkshop.BodyRef, error) {
	if e := s.writeErrors[b.VersionID]; e != nil {
		return novelfetchworkshop.BodyRef{}, e
	}
	return s.MemoryStore.PutBody(c, o, b)
}
func (s *rulesBodyFaultStore) PutDocument(c context.Context, o string, d novelfetchworkshop.Document) error {
	if s.documentError != nil {
		return s.documentError
	}
	return s.MemoryStore.PutDocument(c, o, d)
}
func rulesBodySetup(t *testing.T, d novelfetchworkshop.Document) *rulesBodyFaultStore {
	t.Helper()
	s := &rulesBodyFaultStore{MemoryStore: novelfetchworkshop.NewMemoryStore()}
	d.BookID = "123"
	if d.Meta == nil {
		d.Meta = map[string]any{}
	}
	if e := s.PutDocument(context.Background(), "alice", d); e != nil {
		t.Fatal(e)
	}
	return s
}
func rulesBodyRequest(t *testing.T, s novelfetchworkshop.Store, scope string) map[string]any {
	t.Helper()
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"ids":["123"],"scope":"`+scope+`"}`))
	r = r.WithContext(context.WithValue(r.Context(), bridgeIdentityKey{}, BridgeIdentity{Username: "alice"}))
	w := httptest.NewRecorder()
	applySensitive(w, r, s, false, nil)
	if w.Code != 200 {
		t.Fatalf("HTTP %d %s", w.Code, w.Body.String())
	}
	var out map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	return out
}
func rulesBodyResult(t *testing.T, out map[string]any, v, status string) {
	t.Helper()
	rows, _ := out["version_results"].([]any)
	for _, raw := range rows {
		r := raw.(map[string]any)
		if r["version_id"] == v {
			if r["book_id"] != "123" || r["status"] != status {
				t.Fatalf("bad result %v", r)
			}
			if status == "failed" && r["error"] == "" {
				t.Fatalf("missing error %v", r)
			}
			return
		}
	}
	t.Fatalf("missing %s result %v", v, out)
}
func TestRulesBodyUsesOnlyExplicitText(t *testing.T) {
	cases := []struct {
		name                 string
		raw                  any
		stored, want, status string
	}{
		{"stored metadata", map[string]any{"state": "ready", "versionId": "ai1"}, "原有AI正文", "原有AI正文", "success"}, {"stored wins", "旧字符串", "独立正文", "独立正文", "success"}, {"string", "兼容正文", "", "兼容正文", "success"}, {"text", map[string]any{"state": "ready", "text": "明确正文"}, "", "明确正文", "success"}, {"metadata", map[string]any{"state": "ready"}, "", "", "skipped"}, {"false", false, "", "", "skipped"}, {"nil", nil, "", "", "skipped"}, {"empty", "  ", "", "", "skipped"}, {"bad text", map[string]any{"text": false}, "", "", "skipped"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := rulesBodySetup(t, novelfetchworkshop.Document{Versions: map[string]any{"ai1": tc.raw}})
			if tc.stored != "" {
				s.PutBody(context.Background(), "alice", novelfetchworkshop.BodyRecord{BookID: "123", BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai1"}, Content: tc.stored})
			}
			out := rulesBodyRequest(t, s, "ai")
			b, e := s.GetBody(context.Background(), "alice", "123", "ai1")
			if tc.want == "" {
				if !errors.Is(e, novelfetchworkshop.ErrNotFound) {
					t.Fatalf("fake body %q %v", b.Content, e)
				}
			} else if e != nil || b.Content != tc.want {
				t.Fatalf("body %q error %v want %q", b.Content, e, tc.want)
			}
			rulesBodyResult(t, out, "ai1", tc.status)
		})
	}
}
func TestRulesBodyStoredRefWithoutInlineVersion(t *testing.T) {
	s := rulesBodySetup(t, novelfetchworkshop.Document{})
	s.PutBody(context.Background(), "alice", novelfetchworkshop.BodyRecord{BookID: "123", BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai1"}, Content: "独立正文"})
	rulesBodyResult(t, rulesBodyRequest(t, s, "ai"), "ai1", "success")
}
func TestRulesBodyReadErrorNeverFallsBack(t *testing.T) {
	s := rulesBodySetup(t, novelfetchworkshop.Document{Versions: map[string]any{"ai1": "旧正文"}})
	s.readErrors = map[string]error{"ai1": errors.New("body unavailable")}
	out := rulesBodyRequest(t, s, "ai")
	rulesBodyResult(t, out, "ai1", "failed")
	if out["updated"] != float64(0) || out["failed"] != float64(1) {
		t.Fatal(out)
	}
	_, e := s.MemoryStore.GetBody(context.Background(), "alice", "123", "ai1")
	if !errors.Is(e, novelfetchworkshop.ErrNotFound) {
		t.Fatal("fallback saved after read error")
	}
}
func TestRulesBodyMixedWriteErrorsCountBookOnce(t *testing.T) {
	d := novelfetchworkshop.Document{Versions: map[string]any{"ai1": "正文一", "ai2": map[string]any{"text": "正文二", "state": "ready"}, "ai3": "正文三"}, Meta: map[string]any{"sensitiveStatus": "waiting"}}
	s := rulesBodySetup(t, d)
	s.writeErrors = map[string]error{"ai1": errors.New("write failed"), "ai2": errors.New("write failed")}
	out := rulesBodyRequest(t, s, "ai")
	if out["failed"] != float64(1) || out["updated"] != float64(1) {
		t.Fatal(out)
	}
	rulesBodyResult(t, out, "ai1", "failed")
	rulesBodyResult(t, out, "ai2", "failed")
	rulesBodyResult(t, out, "ai3", "success")
	saved, _ := s.GetDocument(context.Background(), "alice", "123")
	if !reflect.DeepEqual(saved.Versions["ai2"], d.Versions["ai2"]) || saved.Meta["sensitiveStatus"] == "done" {
		t.Fatalf("failure overwritten or done %+v", saved)
	}
}
func TestRulesBodyOriginalWriteFailurePreservesDocument(t *testing.T) {
	s := rulesBodySetup(t, novelfetchworkshop.Document{Original: "原文", Meta: map[string]any{"sensitiveStatus": "waiting"}})
	s.writeErrors = map[string]error{"original": errors.New("write failed")}
	out := rulesBodyRequest(t, s, "original")
	rulesBodyResult(t, out, "original", "failed")
	saved, _ := s.GetDocument(context.Background(), "alice", "123")
	if saved.Meta["sensitiveStatus"] != "waiting" || out["updated"] != float64(0) || out["failed"] != float64(1) {
		t.Fatalf("%v %+v", out, saved)
	}
}
func TestRulesBodyDocumentWriteFailureIsNotSuccess(t *testing.T) {
	s := rulesBodySetup(t, novelfetchworkshop.Document{Versions: map[string]any{"ai1": "正文"}})
	s.documentError = errors.New("document unavailable")
	out := rulesBodyRequest(t, s, "ai")
	rulesBodyResult(t, out, "ai1", "failed")
	if out["updated"] != float64(0) || out["failed"] != float64(1) {
		t.Fatal(out)
	}
}

type rulesBodyConfigErrorStore struct{ *rulesBodyFaultStore }

func (s rulesBodyConfigErrorStore) GetConfig(context.Context, string) (map[string]any, error) {
	return nil, errors.New("config unavailable")
}
func TestRulesBodyOriginalProcessErrorReported(t *testing.T) {
	s := rulesBodySetup(t, novelfetchworkshop.Document{Original: "原文"})
	out := rulesBodyRequest(t, rulesBodyConfigErrorStore{s}, "original")
	rulesBodyResult(t, out, "original", "failed")
	if out["failed"] != float64(1) || out["updated"] != float64(0) {
		t.Fatal(out)
	}
}
func TestRulesBodyRestoreConfigErrorReported(t *testing.T) {
	s := rulesBodySetup(t, novelfetchworkshop.Document{Original: "当前正文", OriginalRaw: "完整原文"})
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"ids":["123"],"scope":"original"}`))
	r = r.WithContext(context.WithValue(r.Context(), bridgeIdentityKey{}, BridgeIdentity{Username: "alice"}))
	w := httptest.NewRecorder()
	applySensitive(w, r, rulesBodyConfigErrorStore{s}, true, nil)
	var out map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		t.Fatal(e)
	}
	rulesBodyResult(t, out, "original", "failed")
	_, e := s.GetBody(context.Background(), "alice", "123", "original")
	if !errors.Is(e, novelfetchworkshop.ErrNotFound) {
		t.Fatal("saved using unreadable rule settings")
	}
}
func TestRulesBodyEmptyVersionsSkipped(t *testing.T) {
	s := rulesBodySetup(t, novelfetchworkshop.Document{})
	out := rulesBodyRequest(t, s, "ai")
	if out["skipped"] != float64(1) || out["updated"] != float64(0) || out["failed"] != float64(0) {
		t.Fatal(out)
	}
	d, _ := s.GetDocument(context.Background(), "alice", "123")
	if d.Meta["sensitiveStatus"] == "done" {
		t.Fatal("empty book marked done")
	}
}
func TestRulesBodyOriginalPrefersStoredBody(t *testing.T) {
	s := rulesBodySetup(t, novelfetchworkshop.Document{Original: "旧内联正文", ProcessedText: "旧处理正文", OriginalRaw: "完整原始正文"})
	s.PutBody(context.Background(), "alice", novelfetchworkshop.BodyRecord{BookID: "123", BodyRef: novelfetchworkshop.BodyRef{VersionID: "original"}, Content: "独立正文"})
	out := rulesBodyRequest(t, s, "original")
	rulesBodyResult(t, out, "original", "success")
	d, _ := s.GetDocument(context.Background(), "alice", "123")
	if d.ProcessedText != "独立正文" || d.OriginalRaw != "完整原始正文" {
		t.Fatalf("%+v", d)
	}
}
func TestRulesBodyAIOnlyDoesNotRewriteOriginal(t *testing.T) {
	s := rulesBodySetup(t, novelfetchworkshop.Document{Original: "原文", Versions: map[string]any{"ai1": "AI正文"}})
	s.PutBody(context.Background(), "alice", novelfetchworkshop.BodyRecord{BookID: "123", BodyRef: novelfetchworkshop.BodyRef{VersionID: "original"}, Content: "原文"})
	before, _ := s.GetBody(context.Background(), "alice", "123", "original")
	rulesBodyRequest(t, s, "ai")
	after, _ := s.GetBody(context.Background(), "alice", "123", "original")
	if after.Revision != before.Revision {
		t.Fatal("AI-only rules rewrote original")
	}
}

type rulesBodySecondConfigErrorStore struct {
	*rulesBodyFaultStore
	reads int
}

func (s *rulesBodySecondConfigErrorStore) GetConfig(ctx context.Context, owner string) (map[string]any, error) {
	s.reads++
	if s.reads > 1 {
		return nil, errors.New("processing settings unavailable")
	}
	return s.MemoryStore.GetConfig(ctx, owner)
}
func TestRulesBodyOriginalProcessingErrorCountsFailure(t *testing.T) {
	s := rulesBodySetup(t, novelfetchworkshop.Document{Original: "原文"})
	out := rulesBodyRequest(t, &rulesBodySecondConfigErrorStore{rulesBodyFaultStore: s}, "original")
	rulesBodyResult(t, out, "original", "failed")
	if out["failed"] != float64(1) || out["updated"] != float64(0) {
		t.Fatal(out)
	}
}
func TestRulesBodyProcessesTextAndPreservesFailedStoredVersion(t *testing.T) {
	ctx := context.Background()
	s := rulesBodySetup(t, novelfetchworkshop.Document{Versions: map[string]any{"ai1": map[string]any{"state": "ready"}, "ai2": "坏词正文"}})
	s.PutConfig(ctx, "alice", map[string]any{"appConfig": map[string]any{"layout": map[string]any{"apply_to_ai": true, "apply_sensitive": true}, "sensitive": map[string]any{"groups": []any{map[string]any{"enabled": true, "apply_to_ai": true, "rules": []any{map[string]any{"find": "坏词", "replace": "好词"}}}}}}})
	ref, e := s.PutBody(ctx, "alice", novelfetchworkshop.BodyRecord{BookID: "123", BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai1"}, Content: "坏词原文"})
	if e != nil {
		t.Fatal(e)
	}
	d, _ := s.GetDocument(ctx, "alice", "123")
	d.BodyRefs = map[string]novelfetchworkshop.BodyRef{"ai1": ref}
	s.PutDocument(ctx, "alice", d)
	s.writeErrors = map[string]error{"ai1": errors.New("save unavailable")}
	out := rulesBodyRequest(t, s, "ai")
	rulesBodyResult(t, out, "ai1", "failed")
	rulesBodyResult(t, out, "ai2", "success")
	old, _ := s.GetBody(ctx, "alice", "123", "ai1")
	newBody, _ := s.GetBody(ctx, "alice", "123", "ai2")
	saved, _ := s.GetDocument(ctx, "alice", "123")
	if old.Content != "坏词原文" || old.Revision != ref.Revision || newBody.Content != "好词正文" || !reflect.DeepEqual(saved.BodyRefs["ai1"], ref) {
		t.Fatalf("old=%+v new=%+v saved=%+v", old, newBody, saved)
	}
	if out["replacements"] != float64(1) {
		t.Fatal(out)
	}
}
