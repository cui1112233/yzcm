package accountcenter

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
)

// SessionCommitAuthority is frozen by the server adapter before dispatch.
// Hashes identify the original login/impersonation; no replacement is accepted.
type SessionCommitAuthority struct {
	Identity                RequestIdentity
	LoginTokenHash, Version string
	ImpersonationTokenHash  []byte
}

// GuardSessionCommit runs inside the screenplay save transaction, after its
// project/session locks. Every mutable authorization row remains locked until
// that same transaction commits or rolls back. No model or autocommit reads run
// here. The governance lock also orders team/role audit changes with this save.
func GuardSessionCommit(ctx context.Context, tx *sql.Tx, frozen SessionCommitAuthority) error {
	if tx == nil || frozen.LoginTokenHash == "" || frozen.Version == "" || frozen.Identity.Actor.Username == "" || frozen.Identity.Subject.Username == "" {
		return ErrForbidden
	}
	actorName, subjectName := frozen.Identity.Actor.Username, frozen.Identity.Subject.Username
	var marker string
	if err := tx.QueryRowContext(ctx, `SELECT import_key FROM account_center_imports WHERE import_key=? AND completed_at IS NOT NULL FOR UPDATE`, legacyImportKey).Scan(&marker); err != nil {
		return err
	}
	people := map[string]Identity{}
	rows, err := tx.QueryContext(ctx, `SELECT a.username,a.active,p.role,p.archived_at FROM accounts a JOIN account_center_profiles p ON p.username=a.username WHERE a.username IN (?,?) ORDER BY a.username FOR UPDATE`, actorName, subjectName)
	if err != nil {
		return err
	}
	for rows.Next() {
		var person Identity
		if err := rows.Scan(&person.Username, &person.Active, &person.Role, &person.ArchivedAt); err != nil {
			rows.Close()
			return err
		}
		people[person.Username] = person
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	actor, subject := people[actorName], people[subjectName]
	if !actor.Active || actor.ArchivedAt != nil || !subject.Active || subject.ArchivedAt != nil || actor.Username != actorName || subject.Username != subjectName || actor.Role != frozen.Identity.Actor.Role || subject.Role != frozen.Identity.Subject.Role {
		return ErrForbidden
	}
	var loginOwner string
	if err := tx.QueryRowContext(ctx, `SELECT username FROM auth_sessions WHERE token_hash=? AND expires_at>CURRENT_TIMESTAMP(6) FOR UPDATE`, frozen.LoginTokenHash).Scan(&loginOwner); err != nil {
		return err
	}
	if loginOwner != actorName {
		return ErrForbidden
	}
	tokens := []string{}
	memberTeams := map[string]string{}
	rows, err = tx.QueryContext(ctx, `SELECT id,username,team_id FROM account_center_memberships WHERE username IN (?,?) AND ended_at IS NULL ORDER BY id FOR UPDATE`, actorName, subjectName)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, user, team string
		if err := rows.Scan(&id, &user, &team); err != nil {
			rows.Close()
			return err
		}
		tokens = append(tokens, "membership:"+id)
		memberTeams[user] = team
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	type authorityGrant struct {
		user, capability, scope, grantor string
		team                             sql.NullString
	}
	grants := []authorityGrant{}
	rows, err = tx.QueryContext(ctx, `SELECT id,username,capability,scope,team_id,granted_by FROM account_center_grants WHERE username IN (?,?) AND revoked_at IS NULL ORDER BY id FOR UPDATE`, actorName, subjectName)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		var grant authorityGrant
		if err := rows.Scan(&id, &grant.user, &grant.capability, &grant.scope, &grant.team, &grant.grantor); err != nil {
			rows.Close()
			return err
		}
		tokens = append(tokens, "grant:"+id)
		grants = append(grants, grant)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	// There is at most one active membership per username. Lock the corresponding
	// team rows too; archive/restore must not cross the validated commit.
	teamIDs := []string{}
	seen := map[string]bool{}
	for _, team := range memberTeams {
		if !seen[team] {
			teamIDs = append(teamIDs, team)
			seen[team] = true
		}
	}
	sort.Strings(teamIDs)
	managers := map[string]string{}
	activeTeams := map[string]bool{}
	for _, id := range teamIDs {
		var manager string
		var archived bool
		if err := tx.QueryRowContext(ctx, `SELECT manager_username,archived_at IS NOT NULL FROM account_center_teams WHERE id=? FOR UPDATE`, id).Scan(&manager, &archived); err != nil {
			return err
		}
		managers[id] = manager
		activeTeams[id] = !archived
	}
	if activeTeams[memberTeams[actorName]] {
		actor.TeamID = memberTeams[actorName]
	}
	if activeTeams[memberTeams[subjectName]] {
		subject.TeamID = memberTeams[subjectName]
	}
	if actor.TeamID != frozen.Identity.Actor.TeamID || subject.TeamID != frozen.Identity.Subject.TeamID {
		return ErrForbidden
	}
	allowed := subject.Role == RoleFounder || subject.TeamID != ""
	if !allowed && subject.Role == RoleDeveloper {
		for _, grant := range grants {
			if grant.user == subjectName && grant.capability == GrantCapabilityPlatformBusiness && grant.scope == GrantScopePlatform && !grant.team.Valid {
				var role string
				if err := tx.QueryRowContext(ctx, `SELECT role FROM account_center_profiles WHERE username=? FOR UPDATE`, grant.grantor).Scan(&role); err != nil {
					return err
				}
				if role == RoleFounder {
					allowed = true
				}
			}
		}
	}
	if !allowed {
		return ErrForbidden
	}
	if frozen.Identity.ImpersonationID != "" {
		var actualActor, actualSubject string
		var valid bool
		if len(frozen.ImpersonationTokenHash) != 32 {
			return ErrForbidden
		}
		if err := tx.QueryRowContext(ctx, `SELECT actor_username,subject_username,revoked_at IS NULL AND expires_at>CURRENT_TIMESTAMP(6) FROM account_center_impersonations WHERE id=? AND token_hash=? FOR UPDATE`, frozen.Identity.ImpersonationID, frozen.ImpersonationTokenHash).Scan(&actualActor, &actualSubject, &valid); err != nil {
			return err
		}
		if !valid || actualActor != actorName || actualSubject != subjectName || !(Policy{}).Can(actor, ActionImpersonate, PolicyTarget{Identity: subject, InManagedTeam: subject.TeamID != "" && managers[subject.TeamID] == actorName}) {
			return ErrForbidden
		}
	} else if actorName != subjectName {
		return ErrForbidden
	}
	// This locking read is a current read, even if a caller established an older
	// repeatable-read snapshot before waiting for the project lock.
	var watermark uint64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id),0) FROM account_center_audit_events WHERE (subject_username IN (?,?) AND action IN ('disable','enable','archive','restore','set_member','set_manager','set_developer')) OR (team_id=? AND action IN ('archive_team','restore_team')) FOR UPDATE`, actorName, subjectName, frozen.Identity.Subject.TeamID).Scan(&watermark); err != nil {
		return err
	}
	tokens = append(tokens, fmt.Sprintf("audit:%d", watermark))
	sort.Strings(tokens)
	digest := sha256.New()
	for _, token := range tokens {
		fmt.Fprintf(digest, "%d:", len(token))
		io.WriteString(digest, token)
	}
	if hex.EncodeToString(digest.Sum(nil)) != frozen.Version {
		return ErrForbidden
	}
	return nil
}
