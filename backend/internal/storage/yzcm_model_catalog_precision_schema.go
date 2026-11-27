package storage

import (
	"context"
	"database/sql"
	"errors"
)

const yzcmCatalogPrecisionAlter = `ALTER TABLE yzcm_model_sync_runs
 ADD COLUMN source_updated_at_ns BIGINT NULL AFTER source_updated_at_us,
 ADD COLUMN snapshot_digest BINARY(32) NULL AFTER source_updated_at_ns,
 ADD COLUMN disposition ENUM('older_source_time','duplicate_snapshot') NULL AFTER failure_code,
 MODIFY COLUMN status ENUM('running','succeeded','failed','ignored','idempotent') NOT NULL,
 MODIFY COLUMN failure_code ENUM('source_error','invalid_snapshot','persistence_error','source_time_conflict') NULL`

const yzcmCatalogHeadDDL = `CREATE TABLE IF NOT EXISTS yzcm_model_catalog_head (
 slot TINYINT UNSIGNED NOT NULL PRIMARY KEY CHECK (slot=1),
 sync_run_id BIGINT UNSIGNED NULL,
 CONSTRAINT yzcm_head_sync_run FOREIGN KEY (sync_run_id) REFERENCES yzcm_model_sync_runs (id)
) ENGINE=InnoDB`

const yzcmCatalogPrecisionBackfill = `UPDATE yzcm_model_sync_runs
SET source_updated_at_ns = source_updated_at_us * 1000
WHERE source_updated_at_ns IS NULL AND source_updated_at_us IS NOT NULL`

const yzcmCatalogSeedHead = `INSERT INTO yzcm_model_catalog_head (slot,sync_run_id) VALUES (1,
 (SELECT id FROM yzcm_model_sync_runs WHERE status='succeeded' ORDER BY source_updated_at_us DESC, id DESC LIMIT 1))
ON DUPLICATE KEY UPDATE slot=yzcm_model_catalog_head.slot`

func YZCMModelCatalogPrecisionMigration() Migration {
	return Migration{
		Version:          8800414,
		SQL:              []string{yzcmCatalogPrecisionAlter, yzcmCatalogHeadDDL, yzcmCatalogPrecisionBackfill, yzcmCatalogSeedHead},
		CallbackChecksum: "yzcm-catalog-nanosecond-head-v1",
		Adopt:            resumeYZCMModelCatalogPrecision,
	}
}

func yzcmCatalogPrecisionShape(ctx context.Context, tx *sql.Tx) (int, int, error) {
	var added, matching int
	err := tx.QueryRowContext(ctx, `SELECT
COALESCE(SUM(column_name IN ('source_updated_at_ns','snapshot_digest','disposition')),0),
COALESCE(SUM(CASE
 WHEN column_name='source_updated_at_ns' AND column_type='bigint' AND is_nullable='YES' THEN 1
 WHEN column_name='snapshot_digest' AND column_type='binary(32)' AND is_nullable='YES' THEN 1
 WHEN column_name='disposition' AND column_type='enum(''older_source_time'',''duplicate_snapshot'')' AND is_nullable='YES' THEN 1
 WHEN column_name='status' AND column_type='enum(''running'',''succeeded'',''failed'',''ignored'',''idempotent'')' AND is_nullable='NO' THEN 1
 WHEN column_name='failure_code' AND column_type='enum(''source_error'',''invalid_snapshot'',''persistence_error'',''source_time_conflict'')' AND is_nullable='YES' THEN 1
 ELSE 0 END),0)
FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='yzcm_model_sync_runs'`).Scan(&added, &matching)
	return added, matching, err
}

func resumeYZCMModelCatalogPrecision(ctx context.Context, tx *sql.Tx) (bool, error) {
	added, matching, err := yzcmCatalogPrecisionShape(ctx, tx)
	if err != nil {
		return false, err
	}
	if added == 0 {
		if _, err := tx.ExecContext(ctx, yzcmCatalogPrecisionAlter); err != nil {
			return false, err
		}
		added, matching, err = yzcmCatalogPrecisionShape(ctx, tx)
		if err != nil {
			return false, err
		}
	}
	if added != 3 || matching != 5 {
		return false, errors.New("invalid YZCM model catalog precision schema")
	}
	// DDL commits independently of the ledger. Each step after ALTER may be
	// resumed, and an already initialized head must never be chosen again.
	for _, statement := range []string{yzcmCatalogHeadDDL, yzcmCatalogPrecisionBackfill, yzcmCatalogSeedHead} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return false, err
		}
	}
	return true, nil
}
