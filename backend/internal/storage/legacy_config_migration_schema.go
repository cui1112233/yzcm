package storage

func legacyConfigMigrationStateSchema() Migration {
	return Migration{Version: 1200007, CallbackChecksum: "novel-fetch-legacy-config-migrations-v1", SQL: []string{`CREATE TABLE IF NOT EXISTS novel_fetch_legacy_config_migrations (
  owner_username VARCHAR(191) NOT NULL,
  phase VARCHAR(32) NOT NULL,
  candidate_ciphertext LONGTEXT NULL,
  candidate_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
  patch_ciphertext LONGTEXT NULL,
  patch_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
  revision BIGINT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (owner_username),
  KEY idx_legacy_config_migrations_phase (phase, updated_at)
) ENGINE=InnoDB`}}
}
