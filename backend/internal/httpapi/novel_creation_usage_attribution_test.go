package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelcreation"
	"qiantie/backend/internal/storage"
	"qiantie/backend/internal/yzcmusage"
)

type novelAttributionPipeline func(context.Context, novelcreation.RunContext) error

func (p novelAttributionPipeline) Run(ctx context.Context, in novelcreation.RunContext, _ novelcreation.EventSink) (novelcreation.ChapterBundle, error) {
	return novelcreation.ChapterBundle{}, p(ctx, in)
}

type novelReceiptIDKey struct{}

func TestNovelRunUsageAttributionMySQLSurvivesTransferRestartAndRetry(t *testing.T) {
	ctx := context.Background()
	db, dsn := novelAttributionDatabase(t)
	novelMigrations := storage.NovelCreationMigrations()
	base := append(storage.AuthMigrations(), storage.AccountCenterMigrations()...)
	base = append(base, storage.YZCMUsageMigrations()...)
	base = append(base, storage.AccountCenterEnergyMigrations()...)
	base = append(base, novelMigrations[:len(novelMigrations)-1]...)
	if err := storage.RunMigrations(ctx, db, base); err != nil {
		t.Fatal(err)
	}
	serverStore := novelcreation.NewMySQLStore(db)
	server := novelcreation.NewService(serverStore, novelcreation.NewMemoryRuntime(), nil)
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, person := range []struct{ name, role string }{{"founder", "founder"}, {"member", "member"}, {"lead-a", "manager"}, {"lead-b", "manager"}} {
		exec(`INSERT INTO accounts(username,password_hash,password_algorithm,is_owner,active,created_at,updated_at) VALUES (?,'fixture','fixture',?,TRUE,NOW(6),NOW(6))`, person.name, person.role == "founder")
		exec(`INSERT INTO account_center_profiles(username,display_name,role) VALUES (?,?,?)`, person.name, person.name, person.role)
	}
	accounts := accountcenter.NewMySQLStore(db)
	for _, name := range []string{"a", "b"} {
		if err := accounts.CreateTeam(ctx, accountcenter.Team{ID: "team-" + name, Name: "team-" + name, ManagerUsername: "lead-" + name, CreatedBy: "founder"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := accounts.AddMembership(ctx, accountcenter.Membership{TeamID: "team-a", Username: "member"}); err != nil {
		t.Fatal(err)
	}
	legacyProject, err := server.CreateProject(ctx, "member", novelcreation.CreateProjectInput{Title: "旧作品", Genre: "都市", ModelID: "fixture-model"})
	if err != nil {
		t.Fatal(err)
	}
	legacySession, err := server.CreateSession(ctx, "member", novelcreation.CreateSessionInput{ProjectID: legacyProject.ID, Mode: "long_original"})
	if err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO novel_runs(run_id,owner_username,project_id,session_id,idempotency_key,status,model_id,prompt_snapshot_json,created_at,updated_at)
VALUES ('legacy-run','member',?,?,'legacy-request','queued','fixture-model','{"chapter_number":1,"user_message":"旧作品"}',NOW(6),NOW(6))`, legacyProject.ID, legacySession.ID)
	if err := storage.RunMigrations(ctx, db, novelMigrations[len(novelMigrations)-1:]); err != nil {
		t.Fatal(err)
	}
	checkSnapshot := func(id, wantTeam, wantActor string) {
		t.Helper()
		var owner string
		var team, actor sql.NullString
		err := db.QueryRowContext(ctx, `SELECT owner_username,usage_team_id,usage_actor_username FROM novel_runs WHERE run_id=?`, id).Scan(&owner, &team, &actor)
		if err != nil || owner != "member" || team != (sql.NullString{String: wantTeam, Valid: wantTeam != ""}) || actor != (sql.NullString{String: wantActor, Valid: wantActor != ""}) {
			t.Fatalf("run %s owner=%q team=%v actor=%v error=%v", id, owner, team, actor, err)
		}
	}
	checkSnapshot("legacy-run", "", "")
	router := NewRouter(RouterOptions{NovelCreation: server, AccountCenterStore: accounts, SessionResolver: func(_ context.Context, token string) (BridgeIdentity, error) {
		if token == "founder" || token == "member" {
			return BridgeIdentity{Username: token, IsOwner: token == "founder"}, nil
		}
		return BridgeIdentity{}, accountcenter.ErrForbidden
	}})
	cookie := startImpersonationForTest(t, router, "founder", "member")
	post := func(path, token, body string, cookie *http.Cookie, status int, dest any) {
		t.Helper()
		response := impersonationRequest(router, http.MethodPost, path, token, body, cookie)
		if response.Code != status {
			t.Fatalf("%s returned %d: %s", path, response.Code, response.Body)
		}
		if err := json.Unmarshal(response.Body.Bytes(), dest); err != nil {
			t.Fatal(err)
		}
	}
	var project struct{ Project novelcreation.Project }
	post("/api/novel-creation/projects", "founder", `{"title":"新作品","genre":"都市","modelId":"fixture-model"}`, cookie, http.StatusCreated, &project)
	var session struct{ Session novelcreation.Session }
	post("/api/novel-creation/projects/"+project.Project.ID+"/sessions", "founder", `{"mode":"long_original"}`, cookie, http.StatusCreated, &session)
	var first, retry struct{ Run novelcreation.Run }
	post("/api/novel-creation/sessions/"+session.Session.ID+"/messages", "founder", `{"content":"写第一章","modelId":"fixture-model","idempotencyKey":"initial"}`, cookie, http.StatusAccepted, &first)
	checkSnapshot(first.Run.ID, "team-a", "founder")
	if err := accounts.EndMembership(ctx, "team-a", "member", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := accounts.AddMembership(ctx, accountcenter.Membership{TeamID: "team-b", Username: "member"}); err != nil {
		t.Fatal(err)
	}

	// A new database connection and service have no request context or queue memory.
	workerDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer workerDB.Close()
	tracker := &yzcmusage.Tracker{Store: yzcmusage.Store{DB: workerDB}, ReceiptDir: t.TempDir()}
	client := &http.Client{Transport: tracker.Wrap(roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		id, _ := r.Context().Value(novelReceiptIDKey{}).(string)
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Oneapi-Request-Id": []string{id}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}]}`))}, nil
	}))}
	resolve := NovelCreationTextResolver(accountcenter.NewMySQLStore(workerDB), func(_ context.Context, owner, model string) (batchfactoryv11.DirectorProvider, error) {
		return &batchfactoryv11.OpenAICompatibleProvider{UsageOwner: owner, Endpoint: "https://newapi.yzcm.site/v1/chat/completions", APIKey: "fixture-key", Model: model, Client: client}, nil
	})
	workerStore := novelcreation.NewMySQLStore(workerDB)
	worker := novelcreation.NewService(workerStore, novelcreation.NewMemoryRuntime(), novelAttributionPipeline(func(ctx context.Context, in novelcreation.RunContext) error {
		ctx = context.WithValue(ctx, novelReceiptIDKey{}, in.Run.ID)
		provider, err := resolve(ctx, in.OwnerUsername, in.Run.ModelID)
		if err != nil {
			return err
		}
		if _, err := provider.Complete(ctx, batchfactoryv11.TextCompletionRequest{}); err != nil {
			return err
		}
		return &novelcreation.DomainError{Code: novelcreation.ErrModelUnavailable, Message: "fixture retryable failure"}
	}))
	workerCtx, stop := context.WithCancel(yzcmusage.WithAttribution(ctx, "member", "team-b", "member"))
	defer stop()
	novelcreation.StartWorker(workerCtx, worker, 100*time.Millisecond)
	checkReceipt := func(id, wantTeam, wantActor string) {
		t.Helper()
		deadline, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		for deadline.Err() == nil {
			run, err := workerStore.GetRun(deadline, "member", id)
			if err == nil && run.Status == novelcreation.RunFailed && run.ErrorMessage == "fixture retryable failure" {
				var owner string
				var team, actor sql.NullString
				err = db.QueryRowContext(deadline, `SELECT owner,team_id,actor_username FROM yzcm_usage_requests WHERE request_id=?`, id).Scan(&owner, &team, &actor)
				if err != nil || owner != "member" || team != (sql.NullString{String: wantTeam, Valid: wantTeam != ""}) || actor != (sql.NullString{String: wantActor, Valid: wantActor != ""}) {
					t.Fatalf("receipt %s owner=%q team=%v actor=%v error=%v", id, owner, team, actor, err)
				}
				return
			}
			if err == nil && run.Status == novelcreation.RunFailed {
				t.Fatalf("unexpected failure for %s: %s %s", id, run.ErrorCode, run.ErrorMessage)
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("worker did not reach the expected failure after receiving usage for %s", id)
	}
	checkReceipt(first.Run.ID, "team-a", "founder")
	checkReceipt("legacy-run", "", "")
	post("/api/novel-creation/runs/"+first.Run.ID+"/retry", "member", "", nil, http.StatusAccepted, &retry)
	if retry.Run.ID == first.Run.ID {
		t.Fatal("retry did not receive a new run ID")
	}
	checkSnapshot(retry.Run.ID, "team-a", "founder")
	checkReceipt(retry.Run.ID, "team-a", "founder")
}

func novelAttributionDatabase(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv("QIANTIE_TEST_MYSQL84_DSN")
	if dsn == "" {
		t.Skip("QIANTIE_TEST_MYSQL84_DSN is required for disposable MySQL verification")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	name := fmt.Sprintf("novel_attribution_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + name); err != nil {
			t.Error(err)
		}
	})
	cfg.DBName, cfg.ParseTime = name, true
	dsn = cfg.FormatDSN()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, dsn
}
