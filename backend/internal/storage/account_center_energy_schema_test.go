package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
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
			if err := RunMigrations(ctx, db, []Migration{migration}); err != nil {
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
				if err := store.Record(ctx, yzcmusage.Request{ID: "receipt", Owner: owner, TeamID: "team-b", ActorUsername: "other", Feature: "script"}, hash, 200); err != nil {
					t.Fatal(err)
				}
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
		})
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
