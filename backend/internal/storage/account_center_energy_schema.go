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
		CallbackChecksum: "v88-yzcm-usage-receipt-binding-v1",
		SQL:              []string{yzcmusage.ReceiptBindingStatement()},
	}}
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
