package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// Ended and archived relationships remain unknown. A receipt predating the
// current membership cannot safely be attributed to a team the owner joined later.
const backfillEnergyTeam = `UPDATE yzcm_usage_requests r
JOIN account_center_memberships m ON m.active_username = r.owner AND m.joined_at <= r.created_at
JOIN account_center_teams t ON t.id = m.team_id AND t.archived_at IS NULL
SET r.team_id = m.team_id
WHERE r.team_id IS NULL`

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
