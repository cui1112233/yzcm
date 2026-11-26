package storage

import (
	"context"
	"database/sql"
	"fmt"

	"qiantie/backend/internal/yzcmusage"
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
		CallbackChecksum: "v88-yzcm-usage-receipt-binding-v2",
		SQL:              []string{yzcmusage.ReceiptBindingStatement()},
		Adopt:            adoptEnergyReceiptBindingTable,
		Reconcile:        verifyEnergyReceiptBindingTable,
	}}
}

func verifyEnergyReceiptBindingTable(ctx context.Context, tx *sql.Tx) error {
	found, err := adoptEnergyReceiptBindingTable(ctx, tx)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("usage receipt binding table missing")
	}
	return nil
}

func adoptEnergyReceiptBindingTable(ctx context.Context, tx *sql.Tx) (bool, error) {
	var engine string
	err := tx.QueryRowContext(ctx, `SELECT ENGINE FROM information_schema.tables
WHERE table_schema = DATABASE() AND table_name = 'yzcm_usage_receipt_bindings'`).Scan(&engine)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var columns, matching, primaryColumns int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE
 WHEN column_name='binding_slot' AND column_type='varchar(64)' AND collation_name='ascii_bin' AND is_nullable='NO' AND column_key='PRI' THEN 1
 WHEN column_name='path_digest' AND column_type='char(64)' AND collation_name='ascii_bin' AND is_nullable='NO' THEN 1
 WHEN column_name='store_id' AND column_type='char(32)' AND collation_name='ascii_bin' AND is_nullable='NO' THEN 1
 WHEN column_name='state' AND column_type='enum(''pending'',''active'')' AND is_nullable='NO' THEN 1
 WHEN column_name='origin' AND column_type='enum(''new'',''upgrade'',''adopt'',''db-only'')' AND is_nullable='NO' THEN 1
 WHEN column_name='filesystem_identity' AND column_type='varchar(224)' AND collation_name='ascii_bin' AND is_nullable='YES' THEN 1
 WHEN column_name='sync_parent_levels' AND column_type='smallint unsigned' AND is_nullable='NO' AND column_default='1' THEN 1
 WHEN column_name='created_at' AND column_type='datetime(6)' AND is_nullable='NO' AND UPPER(column_default)='CURRENT_TIMESTAMP(6)' THEN 1
 WHEN column_name='activated_at' AND column_type='datetime(6)' AND is_nullable='YES' THEN 1
 ELSE 0 END), 0), COALESCE(SUM(column_key='PRI'), 0)
FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'yzcm_usage_receipt_bindings'`).Scan(&columns, &matching, &primaryColumns)
	if err != nil {
		return false, err
	}
	if engine != "InnoDB" || columns != 9 || matching != 9 || primaryColumns != 1 {
		return false, fmt.Errorf("unexpected usage receipt binding definition: engine=%s columns=%d matching=%d primary=%d", engine, columns, matching, primaryColumns)
	}
	return true, nil
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
