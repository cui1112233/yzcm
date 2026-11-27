package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
)

func TestV12VideoProviderStatusIgnoresLegacyPersonalFile(t *testing.T) {
	now := time.Unix(1700000000, 0)
	config := NewUserConfigStore(t.TempDir())
	if err := config.write("alice", map[string]any{"video": map[string]any{"ydApiKey": "retired-test-key"}}); err != nil {
		t.Fatal(err)
	}
	api := NewRouter(RouterOptions{AccountCenterStore: activeModelTestAccounts("alice"), BridgeSecret: "secret", Now: func() time.Time { return now }, UserConfigStore: config, NovelFetchStore: novelfetchworkshop.NewMemoryStore()})
	rec := signedJSONRequest(t, api, now, "alice", http.MethodGet, "/api/batch-factory/v12/video-provider/status?provider=personal_api", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"configured":false`) || strings.Contains(rec.Body.String(), "retired-test-key") {
		t.Fatalf("status = %d, want an unconfigured runtime model", rec.Code)
	}
}

func TestVideoProviderStatusAliasesUseCurrentManagedCatalog(t *testing.T) {
	now := time.Unix(1700000000, 0)
	catalog := novelfetchworkshop.NewMemoryStore()
	if err := catalog.PutConfig(context.Background(), "alice", map[string]any{
		"modelCatalog": []any{map[string]any{"id": "yd2-mini-video", "kind": "video", "modelId": "yd-model", "credential": "fixture-key"}},
	}); err != nil {
		t.Fatal(err)
	}
	center := activeModelTestAccounts("alice")
	api := NewRouter(RouterOptions{AccountCenterStore: center, BridgeSecret: "secret", Now: func() time.Time { return now }, NovelFetchStore: catalog})
	for _, version := range []string{"v11", "v12"} {
		rec := signedJSONRequest(t, api, now, "alice", http.MethodGet, "/api/batch-factory/"+version+"/video-provider/status?provider=personal_api", nil)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"model":"yd-model"`) || !strings.Contains(rec.Body.String(), `"configured":true`) ||
			strings.Contains(rec.Body.String(), "fixture-key") || strings.Contains(rec.Body.String(), "endpoint") {
			t.Fatalf("%s runtime status missing or disclosed configuration, HTTP %d", version, rec.Code)
		}
	}
}

func TestLiveVideoCatalogFollowsCurrentTeamAndDoesNotReuseDeletedKey(t *testing.T) {
	ctx := context.Background()
	center := newModelOwnerTestCenter()
	catalog := novelfetchworkshop.NewMemoryStore()
	save := func(owner, key string) {
		t.Helper()
		settings, err := catalog.GetConfig(ctx, owner)
		if err != nil {
			t.Fatal(err)
		}
		settings["modelCatalog"] = []any{map[string]any{"id": "row", "kind": "video", "modelId": "yd2.0-mini", "credential": key}}
		if err := catalog.PutConfig(ctx, owner, settings); err != nil {
			t.Fatal(err)
		}
	}
	save("manager", "current-key")
	save("other", "next-team-key")
	save("member", "old-personal-key")
	registry := NewCatalogVideoProviderRegistry(center, catalog)
	resolve := func(want string) {
		t.Helper()
		view, err := registry.ResolveModel(ctx, "member", batchfactoryv11.VideoProviderPersonalAPI, "row")
		if want == "" && err == nil || want != "" && (err != nil || view.APIKey != want) {
			t.Fatalf("live selection did not match current team state: %v", err)
		}
	}
	resolve("current-key")
	save("manager", "")
	resolve("")
	center.manager = "other"
	resolve("next-team-key")
	settings, _ := catalog.GetConfig(ctx, "other")
	settings["modelCatalog"] = []any{}
	if err := catalog.PutConfig(ctx, "other", settings); err != nil {
		t.Fatal(err)
	}
	resolve("")
	center.manager = ""
	resolve("")
}
