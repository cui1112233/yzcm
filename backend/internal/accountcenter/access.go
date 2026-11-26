package accountcenter

import "context"

type BusinessAccessReader interface {
	BusinessAllowed(context.Context, string) (bool, error)
}

type PlatformGrantReader interface {
	PlatformGrants(context.Context, string) ([]string, error)
}

// BusinessAllowed checks the current unended relationship. Archived or ended
// relationships remain available to directory and history reads.
func (s *MySQLStore) BusinessAllowed(ctx context.Context, username string) (bool, error) {
	const query = `SELECT EXISTS(
 SELECT 1 FROM accounts a LEFT JOIN account_center_profiles p ON p.username=a.username
 WHERE a.username=? AND a.active=TRUE AND p.archived_at IS NULL AND (
  p.role='founder' OR
  (p.role='developer' AND EXISTS (
   SELECT 1 FROM account_center_grants g JOIN account_center_profiles founder ON founder.username=g.granted_by AND founder.role='founder'
   WHERE g.username=a.username AND g.scope=? AND g.capability=? AND g.team_id IS NULL AND g.revoked_at IS NULL
  )) OR EXISTS (
   SELECT 1 FROM account_center_memberships m JOIN account_center_teams t ON t.id=m.team_id
   WHERE m.username=a.username AND m.ended_at IS NULL AND t.archived_at IS NULL
  )
 )
)`
	var allowed bool
	err := s.db.QueryRowContext(ctx, query, username, GrantScopePlatform, GrantCapabilityPlatformBusiness).Scan(&allowed)
	return allowed, err
}

func (s *MySQLStore) PlatformGrants(ctx context.Context, username string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT g.capability FROM account_center_grants g
JOIN account_center_profiles founder ON founder.username=g.granted_by AND founder.role='founder'
WHERE g.username=? AND g.scope=? AND g.team_id IS NULL AND g.revoked_at IS NULL ORDER BY g.capability`, username, GrantScopePlatform)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var grants []string
	for rows.Next() {
		var capability string
		if err := rows.Scan(&capability); err != nil {
			return nil, err
		}
		grants = append(grants, capability)
	}
	return grants, rows.Err()
}
