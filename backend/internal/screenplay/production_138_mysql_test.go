package screenplay

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	mysql "github.com/go-sql-driver/mysql"
	"net"
	"os"
	"qiantie/backend/internal/storage"
	"reflect"
	"strings"
	"testing"
	"time"
)

type productionJSONArg struct{ value any }

func (a productionJSONArg) Match(value driver.Value) bool {
	var raw []byte
	switch v := value.(type) {
	case string:
		raw = []byte(v)
	case []byte:
		raw = v
	default:
		return false
	}
	var got, want any
	if json.Unmarshal(raw, &got) != nil {
		return false
	}
	b, _ := json.Marshal(a.value)
	_ = json.Unmarshal(b, &want)
	return reflect.DeepEqual(got, want)
}
func productionBatchRows(auth any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "project_id", "owner_username", "session_id", "status", "current_step", "current_episode", "plan_version", "permission_mode", "pending_actions", "last_error", "revision", "created_at", "updated_at", "last_run_at", "production_authorization"}).AddRow("b", "p", "alice", "s", "waiting_user", "5_storyboard", 1, 1, "bypass", nil, "", 3, time.Now(), time.Now(), nil, auth)
}
func expectProductionBatchDetails(mock sqlmock.Sqlmock, generation any) {
	mock.ExpectQuery("SELECT step_key, status, total_episodes").WithArgs("b", "alice").WillReturnRows(sqlmock.NewRows([]string{"step_key", "status", "total_episodes", "started_at", "finished_at", "artifacts", "error", "review"}).AddRow("5_storyboard", "blocked", 1, nil, nil, `["storyboard://old"]`, nil, `{"pass":false,"summary":"保留医生记录"}`))
	mock.ExpectQuery("SELECT step_key, episode_no, status").WithArgs("b", "alice").WillReturnRows(sqlmock.NewRows([]string{"step_key", "episode_no", "status", "artifact", "reason", "updated_at", "storyboard_generation"}).AddRow("5_storyboard", 1, "blocked", "storyboard://old", "等待继续", time.Now(), generation))
}
func TestProduction138MySQLLoadFrozenAuthorizationAndWait(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved", true: "legacy-null"}[legacy], func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			s := NewMySQLStore(db)
			auth := &ProductionAuthorization{Subject: "admin", TeamID: "team", Actor: "member", RunID: "run"}
			wait := &ProductionStoryboardGeneration{RequestKey: "first", ChapterID: "c1", Status: "awaiting_continue"}
			var authJSON, waitJSON any
			if !legacy {
				a, _ := json.Marshal(auth)
				w, _ := json.Marshal(wait)
				authJSON = string(a)
				waitJSON = string(w)
			}
			mock.ExpectQuery("SELECT id, project_id, owner_username").WithArgs("p", "alice").WillReturnRows(productionBatchRows(authJSON))
			expectProductionBatchDetails(mock, waitJSON)
			b, e := s.GetBatch(context.Background(), "alice", "p")
			if e != nil {
				t.Fatal(e)
			}
			if legacy {
				if b.ProductionAuthorization != nil || b.Step("5_storyboard").Episodes[1].StoryboardGeneration != nil {
					t.Fatal("guessed legacy authorization")
				}
			} else if !reflect.DeepEqual(b.ProductionAuthorization, auth) || !reflect.DeepEqual(b.Step("5_storyboard").Episodes[1].StoryboardGeneration, wait) {
				t.Fatalf("lost new fields: %+v", b)
			}
			if b.Step("5_storyboard").Review.Summary != "保留医生记录" || b.Step("5_storyboard").Episodes[1].Artifact != "storyboard://old" {
				t.Fatal("lost previous batch fields")
			}
			if e = mock.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestProduction138MySQLCreateSavesAuthorizationAndWait(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := NewMySQLStore(db)
	ctx := context.Background()
	auth := &ProductionAuthorization{Subject: "admin", TeamID: "team", Actor: "member", RunID: "run"}
	wait := &ProductionStoryboardGeneration{RequestKey: "first", ChapterID: "c1", Status: "awaiting_continue"}
	b := &Batch{ID: "b", ProjectID: "p", OwnerUsername: "alice", ProductionAuthorization: auth, Steps: map[string]*StepState{"5_storyboard": {Key: "5_storyboard", Episodes: map[int]*EpisodeState{1: {EpisodeNo: 1, UpdatedAt: time.Now().UTC(), StoryboardGeneration: wait}}}}}
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO screenplay_batches").WithArgs("b", "p", "alice", "", "", "", 0, 0, "", sqlmock.AnyArg(), "", int64(0), sqlmock.AnyArg(), sqlmock.AnyArg(), nil, productionJSONArg{auth}).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO screenplay_batch_steps").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO screenplay_batch_episodes").WithArgs("b", "alice", "5_storyboard", 1, "", "", nil, sqlmock.AnyArg(), productionJSONArg{wait}).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	if e := s.CreateBatch(ctx, b); e != nil {
		t.Fatal(e)
	}
	if e := mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestProduction138MySQLUpdateCannotReplaceFrozenAuthorization(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := NewMySQLStore(db)
	auth := &ProductionAuthorization{Subject: "admin", TeamID: "team", Actor: "member", RunID: "old-run"}
	raw, _ := json.Marshal(auth)
	b := &Batch{ID: "b", ProjectID: "p", OwnerUsername: "alice", ProductionAuthorization: &ProductionAuthorization{Subject: "forged", Actor: "owner", RunID: "new-run"}}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT production_authorization FROM screenplay_batches").WithArgs("b", "alice", int64(3)).WillReturnRows(sqlmock.NewRows([]string{"production_authorization"}).AddRow(raw))
	mock.ExpectExec("UPDATE screenplay_batches SET").WithArgs("", "", "", 0, 0, "", sqlmock.AnyArg(), "", sqlmock.AnyArg(), nil, productionJSONArg{auth}, "b", "alice", int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if e := s.UpdateBatch(context.Background(), b, 3); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(b.ProductionAuthorization, auth) {
		t.Fatal("caller replaced frozen attribution")
	}
	if e := mock.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

// Opt-in only. Every caller gets a new disposable database, never the DSN's
// selected database. The connection string is never printed.
func newProduction138MySQLDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("QIANTIE_PRODUCTION_138_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("isolated 138 production MySQL test connection is not configured")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid isolated MySQL test connection")
	}
	if !production138IsolatedAddress(cfg.Net, cfg.Addr) {
		t.Fatal("138 MySQL tests require local isolated TCP 3307/3308")
	}
	cfg.DBName = ""
	cfg.ParseTime = true
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal("open isolated MySQL test server")
	}
	name := fmt.Sprintf("qiantie_production138_test_%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	if _, err = admin.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4"); err != nil {
		admin.Close()
		t.Fatal("create isolated database:", err)
	}
	cfg.DBName = name
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		admin.Close()
		t.Fatal("open isolated database")
	}
	t.Cleanup(func() {
		db.Close()
		clean, stop := context.WithTimeout(context.Background(), 300*time.Second)
		defer stop()
		if strings.HasPrefix(name, "qiantie_production138_test_") {
			if _, err := admin.ExecContext(clean, "DROP DATABASE `"+name+"`"); err != nil {
				t.Error("drop isolated database:", err)
			}
		}
		admin.Close()
	})
	var version string
	if err = db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	t.Log("isolated MySQL version:", version)
	var base []storage.Migration
	var upgrade storage.Migration
	for _, m := range storage.ScreenplayMigrations() {
		if m.Version == 8800124 {
			upgrade = m
		} else {
			base = append(base, m)
		}
	}
	base = append(base, storage.ScreenplayAuthoringMigrations()...)
	base = append(base, storage.ScreenplayProductionMigrations()...)
	if err = storage.RunMigrations(ctx, db, base); err != nil {
		t.Fatal("base migrate:", err)
	}
	if len(upgrade.SQL) != 2 {
		t.Fatal("missing production 138 upgrade")
	}
	// First ALTER committed, process died before the second and ledger insert.
	if _, err = db.ExecContext(ctx, upgrade.SQL[0]); err != nil {
		t.Fatal("simulate half migration:", err)
	}
	for i := 0; i < 2; i++ {
		if err = storage.RunMigrations(ctx, db, []storage.Migration{upgrade}); err != nil {
			t.Fatal("resume/repeat migration:", err)
		}
	}
	return db
}
func TestProduction138MySQLIsolatedPersistence(t *testing.T) {
	db := newProduction138MySQLDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := NewMySQLStore(db)
	if _, err := s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice", Title: "138持久等待"}); err != nil {
		t.Fatal(err)
	}
	b := NewBatch("b", "p", "alice", "s", PermissionBypass)
	if err := s.CreateBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	var missing bool
	if err := db.QueryRowContext(ctx, `SELECT production_authorization IS NULL FROM screenplay_batches WHERE id='b' AND owner_username='alice'`).Scan(&missing); err != nil || !missing {
		t.Fatal("legacy identity must remain SQL NULL", err)
	}
	auth := &ProductionAuthorization{Subject: "admin", TeamID: "team", Actor: "member", RunID: "run"}
	wait := &ProductionStoryboardGeneration{RequestKey: "first", ChapterID: "c1", Status: "awaiting_continue"}
	saved, err := s.MutateBatch(ctx, "alice", "p", func(batch *Batch) error {
		batch.ProductionAuthorization = auth
		batch.Status = BatchWaitingUser
		st := batch.Step("5_storyboard")
		st.Status = StepBlocked
		st.Review = &Review{Summary: "保留旧医生记录"}
		st.Episodes = map[int]*EpisodeState{1: {EpisodeNo: 1, Status: StepBlocked, Artifact: "storyboard://old", UpdatedAt: time.Now().UTC(), StoryboardGeneration: wait}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := NewMySQLStore(db).GetBatch(ctx, "alice", "p")
	if err != nil || !reflect.DeepEqual(reopened.ProductionAuthorization, auth) || !reflect.DeepEqual(reopened.Step("5_storyboard").Episodes[1].StoryboardGeneration, wait) {
		t.Fatalf("reopen lost authorization/wait: %+v %v", reopened, err)
	}
	reopened.ProductionAuthorization = &ProductionAuthorization{Subject: "forged", Actor: "owner", RunID: "forged"}
	if err = s.UpdateBatch(ctx, reopened, saved.Revision); err != nil {
		t.Fatal(err)
	}
	checked, err := s.GetBatch(ctx, "alice", "p")
	if err != nil || !reflect.DeepEqual(checked.ProductionAuthorization, auth) {
		t.Fatal("UpdateBatch replaced frozen identity", err)
	}
	if checked.Step("5_storyboard").Review.Summary != "保留旧医生记录" || checked.Step("5_storyboard").Episodes[1].Artifact != "storyboard://old" {
		t.Fatal("previous review/result changed")
	}
	if _, err = s.GetBatch(ctx, "bob", "p"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross owner read", err)
	}
}

func TestProduction138MySQLMutatePersistsTrustedAuthorizationAndWait(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := NewMySQLStore(db)
	auth := &ProductionAuthorization{Subject: "admin", TeamID: "team", Actor: "member", RunID: "new-run"}
	wait := &ProductionStoryboardGeneration{RequestKey: "next", RetryOf: "first", ChapterID: "c1", Status: "awaiting_continue"}
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id, project_id, owner_username").WithArgs("p", "alice").WillReturnRows(productionBatchRows(nil))
	expectProductionBatchDetails(mock, nil)
	mock.ExpectExec("UPDATE screenplay_batches SET").WithArgs("s", "waiting_user", "5_storyboard", 1, 1, "bypass", sqlmock.AnyArg(), "", sqlmock.AnyArg(), nil, productionJSONArg{auth}, "b", "alice", int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO screenplay_batch_steps").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO screenplay_batch_episodes").WithArgs("b", "alice", "5_storyboard", 1, "blocked", "storyboard://old", "等待继续", sqlmock.AnyArg(), productionJSONArg{wait}).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	b, err := s.MutateBatch(context.Background(), "alice", "p", func(b *Batch) error {
		st := b.Step("5_storyboard")
		b.Steps = map[string]*StepState{"5_storyboard": st}
		b.ProductionAuthorization = auth
		st.Episodes[1].StoryboardGeneration = wait
		return nil
	})
	if err != nil || b.Revision != 4 {
		t.Fatal("trusted mutation failed", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestProduction138MySQLNilWaitWritesSQLNull(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := NewMySQLStore(db)
	b := &Batch{ID: "b", ProjectID: "p", OwnerUsername: "alice", Steps: map[string]*StepState{"5_storyboard": {Key: "5_storyboard", Episodes: map[int]*EpisodeState{1: {EpisodeNo: 1, UpdatedAt: time.Now().UTC()}}}}}
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO screenplay_batches").WithArgs("b", "p", "alice", "", "", "", 0, 0, "", sqlmock.AnyArg(), "", int64(0), sqlmock.AnyArg(), sqlmock.AnyArg(), nil, nil).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO screenplay_batch_steps").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO screenplay_batch_episodes").WithArgs("b", "alice", "5_storyboard", 1, "", "", nil, sqlmock.AnyArg(), nil).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := s.CreateBatch(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProduction138MySQLUpdateRejectsStaleRevisionBeforeWrite(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := NewMySQLStore(db)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT production_authorization FROM screenplay_batches").WithArgs("b", "alice", int64(3)).WillReturnRows(sqlmock.NewRows([]string{"production_authorization"}))
	mock.ExpectRollback()
	err := s.UpdateBatch(context.Background(), &Batch{ID: "b", OwnerUsername: "alice"}, 3)
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatal("stale revision wrote batch", err)
	}
	if err = mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func production138IsolatedAddress(network, address string) bool {
	host, port, err := net.SplitHostPort(address)
	return network == "tcp" && err == nil && (port == "3307" || port == "3308") && (host == "127.0.0.1" || host == "localhost" || host == "::1")
}
func TestProduction138MySQLIsolatedAddressGuard(t *testing.T) {
	for _, address := range []string{"127.0.0.1:3307", "127.0.0.1:3308", "localhost:3307", "localhost:3308", "[::1]:3307", "[::1]:3308"} {
		if !production138IsolatedAddress("tcp", address) {
			t.Fatal("isolated local test port rejected", address)
		}
	}
	for _, input := range [][2]string{{"tcp", "127.0.0.1:3306"}, {"tcp", "localhost:3306"}, {"tcp", "[::1]:3306"}, {"tcp", "192.0.2.10:3308"}, {"tcp", "0.0.0.0:3308"}, {"tcp", "test.example:3307"}, {"unix", "/tmp/mysql.sock"}, {"tcp", "localhost"}, {"tcp4", "127.0.0.1:3308"}} {
		if production138IsolatedAddress(input[0], input[1]) {
			t.Fatal("non-isolated connection allowed", input)
		}
	}
}
