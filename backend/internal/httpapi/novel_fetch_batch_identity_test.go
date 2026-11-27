package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
)

type batchConfigReadErrorStore struct {
	*novelfetchworkshop.MemoryStore
}

func (s batchConfigReadErrorStore) GetConfig(context.Context, string) (map[string]any, error) {
	return nil, errors.New("configuration unavailable")
}

func TestNovelFetchCurrentBatchDoesNotInventIdentity(t *testing.T) {
	for _, id := range []any{nil, "", "<nil>", "null", "undefined", false, map[string]any{}} {
		t.Run(stringForBatchTest(id), func(t *testing.T) {
			store := novelfetchworkshop.NewMemoryStore()
			_ = store.PutConfig(context.Background(), "tester", map[string]any{"novel_fetch_current_batch_id": id})
			_ = store.PutDocument(context.Background(), "tester", novelfetchworkshop.Document{BookID: "12345678901", Meta: map[string]any{"currentBatchId": "real-history"}})
			handler := NewRouter(RouterOptions{BridgeSecret: "test-secret", Now: func() time.Time { return time.Unix(1800000000, 0) }, NovelFetchStore: store})
			w := doNovelFetchRequest(t, handler, http.MethodGet, "/api/novel-fetch-workshop/batches/current", nil, time.Unix(1800000000, 0), "test-secret")
			var result map[string]any
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if result["batch"] != nil {
				t.Fatalf("invented current batch from invalid pointer: %s", w.Body.String())
			}
		})
	}
}
func stringForBatchTest(v any) string { raw, _ := json.Marshal(v); return string(raw) }

func TestNovelFetchCurrentBatchPreservesLegacyAliases(t *testing.T) {
	store := novelfetchworkshop.NewMemoryStore()
	ctx := context.Background()
	if err := store.PutConfig(ctx, "tester", map[string]any{"novelFetchCurrentBatchId": "actual-batch"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutDocument(ctx, "tester", novelfetchworkshop.Document{BookID: "12345678901", Meta: map[string]any{"current_batch_id": "actual-batch"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutDocument(ctx, "tester", novelfetchworkshop.Document{BookID: "22345678901", Meta: map[string]any{"currentBatchId": "history-batch"}}); err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(RouterOptions{BridgeSecret: "test-secret", NovelFetchStore: store})
	w := doNovelFetchRequest(t, handler, http.MethodGet, "/api/novel-fetch-workshop/batches/current", nil, time.Now(), "test-secret")
	var result struct {
		Batch struct {
			ID      string   `json:"id"`
			TaskIDs []string `json:"taskIds"`
		} `json:"batch"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Batch.ID != "actual-batch" || len(result.Batch.TaskIDs) != 1 || result.Batch.TaskIDs[0] != "12345678901" {
		t.Fatalf("legacy identity or isolation lost: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestNovelFetchCurrentBatchRequiresExistingMembers(t *testing.T) {
	store := novelfetchworkshop.NewMemoryStore()
	_ = store.PutConfig(context.Background(), "tester", map[string]any{"novel_fetch_current_batch_id": "removed-batch"})
	handler := NewRouter(RouterOptions{BridgeSecret: "test-secret", NovelFetchStore: store})
	w := doNovelFetchRequest(t, handler, http.MethodGet, "/api/novel-fetch-workshop/batches/current", nil, time.Now(), "test-secret")
	var result map[string]any
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result["batch"] != nil {
		t.Fatalf("stale pointer exposed ghost batch: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestNovelFetchCurrentBatchReadErrorIsNotEmptySuccess(t *testing.T) {
	handler := NewRouter(RouterOptions{BridgeSecret: "test-secret", NovelFetchStore: batchConfigReadErrorStore{novelfetchworkshop.NewMemoryStore()}})
	w := doNovelFetchRequest(t, handler, http.MethodGet, "/api/novel-fetch-workshop/batches/current", nil, time.Now(), "test-secret")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("config failure was hidden: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestNovelFetchBatchHistoryIgnoresMissingAndInvalidIdentity(t *testing.T) {
	store := novelfetchworkshop.NewMemoryStore()
	for i, id := range []any{nil, "<nil>", false, "real-history"} {
		_ = store.PutDocument(context.Background(), "tester", novelfetchworkshop.Document{BookID: []string{"12345678901", "22345678901", "32345678901", "42345678901"}[i], Meta: map[string]any{"currentBatchId": id}})
	}
	handler := NewRouter(RouterOptions{BridgeSecret: "test-secret", NovelFetchStore: store})
	w := doNovelFetchRequest(t, handler, http.MethodGet, "/api/novel-fetch-workshop/batches", nil, time.Now(), "test-secret")
	var result struct {
		Batches []map[string]any `json:"batches"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Batches) != 1 || result.Batches[0]["id"] != "real-history" {
		t.Fatalf("history invented batches: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestNovelFetchBatchSummaryKeepsActionableWaitingState(t *testing.T) {
	for _, status := range []string{"waiting_retry", "waiting_login", "waiting_configuration", "waiting_classification", "waiting_sensitive_ai", "upload_pending", "scheduled_waiting", "stopped"} {
		t.Run(status, func(t *testing.T) {
			rows := []novelfetchworkshop.Document{{Meta: map[string]any{"status": "completed"}}, {Meta: map[string]any{"status": status}}}
			if got := deriveNovelFetchBatchStatus(rows); got != status {
				t.Fatalf("batch status=%q, want %q", got, status)
			}
		})
	}
	t.Run("pending_submit", func(t *testing.T) {
		row := novelfetchworkshop.Document{OriginalRawChars: 1200, Meta: map[string]any{"status": "completed", "originalStatus": "done", "aiStatus": "ai_done"}}
		if got := deriveNovelFetchBatchStatus([]novelfetchworkshop.Document{row}); got != "pending_submit" {
			t.Fatalf("completed prose lost upload state: %q", got)
		}
	})
}
