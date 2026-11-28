package accountcenter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
)

// SessionAuthorityReader is used only by the paid screenplay-session channel.
// Current booleans alone cannot distinguish a relationship revoked and restored
// while a provider was running.
type SessionAuthorityReader interface {
	SessionAuthorityVersion(context.Context, string, string, string) (string, error)
}

// SessionAuthorityVersion reads one consistent statement snapshot. Membership
// and grant IDs cannot be reused by the normal end/rejoin and revoke/regrant
// paths. Team archive/restore retains membership IDs, so its append-only admin
// audit watermark is included as well. No GROUP_CONCAT truncation is possible.
func (s *MySQLStore) SessionAuthorityVersion(ctx context.Context, actor, subject, team string) (string, error) {
	if s == nil || s.db == nil || actor == "" || subject == "" {
		return "", ErrForbidden
	}
	rows, err := s.db.QueryContext(ctx, `SELECT CONCAT('membership:',id) AS token FROM account_center_memberships
WHERE username IN (?,?) AND ended_at IS NULL
UNION ALL SELECT CONCAT('grant:',id) AS token FROM account_center_grants
WHERE username IN (?,?) AND revoked_at IS NULL
UNION ALL SELECT CONCAT('audit:',COALESCE(MAX(id),0)) AS token FROM account_center_audit_events
WHERE (subject_username IN (?,?) AND action IN ('disable','enable','archive','restore','set_member','set_manager','set_developer'))
 OR (team_id=? AND action IN ('archive_team','restore_team'))
ORDER BY BINARY token`, actor, subject, actor, subject, actor, subject, team)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	digest := sha256.New()
	for rows.Next() {
		var token string
		if err := rows.Scan(&token); err != nil {
			return "", err
		}
		fmt.Fprintf(digest, "%d:", len(token))
		io.WriteString(digest, token)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}
