package storage

import (
	"context"
	"strings"
	"testing"
)

func TestLegacyConfigMigrationStateSchema(t *testing.T) {
	var registered []Migration
	for _, m := range NovelFetchWorkshopMigrations() {
		if m.Version == 1200007 {
			registered = append(registered, m)
		}
	}
	if len(registered) != 1 {
		t.Fatalf("1200007 registrations=%d", len(registered))
	}
	migration := registered[0]
	sql := strings.Join(migration.SQL, " ")
	for _, field := range []string{"CREATE TABLE IF NOT EXISTS novel_fetch_legacy_config_migrations", "owner_username VARCHAR(191) NOT NULL", "phase VARCHAR(32) NOT NULL", "candidate_ciphertext LONGTEXT NULL", "candidate_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL", "patch_ciphertext LONGTEXT NULL", "patch_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL", "revision BIGINT UNSIGNED NOT NULL DEFAULT 1", "created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)", "updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)", "PRIMARY KEY (owner_username)", "KEY idx_legacy_config_migrations_phase (phase, updated_at)", "ENGINE=InnoDB"} {
		if !strings.Contains(sql, field) {
			t.Fatalf("missing %s", field)
		}
	}
	ctx := context.Background()
	db := energyMigrationTestDatabase(t)
	for i := 0; i < 2; i++ {
		if err := RunMigrations(ctx, db, registered); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var checksum, version string
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*),MAX(checksum),VERSION() FROM schema_migrations WHERE version=1200007").Scan(&count, &checksum, &version); err != nil || count != 1 || checksum != ChecksumFor(migration) {
		t.Fatal("invalid migration receipt", count, err)
	}
	t.Logf("MySQL %s migration 1200007 registered once with checksum %s", version, checksum)
}
