package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/safehttp"
	"qiantie/backend/internal/storage"
)

func TestAPIConfigPolicyMySQLVideoPollingKeepsTheAcceptedRow(t *testing.T) {
	ctx := context.Background()
	db, _ := novelAttributionDatabase(t)
	migrations := append(storage.V11Migrations(), storage.V12Migrations()...)
	migrations = append(migrations, storage.NovelFetchWorkshopMigrations()...)
	if err := storage.RunMigrations(ctx, db, migrations); err != nil {
		t.Fatal(err)
	}
	const owner = "member"
	store := batchfactoryv11.NewMySQLStore(db)
	batch, err := store.CreateBatch(ctx, owner, batchfactoryv11.CreateBatchInput{Title: "fixture", Books: []batchfactoryv11.CreateBookInput{{
		Title: "fixture", Videos: []batchfactoryv11.CreateVideoInput{{Label: "fixture"}},
	}}})
	if err != nil || len(batch.Books) != 1 || len(batch.Books[0].Videos) != 1 {
		t.Fatalf("seed batch: %v", err)
	}
	book := batch.Books[0]
	for _, query := range []string{
		`INSERT INTO batch_factory_v12_config_snapshots(id,owner_username,batch_id,book_id,effective_json,created_at) VALUES('poll-fixture-snapshot','member',?,?,'{}',NOW(6))`,
		`INSERT INTO batch_factory_v12_director_revisions(id,batch_id,book_id,owner_username,revision,snapshot_id,mode,source_digest,output_json,created_at) VALUES('poll-fixture-director',?,?,'member',1,'poll-fixture-snapshot','original','fixture','{}',NOW(6))`,
	} {
		if _, err := db.ExecContext(ctx, query, batch.ID, book.ID); err != nil {
			t.Fatal(err)
		}
	}
	center := newModelOwnerTestCenter()
	catalog := novelfetchworkshop.NewMySQLStore(db)
	catalog.ModelSecrets = NewUserConfigStore(t.TempDir()).ModelConfigSecrets()
	oldRow := map[string]any{"id": "original-row", "kind": "video", "modelId": batchfactoryv11.DefaultPersonalVideoModel, "credential": "fixture-original"}
	newRow := map[string]any{"id": "replacement-row", "kind": "video", "modelId": batchfactoryv11.DefaultPersonalVideoModel, "credential": "fixture-replacement"}
	setRows := func(username string, rows ...map[string]any) {
		t.Helper()
		config, err := catalog.GetConfig(ctx, username)
		if err != nil {
			t.Fatal(err)
		}
		config["modelCatalog"] = rows
		if err := catalog.PutConfig(ctx, username, config); err != nil {
			t.Fatal(err)
		}
	}
	setRows("manager", oldRow, newRow)
	setRows("other", oldRow, newRow)
	registry := NewCatalogVideoProviderRegistry(center, catalog)
	cfg, err := registry.ResolveExactVideoModel(ctx, owner, "original-row")
	if err != nil || cfg.ConfigID != "original-row" || cfg.ConfigOwner != "manager" {
		t.Fatalf("initial row: %v", err)
	}
	job, err := store.CreateProductionJob(ctx, batchfactoryv11.ProductionJob{Owner: owner, BatchID: batch.ID, BookID: book.ID,
		RequestID: "poll-fixture-request", DirectorRevisionID: "poll-fixture-director", Tasks: []batchfactoryv11.ProductionTask{{
			VideoID: book.Videos[0].ID, Provider: cfg.Provider, ProviderTaskID: "local-fixture-provider-id", Status: batchfactoryv11.ProductionRunning,
			Binding: cfg.ExecutionBinding(owner), FinalPromptHash: "fixture", CompiledPrompt: "fixture",
		}}})
	if err != nil || len(job.Tasks) != 1 {
		t.Fatalf("durable binding: %v", err)
	}
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT video_execution_binding FROM batch_factory_v12_production_tasks WHERE id=?`, job.Tasks[0].ID).Scan(&raw); err != nil ||
		!strings.Contains(raw, "original-row") || strings.Contains(raw, "fixture-original") {
		t.Fatalf("stored video binding is incomplete or contains a test key: %v", err)
	}
	calls := 0
	restore := safehttp.SetOutboundDecorator(func(http.RoundTripper) http.RoundTripper {
		return roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			calls++
			if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "local-fixture-provider-id") || r.Header.Get("Authorization") != "Bearer fixture-original" {
				t.Fatal("poll used a different configuration or task")
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"status":"RUNNING"}`)), Header: http.Header{}}, nil
		})
	})
	defer restore()
	poll := func() batchfactoryv11.ProductionTask {
		t.Helper()
		service := &batchfactoryv11.ProductionService{Store: batchfactoryv11.NewMySQLStore(db),
			ProviderRegistry: NewCatalogVideoProviderRegistry(center, catalog),
			Poller:           &batchfactoryv11.YadiVideoAdapter{APIKey: "unused-legacy-fixture", CreateURL: cfg.CreateURL, TasksURL: cfg.TasksURL, ResultURL: cfg.ResultURL},
		}
		status, err := service.GetBatchStatus(ctx, owner, batch.ID)
		if err != nil || len(status.Jobs) != 1 || len(status.Jobs[0].Tasks) != 1 {
			t.Fatalf("poll after reloading MySQL: %v", err)
		}
		return status.Jobs[0].Tasks[0]
	}
	if task := poll(); calls != 1 || task.ErrorMessage != "" {
		t.Fatalf("two same-name rows should poll through the accepted row, calls=%d task=%+v", calls, task)
	}
	setRows("manager", newRow)
	if task := poll(); calls != 1 || task.ErrorMessage == "" || task.Status != batchfactoryv11.ProductionRunning {
		t.Fatalf("deleted binding should leave polling on hold, calls=%d task=%+v", calls, task)
	}
	if cleaned, err := store.FailStaleProductionJobs(ctx, time.Now().Add(time.Hour), "test stale cleanup"); err != nil || cleaned != 0 {
		t.Fatalf("startup cleanup incorrectly expired an accepted task: %d %v", cleaned, err)
	}
	center.manager = "other"
	if task := poll(); calls != 1 || task.ErrorMessage == "" || task.Status != batchfactoryv11.ProductionRunning {
		t.Fatalf("same row ID at a new manager should not poll an old job, calls=%d task=%+v", calls, task)
	}
	center.manager = "manager"
	setRows("manager", oldRow, newRow)
	if task := poll(); calls != 2 || task.ErrorMessage != "" {
		t.Fatalf("same owner and row should resume polling, calls=%d task=%+v", calls, task)
	}
}

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
