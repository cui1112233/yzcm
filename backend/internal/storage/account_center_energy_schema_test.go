package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/yzcmusage"
)

func TestEnergyAttributionMigrationRegisteredAfterAccountGovernance(t *testing.T) {
	// 8800405 is already recorded for account governance. The energy migration
	// takes the next version so existing installations keep that checksum.
	var found []Migration
	for _, migration := range AppMigrations() {
		if migration.Version == 8800406 {
			found = append(found, migration)
		}
	}
	if len(found) != 1 {
		t.Fatalf("registered energy migrations = %d, want one 8800406", len(found))
	}
	ddl := strings.Join(found[0].SQL, "\n")
	for _, want := range []string{
		"team_id VARCHAR(64) NULL",
		"actor_username VARCHAR(32) NULL",
		"(team_id, created_at)",
		"(actor_username, created_at)",
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("energy migration missing %s", want)
		}
	}
}

func TestEnergyReceiptBindingMigrationRegisteredOnce(t *testing.T) {
	var count int
	for _, migration := range AppMigrations() {
		if migration.Version == 8800408 {
			count++
			if len(migration.SQL) != 1 || migration.SQL[0] != yzcmusage.ReceiptBindingStatement() {
				t.Fatalf("unexpected receipt-binding migration: %+v", migration)
			}
		}
	}
	if count != 1 {
		t.Fatalf("receipt-binding migration registered %d times", count)
	}
}

func TestEnergyReceiptBindingMigrationMySQLResumesAfterDDLCommit(t *testing.T) {
	ctx := context.Background()
	db := energyMigrationTestDatabase(t)
	var migration Migration
	for _, candidate := range AccountCenterEnergyMigrations() {
		if candidate.Version == 8800408 {
			migration = candidate
		}
	}
	if migration.Version == 0 {
		t.Fatal("receipt-binding migration missing")
	}
	if _, err := db.ExecContext(ctx, migration.SQL[0]); err != nil {
		t.Fatal(err)
	}
	const storeID = "0123456789abcdef0123456789abcdef"
	if _, err := db.ExecContext(ctx, `INSERT INTO yzcm_usage_receipt_bindings(binding_slot,path_digest,store_id,state,origin) VALUES ('member-system',?,?,'pending','new')`, yzcmusage.Fingerprint("fixture"), storeID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := RunMigrations(ctx, db, []Migration{migration}); err != nil {
			t.Fatal(err)
		}
	}
	var actual, state, checksum string
	if err := db.QueryRowContext(ctx, `SELECT store_id,state FROM yzcm_usage_receipt_bindings WHERE binding_slot='member-system'`).Scan(&actual, &state); err != nil || actual != storeID || state != "pending" {
		t.Fatalf("migration altered reservation: id=%s state=%s error=%v", actual, state, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version=8800408`).Scan(&checksum); err != nil || checksum != ChecksumFor(migration) {
		t.Fatalf("missing migration ledger checksum: %s %v", checksum, err)
	}
}

func TestEnergyReceiptBindingMigrationMySQLRejectsIncompleteDDL(t *testing.T) {
	ctx := context.Background()
	db := energyMigrationTestDatabase(t)
	var migration Migration
	for _, candidate := range AccountCenterEnergyMigrations() {
		if candidate.Version == 8800408 {
			migration = candidate
		}
	}
	if migration.Version == 0 {
		t.Fatal("receipt-binding migration missing")
	}
	wrong := strings.Replace(migration.SQL[0], "filesystem_identity VARCHAR(224)", "filesystem_identity VARCHAR(64)", 1)
	if _, err := db.ExecContext(ctx, wrong); err != nil {
		t.Fatal(err)
	}
	if err := RunMigrations(ctx, db, []Migration{migration}); err == nil || !strings.Contains(err.Error(), "unexpected usage receipt binding definition") {
		t.Fatalf("incomplete DDL was adopted: %v", err)
	}
	var rows int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version=8800408`).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("incomplete DDL was stamped in the ledger: rows=%d error=%v", rows, err)
	}
}

func TestEnergyAttributionMySQLPreservesOwnerActorAndTeam(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(fmt.Sprintf("interrupted=%t", interrupted), func(t *testing.T) {
			ctx := context.Background()
			db := energyMigrationTestDatabase(t)
			base := append(YZCMUsageMigrations(), AccountCenterMigrations()...)
			if err := RunMigrations(ctx, db, base); err != nil {
				t.Fatal(err)
			}
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := db.ExecContext(ctx, query, args...); err != nil {
					t.Fatal(err)
				}
			}
			for _, team := range []string{"team-a", "team-b", "archived-team"} {
				exec(`INSERT INTO account_center_teams (id,name,manager_username,created_by) VALUES (?,?,?,?)`, team, team, "manager", "founder")
			}
			exec(`UPDATE account_center_teams SET archived_at=NOW(6) WHERE id='archived-team'`)
			exec(`INSERT INTO account_center_memberships (team_id,username,joined_at,ended_at) VALUES
('team-a','member','2025-01-01',NULL), ('archived-team','archived','2025-01-01',NULL), ('team-a','former','2025-01-01','2025-02-01')`)
			exec(`INSERT INTO account_center_audit_events(actor_username,subject_username,team_id,action,created_at)
VALUES ('founder','member','team-a','create_account','2025-01-01 00:00:01')`)
			hash := yzcmusage.Fingerprint("fixture-key")
			for _, legacy := range []struct{ id, owner, at string }{
				{"legacy", "member", "2025-02-01"}, {"earlier", "member", "2024-12-31"},
				{"archived", "archived", "2025-02-01"}, {"former", "former", "2025-01-02"},
			} {
				exec(`INSERT INTO yzcm_usage_requests(request_id,owner,feature,credential_hash,created_at,http_status) VALUES(?,?,?,?,?,200)`, legacy.id, legacy.owner, "script", hash, legacy.at)
			}
			var migration Migration
			for _, candidate := range AppMigrations() {
				if candidate.Version == 8800406 {
					migration = candidate
				}
			}
			if migration.Version == 0 {
				t.Fatal("energy attribution migration is missing")
			}
			if interrupted {
				exec(migration.SQL[0]) // MySQL committed the DDL before writing the ledger row.
			}
			if err := RunMigrations(ctx, db, AccountCenterEnergyMigrations()); err != nil {
				t.Fatal(err)
			}
			check := func(id, wantOwner, wantTeam, wantActor string, wantTokens sql.NullInt64) {
				t.Helper()
				var owner string
				var team, actor sql.NullString
				var tokens sql.NullInt64
				err := db.QueryRowContext(ctx, `SELECT owner,team_id,actor_username,input_tokens FROM yzcm_usage_requests WHERE request_id=?`, id).Scan(&owner, &team, &actor, &tokens)
				if err != nil || owner != wantOwner || team != (sql.NullString{String: wantTeam, Valid: wantTeam != ""}) || actor != (sql.NullString{String: wantActor, Valid: wantActor != ""}) || tokens != wantTokens {
					t.Fatalf("%s: owner=%q team=%v actor=%v tokens=%v err=%v", id, owner, team, actor, tokens, err)
				}
			}
			check("legacy", "member", "team-a", "", sql.NullInt64{})
			check("earlier", "member", "", "", sql.NullInt64{})
			check("archived", "archived", "", "", sql.NullInt64{})
			check("former", "former", "", "", sql.NullInt64{})
			store := yzcmusage.Store{DB: db}
			if err := store.Record(ctx, yzcmusage.Request{ID: "receipt", Owner: "member", TeamID: "team-a", ActorUsername: "founder", Feature: "script"}, hash, 200); err != nil {
				t.Fatal(err)
			}
			exec(`UPDATE account_center_memberships SET ended_at=NOW(6) WHERE active_username='member'`)
			exec(`INSERT INTO account_center_memberships (team_id,username) VALUES ('team-b','member')`)
			exec(`UPDATE account_center_teams SET archived_at=NOW(6) WHERE id='team-a'`)
			if err := RunMigrations(ctx, db, []Migration{migration}); err != nil {
				t.Fatal(err)
			}
			for _, owner := range []string{"member", "other"} {
				if err := store.Record(ctx, yzcmusage.Request{ID: "receipt", Owner: owner, TeamID: "team-b", ActorUsername: "other", Feature: "script"}, hash, 200); !errors.Is(err, yzcmusage.ErrReceiptAttributionConflict) {
					t.Fatalf("unexpected duplicate result: %v", err)
				}
			}
			if err := store.Record(ctx, yzcmusage.Request{ID: "receipt", Owner: "member", TeamID: "team-a", ActorUsername: "founder", Feature: "script"}, hash, 200); err != nil {
				t.Fatal(err)
			}
			if err := store.Settle(ctx, yzcmusage.Entry{ID: "receipt", Owner: "other", InputTokens: 999}); err != nil {
				t.Fatal(err)
			}
			check("receipt", "member", "team-a", "founder", sql.NullInt64{})
			if err := store.Settle(ctx, yzcmusage.Entry{ID: "receipt", Owner: "member", InputTokens: 7, Cost: 0.05, Currency: "CNY"}); err != nil {
				t.Fatal(err)
			}
			if err := store.Settle(ctx, yzcmusage.Entry{ID: "receipt", Owner: "member", InputTokens: 999}); err != nil {
				t.Fatal(err)
			}
			check("receipt", "member", "team-a", "founder", sql.NullInt64{Int64: 7, Valid: true})
			check("legacy", "member", "team-a", "", sql.NullInt64{})
			summary, err := store.ReadSummary(ctx, "member")
			if err != nil {
				t.Fatal(err)
			}
			month := summary["month"].(map[string]any)
			if month["calls"] != int64(1) || month["pricedCalls"] != int64(1) || month["actualCost"] != 0.05 {
				t.Fatalf("duplicate receipt changed the member's charge: %v", month)
			}
			if !interrupted {
				checkConcurrentEnergyTrackers(t, db)
				check("cross-tracker", "member", "team-a", "founder", sql.NullInt64{})
			}
		})
	}
}

type energyReceiptTransport struct{}

func (energyReceiptTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{"X-Oneapi-Request-Id": []string{"cross-tracker"}}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
}

func checkConcurrentEnergyTrackers(t *testing.T, db *sql.DB) {
	t.Helper()
	started := make(chan struct{})
	var once sync.Once
	failedDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		if strings.Contains(actual, "INSERT INTO yzcm_usage_requests") {
			once.Do(func() { close(started) })
		}
		return sqlmock.QueryMatcherRegexp.Match(expected, actual)
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer failedDB.Close()
	dir := t.TempDir()
	first := &yzcmusage.Tracker{Store: yzcmusage.Store{DB: failedDB}, ReceiptDir: dir}
	second := &yzcmusage.Tracker{Store: yzcmusage.Store{DB: db}, ReceiptDir: dir}
	if err := second.InitializeReceiptStorage(context.Background()); err != nil {
		t.Fatal(err)
	}
	var pathDigest, storeID, state, origin, filesystemIdentity string
	var parentLevels int
	if err := db.QueryRow(`SELECT path_digest,store_id,state,origin,filesystem_identity,sync_parent_levels FROM yzcm_usage_receipt_bindings WHERE binding_slot='member-system'`).Scan(&pathDigest, &storeID, &state, &origin, &filesystemIdentity, &parentLevels); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT path_digest,store_id,state,origin").WithArgs("member-system").
		WillReturnRows(sqlmock.NewRows([]string{"path_digest", "store_id", "state", "origin", "filesystem_identity", "sync_parent_levels"}).AddRow(pathDigest, storeID, state, origin, filesystemIdentity, parentLevels))
	mock.ExpectExec("INSERT INTO yzcm_usage_requests").
		WithArgs("cross-tracker", "member", "team-a", "founder", "model", yzcmusage.Fingerprint("fixture-key"), sqlmock.AnyArg(), 200).
		WillDelayFor(200 * time.Millisecond).WillReturnError(errors.New("fixture connection failure"))
	request := func(tracker *yzcmusage.Tracker, owner, team, actor string) error {
		ctx := yzcmusage.WithAttribution(context.Background(), owner, team, actor)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, yzcmusage.Origin+"/v1/chat/completions", nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer fixture-key")
		response, err := tracker.Wrap(energyReceiptTransport{}).RoundTrip(req)
		if response != nil {
			_ = response.Body.Close()
		}
		return err
	}
	done := make(chan error, 1)
	go func() { done <- request(first, "member", "team-a", "founder") }()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("first tracker did not reach its database: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("first tracker did not reach its database")
	}
	if err := request(second, "other", "team-b", "manager"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "receipt-*.json"))
	if err != nil || len(files) != 0 {
		t.Fatalf("a confirmed receipt must be cleaned up, files=%v err=%v", files, err)
	}
}

func TestEnergyMySQLBackfillRequiresFormalUnambiguousMembership(t *testing.T) {
	ctx := context.Background()
	db := energyMigrationTestDatabase(t)
	base := append(AuthMigrations(), YZCMUsageMigrations()...)
	base = append(base, AccountCenterMigrations()...)
	if err := RunMigrations(ctx, db, base); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, username := range []string{"founder", "lead", "onlylead", "imported", "unproved"} {
		exec(`INSERT INTO accounts(username,password_hash,password_algorithm,is_owner,active,created_at,updated_at)
VALUES (?, 'fixture', 'fixture', ?, TRUE, NOW(6), NOW(6))`, username, username == "founder")
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"member-profiles.json": `[{"username":"lead","role":"manager"},{"username":"onlylead","role":"manager"},{"username":"imported","boundTo":"lead"},{"username":"unproved","boundTo":"onlylead"}]`,
		"team-collaboration.json": `{"teams":[
{"id":"go-team-lead","managerUsername":"lead","createdAt":"2025-01-01T00:00:00Z"},
{"id":"archived","managerUsername":"lead","createdAt":"2025-02-01T00:00:00Z","archivedAt":"2025-04-01T00:00:00Z"},
{"id":"only","managerUsername":"onlylead","createdAt":"2025-01-01T00:00:00Z"}]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store := accountcenter.NewMySQLStore(db)
	if err := store.ImportLegacy(ctx, dir); err != nil {
		t.Fatal(err)
	}
	for username, action := range map[string]string{"proved": "create_account", "ambiguous": "create_account", "invited": "accept_invite"} {
		if err := store.WithAdmin(ctx, true, func(tx accountcenter.AdminTx) error {
			if err := tx.Change(ctx, accountcenter.AdminChange{Action: action, TeamID: "go-team-lead", Actor: "founder", Username: username, DisplayName: username, Role: accountcenter.RoleMember, PasswordHash: "fixture", PasswordAlgorithm: "fixture", InviteDigest: yzcmusage.Fingerprint("fixture-invitation")}); err != nil {
				return err
			}
			return tx.AppendAudit(ctx, accountcenter.AuditEvent{ActorUsername: "founder", SubjectUsername: username, TeamID: "go-team-lead", Action: action})
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Even an audited current membership cannot disambiguate another applicable interval.
	exec(`INSERT INTO account_center_memberships(team_id,username,joined_at,ended_at)
VALUES ('only','ambiguous',NOW(6)-INTERVAL 1 DAY,NOW(6)+INTERVAL 2 DAY)`)
	hash := yzcmusage.Fingerprint("fixture-key")
	for _, username := range []string{"imported", "unproved"} {
		exec(`INSERT INTO yzcm_usage_requests(request_id,owner,feature,credential_hash,created_at,http_status) VALUES (?,?,'script',?,'2025-03-02',200)`, username, username, hash)
	}
	for _, username := range []string{"proved", "ambiguous", "invited"} {
		exec(`INSERT INTO yzcm_usage_requests(request_id,owner,feature,credential_hash,created_at,http_status) VALUES (?,?,'script',?,NOW(6)+INTERVAL 1 DAY,200)`, username, username, hash)
	}
	if err := RunMigrations(ctx, db, AccountCenterEnergyMigrations()); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{"imported": "", "unproved": "", "ambiguous": "", "proved": "go-team-lead", "invited": "go-team-lead"} {
		var team sql.NullString
		err := db.QueryRowContext(ctx, `SELECT team_id FROM yzcm_usage_requests WHERE request_id=?`, id).Scan(&team)
		if err != nil || team != (sql.NullString{String: want, Valid: want != ""}) {
			t.Errorf("%s team=%v, want %q, err=%v", id, team, want, err)
		}
	}
}

func energyMigrationTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("QIANTIE_TEST_MYSQL84_DSN")
	if dsn == "" {
		t.Skip("QIANTIE_TEST_MYSQL84_DSN not configured for disposable MySQL 8.4 verification")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.DBName = ""
	admin, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	name := fmt.Sprintf("energy_attribution_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + name); err != nil {
			t.Error(err)
		}
	})
	config.DBName, config.ParseTime = name, true
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}
