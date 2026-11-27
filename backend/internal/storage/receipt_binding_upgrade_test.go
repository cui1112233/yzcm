package storage

import (
	"context"
	"strings"
	"testing"
)

func receiptBindingUpgradeMigrations(t *testing.T) []Migration {
	t.Helper()
	var result []Migration
	for _, m := range AppMigrations() {
		if m.Version == 8800408 || m.Version == 8800411 {
			result = append(result, m)
		}
	}
	if len(result) != 2 {
		t.Fatalf("want migrations 8800408 and 8800411, got %d", len(result))
	}
	return result
}
func TestReceiptBindingUpgradeRegistered(t *testing.T) {
	migrations := receiptBindingUpgradeMigrations(t)
	if migrations[0].Version != 8800408 || migrations[1].Version != 8800411 {
		t.Fatalf("wrong migration order: %+v", migrations)
	}
	if got := ChecksumFor(migrations[0]); got != "96ae8e321ca41d3988cc0c4b29ac6211e3156f79555cc0afa28d4f379c9df4be" {
		t.Fatalf("original migration changed: %s", got)
	}
}
func TestReceiptBindingUpgradeMySQL(t *testing.T) {
	for _, scenario := range []string{"fresh", "v1", "ddl-committed", "malformed", "malformed-default", "unknown-checksum", "v2-checksum"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			db := energyMigrationTestDatabase(t)
			migrations := receiptBindingUpgradeMigrations(t)
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := db.ExecContext(ctx, q, args...); err != nil {
					t.Fatal(err)
				}
			}
			if scenario != "fresh" {
				if err := RunMigrations(ctx, db, migrations[:1]); err != nil {
					t.Fatal(err)
				}
				exec(`INSERT INTO yzcm_usage_receipt_bindings(binding_slot,path_digest,store_id,state,origin) VALUES ('member-system',REPEAT('a',64),REPEAT('b',32),'pending','new')`)
			}
			switch scenario {
			case "ddl-committed":
				exec(`ALTER TABLE yzcm_usage_receipt_bindings ADD COLUMN filesystem_identity VARCHAR(224) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER origin, ADD COLUMN sync_parent_levels SMALLINT UNSIGNED NOT NULL DEFAULT 1 AFTER filesystem_identity`)
			case "malformed":
				exec(`ALTER TABLE yzcm_usage_receipt_bindings ADD COLUMN filesystem_identity VARCHAR(64) NULL`)
			case "malformed-default":
				exec(`ALTER TABLE yzcm_usage_receipt_bindings ALTER COLUMN state SET DEFAULT 'active'`)
			case "unknown-checksum":
				exec(`UPDATE schema_migrations SET checksum=REPEAT('0',64) WHERE version=8800408`)
			case "v2-checksum":
				exec(`UPDATE schema_migrations SET checksum='fe46cef320015391c7988fb517b8d70673f763ae83cd95458dcbbf135849ed02' WHERE version=8800408`)
			}
			err := RunMigrations(ctx, db, migrations)
			if strings.HasPrefix(scenario, "malformed") || strings.Contains(scenario, "checksum") {
				if err == nil {
					t.Fatal("accepted invalid schema/checksum")
				}
				var n int
				if e := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=8800411`).Scan(&n); e != nil || n != 0 {
					t.Fatalf("invalid ledger %d %v", n, e)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := RunMigrations(ctx, db, migrations); err != nil {
				t.Fatal(err)
			}
			for _, migration := range migrations {
				var checksum string
				if e := db.QueryRow(`SELECT checksum FROM schema_migrations WHERE version=?`, migration.Version).Scan(&checksum); e != nil || checksum != ChecksumFor(migration) {
					t.Fatalf("ledger %d changed: %s %v", migration.Version, checksum, e)
				}
			}
			var n int
			if e := db.QueryRow(`SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='yzcm_usage_receipt_bindings'`).Scan(&n); e != nil || n != 9 {
				t.Fatalf("columns %d %v", n, e)
			}
			if scenario != "fresh" {
				var id, state string
				var levels int
				if e := db.QueryRow(`SELECT store_id,state,sync_parent_levels FROM yzcm_usage_receipt_bindings WHERE binding_slot='member-system'`).Scan(&id, &state, &levels); e != nil || id != strings.Repeat("b", 32) || state != "pending" || levels != 1 {
					t.Fatalf("reservation %s %s %d %v", id, state, levels, e)
				}
			}
		})
	}
}
