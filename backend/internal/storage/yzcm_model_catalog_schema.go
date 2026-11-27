package storage

func YZCMModelCatalogMigrations() []Migration {
	return []Migration{{
		Version: 8800413,
		SQL: []string{
			`CREATE TABLE IF NOT EXISTS yzcm_model_sync_runs (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
 provenance ENUM('yzcm_relay') NOT NULL,
 status ENUM('running','succeeded','failed') NOT NULL,
 started_at_us BIGINT NOT NULL,
 finished_at_us BIGINT NULL,
 source_updated_at_us BIGINT NULL,
 model_count INT UNSIGNED NULL,
 failure_code ENUM('source_error','invalid_snapshot','persistence_error') NULL,
 KEY yzcm_latest_catalog (status, source_updated_at_us DESC, id DESC)
) ENGINE=InnoDB`,
			`CREATE TABLE IF NOT EXISTS yzcm_model_catalog (
 sync_run_id BIGINT UNSIGNED NOT NULL,
 kind ENUM('text','image','video','audio','asr') NOT NULL,
 model_id VARCHAR(191) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_bin NOT NULL,
 display_name VARCHAR(256) CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_ci NOT NULL,
 adapted BOOLEAN NOT NULL,
 allowed BOOLEAN NOT NULL,
 availability_reason ENUM('adapted_and_allowed','not_adapted','not_allowed') NOT NULL,
 PRIMARY KEY (sync_run_id, kind, model_id),
 KEY yzcm_catalog_search (sync_run_id, adapted, allowed, kind, display_name, model_id),
 CONSTRAINT yzcm_catalog_sync_run FOREIGN KEY (sync_run_id) REFERENCES yzcm_model_sync_runs (id)
) ENGINE=InnoDB`,
		},
	}}
}
