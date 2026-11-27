package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"testing"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/storage"
	"qiantie/backend/internal/yzcmmodels"
)

func videoCatalogFixture(_ context.Context, query yzcmmodels.Query) (yzcmmodels.Page, error) {
	return yzcmmodels.Page{Status: "available", Models: []yzcmmodels.CatalogRow{{Model: yzcmmodels.Model{
		Kind: "video", ModelID: query.Q, Adapted: true, Allowed: query.Q == "fixture-video",
	}}}}, nil
}

func TestAPIConfigPolicyMySQLVideoAliasesStoreOnlyBoundModels(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
		t.Fatal(err)
	}
	workshop := novelfetchworkshop.NewMySQLStore(db)
	workshop.ModelSecrets = NewUserConfigStore(t.TempDir()).ModelConfigSecrets()
	h, _, _ := newImpersonationTestRouter(t, func(options *RouterOptions) {
		store := batchfactoryv11.NewMemoryStore()
		options.Slice, options.Store, options.NovelFetchStore = 4, store, workshop
		options.Production = &batchfactoryv11.ProductionService{Store: store, Enabled: true}
		options.YZCMModelCatalog = yzcmCatalogReaderFixture(videoCatalogFixture)
	})
	raw := func(owner string) []byte {
		t.Helper()
		var document []byte
		if err := db.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=?`, owner).Scan(&document); err != nil {
			t.Fatal(err)
		}
		return document
	}
	for _, version := range []string{"v11", "v12"} {
		path := "/api/batch-factory/" + version + "/video-provider/config"
		allow := `{"modelId":"fixture-video","credential":"fixture-allowed-key"}`
		for _, owner := range []string{"manager", "dev"} {
			for _, body := range []string{allow, `{"model":"fixture-video","apiKey":"fixture-allowed-key"}`} {
				if response := impersonationRequest(h, "PUT", path, owner, body, nil); response.Code != http.StatusOK {
					t.Fatalf("%s video selection for %s: HTTP %d", version, owner, response.Code)
				}
			}
			initial := raw(owner)
			for _, body := range []string{`{"modelId":"fixture-video","credential":"replacement","createUrl":"https://local.example.test/custom"}`,
				`{"modelId":"unlisted","credential":"replacement"}`} {
				if response := impersonationRequest(h, "PUT", path, owner, body, nil); response.Code != http.StatusForbidden || !bytes.Equal(raw(owner), initial) {
					t.Fatalf("%s invalid edit changed %s's config, HTTP %d", version, owner, response.Code)
				}
			}
			if bytes.Contains(initial, []byte("fixture-allowed-key")) || bytes.Contains(initial, []byte("videoProviders")) || !bytes.Contains(initial, []byte("enc:v2:")) {
				t.Fatal("restricted video model was not stored only as bound ciphertext")
			}
		}
		if response := impersonationRequest(h, "PUT", path, "member", allow, nil); response.Code != http.StatusForbidden {
			t.Fatalf("%s member config returned HTTP %d", version, response.Code)
		}
		custom := fmt.Sprintf(`{"provider":"personal_api","model":"native-fixture","apiKey":"private-fixture-%s","createUrl":"https://video.example.test/v1"}`, version)
		if response := impersonationRequest(h, "PUT", path, "founder", custom, nil); response.Code != http.StatusOK {
			t.Fatalf("%s authorized video config: HTTP %d", version, response.Code)
		}
		initial := raw("founder")
		cookie := startImpersonationForTest(t, h, "founder", "manager")
		beforeManager := raw("manager")
		if response := impersonationRequest(h, "PUT", path, "founder", custom, cookie); response.Code != http.StatusForbidden ||
			!bytes.Equal(initial, raw("founder")) || !bytes.Equal(beforeManager, raw("manager")) {
			t.Fatalf("%s switched alias is writable, HTTP %d", version, response.Code)
		}
		if bytes.Contains(initial, []byte("private-fixture-")) || bytes.Contains(initial, []byte("videoProviders")) || !bytes.Contains(initial, []byte("enc:v2:")) {
			t.Fatal("native video settings were not stored only as bound ciphertext")
		}
	}
}

func TestVideoConfigAliasFailsClosedWithoutFormalStore(t *testing.T) {
	for _, missing := range []string{"accounts", "models"} {
		t.Run(missing, func(t *testing.T) {
			h, _, _ := newImpersonationTestRouter(t, func(options *RouterOptions) {
				store := batchfactoryv11.NewMemoryStore()
				options.Slice, options.Store = 4, store
				options.Production = &batchfactoryv11.ProductionService{Store: store, Enabled: true}
				if missing == "accounts" {
					options.AccountCenterStore = nil
				} else {
					options.NovelFetchStore = nil
				}
			})
			for _, version := range []string{"v11", "v12"} {
				path := "/api/batch-factory/" + version + "/video-provider/config"
				if response := impersonationRequest(h, "PUT", path, "founder", `{"provider":"personal_api","apiKey":"fixture"}`, nil); response.Code != http.StatusForbidden {
					t.Fatalf("%s accepted a video write without %s, HTTP %d", version, missing, response.Code)
				}
			}
		})
	}
}
