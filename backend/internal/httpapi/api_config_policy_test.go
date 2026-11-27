package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/apisecrets"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/storage"
	"qiantie/backend/internal/yzcmmodels"
)

func TestAPIConfigPolicyRolesAndForbiddenShape(t *testing.T) {
	accounts := &authTestStore{account: auth.Account{Username: "founder", Active: true, IsOwner: true}}
	fixture := yzcmCatalogReaderFixture(func(_ context.Context, query yzcmmodels.Query) (yzcmmodels.Page, error) {
		return yzcmmodels.Page{Status: "available", Models: []yzcmmodels.CatalogRow{{Model: yzcmmodels.Model{
			ModelID: "relay-text", DisplayName: "本地目录测试", Kind: "text", Allowed: true, Adapted: true,
		}}}}, nil
	})
	h, _, _ := newImpersonationTestRouter(t, func(options *RouterOptions) {
		options.AuthStore = accounts
		options.Store = batchfactoryv11.NewMemoryStore()
		options.UserConfigStore = NewUserConfigStore(t.TempDir())
		options.YZCMModelCatalog = fixture
	})
	for _, tc := range []struct {
		username string
		read     int
		custom   bool
	}{{"founder", 200, true}, {"dev", 200, false}, {"manager", 200, false}, {"member", 403, false}} {
		t.Run(tc.username, func(t *testing.T) {
			response := impersonationRequest(h, http.MethodGet, "/api/config/models", tc.username, "", nil)
			if response.Code != tc.read {
				t.Fatalf("models list status = %d: %s", response.Code, response.Body)
			}
			if tc.read == 200 {
				var payload map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload["canConfigureAPI"] != true || payload["canAddCustomModel"] != tc.custom || payload["canEditAddress"] != tc.custom {
					t.Fatalf("missing formal capability flags: %+v %v", payload, err)
				}
			}
		})
	}
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/api/config/models", `{"kind":"text","modelId":"relay-text","baseURL":"https://forged.invalid"}`},
		{"POST", "/api/config/models", `{"kind":"text","modelId":"relay-text","apiFormat":"forged"}`},
		{"POST", "/api/config", `{"baseUrl":"https://forged.invalid","notifications":{"enabled":true}}`},
		{"POST", "/api/settings/api-integrations", `{"customChannels":[{"baseUrl":"https://forged.invalid"}]}`},
		{"PUT", "/api/shuihuo-production/account-ai-config", `{"baseUrl":"https://forged.invalid","model":"x","apiKey":"local"}`},
		{"POST", "/api/shuihuo-production/admin/models", `{"modelId":"x","kind":"text","endpoint":"https://forged.invalid"}`},
		{"POST", "/api/test/text", `{"baseUrl":"https://forged.invalid","model":"x","apiKey":"local"}`},
		{"POST", "/api/batch-rewrite/config", `{"settings":{"modelCatalog":[{"baseUrl":"https://forged.invalid"}]}}`},
	} {
		response := impersonationRequest(h, tc.method, tc.path, "manager", tc.body, nil)
		if response.Code != http.StatusForbidden {
			t.Fatalf("unauthorized %s %s accepted: %d %s", tc.method, tc.path, response.Code, response.Body)
		}
	}
}

func TestAPIConfigPolicyMissingCenterFailsClosed(t *testing.T) {
	store := novelfetchworkshop.NewMemoryStore()
	router := NewRouter(RouterOptions{
		NovelFetchStore: store, Store: batchfactoryv11.NewMemoryStore(), UserConfigStore: NewUserConfigStore(t.TempDir()),
		AuthStore: &authTestStore{account: auth.Account{Username: "founder", Active: true, IsOwner: true}},
		SessionResolver: func(context.Context, string) (BridgeIdentity, error) {
			return BridgeIdentity{Username: "founder", IsOwner: true}, nil
		},
	})
	for _, request := range []struct{ method, path, body string }{
		{"POST", "/api/config", `{"apiKey":"fixture"}`},
		{"POST", "/api/config/models", `{"baseUrl":"https://fixture.invalid"}`},
		{"POST", "/api/settings/api-integrations", `{"values":{"yfaiApiKey":"fixture"}}`},
		{"POST", "/api/batch-rewrite/config", `{"settings":{"modelCatalog":[{"credential":"fixture"}]}}`},
		{"PUT", "/api/shuihuo-production/config", `{"modelCatalog":[{"credential":"fixture"}]}`},
		{"PUT", "/api/shuihuo-production/account-ai-config", `{"baseUrl":"https://fixture.invalid","model":"x","apiKey":"fixture"}`},
		{"POST", "/api/shuihuo-production/admin/models", `{"modelId":"x","kind":"text","credential":"fixture"}`},
	} {
		if got := impersonationRequest(router, request.method, request.path, "founder", request.body, nil); got.Code != http.StatusForbidden {
			t.Fatalf("%s %s without account center: %d %s", request.method, request.path, got.Code, got.Body)
		}
	}
	config, err := store.GetConfig(context.Background(), "founder")
	if err != nil || len(modelCatalog(config)) != 0 {
		t.Fatalf("denied requests modified the catalog: %v", err)
	}
}

func TestAPIConfigPolicyCustomKeysStayWithTheirModelTarget(t *testing.T) {
	accounts := &authTestStore{account: auth.Account{Username: "founder", Active: true, IsOwner: true}}
	h, _, _ := newImpersonationTestRouter(t, func(options *RouterOptions) {
		options.AuthStore = accounts
		options.Store = batchfactoryv11.NewMemoryStore()
		options.UserConfigStore = NewUserConfigStore(t.TempDir())
	})
	request := func(method, path, body string, status int) {
		t.Helper()
		got := impersonationRequest(h, method, path, "founder", body, nil)
		if got.Code != status {
			t.Errorf("%s %s = %d, want %d", method, path, got.Code, status)
		}
	}
	const path = "/api/config/models"
	request("POST", path, `{"id":"new","kind":"audio","modelId":"test-audio","endpoint":"https://audio.example.test/v1","credentialRef":"invalid-configuration-field"}`, 403)
	request("POST", path, `{"id":"audio","kind":"audio","modelId":"test-audio","endpoint":"https://audio.example.test/v1","credential":"fixture-key"}`, 201)
	request("PATCH", path+"/audio", `{"endpoint":"https://audio.example.test/v2"}`, 403)
	request("PATCH", path+"/audio", `{"endpoint":"https://audio.example.test/v2","credential":"fixture-new-key"}`, 200)
	request("POST", "/api/shuihuo-production/admin/models", `{"kind":"audio","name":"test-audio","modelId":"fixture","credentialRef":"invalid-configuration-field"}`, 403)
	request("POST", "/api/config", `{"settings":{"modelCatalog":[{"id":"read-only-model-catalog"}]},"notifications":{"soundEnabled":false}}`, 403)
}

func TestAPIConfigPolicyRevealRequiresSessionAndAudit(t *testing.T) {
	accounts := &authTestStore{account: auth.Account{Username: "founder", Active: true, IsOwner: true}}
	h, center, now := newImpersonationTestRouter(t, func(options *RouterOptions) {
		options.BridgeSecret = "local-fixture"
		options.AuthStore = accounts
		options.UserConfigStore = NewUserConfigStore(t.TempDir())
	})
	create := impersonationRequest(h, "POST", "/api/config/models", "founder", `{"id":"fixture","kind":"text","modelId":"test","baseUrl":"https://text.example.test/v1","credential":"local-fixture-key"}`, nil)
	if create.Code != http.StatusCreated {
		t.Fatalf("fixture model create: %d", create.Code)
	}
	const path = "/api/settings/api-integrations/reveal"
	const payload = `{"key":"model:fixture"}`
	bridge := httptest.NewRequest("POST", path, strings.NewReader(payload))
	SignBridgeRequest(bridge, "founder", true, *now, "local-fixture")
	accounts.session = auth.Session{Username: "founder", Token: "expired-fixture", ExpiresAt: now.Add(-time.Minute)}
	makeBearer := func(token string) *http.Request {
		req := httptest.NewRequest("POST", path, strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		return req
	}
	for _, request := range []*http.Request{bridge, makeBearer(""), makeBearer("expired-fixture"), makeBearer("unknown-fixture")} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || strings.Contains(response.Body.String(), "local-fixture-key") {
			t.Fatalf("ordinary user session required, status %d", response.Code)
		}
	}
	center.auditErr = errors.New("fixture audit unavailable")
	if response := impersonationRequest(h, "POST", path, "founder", payload, nil); response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), "local-fixture-key") {
		t.Fatalf("audit failure disclosed key, status %d", response.Code)
	}
}

func TestAPIConfigPolicyWorkshopProjectionAndPreferenceEcho(t *testing.T) {
	ctx := context.Background()
	center := newModelOwnerTestCenter()
	center.identities["founder"] = accountcenter.Identity{Username: "founder", Role: accountcenter.RoleFounder, Active: true}
	center.identities["dev"] = accountcenter.Identity{Username: "dev", Role: accountcenter.RoleDeveloper, Active: true}
	catalog := novelfetchworkshop.NewMemoryStore()
	for _, owner := range []string{"founder", "dev", "manager"} {
		rows := []any{map[string]any{"id": "managed-yzcm", "kind": "text", "modelId": "approved", "provider": "yzcm", "catalogSource": "yzcm_saved",
			"baseUrl": "http://newapi.yzcm.site/v1", "credential": "private-key",
			"capabilities": map[string]any{"privateAdvancedMark": "fixture-advanced"}},
			map[string]any{"id": "custom-hidden", "kind": "text", "modelId": "custom", "baseUrl": "https://text.example.test/v1", "credential": "another-key"}}
		if err := catalog.PutConfig(ctx, owner, map[string]any{"modelCatalog": rows}); err != nil {
			t.Fatal(err)
		}
	}
	h := NewRouter(RouterOptions{AccountCenterStore: center, NovelFetchStore: catalog, SessionResolver: func(_ context.Context, raw string) (BridgeIdentity, error) {
		owner, _, _ := strings.Cut(raw, ":")
		return BridgeIdentity{Username: owner, IsOwner: owner == "founder"}, nil
	}})
	for _, role := range []string{"founder", "dev", "manager", "member"} {
		t.Run(role, func(t *testing.T) {
			const path = "/api/novel-fetch-workshop/config"
			for _, compat := range []string{path, "/api/batch-rewrite/config"} {
				response := impersonationRequest(h, "GET", compat, role, "", nil)
				if response.Code != 200 || !strings.Contains(response.Body.String(), "managed-yzcm") || strings.Contains(response.Body.String(), "private-key") {
					t.Fatalf("%s missing safe runtime/catalogue model: %d", compat, response.Code)
				}
				hasCustom := strings.Contains(response.Body.String(), "custom-hidden")
				if (role == "dev" || role == "manager") && hasCustom || role == "founder" && !hasCustom || role != "founder" && strings.Contains(response.Body.String(), "privateAdvancedMark") {
					t.Fatalf("%s projected wrong role fields for %s", compat, role)
				}
			}
			get := impersonationRequest(h, "GET", path, role, "", nil)
			var body struct {
				AppConfig map[string]any `json:"appConfig"`
			}
			if err := json.Unmarshal(get.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			patch := map[string]any{"modelCatalog": body.AppConfig["modelCatalog"], "layout": map[string]any{"remove_empty_lines": true}}
			raw, _ := json.Marshal(patch)
			response := impersonationRequest(h, "POST", path, role, string(raw), nil)
			if response.Code != 200 {
				t.Fatalf("exact echo and layout preference should save for %s: %d", role, response.Code)
			}
			before, _ := catalog.GetConfig(ctx, role)
			patch["modelCatalog"] = []any{map[string]any{"id": "forbidden-change", "baseUrl": "https://different.example.test/v1"}}
			raw, _ = json.Marshal(patch)
			response = impersonationRequest(h, "POST", path, role, string(raw), nil)
			after, _ := catalog.GetConfig(ctx, role)
			beforeJSON, _ := json.Marshal(before)
			afterJSON, _ := json.Marshal(after)
			if response.Code != 403 || !bytes.Equal(beforeJSON, afterJSON) {
				t.Fatalf("mutated model echo must be rejected unchanged for %s", role)
			}
		})
	}
}

func TestAPIConfigPolicyMySQLGrantEncryptionAndRuntime(t *testing.T) {
	t.Setenv("QIANTIE_ENABLE_LEGACY_V11", "1")
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	migrations := append(storage.AuthMigrations(), storage.AccountCenterMigrations()...)
	migrations = append(migrations, storage.NovelFetchWorkshopMigrations()...)
	migrations = append(migrations, storage.YZCMModelCatalogMigrations()...)
	migrations = append(migrations, storage.YZCMModelCatalogPrecisionMigration())
	if err := storage.RunMigrations(ctx, db, migrations); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, person := range []struct{ name, role string }{{"founder", "founder"}, {"dev", "developer"}, {"manager", "manager"}, {"member", "member"}} {
		exec(`INSERT INTO accounts(username,password_hash,password_algorithm,is_owner,active,created_at,updated_at) VALUES (?,'fixture','fixture',?,TRUE,NOW(6),NOW(6))`, person.name, person.role == "founder")
		exec(`INSERT INTO account_center_profiles(username,display_name,role) VALUES (?,?,?)`, person.name, person.name, person.role)
	}
	center := accountcenter.NewMySQLStore(db)
	exec(`INSERT INTO account_center_imports(import_key,completed_at) VALUES ('member-center-legacy-v1',NOW(6))`)
	if err := center.CreateTeam(ctx, accountcenter.Team{ID: "main", Name: "本地测试", ManagerUsername: "manager", CreatedBy: "founder"}); err != nil {
		t.Fatal(err)
	}
	if err := center.AddMembership(ctx, accountcenter.Membership{TeamID: "main", Username: "member"}); err != nil {
		t.Fatal(err)
	}
	accounts := auth.NewMySQLStore(db)
	settings := NewUserConfigStore(t.TempDir())
	workshop := novelfetchworkshop.NewMySQLStore(db)
	workshop.ModelSecrets = settings.ModelConfigSecrets()
	saved := &yzcmmodels.Store{DB: db}
	if _, err := saved.Sync(ctx, yzcmCatalogSourceFixture(func(context.Context) (yzcmmodels.Snapshot, error) {
		return yzcmmodels.Snapshot{Complete: true, SourceUpdatedAt: time.Now(), Models: []yzcmmodels.Model{
			{ModelID: "first", DisplayName: "一", Kind: "text", Allowed: true, Adapted: true},
			{ModelID: "second", DisplayName: "二", Kind: "text", Allowed: true, Adapted: true},
		}}, nil
	})); err != nil {
		t.Fatal(err)
	}
	batchStore := batchfactoryv11.NewMemoryStore()
	liveVideo := NewCatalogVideoProviderRegistry(center, workshop)
	h := NewRouter(RouterOptions{AuthStore: accounts, AccountCenterStore: center, NovelFetchStore: workshop, UserConfigStore: settings, YZCMModelCatalog: saved,
		Slice: 4, Store: batchStore, Production: &batchfactoryv11.ProductionService{Store: batchStore, ProviderRegistry: liveVideo, Enabled: true},
		SessionResolver: func(ctx context.Context, token string) (BridgeIdentity, error) {
			username, _, _ := strings.Cut(token, ":")
			identity, err := center.ResolveIdentity(ctx, username)
			if err != nil || !identity.Active {
				return BridgeIdentity{}, accountcenter.ErrForbidden
			}
			return BridgeIdentity{Username: username, IsOwner: identity.Role == accountcenter.RoleFounder}, nil
		}})
	request := func(role, verb, path, body string, status int) []byte {
		t.Helper()
		r := impersonationRequest(h, verb, path, role, body, nil)
		if r.Code != status {
			t.Fatalf("%s %s %s: want %d got %d %.250s", role, verb, path, status, r.Code, r.Body.String())
		}
		return r.Body.Bytes()
	}
	rawConfig := func(owner string) []byte {
		t.Helper()
		var raw []byte
		if err := db.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=?`, owner).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		return raw
	}
	const catalog = "/api/config/models"
	const developerGrant = "/api/account-center/admin/accounts/dev/"
	request("dev", "POST", developerGrant+"grant_custom_api", "{}", 403)
	request("founder", "POST", developerGrant+"grant_custom_api", "{}", 200)
	custom := `{"id":"custom","kind":"text","modelId":"custom-model","baseUrl":"https://trusted-fixture.invalid/v1","credential":"fixture-dev-custom","enabled":true}`
	request("dev", "POST", catalog, custom, 201)
	for _, version := range []string{"v11", "v12"} {
		request("dev", "PUT", "/api/batch-factory/"+version+"/video-provider/config",
			`{"provider":"personal_api","model":"native-fixture","apiKey":"fixture-dev-video","createUrl":"https://video.example.test/create"}`, 200)
	}
	if value, err := liveVideo.ResolveModel(ctx, "dev", "personal_api", "legacy-video:personal_api"); err != nil || value.APIKey != "fixture-dev-video" {
		t.Fatal("granted developer could not resolve the configured video row")
	}
	beforeDev := rawConfig("dev")
	const workshopCompat = "/api/novel-fetch-workshop/config"
	if response := request("dev", "GET", workshopCompat, "", 200); !bytes.Contains(response, []byte("custom-model")) {
		t.Fatal("granted developer compatibility view omitted own custom model")
	}
	request("founder", "POST", developerGrant+"revoke_custom_api", "{}", 200)
	if _, err := liveVideo.ResolveModel(ctx, "dev", "personal_api", "legacy-video:personal_api"); err == nil {
		t.Fatal("revoked developer retained the custom video key in runtime")
	}
	request("dev", "PATCH", catalog+"/custom", `{"baseUrl":"https://forged.invalid"}`, 403)
	if response := request("dev", "GET", workshopCompat, "", 200); bytes.Contains(response, []byte("custom-model")) {
		t.Fatal("revoked developer retained custom model in compatibility view")
	}
	if !bytes.Equal(beforeDev, rawConfig("dev")) || bytes.Contains(beforeDev, []byte("fixture-dev-custom")) {
		t.Fatal("developer secret unsealed or rejected write changed DB")
	}
	var audit int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_center_audit_events WHERE actor_username='founder' AND subject_username='dev' AND action IN ('grant_custom_api','revoke_custom_api')`).Scan(&audit); err != nil || audit != 2 {
		t.Fatalf("durable grant audit = %d %v", audit, err)
	}
	for _, id := range []string{"first", "second"} {
		request("manager", "POST", catalog, fmt.Sprintf(`{"kind":"text","modelId":%q,"credential":%q}`, id, "fixture-"+id), 201)
	}
	before := rawConfig("manager")
	request("manager", "PATCH", catalog+"/yzcm:text:first", `{"credential":"forged","provider":"external"}`, 403)
	if !bytes.Equal(before, rawConfig("manager")) {
		t.Fatal("rejected mixed payload mutated config")
	}
	var stored map[string]any
	if err := json.Unmarshal(before, &stored); err != nil {
		t.Fatal(err)
	}
	firstCipher, secondCipher := firstText(modelCatalog(stored)[0]["credential"]), firstText(modelCatalog(stored)[1]["credential"])
	if !strings.HasPrefix(firstCipher, apisecrets.BoundPrefix) || !strings.HasPrefix(secondCipher, apisecrets.BoundPrefix) || firstCipher == secondCipher {
		t.Fatal("keys are not independently bound")
	}
	request("manager", "PATCH", catalog+"/yzcm:text:first", `{"credential":"rotated-first"}`, 200)
	if err := json.Unmarshal(rawConfig("manager"), &stored); err != nil {
		t.Fatal(err)
	}
	if modelCatalog(stored)[1]["credential"] != secondCipher || modelCatalog(stored)[0]["credential"] == firstCipher {
		t.Fatal("rotation rewrote another model ciphertext")
	}
	copied := cloneConfigMap(stored)
	modelCatalog(copied)[1]["credential"] = firstCipher
	if err := settings.ModelConfigSecrets().OpenModelConfig("manager", copied); err == nil {
		t.Fatal("cross-model ciphertext accepted")
	}
	if err := settings.ModelConfigSecrets().OpenModelConfig("member", stored); err == nil {
		t.Fatal("cross-owner ciphertext accepted")
	}
	const reveal = "/api/settings/api-integrations/reveal"
	if got := request("manager", "POST", reveal, `{"key":"model:yzcm:text:first"}`, 200); !bytes.Contains(got, []byte("rotated-first")) || bytes.Contains(got, []byte("fixture-second")) {
		t.Fatal("reveal returned incorrect field")
	}
	request("member", "POST", reveal, `{"key":"model:yzcm:text:first"}`, 403)
	cookie := startImpersonationForTest(t, h, "founder", "manager")
	if r := impersonationRequest(h, "POST", reveal, "founder", `{"key":"model:yzcm:text:first"}`, cookie); r.Code != 403 {
		t.Fatalf("switched reveal = %d", r.Code)
	}

	// Formal account membership, rather than a legacy member copy, resolves the provider.
	var owner string
	resolve := NovelCreationTextResolver(center, func(_ context.Context, username, _ string) (batchfactoryv11.DirectorProvider, error) {
		owner = username
		return nil, nil
	})
	if _, err := resolve(ctx, "member", "yzcm:text:first"); err != nil || owner != "manager" {
		t.Fatalf("runtime owner = %q %v", owner, err)
	}
	if response := request("member", "GET", "/api/models?kind=text", "", 200); !bytes.Contains(response, []byte("yzcm:text:first")) || bytes.Contains(response, []byte("rotated-first")) || bytes.Contains(response, []byte("baseUrl")) {
		t.Fatal("member runtime picker lost model or exposed private configuration")
	}
	managerConfig, err := workshop.GetConfig(ctx, "manager")
	if err != nil {
		t.Fatal(err)
	}
	managerConfig["modelCatalog"] = append(modelCatalog(managerConfig), map[string]any{
		"id": "capacity-image", "kind": "image", "modelId": "fixture-image", "endpoint": "https://image.example.test/v1", "credential": "fixture-manager-image",
	})
	if err := workshop.PutConfig(ctx, "manager", managerConfig); err != nil {
		t.Fatal(err)
	}
	capacity, err := resolveImageCapacityRequest(ctx, RouterOptions{AccountCenterStore: center, Store: batchfactoryv11.NewMemoryStore(), NovelFetchStore: workshop}, BridgeIdentity{Username: "member"}, "capacity-image", "9:16", "", false)
	if err != nil || capacity.Owner != "member" || capacity.Credential != "fixture-manager-image" {
		t.Fatalf("image capacity must choose the current team catalog and retain member attribution: %v", err)
	}

	// Legacy values become bound ciphertext before the first successful read.
	exec(`INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES ('founder',?,NOW(6),NOW(6))`, `{"modelCatalog":[{"id":"legacy","kind":"text","credential":"fixture-legacy-plain"}]}`)
	request("founder", "GET", catalog, "", 200)
	legacyRaw := rawConfig("founder")
	if bytes.Contains(legacyRaw, []byte("fixture-legacy-plain")) || !bytes.Contains(legacyRaw, []byte(apisecrets.BoundPrefix)) || !bytes.Contains(legacyRaw, []byte(novelfetchworkshop.ModelSecretVersion)) {
		t.Fatal("first access did not migrate legacy plaintext")
	}
}

func TestAPIConfigPolicyRestrictedSelectionsAndSingleKey(t *testing.T) {
	ctx := context.Background()
	accounts := &authTestStore{account: auth.Account{Username: "founder", Active: true, IsOwner: true}}
	available := true
	h, center, _ := newImpersonationTestRouter(t, func(options *RouterOptions) {
		options.AuthStore = accounts
		options.UserConfigStore = NewUserConfigStore(t.TempDir())
		options.YZCMModelCatalog = yzcmCatalogReaderFixture(func(_ context.Context, query yzcmmodels.Query) (yzcmmodels.Page, error) {
			if !available {
				return yzcmmodels.Page{Status: "unavailable"}, nil
			}
			return yzcmmodels.Page{Status: "available", Stale: true, Models: []yzcmmodels.CatalogRow{{Model: yzcmmodels.Model{
				ModelID: query.Q, DisplayName: "本地目录测试", Kind: query.Kind, Adapted: true, Allowed: query.Q != "disallowed",
			}}}}, nil
		})
	})
	request := func(verb, path, body string, status int) map[string]any {
		t.Helper()
		r := impersonationRequest(h, verb, path, "manager", body, nil)
		if r.Code != status {
			t.Fatalf("%s %s: want %d, got %d %s", verb, path, status, r.Code, r.Body)
		}
		var payload map[string]any
		_ = json.Unmarshal(r.Body.Bytes(), &payload)
		return payload
	}
	const path = "/api/config/models"
	request("POST", path, `{"kind":"text","modelId":"disallowed"}`, 403)
	first := request("POST", path, `{"kind":"text","modelId":"first","credential":"fixture-key-one"}`, 201)["model"].(map[string]any)["id"].(string)
	second := request("POST", path, `{"kind":"text","modelId":"second","credential":"fixture-key-two"}`, 201)["model"].(map[string]any)["id"].(string)
	wire := impersonationRequest(h, "GET", path, "manager", "", nil)
	for _, forbidden := range []string{"baseUrl", "endpoint", "apiFormat", "provider", "fixture-key", "enc:v"} {
		if strings.Contains(wire.Body.String(), forbidden) {
			t.Fatalf("restricted list disclosed %s: %s", forbidden, wire.Body)
		}
	}
	reveal := func(id, key string) {
		t.Helper()
		result := request("POST", "/api/settings/api-integrations/reveal", `{"key":"model:`+id+`"}`, 200)
		if result["value"] != key || len(result) != 2 {
			t.Fatalf("reveal is not isolated to one key: %+v", result)
		}
	}
	reveal(first, "fixture-key-one")
	available = false
	request("POST", path, `{"kind":"text","modelId":"third"}`, 403)
	request("PATCH", path+"/"+first, `{"credential":"rotated-only-first"}`, 200)
	reveal(first, "rotated-only-first")
	reveal(second, "fixture-key-two")
	request("PATCH", path+"/"+first, `{"clearCredential":true}`, 200)
	reveal(first, "")
	request("DELETE", path+"/"+first, "", 204)
	reveal(second, "fixture-key-two")
	if len(center.audits) == 0 || ctx.Err() != nil {
		t.Fatal("successful reveal must leave an audit event")
	}
}
