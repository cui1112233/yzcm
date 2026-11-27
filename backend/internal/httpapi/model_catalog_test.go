package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/storage"
	"qiantie/backend/internal/yzcmmodels"
)

type yzcmCatalogReaderFixture func(context.Context, yzcmmodels.Query) (yzcmmodels.Page, error)

func (f yzcmCatalogReaderFixture) Search(ctx context.Context, query yzcmmodels.Query) (yzcmmodels.Page, error) {
	return f(ctx, query)
}

type yzcmCatalogSourceFixture func(context.Context) (yzcmmodels.Snapshot, error)

func (f yzcmCatalogSourceFixture) Fetch(ctx context.Context) (yzcmmodels.Snapshot, error) {
	return f(ctx)
}

func TestYZCMModelCatalogRouteAuthAndValidation(t *testing.T) {
	calls := 0
	const secret = "fixture-admin-credential"
	api, _, _ := newImpersonationTestRouter(t, func(options *RouterOptions) {
		options.NovelFetchStore = nil
		options.YZCMModelCatalog = yzcmCatalogReaderFixture(func(_ context.Context, q yzcmmodels.Query) (yzcmmodels.Page, error) {
			calls++
			if q.Kind != "image" || q.Q != "画面" || q.Limit != 2 || q.Offset != 0 {
				t.Errorf("invalid query handed to search: %+v", q)
			}
			return yzcmmodels.Page{}, errors.New(secret)
		})
	})
	path := "/api/config/models/yzcm?kind=image&q=%E7%94%BB%E9%9D%A2&limit=2"
	for _, scenario := range []struct {
		token, path string
		status      int
	}{
		{"", path, http.StatusUnauthorized}, {"member", path, http.StatusForbidden},
		{"disabled", path, http.StatusUnauthorized}, {"archived", path, http.StatusForbidden},
		{"manager", path + "&kind=video", http.StatusBadRequest}, {"manager", path + "&format=debug", http.StatusBadRequest},
		{"manager", "/api/config/models/yzcm?kind=unknown", http.StatusBadRequest}, {"manager", "/api/config/models/yzcm?limit=101", http.StatusBadRequest},
		{"manager", path, http.StatusServiceUnavailable},
	} {
		response := impersonationRequest(api, http.MethodGet, scenario.path, scenario.token, "", nil)
		if response.Code != scenario.status || strings.Contains(response.Body.String(), secret) {
			t.Fatalf("%q %s: got %d %s; want %d", scenario.token, scenario.path, response.Code, response.Body, scenario.status)
		}
	}
	if calls != 1 {
		t.Fatalf("unexpected read count: %d", calls)
	}
	if response := impersonationRequest(api, http.MethodPost, "/api/config/models/yzcm", "founder", "{}", nil); response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("catalog must have no write route: %d %s", response.Code, response.Body)
	}
}

func TestYZCMModelCatalogHTTPMySQLReadsOnlySavedRows(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	if err := storage.RunMigrations(ctx, db, storage.YZCMModelCatalogMigrations()); err != nil {
		t.Fatal(err)
	}
	store := &yzcmmodels.Store{DB: db}
	api, _, _ := newImpersonationTestRouter(t, func(options *RouterOptions) { options.YZCMModelCatalog = store })
	const path = "/api/config/models/yzcm?kind=text&q=relay"
	read := func(token, path string, expected int) yzcmmodels.Page {
		t.Helper()
		response := impersonationRequest(api, http.MethodGet, path, token, "", nil)
		if response.Code != expected || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%q: wrong status or caching policy: %d %v %s", token, response.Code, response.Header(), response.Body)
		}
		body := response.Body.String()
		for _, forbidden := range []string{"admin-credential", "credential", "baseUrl", "apiKey", "requestTemplate"} {
			if strings.Contains(body, forbidden) {
				t.Fatalf("%s leaked in catalog: %s", forbidden, body)
			}
		}
		var result struct {
			yzcmmodels.Page
			SyncSource string `json:"syncSource"`
		}
		if err := json.Unmarshal([]byte(body), &result); err != nil || result.SyncSource != "not_configured" {
			t.Fatalf("bad catalog response: %+v %v", result, err)
		}
		return result.Page
	}
	if empty := read("manager", path, http.StatusOK); empty.Status != "unavailable" || empty.Reason != "no_saved_snapshot" || empty.Models == nil {
		t.Fatalf("expected unavailable before first sync, got %+v", empty)
	}
	fetches := 0
	_, err := store.Sync(ctx, yzcmCatalogSourceFixture(func(context.Context) (yzcmmodels.Snapshot, error) {
		fetches++
		return yzcmmodels.Snapshot{Complete: true, SourceUpdatedAt: time.Now(), Models: []yzcmmodels.Model{
			{ModelID: "fixture-relay-text", DisplayName: "测试文案", Kind: "text", Adapted: true, Allowed: true},
			{ModelID: "fixture-relay-unadapted", DisplayName: "未适配", Kind: "text", Adapted: false, Allowed: true},
			{ModelID: "fixture-relay-image", DisplayName: "测试图片", Kind: "image", Adapted: true, Allowed: true},
		}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"founder", "dev", "manager", "manager"} {
		page := read(token, path, http.StatusOK)
		if page.Status != "available" || len(page.Models) != 1 || page.Models[0].ModelID != "fixture-relay-text" {
			t.Fatalf("wrong local results: %+v", page)
		}
	}
	if _, err := store.Sync(ctx, yzcmCatalogSourceFixture(func(context.Context) (yzcmmodels.Snapshot, error) {
		fetches++
		return yzcmmodels.Snapshot{}, errors.New("fixture leaked upstream admin-credential")
	})); err == nil {
		t.Fatal("expected fixture sync failure")
	}
	if page := read("manager", path, http.StatusOK); !page.Stale || page.LastSync == nil || page.LastSync.FailureCode != "source_error" || len(page.Models) != 1 {
		t.Fatalf("failure lost snapshot or diagnostic: %+v", page)
	}
	if empty := read("manager", "/api/config/models/yzcm?kind=video&q=relay", http.StatusOK); empty.Status != "empty" || empty.Reason != "no_matches" || len(empty.Models) != 0 {
		t.Fatalf("expected no exact matches from saved catalog: %+v", empty)
	}
	if fetches != 2 {
		t.Fatalf("catalog reads unexpectedly fetched upstream: %d", fetches)
	}
}

func TestIntegrationChannelModelsKeepsTieredChannels(t *testing.T) {
	userConfig := newIntegrationTestUserConfig(t, map[string]any{
		"customChannels": []any{
			map[string]any{
				"id": "ch_t2k", "name": "【0.019】2K", "group": "image",
				"baseUrl": "https://yf.test/v1", "model": "gpt-image-2.6-flare", "apiKey": "sk-x", "enabled": true,
				"params": map[string]any{"resolution": "2K", "quality": "high"},
			},
			map[string]any{
				"id": "ch_t1k", "name": "【0.0064】1K", "group": "image",
				"baseUrl": "https://yf.test/v1", "model": "gpt-image-2.6-flare", "apiKey": "sk-x", "enabled": true,
				"params": map[string]any{"resolution": "1K", "quality": "low"},
			},
		},
	})
	models := integrationChannelModels(context.Background(), "image", userConfig, "tester", nil)
	ids := map[string]bool{}
	for _, model := range models {
		ids[fmt.Sprint(model["id"])] = true
	}
	if !ids["custom:ch_t2k"] || !ids["custom:ch_t1k"] {
		t.Fatalf("同模型不同 params 的渠道被合并了：%v", models)
	}
}

func TestModelTestReusesSavedCredentialWhenEditing(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "test-secret"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer saved-secret" {
			t.Errorf("authorization = %q", req.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer upstream.Close()
	store := novelfetchworkshop.NewMemoryStore()
	if err := store.PutConfig(context.Background(), "tester", map[string]any{"modelCatalog": []any{map[string]any{
		"id": "text-saved", "kind": "text", "enabled": true, "modelId": "saved-model", "baseUrl": upstream.URL, "credential": "saved-secret",
	}}}); err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: store})
	response := doNovelFetchRequest(t, handler, http.MethodPost, "/api/config/models/test", map[string]any{
		"kind": "text", "existingModelId": "text-saved", "baseUrl": upstream.URL, "modelId": "saved-model",
	}, now, secret)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestModelCatalogCRUDHidesCredential(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "test-secret"
	store := novelfetchworkshop.NewMemoryStore()
	handler := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: store})
	create := doNovelFetchRequest(t, handler, http.MethodPost, "/api/config/models", map[string]any{
		"id": "text-demo", "kind": "text", "displayName": "测试文本", "modelId": "demo", "baseUrl": "https://example.com/v1/chat/completions", "credential": "secret-value", "enabled": true,
	}, now, secret)
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	if strings.Contains(create.Body.String(), "secret-value") {
		t.Fatal("model credential leaked in response")
	}
	list := doNovelFetchRequest(t, handler, http.MethodGet, "/api/models?kind=text", nil, now, secret)
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var payload struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Models) != 1 || payload.Models[0]["hasCredential"] != true {
		t.Fatalf("unexpected public catalog: %#v", payload.Models)
	}
}

func TestMemberModelCatalogUsesBoundManagerWithoutLeakingCredential(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "test-secret"
	accounts := &applicationTestStore{accounts: map[string]auth.Account{
		"manager": {Username: "manager", Active: true},
		"member":  {Username: "member", Active: true},
	}}
	profiles := NewMemberProfileStore(t.TempDir())
	if err := profiles.writeArray("member-profiles.json", []map[string]any{
		{"username": "manager", "role": "manager"},
		{"username": "member", "role": "member", "boundTo": "manager"},
	}); err != nil {
		t.Fatal(err)
	}
	catalog := novelfetchworkshop.NewMemoryStore()
	if err := catalog.PutConfig(context.Background(), "manager", map[string]any{"modelCatalog": []any{map[string]any{
		"id": "manager-image", "kind": "image", "displayName": "组长图片模型", "modelId": "gpt-image-2", "baseUrl": "https://image.example/v1", "credential": "manager-secret", "enabled": true,
	}}}); err != nil {
		t.Fatal(err)
	}

	handler := NewRouter(RouterOptions{
		BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: catalog,
		AuthStore: accounts, MemberProfileStore: profiles, UserConfigStore: NewUserConfigStore(t.TempDir()),
	})
	req := httptest.NewRequest(http.MethodGet, "/api/models?kind=image", nil)
	signNovelFetchRequest(req, "member", false, now, secret)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Models) != 1 || payload.Models[0]["id"] != "manager-image" {
		t.Fatalf("组员应读取绑定组长的模型目录，got %#v", payload.Models)
	}
	if strings.Contains(response.Body.String(), "manager-secret") {
		t.Fatalf("组长密钥不能返回给组员：%s", response.Body.String())
	}
}

func TestModelCatalogListsLegacyStringEnabledModel(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "test-secret"
	store := novelfetchworkshop.NewMemoryStore()
	if err := store.PutConfig(context.Background(), "tester", map[string]any{"modelCatalog": []any{map[string]any{
		"id": "legacy-text", "kind": "text", "displayName": "旧文字模型", "modelId": "legacy-model", "baseUrl": "https://example.com/v1", "credential": "secret", "enabled": "true",
	}}}); err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: store})
	response := doNovelFetchRequest(t, handler, http.MethodGet, "/api/models?kind=text", nil, now, secret)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Models) != 1 || payload.Models[0]["id"] != "legacy-text" || payload.Models[0]["enabled"] != true {
		t.Fatalf("legacy enabled model was not available: %#v", payload.Models)
	}
}

func TestModelCatalogListsBuiltInTTSWhenAudioCatalogIsEmpty(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "test-secret"
	store := novelfetchworkshop.NewMemoryStore()
	handler := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: store})

	response := doNovelFetchRequest(t, handler, http.MethodGet, "/api/models?kind=audio", nil, now, secret)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Models) != 1 || payload.Models[0]["id"] != builtInTTSModelID || payload.Models[0]["displayName"] != "默认 TTS" {
		t.Fatalf("audio catalog=%#v", payload.Models)
	}
}

func TestLegacyModelTestRoutesAreServedByGo(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "test-secret"
	store := novelfetchworkshop.NewMemoryStore()
	handler := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: store})
	for _, path := range []string{"/api/test/text", "/api/test/image"} {
		response := doNovelFetchRequest(t, handler, http.MethodPost, path, map[string]any{"baseUrl": "ftp://invalid", "model": "demo", "apiKey": "secret"}, now, secret)
		if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "Base URL 格式不正确") {
			t.Fatalf("%s should be Go-owned validation, status=%d body=%s", path, response.Code, response.Body.String())
		}
	}
}

func TestModelCatalogPatchPreservesCredentialAndValidatesWorkflow(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "test-secret"
	store := novelfetchworkshop.NewMemoryStore()
	if err := store.PutConfig(context.Background(), "tester", map[string]any{"modelCatalog": []any{map[string]any{
		"id": "minimax-h3-video", "kind": "video", "displayName": "H3", "workflowId": "saved-workflow", "credential": "saved-secret", "enabled": false,
	}}}); err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: store})
	response := doNovelFetchRequest(t, handler, http.MethodPatch, "/api/config/models/minimax-h3-video", map[string]any{"enabled": true, "workflowId": "new-workflow"}, now, secret)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "saved-secret") {
		t.Fatalf("patch status=%d body=%s", response.Code, response.Body.String())
	}
	settings, err := store.GetConfig(context.Background(), "tester")
	if err != nil {
		t.Fatal(err)
	}
	model := modelCatalog(settings)[0]
	if model["credential"] != "saved-secret" || model["workflowId"] != "new-workflow" {
		t.Fatalf("patch lost protected fields: %#v", model)
	}
}

func TestModelCatalogH3ConfigRequiresKeyAndSavesWorkflowTogether(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "test-secret"
	store := novelfetchworkshop.NewMemoryStore()
	handler := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: store})

	missingKey := doNovelFetchRequest(t, handler, http.MethodPost, "/api/config/models", map[string]any{
		"id": "minimax-h3-video", "kind": "video", "displayName": "H3", "workflowId": "saved-workflow", "enabled": false,
	}, now, secret)
	if missingKey.Code != http.StatusBadRequest || !strings.Contains(missingKey.Body.String(), "API Key") {
		t.Fatalf("H3 save without key must be rejected, status=%d body=%s", missingKey.Code, missingKey.Body.String())
	}

	withKey := doNovelFetchRequest(t, handler, http.MethodPost, "/api/config/models", map[string]any{
		"id": "minimax-h3-video", "kind": "video", "displayName": "H3", "workflowId": "saved-workflow", "credential": "h3-secret", "enabled": false,
	}, now, secret)
	if withKey.Code != http.StatusCreated || strings.Contains(withKey.Body.String(), "h3-secret") {
		t.Fatalf("H3 save with key should succeed without leaking it, status=%d body=%s", withKey.Code, withKey.Body.String())
	}
	settings, err := store.GetConfig(context.Background(), "tester")
	if err != nil {
		t.Fatal(err)
	}
	model := modelCatalog(settings)[0]
	if model["credential"] != "h3-secret" || model["workflowId"] != "saved-workflow" {
		t.Fatalf("H3 key and workflow must be saved together: %#v", model)
	}
}

func TestModelCatalogRejectsEmptyH3Workflow(t *testing.T) {
	model, err := normalizeManagedModel(map[string]any{"id": "minimax-h3-video", "kind": "video", "workflowId": ""})
	if err == nil || model != nil {
		t.Fatalf("empty H3 workflow must be rejected: %#v, %v", model, err)
	}
}

func TestModelCatalogAcceptsLegacyH3ApiKeyAlias(t *testing.T) {
	model, err := normalizeManagedModel(map[string]any{
		"id": "minimax-h3-video", "kind": "video", "apiKey": "legacy-secret", "workflowId": "saved-workflow",
	})
	if err != nil {
		t.Fatalf("legacy apiKey should be accepted: %v", err)
	}
	if model["credential"] != "legacy-secret" || model["apiKey"] != nil {
		t.Fatalf("legacy apiKey must normalize to credential: %#v", model)
	}
}

func TestModelCatalogPatchAcceptsLegacyH3ApiKeyAlias(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "test-secret"
	store := novelfetchworkshop.NewMemoryStore()
	if err := store.PutConfig(context.Background(), "tester", map[string]any{"modelCatalog": []any{map[string]any{
		"id": "minimax-h3-video", "kind": "video", "displayName": "H3", "workflowId": "saved-workflow", "apiKey": "legacy-secret", "enabled": false,
	}}}); err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: store})
	response := doNovelFetchRequest(t, handler, http.MethodPatch, "/api/config/models/minimax-h3-video", map[string]any{
		"workflowId": "new-workflow",
	}, now, secret)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "legacy-secret") {
		t.Fatalf("legacy H3 patch status=%d body=%s", response.Code, response.Body.String())
	}
	settings, err := store.GetConfig(context.Background(), "tester")
	if err != nil {
		t.Fatal(err)
	}
	model := modelCatalog(settings)[0]
	if model["credential"] != "legacy-secret" || model["workflowId"] != "new-workflow" {
		t.Fatalf("legacy key must survive patch normalization: %#v", model)
	}
}

func TestModelCatalogAcceptsManagedAudioFields(t *testing.T) {
	model, err := normalizeManagedModel(map[string]any{
		"id": "tts-demo", "kind": "audio", "displayName": "测试配音", "enabled": false,
		"endpoint": "https://audio.example.test/speech", "requestTemplate": `{"method":"POST","body":{"text":"{{input}}"}}`,
		"responseMapping": `{"resultUrl":"data.url"}`, "credential": "secret-value",
	})
	if err != nil {
		t.Fatalf("audio model should be accepted: %v", err)
	}
	for key, want := range map[string]string{"endpoint": "https://audio.example.test/speech", "requestTemplate": `{"method":"POST","body":{"text":"{{input}}"}}`, "responseMapping": `{"resultUrl":"data.url"}`} {
		if model[key] != want {
			t.Fatalf("%s=%#v, want %q", key, model[key], want)
		}
	}
}

func TestModelCatalogAcceptsVoiceCloneAudioFields(t *testing.T) {
	model, err := normalizeManagedModel(map[string]any{
		"id": "voice-clone-main", "kind": "audio", "displayName": "声音克隆", "enabled": true,
		"capabilities":              map[string]any{"supportsVoiceClone": true},
		"voiceCloneRequestTemplate": `{"body":{"reference":"{{reference_audio_base64}}"}}`,
		"voiceCloneResponseMapping": `{"resultUrl":"data.url"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	capabilities, _ := model["capabilities"].(map[string]any)
	if capabilities["supportsVoiceClone"] != true || modelString(model, "voiceCloneRequestTemplate") == "" || modelString(model, "voiceCloneResponseMapping") == "" {
		t.Fatalf("voice clone configuration was dropped: %+v", model)
	}
}

func TestModelCatalogAcceptsASRModelWithoutExposingCredential(t *testing.T) {
	model, err := normalizeManagedModel(map[string]any{"id": "asr-main", "kind": "asr", "displayName": "语音识别", "baseUrl": "https://api.example.test/v1", "modelId": "whisper-large-v3", "credential": "secret", "enabled": true})
	if err != nil {
		t.Fatal(err)
	}
	if model["kind"] != "asr" || model["modelId"] != "whisper-large-v3" {
		t.Fatalf("model=%#v", model)
	}
	public := publicManagedModel(model)
	if _, ok := public["credential"]; ok {
		t.Fatalf("credential leaked: %#v", public)
	}
	if public["hasCredential"] != true {
		t.Fatalf("hasCredential=%#v", public["hasCredential"])
	}
}

func TestModelCatalogPatchKeepsAdminExtensionFields(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "test-secret"
	store := novelfetchworkshop.NewMemoryStore()
	if err := store.PutConfig(context.Background(), "tester", map[string]any{"modelCatalog": []any{map[string]any{
		"id": "yd2-mini-video", "kind": "video", "displayName": "YD2.0 Mini（图生）",
		"name": "YD2 Mini", "adapterKind": "vidu", "credentialRef": "env:YD2", "enabled": false,
	}}}); err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: store})
	// 个人中心平台预设卡只改 Key：不能丢管理端写入的 name / adapterKind / credentialRef。
	response := doNovelFetchRequest(t, handler, http.MethodPatch, "/api/config/models/yd2-mini-video", map[string]any{
		"credential": "sk-new",
	}, now, secret)
	if response.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", response.Code, response.Body.String())
	}
	settings, err := store.GetConfig(context.Background(), "tester")
	if err != nil {
		t.Fatal(err)
	}
	model := modelCatalog(settings)[0]
	for key, want := range map[string]any{"name": "YD2 Mini", "adapterKind": "vidu", "credentialRef": "env:YD2", "credential": "sk-new"} {
		if model[key] != want {
			t.Fatalf("%s=%#v, want %#v (model=%#v)", key, model[key], want, model)
		}
	}
}

func TestModelCatalogListsEnabledIntegrationChannels(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "test-secret"
	store := novelfetchworkshop.NewMemoryStore()
	userConfig := NewUserConfigStore(t.TempDir())
	if err := userConfig.write("tester", map[string]any{
		apiIntegrationsSettingKey: map[string]any{
			"values": map[string]any{
				"deepseekOfficialApiKey": "sk-demo",
				"kimiApiKey":             "",
			},
			"channelSelection": map[string]any{
				"text": map[string]any{"enabled": []any{"deepseek", "kimi"}},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	handler := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: store, UserConfigStore: userConfig})

	list := doNovelFetchRequest(t, handler, http.MethodGet, "/api/models?kind=text", nil, now, secret)
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	var payload struct {
		Models []map[string]any `json:"models"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, model := range payload.Models {
		found[fmt.Sprint(model["id"])] = true
	}
	if !found["channel:deepseek"] {
		t.Fatalf("enabled channel with key must be selectable: %#v", payload.Models)
	}
	if found["channel:kimi"] {
		t.Fatalf("channel without key must stay hidden: %#v", payload.Models)
	}

	managed := doNovelFetchRequest(t, handler, http.MethodGet, "/api/config/models?kind=text", nil, now, secret)
	if managed.Code != http.StatusOK {
		t.Fatalf("managed list status=%d body=%s", managed.Code, managed.Body.String())
	}
	if strings.Contains(managed.Body.String(), "channel:deepseek") {
		t.Fatalf("channel candidates must not leak into the managed catalog: %s", managed.Body.String())
	}
}

func TestManagedAudioModelConnectionTestAcceptsAudioResponse(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	secret := "test-secret"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer audio-secret" {
			t.Errorf("authorization = %q", req.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("audio-probe"))
	}))
	defer upstream.Close()
	store := novelfetchworkshop.NewMemoryStore()
	handler := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: store})
	response := doNovelFetchRequest(t, handler, http.MethodPost, "/api/config/models/test-audio", map[string]any{
		"kind": "audio", "endpoint": upstream.URL, "credential": "audio-secret",
		"requestTemplate": `{"method":"POST","body":{"text":"{{input}}"}}`, "responseMapping": `{}`,
	}, now, secret)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "音频模型连接成功") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}
