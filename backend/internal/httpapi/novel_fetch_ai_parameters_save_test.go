package httpapi

import (
	"context"
	"net/http"
	"qiantie/backend/internal/novelfetchworkshop"
	"testing"
	"time"
)

func TestNovelFetchFlatAIParametersUpdateExecutionConfig(t *testing.T) {
	ctx := context.Background()
	store := novelfetchworkshop.NewMemoryStore()
	if err := store.PutConfig(ctx, "tester", map[string]any{"appConfig": map[string]any{"ai": map[string]any{"max_tokens": 1200, "timeout_seconds": 180}}}); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1800000000, 0)
	handler := NewRouter(RouterOptions{BridgeSecret: "fixture", Now: func() time.Time { return now }, NovelFetchStore: store})
	response := doNovelFetchRequest(t, handler, http.MethodPost, "/api/novel-fetch-workshop/config", map[string]any{"ai": map[string]any{"max_tokens": 8192}}, now, "fixture")
	if response.Code != http.StatusOK {
		t.Fatalf("save: %d %s", response.Code, response.Body.String())
	}
	saved, err := novelfetchworkshop.ReadWorkflowConfig(ctx, store, "tester")
	if err != nil {
		t.Fatal(err)
	}
	ai := mapOrEmpty(mapOrEmpty(saved["appConfig"])["ai"])
	if intValue(ai["max_tokens"]) != 8192 || intValue(ai["timeout_seconds"]) != 180 {
		t.Fatalf("execution config differs from saved form: %#v", ai)
	}
}
