package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"qiantie/backend/internal/novelfetchworkshop"
)

type workflowReadIsolationStore struct {
	*novelfetchworkshop.MemoryStore
	workflowErr         error
	strictReads, writes int
}

func (s *workflowReadIsolationStore) GetConfig(context.Context, string) (map[string]any, error) {
	s.strictReads++
	return nil, errors.New("legacy cleanup is pending")
}
func (s *workflowReadIsolationStore) GetWorkflowConfig(ctx context.Context, owner string) (map[string]any, error) {
	if s.workflowErr != nil {
		return nil, s.workflowErr
	}
	cfg, err := s.MemoryStore.GetConfig(ctx, owner)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"modelCatalog", "modelCatalogLegacyVideoPending", "modelCatalogLegacyScrubPending", "modelCatalogSecretVersion"} {
		delete(cfg, key)
	}
	return cfg, nil
}
func (s *workflowReadIsolationStore) ReadBoundModelConfig(context.Context, string) (map[string]any, error) {
	return map[string]any{"modelCatalog": []any{}, novelfetchworkshop.ModelSecretVersionKey: novelfetchworkshop.ModelSecretVersion}, nil
}
func (s *workflowReadIsolationStore) PutConfig(ctx context.Context, owner string, cfg map[string]any) error {
	s.writes++
	return s.MemoryStore.PutConfig(ctx, owner, cfg)
}
func workflowIsolationFixture(t *testing.T) *workflowReadIsolationStore {
	t.Helper()
	s := &workflowReadIsolationStore{MemoryStore: novelfetchworkshop.NewMemoryStore()}
	cfg := map[string]any{"platforms": []any{map[string]any{"id": "9", "name": "saved-platform", "visible": true}}, "novel_fetch_current_batch_id": "batch-current", "novel_fetch_schedules": []any{map[string]any{"id": "kept-schedule"}}, "modelCatalog": []any{map[string]any{"id": "keep-bound", "apiKey": "sealed-test-placeholder"}}, "modelCatalogLegacyVideoPending": []any{map[string]any{"id": "unverified-video"}}, "modelCatalogLegacyScrubPending": "keep-scrub-plan", "appConfig": map[string]any{"layout": map[string]any{"apply_to_original": true, "apply_sensitive": true}, "sensitive": map[string]any{"groups": []any{map[string]any{"enabled": true, "apply_to_original": true, "rules": []any{map[string]any{"find": "坏词", "replace": "好词"}}}}}}}
	if err := s.MemoryStore.PutConfig(context.Background(), "alice", cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.PutDocument(context.Background(), "alice", novelfetchworkshop.Document{BookID: "123", Original: "坏词正文", Meta: map[string]any{"currentBatchId": "batch-current"}}); err != nil {
		t.Fatal(err)
	}
	return s
}
func workflowIsolationRouter(s novelfetchworkshop.Store) http.Handler {
	mux := http.NewServeMux()
	registerNovelFetchWorkshopRoutes(mux, s, nil, nil, nil, nil, nil, nil, "", nil, nil, nil)
	registerNovelFetchLegacyWorkflowRoutes(mux, s, nil, "", nil)
	return mux
}
func workflowIsolationRequest(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r = r.WithContext(context.WithValue(r.Context(), bridgeIdentityKey{}, BridgeIdentity{Username: "alice"}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func TestWorkflowReadsIgnorePendingLegacyCleanupWithoutWriting(t *testing.T) {
	for _, item := range []struct{ method, path, body string }{{"GET", "/api/novel-fetch-workshop/platforms", ""}, {"GET", "/api/novel-fetch-workshop/batches/current", ""}, {"GET", "/api/novel-fetch-workshop/schedules", ""}, {"GET", "/api/novel-fetch-workshop/knowledge/summary", ""}, {"GET", "/api/novel-fetch-workshop/config", ""}, {"POST", "/api/novel-fetch-workshop/rules/preview", `{"text":"坏词正文","scope":"original"}`}} {
		t.Run(item.path, func(t *testing.T) {
			s := workflowIsolationFixture(t)
			before, _ := s.MemoryStore.GetConfig(context.Background(), "alice")
			beforeJSON, _ := json.Marshal(before)
			r := workflowIsolationRequest(workflowIsolationRouter(s), item.method, item.path, item.body)
			if r.Code != 200 {
				t.Fatalf("pending cleanup blocked ordinary read: %d %s", r.Code, r.Body.String())
			}
			after, _ := s.MemoryStore.GetConfig(context.Background(), "alice")
			afterJSON, _ := json.Marshal(after)
			if s.strictReads != 0 || s.writes != 0 || string(beforeJSON) != string(afterJSON) {
				t.Fatalf("read triggered strict migration/config mutation: strict=%d writes=%d", s.strictReads, s.writes)
			}
		})
	}
}
func TestWorkflowReadDatabaseErrorsStillFail(t *testing.T) {
	for _, item := range []struct{ method, path, body string }{{"GET", "/api/novel-fetch-workshop/platforms", ""}, {"GET", "/api/novel-fetch-workshop/batches/current", ""}, {"GET", "/api/novel-fetch-workshop/schedules", ""}, {"GET", "/api/novel-fetch-workshop/config", ""}, {"POST", "/api/novel-fetch-workshop/rules/preview", `{"text":"正文"}`}} {
		t.Run(item.path, func(t *testing.T) {
			s := workflowIsolationFixture(t)
			s.workflowErr = errors.New("database read unavailable")
			r := workflowIsolationRequest(workflowIsolationRouter(s), item.method, item.path, item.body)
			if r.Code != 500 || s.writes != 0 {
				t.Fatalf("DB error swallowed: HTTP%d writes=%d", r.Code, s.writes)
			}
		})
	}
}
func TestWorkflowTrimmedViewIsNeverSavedAsFullConfig(t *testing.T) {
	s := workflowIsolationFixture(t)
	before, _ := s.MemoryStore.GetConfig(context.Background(), "alice")
	want, _ := json.Marshal(before)
	r := workflowIsolationRequest(workflowIsolationRouter(s), "PUT", "/api/novel-fetch-workshop/config", `{"workflow":{"min_original_chars":1234}}`)
	after, _ := s.MemoryStore.GetConfig(context.Background(), "alice")
	got, _ := json.Marshal(after)
	if r.Code != 409 || s.writes != 0 || string(want) != string(got) {
		t.Fatalf("sliced workflow overwrote protected model state: HTTP%d writes=%d", r.Code, s.writes)
	}
}
func TestWorkflowRuleApplyUsesPreferenceReadThroughWorkerWrappers(t *testing.T) {
	s := workflowIsolationFixture(t)
	lease := workshopProcessLeaseStore{Store: s, checkOwnership: func() error { return nil }}
	guarded := workshopBookExecutionStore{Store: lease, owner: "alice", bookID: "123"}
	r := workflowIsolationRequest(workflowIsolationRouter(guarded), "POST", "/api/novel-fetch-workshop/tasks/apply-rules", `{"ids":["123"],"scope":"original"}`)
	var out map[string]any
	json.Unmarshal(r.Body.Bytes(), &out)
	body, err := s.GetBody(context.Background(), "alice", "123", "original")
	if r.Code != 200 || out["updated"] != float64(1) || err != nil || body.Content != "好词正文" || s.strictReads != 0 || s.writes != 0 {
		t.Fatalf("rules failed through read-only wrappers: HTTP%d result=%v body=%q err=%v strict=%d", r.Code, out, body.Content, err, s.strictReads)
	}
}

func (s *workflowReadIsolationStore) ReadRuntimeConfig(ctx context.Context, owner string) (map[string]any, error) {
	cfg, err := s.GetWorkflowConfig(ctx, owner)
	if err != nil {
		return nil, err
	}
	cfg["modelCatalog"] = []any{}
	return cfg, nil
}

type workflowWriteIsolationStore struct {
	*workflowReadIsolationStore
	preferenceWrites int
	writeErr         error
}

func (s *workflowWriteIsolationStore) PutWorkflowConfig(ctx context.Context, owner string, prefs map[string]any) error {
	s.preferenceWrites++
	if s.writeErr != nil {
		return s.writeErr
	}
	raw, err := s.MemoryStore.GetConfig(ctx, owner)
	if err != nil {
		return err
	}
	next := cloneAnyMap(prefs)
	for key, value := range raw {
		if strings.HasPrefix(strings.ToLower(strings.ReplaceAll(key, "_", "")), "modelcatalog") || key == "videoProviders" {
			next[key] = value
		}
	}
	return s.MemoryStore.PutConfig(ctx, owner, next)
}
func TestWorkflowPreferenceWriteRetainsProtectedState(t *testing.T) {
	s := &workflowWriteIsolationStore{workflowReadIsolationStore: workflowIsolationFixture(t)}
	ctx := context.Background()
	before, _ := s.MemoryStore.GetConfig(ctx, "alice")
	r := workflowIsolationRequest(workflowIsolationRouter(s), "PUT", "/api/novel-fetch-workshop/config", `{"workflow":{"min_original_chars":1234}}`)
	after, _ := s.MemoryStore.GetConfig(ctx, "alice")
	if r.Code != 200 || s.preferenceWrites != 1 || s.writes != 0 || s.strictReads != 0 {
		t.Fatalf("preference update blocked/misdirected HTTP%d prefs=%d full=%d strict=%d body=%s", r.Code, s.preferenceWrites, s.writes, s.strictReads, r.Body.String())
	}
	for _, key := range []string{"modelCatalog", "modelCatalogLegacyVideoPending", "modelCatalogLegacyScrubPending"} {
		b, _ := json.Marshal(before[key])
		a, _ := json.Marshal(after[key])
		if string(b) != string(a) {
			t.Fatalf("protected state changed: %s", key)
		}
	}
	if intValue(mapOrEmpty(after["workflow"])["min_original_chars"]) != 1234 {
		t.Fatal("preference was not saved")
	}
}
func TestWorkflowCurrentBatchPersistenceFailureIsReported(t *testing.T) {
	for _, readFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "write failure", true: "read failure"}[readFailure], func(t *testing.T) {
			s := &workflowWriteIsolationStore{workflowReadIsolationStore: workflowIsolationFixture(t), writeErr: errors.New("preference save unavailable")}
			ctx := context.Background()
			_ = s.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "12345678901", Original: "已有正文", Meta: map[string]any{"status": "failed", "originalStatus": "done", "currentBatchId": "batch-current"}})
			before, _ := s.GetDocument(ctx, "alice", "12345678901")
			if readFailure {
				s.workflowErr = errors.New("preference database unavailable")
			}
			out, err := workshopProcessResult(ctx, s, nil, "alice", "12345678901	新书名", "9", "new-batch", 4000)
			if err == nil || out != nil {
				t.Fatalf("batch pretended saved after config failure: result=%v err=%v", out, err)
			}
			saved, _ := s.MemoryStore.GetConfig(ctx, "alice")
			if saved["novel_fetch_current_batch_id"] != "batch-current" || s.writes != 0 || s.strictReads != 0 {
				t.Fatal("config failure overwrote protected/raw state")
			}
			if readFailure {
				after, _ := s.GetDocument(ctx, "alice", "12345678901")
				if after.UpdatedAt != before.UpdatedAt {
					t.Fatal("config read failure still changed document")
				}
			}
		})
	}
}

func TestWorkflowPreferenceWriteUsesPairedWrapperDelegation(t *testing.T) {
	for _, ownershipLost := range []bool{false, true} {
		t.Run(map[bool]string{false: "nested writer", true: "lost lease"}[ownershipLost], func(t *testing.T) {
			s := &workflowWriteIsolationStore{workflowReadIsolationStore: workflowIsolationFixture(t)}
			imported := manualImportStore{Store: s}
			lease := workshopProcessLeaseStore{Store: imported, checkOwnership: func() error {
				if ownershipLost {
					return novelfetchworkshop.ErrProcessLeaseLost
				}
				return nil
			}}
			book := workshopBookExecutionStore{Store: lease, owner: "alice", bookID: "123"}
			r := workflowIsolationRequest(workflowIsolationRouter(book), "PUT", "/api/novel-fetch-workshop/config", `{"workflow":{"min_original_chars":1234}}`)
			if ownershipLost {
				if r.Code != 500 || s.preferenceWrites != 0 {
					t.Fatalf("lost lease changed config HTTP%d writes%d", r.Code, s.preferenceWrites)
				}
			} else if r.Code != 200 || s.preferenceWrites != 1 || s.strictReads != 0 || s.writes != 0 {
				t.Fatalf("wrapper hid safe writer HTTP%d pref=%d strict=%d full=%d", r.Code, s.preferenceWrites, s.strictReads, s.writes)
			}
		})
	}
}
