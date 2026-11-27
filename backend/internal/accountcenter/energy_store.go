package accountcenter

import (
	"context"
	"database/sql"
)

const energyHasCost = `r.reconciled_at IS NOT NULL AND r.actual_cost IS NOT NULL`

const energyAggregate = `COALESCE(SUM(CASE WHEN ` + energyHasCost + ` THEN r.actual_cost ELSE 0 END),0) AS actual_cost,
COUNT(r.request_id) AS request_count,COUNT(CASE WHEN ` + energyHasCost + ` THEN 1 END) AS reconciled_count,
COUNT(r.request_id)-COUNT(CASE WHEN ` + energyHasCost + ` THEN 1 END) AS pending_count`

var _ energyReadStore = (*MySQLStore)(nil)

func (s *MySQLStore) energyTeam(ctx context.Context, id string) (EnergyTeam, error) {
	var team EnergyTeam
	err := s.db.QueryRowContext(ctx, `SELECT id,name,archived_at FROM account_center_teams WHERE id=?`, id).
		Scan(&team.ID, &team.Name, &team.ArchivedAt)
	return team, recordError(err)
}

func (s *MySQLStore) energyHasMember(ctx context.Context, filter EnergyFilter, currentOnly bool) (bool, error) {
	var found bool
	if currentOnly {
		err := s.db.QueryRowContext(ctx, `SELECT EXISTS(
SELECT 1 FROM account_center_memberships m JOIN accounts a ON a.username=m.active_username
LEFT JOIN account_center_profiles p ON p.username=a.username
WHERE m.team_id=? AND m.active_username=? AND a.active=TRUE AND p.archived_at IS NULL)`, filter.TeamID, filter.Member).Scan(&found)
		return found, err
	}
	if filter.TeamID == "" {
		window, args := energyUsageWindow(filter)
		err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_center_memberships WHERE username=?)
 OR EXISTS(SELECT 1 FROM yzcm_usage_requests r WHERE r.team_id IS NOT NULL AND `+window+`)`, append([]any{filter.Member}, args...)...).Scan(&found)
		return found, err
	}
	where, args := energyTeamWhere(filter)
	// Membership history allows a manager to select a transferred member of
	// this team. Immutable usage attribution also covers retired relationships.
	err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_center_memberships WHERE username=? AND team_id=?)
 OR EXISTS(SELECT 1 FROM yzcm_usage_requests r WHERE `+where+`)`, append([]any{filter.Member, filter.TeamID}, args...)...).Scan(&found)
	return found, err
}

func energyUsageWindow(filter EnergyFilter) (string, []any) {
	query := `r.created_at >= ? AND r.created_at < ?`
	args := []any{filter.From, filter.To}
	if filter.Member != "" {
		query += ` AND r.owner=?`
		args = append(args, filter.Member)
	}
	return query, args
}

func energyTeamWhere(filter EnergyFilter) (string, []any) {
	window, args := energyUsageWindow(filter)
	return `r.team_id=? AND ` + window, append([]any{filter.TeamID}, args...)
}

func (s *MySQLStore) energyTotals(ctx context.Context, filter EnergyFilter) (EnergyTotals, error) {
	where, args := energyTeamWhere(filter)
	totals := EnergyTotals{Unit: EnergyUnit}
	err := s.db.QueryRowContext(ctx, `SELECT `+energyAggregate+` FROM yzcm_usage_requests r WHERE `+where, args...).
		Scan(&totals.ActualCost, &totals.RequestCount, &totals.ReconciledCount, &totals.PendingCount)
	return totals, err
}

func (s *MySQLStore) energyLedger(ctx context.Context, filter EnergyFilter, page EnergyPage, includeActor bool) ([]EnergyLedgerEntry, error) {
	where, args := energyTeamWhere(filter)
	args = append([]any{includeActor}, args...)
	args = append(args, page.Limit+1, page.Offset)
	rows, err := s.db.QueryContext(ctx, `SELECT r.request_id,r.owner,IF(?,COALESCE(r.actor_username,''),''),r.created_at,
r.feature,r.model,r.http_status,CASE WHEN `+energyHasCost+` THEN r.actual_cost END,
CASE WHEN `+energyHasCost+` THEN r.reconciled_at END
FROM yzcm_usage_requests r WHERE `+where+` ORDER BY r.created_at DESC,r.request_id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]EnergyLedgerEntry, 0, page.Limit)
	for rows.Next() {
		var entry EnergyLedgerEntry
		var amount sql.NullString
		if err := rows.Scan(&entry.RequestID, &entry.Owner, &entry.ActorUsername, &entry.At, &entry.Feature, &entry.Model,
			&entry.HTTPStatus, &amount, &entry.ReconciledAt); err != nil {
			return nil, err
		}
		entry.At = entry.At.UTC()
		entry.Status = "pending"
		if amount.Valid {
			entry.Status = "reconciled"
			entry.ActualCost = &amount.String
			settled := entry.ReconciledAt.UTC()
			entry.ReconciledAt = &settled
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *MySQLStore) energyRanking(ctx context.Context, filter EnergyFilter, page EnergyPage) ([]EnergyRankingEntry, error) {
	where, args := energyTeamWhere(filter)
	// YZCM consumption receipts carry non-negative deductions. Keep real zero
	// deductions, and omit anomalous negative amounts from the consumption rank.
	rows, err := s.db.QueryContext(ctx, `SELECT owner,actual_cost,request_count,reconciled_count,pending_count,
RANK() OVER (ORDER BY actual_cost DESC) AS consumption_rank
FROM (SELECT r.owner,`+energyAggregate+`
FROM yzcm_usage_requests r WHERE `+where+` AND NOT (`+energyHasCost+` AND r.actual_cost < 0)
GROUP BY r.owner HAVING reconciled_count > 0) ranked
ORDER BY actual_cost DESC,owner ASC LIMIT ? OFFSET ?`,
		append(args, page.Limit+1, page.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]EnergyRankingEntry, 0, page.Limit)
	for rows.Next() {
		entry := EnergyRankingEntry{EnergyTotals: EnergyTotals{Unit: EnergyUnit}}
		if err := rows.Scan(&entry.Username, &entry.ActualCost, &entry.RequestCount, &entry.ReconciledCount, &entry.PendingCount, &entry.Rank); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *MySQLStore) energyTeams(ctx context.Context, filter EnergyFilter, page EnergyPage) ([]EnergyTeamSummary, error) {
	teamPage := `SELECT id,name,archived_at FROM account_center_teams`
	var args []any
	if filter.TeamID != "" {
		teamPage += ` WHERE id=?`
		args = append(args, filter.TeamID)
	}
	teamPage += ` ORDER BY id LIMIT ? OFFSET ?`
	args = append(args, page.Limit+1, page.Offset)
	window, usageArgs := energyUsageWindow(filter)
	rows, err := s.db.QueryContext(ctx, `SELECT t.id,t.name,t.archived_at,`+energyAggregate+`
FROM (`+teamPage+`) t LEFT JOIN yzcm_usage_requests r ON r.team_id=t.id AND `+window+`
GROUP BY t.id,t.name,t.archived_at ORDER BY t.id`, append(args, usageArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	teams := make([]EnergyTeamSummary, 0, page.Limit)
	for rows.Next() {
		var team EnergyTeam
		totals := EnergyTotals{Unit: EnergyUnit}
		if err := rows.Scan(&team.ID, &team.Name, &team.ArchivedAt, &totals.ActualCost, &totals.RequestCount,
			&totals.ReconciledCount, &totals.PendingCount); err != nil {
			return nil, err
		}
		teams = append(teams, EnergyTeamSummary{Team: &team, Consumption: &totals})
	}
	return teams, rows.Err()
}
