package yzcmusage

import (
	"context"
	"database/sql"
	"errors"

	"github.com/go-sql-driver/mysql"
)

// Replays can only complete the exact charged attempt. A prior unbound usage
// row with the same provider ID is evidence of a conflict, never a match.
func (s Store) recordSessionReceipt(ctx context.Context, r receipt) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var subject, team, actor, hash, state string
	var linked sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT subject,team_id,actor,credential_hash,state,provider_id
 FROM screenplay_session_usage_attempts WHERE attempt_id=? FOR UPDATE`, r.AttemptID).Scan(&subject, &team, &actor, &hash, &state, &linked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrReceiptAttributionConflict
	}
	if err != nil {
		return err
	}
	if subject != r.Request.Owner || team != r.Request.TeamID || actor != r.Request.ActorUsername || hash != r.CredentialHash {
		return ErrReceiptAttributionConflict
	}
	if linked.Valid {
		if state != "captured" || linked.String != r.Request.ID {
			return ErrReceiptAttributionConflict
		}
		var owner, t, a, h string
		err = tx.QueryRowContext(ctx, `SELECT owner,COALESCE(team_id,''),COALESCE(actor_username,''),credential_hash FROM yzcm_usage_requests WHERE request_id=?`, linked.String).Scan(&owner, &t, &a, &h)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrReceiptAttributionConflict
		}
		if err != nil {
			return err
		}
		if owner != subject || t != team || a != actor || h != hash {
			return ErrReceiptAttributionConflict
		}
		return tx.Commit()
	}
	if state != "unknown" {
		return ErrReceiptAttributionConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO yzcm_usage_requests (request_id,owner,team_id,actor_username,feature,credential_hash,created_at,http_status) VALUES(?,?,NULLIF(?,''),NULLIF(?,''),?,?,?,?)`, r.Request.ID, subject, team, actor, r.Request.Feature, hash, r.At.UTC(), r.Status)
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE screenplay_session_usage_attempts SET state='captured',provider_id=? WHERE attempt_id=?`, r.Request.ID, r.AttemptID)
	}
	var duplicate *mysql.MySQLError
	if errors.As(err, &duplicate) && duplicate.Number == 1062 {
		return ErrReceiptAttributionConflict
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
