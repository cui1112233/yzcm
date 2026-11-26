package httpapi

import (
	"context"
	"net/http"
	"qiantie/backend/internal/batchfactoryv11"
	"testing"
	"time"
)

func TestMultiModeCreateHTTPReturnsFourStableProjectsAndFullSource(t *testing.T) {
	now := time.Unix(1700000000, 0)
	store := batchfactoryv11.NewMemoryStore()
	api := NewRouter(RouterOptions{Store: store, Slice: 1, BridgeSecret: "secret", Now: func() time.Time { return now }})
	payload := map[string]any{"requestId": "http-group", "title": "four modes", "books": []any{map[string]any{"bookId": "738579", "title": "book", "originalRaw": "完整原文。第二段内容。", "sourceMetadata": map[string]any{"gender": "女频", "style": "现代言情"}}}, "projects": []any{map[string]any{"mode": "video"}, map[string]any{"mode": "comic"}, map[string]any{"mode": "illustrated_text"}, map[string]any{"mode": "text"}}}
	path := "/api/batch-factory/v12/multi-mode-batches"
	first := signedJSONRequest(t, api, now, "alice", http.MethodPost, path, payload)
	if first.Code != 201 {
		t.Fatalf("%d %s", first.Code, first.Body.String())
	}
	a := decodeBody[batchfactoryv11.MultiModeCreationResult](t, first)
	second := signedJSONRequest(t, api, now, "alice", http.MethodPost, path, payload)
	if second.Code != 201 {
		t.Fatal(second.Body.String())
	}
	b := decodeBody[batchfactoryv11.MultiModeCreationResult](t, second)
	if a.GroupID != b.GroupID || len(a.Batches) != 4 {
		t.Fatal("lost idempotency")
	}
	for _, batch := range a.Batches {
		if batch.Books[0].TxtText != "完整原文。第二段内容。" {
			t.Fatalf("missing full source: %+v", batch.Books[0])
		}
	}
	summary := signedJSONRequest(t, api, now, "alice", http.MethodGet, "/api/batch-factory/v12/batches/summary", nil)
	if summary.Code != 200 {
		t.Fatal(summary.Body.String())
	}
	index := decodeBody[struct {
		Batches []map[string]any `json:"batches"`
	}](t, summary)
	if len(index.Batches) != 4 {
		t.Fatalf("missing library cards: %s", summary.Body.String())
	}
	found := false
	for _, batch := range index.Batches {
		if batch["outputKind"] == "illustrated_text" {
			found = true
		}
	}
	if !found {
		t.Fatal("illustrated text identity lost")
	}
	foreign := signedJSONRequest(t, api, now, "bob", http.MethodGet, path+"/"+a.GroupID, nil)
	if foreign.Code != 404 {
		t.Fatal("foreign group readable")
	}
	persisted, err := store.GetMultiModeCreation(context.Background(), "alice", a.GroupID)
	if err != nil || persisted.Batches[0].ID != a.Batches[0].ID {
		t.Fatal(err)
	}
}

func TestMultiModePresetCompatibility(t *testing.T) {
	if multiModePresetCompatible("comic", map[string]any{"productionMode": "video"}) {
		t.Fatal("video preset accepted for comic")
	}
	if !multiModePresetCompatible("illustrated_text", map[string]any{"productionMode": "comic"}) {
		t.Fatal("existing comic execution preset rejected")
	}
}
