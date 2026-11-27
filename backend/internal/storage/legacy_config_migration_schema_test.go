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

func TestLegacySettingFileReceiptsUpgrade(t *testing.T) {
	ctx := context.Background()
	db := energyMigrationTestDatabase(t)
	old := legacyConfigMigrationStateSchema()
	if err := RunMigrations(ctx, db, []Migration{old}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO novel_fetch_legacy_config_migrations(owner_username,phase,revision,candidate_ciphertext) VALUES('kept','finalizing',9,'synthetic')"); err != nil {
		t.Fatal(err)
	}
	var registration []Migration
	for _, m := range NovelFetchWorkshopMigrations() {
		if m.Version == 1200008 {
			registration = append(registration, m)
		}
	}
	if len(registration) != 1 {
		t.Fatalf("file receipt migration registrations=%d", len(registration))
	}
	for range 2 {
		if err := RunMigrations(ctx, db, registration); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM novel_fetch_legacy_config_migrations WHERE owner_username='kept' AND phase='finalizing' AND revision=9 AND candidate_ciphertext='synthetic'").Scan(&n); err != nil || n != 1 {
		t.Fatal("existing state changed during additive migration", err)
	}
	var checksum string
	if err := db.QueryRow("SELECT checksum FROM schema_migrations WHERE version=1200007").Scan(&checksum); err != nil || checksum != ChecksumFor(old) {
		t.Fatal("historical migration checksum changed", err)
	}
}

func TestLegacySettingFileEvidenceUpgradePreservesReceipts(t *testing.T) {
	ctx := context.Background()
	db := energyMigrationTestDatabase(t)
	prior := legacySettingFileReceiptsSchema()
	if err := RunMigrations(ctx, db, []Migration{prior}); err != nil {
		t.Fatal(err)
	}
	fp := strings.Repeat("1", 64)
	if _, err := db.Exec("INSERT INTO novel_fetch_legacy_setting_files(owner_username,fingerprint) VALUES('owner',?)", fp); err != nil {
		t.Fatal(err)
	}
	var upgrade []Migration
	for _, m := range NovelFetchWorkshopMigrations() {
		if m.Version == 1200009 {
			upgrade = append(upgrade, m)
		}
	}
	if len(upgrade) != 1 {
		t.Fatal("missing additive evidence migration")
	}
	for range 2 {
		if err := RunMigrations(ctx, db, upgrade); err != nil {
			t.Fatal(err)
		}
	}
	var counted int
	if err := db.QueryRow("SELECT COUNT(*) FROM novel_fetch_legacy_setting_files WHERE owner_username='owner' AND fingerprint=? AND source_ciphertext IS NULL", fp).Scan(&counted); err != nil || counted != 1 {
		t.Fatal("lost old receipt", err)
	}
	var checksum string
	if err := db.QueryRow("SELECT checksum FROM schema_migrations WHERE version=1200008").Scan(&checksum); err != nil || checksum != ChecksumFor(prior) {
		t.Fatal("receipt migration changed", err)
	}
}
