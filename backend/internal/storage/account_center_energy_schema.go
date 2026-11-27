package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// Governance writes record membership creation and its audit event together.
// Legacy imports infer joined_at from team creation and never emit those events.
const backfillEnergyTeam = `UPDATE yzcm_usage_requests r
JOIN account_center_memberships m ON m.active_username = r.owner AND m.joined_at <= r.created_at
JOIN account_center_teams t ON t.id = m.team_id AND t.archived_at IS NULL
SET r.team_id = m.team_id
WHERE r.team_id IS NULL
AND EXISTS (
 SELECT 1 FROM account_center_audit_events a
 WHERE a.team_id = m.team_id AND a.subject_username = m.username
 AND a.action IN ('create_team','create_account','accept_invite')
 AND a.created_at >= m.joined_at AND a.created_at <= r.created_at
)
AND NOT EXISTS (
 SELECT 1 FROM account_center_memberships other
 WHERE other.username = r.owner AND other.id <> m.id
 AND other.joined_at <= r.created_at AND (other.ended_at IS NULL OR other.ended_at > r.created_at)
)
AND COALESCE((
 SELECT a.action FROM account_center_audit_events a
 WHERE a.team_id = m.team_id AND a.action IN ('archive_team','restore_team') AND a.created_at <= r.created_at
 ORDER BY a.created_at DESC, a.id DESC LIMIT 1
), '') <> 'archive_team'`

// Frozen historical SQL: never use the evolving runtime DDL for an applied migration.
const receiptBindingV1Statement = `CREATE TABLE IF NOT EXISTS yzcm_usage_receipt_bindings (
 binding_slot VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 path_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 store_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 state ENUM('pending','active') NOT NULL,
 origin ENUM('new','upgrade','adopt','db-only') NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 activated_at DATETIME(6) NULL
 ) ENGINE=InnoDB`

const receiptBindingIdentityUpgradeStatement = `ALTER TABLE yzcm_usage_receipt_bindings
ADD COLUMN filesystem_identity VARCHAR(224) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER origin,
ADD COLUMN sync_parent_levels SMALLINT UNSIGNED NOT NULL DEFAULT 1 AFTER filesystem_identity`

func AccountCenterEnergyMigrations() []Migration {
	return []Migration{{
		Version: 8800406,
		SQL: []string{`ALTER TABLE yzcm_usage_requests
ADD COLUMN team_id VARCHAR(64) NULL,
ADD COLUMN actor_username VARCHAR(32) NULL,
ADD INDEX yzcm_usage_team_time (team_id, created_at),
ADD INDEX yzcm_usage_actor_time (actor_username, created_at)`, backfillEnergyTeam},
		CallbackChecksum: "v88-yzcm-usage-attribution-v1",
		Adopt:            resumeEnergyTeamBackfill,
	}, {
		Version:          8800408,
		CallbackChecksum: "v88-yzcm-usage-receipt-binding-v1",
		SQL:              []string{receiptBindingV1Statement},
		Adopt:            adoptEnergyReceiptBindingTable,
		Reconcile:        verifyHistoricalEnergyReceiptBindingTable,
	}, {
		Version:          8800411,
		CallbackChecksum: "v88-yzcm-usage-receipt-binding-filesystem-identity-v1",
		SQL:              []string{receiptBindingIdentityUpgradeStatement},
		Adopt:            upgradeEnergyReceiptBindingIdentity,
		Reconcile:        verifyEnergyReceiptBindingTable,
	}}
}

// Historical ledger checks accept the exact original or upgraded shape. The
// separate forward migration is responsible for requiring the upgraded shape.
func verifyHistoricalEnergyReceiptBindingTable(ctx context.Context, tx *sql.Tx) error {
	n, err := energyReceiptBindingColumnCount(ctx, tx)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("usage receipt binding table missing")
	}
	return nil
}

func verifyEnergyReceiptBindingTable(ctx context.Context, tx *sql.Tx) error {
	n, err := energyReceiptBindingColumnCount(ctx, tx)
	if err != nil {
		return err
	}
	if n != 9 {
		return fmt.Errorf("usage receipt binding identity upgrade missing: columns=%d", n)
	}
	return nil
}

func upgradeEnergyReceiptBindingIdentity(ctx context.Context, tx *sql.Tx) (bool, error) {
	n, err := energyReceiptBindingColumnCount(ctx, tx)
	if err != nil {
		return false, err
	}
	if n == 7 {
		// MySQL commits this atomic ALTER independently of the migration ledger.
		// An interrupted attempt is recognized as the exact nine-column shape.
		if _, err := tx.ExecContext(ctx, receiptBindingIdentityUpgradeStatement); err != nil {
			return false, err
		}
	}
	if err := verifyEnergyReceiptBindingTable(ctx, tx); err != nil {
		return false, err
	}
	return true, nil
}

func adoptEnergyReceiptBindingTable(ctx context.Context, tx *sql.Tx) (bool, error) {
	n, err := energyReceiptBindingColumnCount(ctx, tx)
	return n != 0 && err == nil, err
}

func energyReceiptBindingColumnCount(ctx context.Context, tx *sql.Tx) (int, error) {
	var engine string
	err := tx.QueryRowContext(ctx, `SELECT ENGINE FROM information_schema.tables
WHERE table_schema = DATABASE() AND table_name = 'yzcm_usage_receipt_bindings'`).Scan(&engine)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var columns, matching, primaryColumns, identityColumns int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE
 WHEN extra NOT IN ('','DEFAULT_GENERATED') OR COALESCE(generation_expression,'')<>'' THEN 0
 WHEN column_name='binding_slot' AND column_type='varchar(64)' AND collation_name='ascii_bin' AND is_nullable='NO' AND column_key='PRI' AND column_default IS NULL THEN 1
 WHEN column_name='path_digest' AND column_type='char(64)' AND collation_name='ascii_bin' AND is_nullable='NO' AND column_default IS NULL THEN 1
 WHEN column_name='store_id' AND column_type='char(32)' AND collation_name='ascii_bin' AND is_nullable='NO' AND column_default IS NULL THEN 1
 WHEN column_name='state' AND column_type='enum(''pending'',''active'')' AND is_nullable='NO' AND column_default IS NULL THEN 1
 WHEN column_name='origin' AND column_type='enum(''new'',''upgrade'',''adopt'',''db-only'')' AND is_nullable='NO' AND column_default IS NULL THEN 1
 WHEN column_name='filesystem_identity' AND column_type='varchar(224)' AND collation_name='ascii_bin' AND is_nullable='YES' AND column_default IS NULL THEN 1
 WHEN column_name='sync_parent_levels' AND column_type='smallint unsigned' AND is_nullable='NO' AND column_default='1' THEN 1
 WHEN column_name='created_at' AND column_type='datetime(6)' AND is_nullable='NO' AND UPPER(column_default)='CURRENT_TIMESTAMP(6)' THEN 1
 WHEN column_name='activated_at' AND column_type='datetime(6)' AND is_nullable='YES' AND column_default IS NULL THEN 1
 ELSE 0 END), 0), COALESCE(SUM(column_key='PRI'), 0),
COALESCE(SUM(column_name IN ('filesystem_identity','sync_parent_levels')),0)
FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'yzcm_usage_receipt_bindings'`).Scan(&columns, &matching, &primaryColumns, &identityColumns)
	if err != nil {
		return 0, err
	}
	if engine != "InnoDB" || (columns != 7 && columns != 9) || matching != columns || primaryColumns != 1 || identityColumns != columns-7 {
		return 0, fmt.Errorf("unexpected usage receipt binding definition: engine=%s columns=%d matching=%d primary=%d", engine, columns, matching, primaryColumns)
	}
	var indexes, primary int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(index_name='PRIMARY' AND column_name='binding_slot' AND seq_in_index=1 AND non_unique=0),0)
FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='yzcm_usage_receipt_bindings'`).Scan(&indexes, &primary); err != nil {
		return 0, err
	}
	if indexes != 1 || primary != 1 {
		return 0, fmt.Errorf("unexpected usage receipt binding definition: indexes=%d primary=%d", indexes, primary)
	}
	return columns, nil
}

// MySQL commits the atomic ALTER separately from the backfill and ledger row.
// Resume the backfill in the ledger transaction if that ALTER already committed.
func resumeEnergyTeamBackfill(ctx context.Context, tx *sql.Tx) (bool, error) {
	var columns, indexes int
	err := tx.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM information_schema.columns
  WHERE table_schema = DATABASE() AND table_name = 'yzcm_usage_requests'
  AND column_name IN ('team_id', 'actor_username')),
 (SELECT COUNT(DISTINCT index_name) FROM information_schema.statistics
  WHERE table_schema = DATABASE() AND table_name = 'yzcm_usage_requests'
  AND index_name IN ('yzcm_usage_team_time', 'yzcm_usage_actor_time'))`).Scan(&columns, &indexes)
	if err != nil || columns == 0 && indexes == 0 {
		return false, err
	}
	if columns != 2 || indexes != 2 {
		return false, fmt.Errorf("incomplete usage attribution schema: %d columns, %d indexes", columns, indexes)
	}
	_, err = tx.ExecContext(ctx, backfillEnergyTeam)
	return err == nil, err
}
