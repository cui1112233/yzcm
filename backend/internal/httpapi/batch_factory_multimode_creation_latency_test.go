package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
)

func TestMultiModeCreationReturnsBeforeSourceReads(t *testing.T) {
	var reads atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reads.Add(1)
		time.Sleep(120 * time.Millisecond)
		_, _ = w.Write([]byte(`{"code":200,"data":"完整原文。","bookinfo":{"category":"女频","genre":"现代"}}`))
	}))
	defer upstream.Close()
	now := time.Unix(1700000000, 0)
	store := batchfactoryv11.NewMemoryStore()
	api := NewRouter(RouterOptions{Store: store, Slice: 1, BridgeSecret: "secret", Now: func() time.Time { return now }, NovelFetchFetcher: &novelfetchworkshop.UpstreamFetcher{Client: upstream.Client(), Endpoint: upstream.URL}})
	payload := map[string]any{"requestId": "fast-create", "title": "先进入再执行", "books": []any{map[string]any{"bookId": "738579", "platform": "15", "title": "待获取", "sourceMetadata": map[string]any{"gender": "女频", "style": "现代言情"}}}, "projects": []any{map[string]any{"mode": "comic"}, map[string]any{"mode": "text"}}}
	response := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/batch-factory/v12/multi-mode-batches", payload)
	if response.Code != http.StatusCreated {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	if reads.Load() != 0 {
		t.Fatalf("creation waited for %d external source reads", reads.Load())
	}
	group := decodeBody[batchfactoryv11.MultiModeCreationResult](t, response)
	sources, err := store.ListMultiModeSources(context.Background(), "alice", group.GroupID)
	if err != nil || len(sources) != 1 || sources[0].Status != "pending" || len(group.Batches) != 2 {
		t.Fatalf("lost durable pending source: %+v %v", sources, err)
	}
}
