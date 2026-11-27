package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

func TestAPIConfigPolicyMySQLGrantEncryptionAndRuntime(t *testing.T) {
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
	h := NewRouter(RouterOptions{AuthStore: accounts, AccountCenterStore: center, NovelFetchStore: workshop, UserConfigStore: settings, YZCMModelCatalog: saved,
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
	beforeDev := rawConfig("dev")
	request("founder", "POST", developerGrant+"revoke_custom_api", "{}", 200)
	request("dev", "PATCH", catalog+"/custom", `{"baseUrl":"https://forged.invalid"}`, 403)
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
	resolve := NovelCreationTextResolver(accounts, nil, func(_ context.Context, username, _ string) (batchfactoryv11.DirectorProvider, error) {
		owner = username
		return nil, nil
	}, center)
	if _, err := resolve(ctx, "member", "yzcm:text:first"); err != nil || owner != "manager" {
		t.Fatalf("runtime owner = %q %v", owner, err)
	}
	if response := request("member", "GET", "/api/models?kind=text", "", 200); !bytes.Contains(response, []byte("yzcm:text:first")) || bytes.Contains(response, []byte("rotated-first")) || bytes.Contains(response, []byte("baseUrl")) {
		t.Fatal("member runtime picker lost model or exposed private configuration")
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
