package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"qiantie/backend/internal/novelfetchworkshop"
)

func TestNonNovelFetchCompatibilityEchoKeepsBoundReader(t *testing.T) {
	s := &pendingVideoEchoStore{MemoryStore: novelfetchworkshop.NewMemoryStore()}
	if err := s.MemoryStore.PutConfig(context.Background(), "manager", map[string]any{"modelCatalog": []any{map[string]any{"id": "video", "kind": "video", "enabled": true}}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/api/batch-rewrite/config", nil)
	req = req.WithContext(context.WithValue(req.Context(), apiConfigCompatKey{}, BridgeAuth{AccountCenterStore: newModelOwnerTestCenter()}))
	config, err := compatibilityConfigForEcho(req, s, "manager")
	if err != nil || s.boundReads != 1 || modelCatalog(config)[0]["enabled"] != true || s.strictReads != 0 {
		t.Fatalf("legacy compatibility reader changed: %v", err)
	}
}

type pendingVideoEchoStore struct {
	*novelfetchworkshop.MemoryStore
	writes, strictReads, boundReads int
}

func (s *pendingVideoEchoStore) GetConfig(context.Context, string) (map[string]any, error) {
	s.strictReads++
	return nil, errors.New("legacy cleanup held")
}
func (s *pendingVideoEchoStore) ReadBoundModelConfig(ctx context.Context, owner string) (map[string]any, error) {
	s.boundReads++
	return s.MemoryStore.GetConfig(ctx, owner)
}
func (s *pendingVideoEchoStore) GetWorkflowConfig(ctx context.Context, owner string) (map[string]any, error) {
	config, err := s.MemoryStore.GetConfig(ctx, owner)
	delete(config, "modelCatalog")
	delete(config, "modelCatalogVersion")
	return config, err
}
func (s *pendingVideoEchoStore) ReadRuntimeConfig(ctx context.Context, owner string) (map[string]any, error) {
	config, err := s.MemoryStore.GetConfig(ctx, owner)
	rows := modelCatalog(config)
	for _, row := range rows {
		if row["kind"] == "video" {
			row["enabled"] = false
			row["runtimeAvailable"] = false
			row["runtimeReason"] = "legacy_video_migration_pending"
			delete(row, "credential")
			delete(row, "apiKey")
		}
	}
	config["modelCatalog"] = rows
	return config, err
}
func (s *pendingVideoEchoStore) PutWorkflowConfig(ctx context.Context, owner string, prefs map[string]any) error {
	if _, exists := prefs["modelCatalog"]; exists {
		return errors.New("model echo reached workflow writer")
	}
	config, err := s.MemoryStore.GetConfig(ctx, owner)
	if err != nil {
		return err
	}
	catalog := config["modelCatalog"]
	next := cloneAnyMap(prefs)
	next["modelCatalog"] = catalog
	s.writes++
	return s.MemoryStore.PutConfig(ctx, owner, next)
}

func TestNovelFetchPendingVideoRuntimeEchoSavesPreferencesOnly(t *testing.T) {
	ctx := context.Background()
	s := &pendingVideoEchoStore{MemoryStore: novelfetchworkshop.NewMemoryStore()}
	original := map[string]any{"id": "yzcm:video:fixture", "kind": "video", "modelId": "fixture", "provider": "yzcm", "baseUrl": "http://newapi.yzcm.site/v1", "catalogSource": "yzcm_saved", "enabled": true, "credential": "private-fixture-key"}
	if err := s.MemoryStore.PutConfig(ctx, "manager", map[string]any{"modelCatalog": []any{original}}); err != nil {
		t.Fatal(err)
	}
	router := NewRouter(RouterOptions{AccountCenterStore: newModelOwnerTestCenter(), NovelFetchStore: s, SessionResolver: func(context.Context, string) (BridgeIdentity, error) { return BridgeIdentity{Username: "manager"}, nil }})
	const path = "/api/novel-fetch-workshop/config"
	get := impersonationRequest(router, "GET", path, "manager", "", nil)
	if get.Code != 200 {
		t.Fatalf("GET status=%d", get.Code)
	}
	var view struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	rows := view.Settings["modelCatalog"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["enabled"] != false {
		t.Fatal("GET did not return the pending-video runtime view")
	}
	// This is the supported legacy/full-form echo contract. The current React
	// strict form normally sends preferences alone; older clients echo catalog.
	patch := map[string]any{"modelCatalog": rows, "layout": map[string]any{"remove_empty_lines": true}}
	payload, _ := json.Marshal(patch)
	saved := impersonationRequest(router, "POST", path, "manager", string(payload), nil)
	if saved.Code != 200 || s.writes != 1 {
		t.Fatalf("unchanged runtime echo blocked: status=%d writes=%d body=%s", saved.Code, s.writes, saved.Body.String())
	}
	var savedView struct {
		Settings map[string]any `json:"settings"`
	}
	if err := json.Unmarshal(saved.Body.Bytes(), &savedView); err != nil {
		t.Fatal(err)
	}
	if models, ok := savedView.Settings["modelCatalog"].([]any); !ok || len(models) != 1 {
		t.Fatal("successful same-owner preference save lost formal catalog in response")
	}
	for _, mutation := range []map[string]any{{"enabled": true}, {"credential": "replacement-key"}, {"endpoint": "https://different.invalid"}, {"id": "different-model"}} {
		changed := cloneAnyMap(rows[0].(map[string]any))
		for key, value := range mutation {
			changed[key] = value
		}
		payload, _ := json.Marshal(map[string]any{"modelCatalog": []any{changed}, "layout": map[string]any{"remove_empty_lines": false}})
		denied := impersonationRequest(router, "POST", path, "manager", string(payload), nil)
		if denied.Code != 403 || s.writes != 1 {
			t.Fatalf("model mutation accepted: status=%d writes=%d", denied.Code, s.writes)
		}
	}
	persisted, err := s.MemoryStore.GetConfig(ctx, "manager")
	if err != nil {
		t.Fatal(err)
	}
	model := modelCatalog(persisted)[0]
	if model["enabled"] != true || model["credential"] != "private-fixture-key" || s.strictReads != 0 {
		t.Fatal("preference echo altered actual video credentials/state or ran legacy cleanup")
	}
}
