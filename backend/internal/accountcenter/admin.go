package accountcenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AdminDirectory struct {
	People      []Identity
	Teams       []Team
	Memberships []Membership
	Grants      []Grant
}

// AdminStore runs authorization, the requested change and its audit entry against
// one consistent database snapshot. Writers serialize on the governance row.
type AdminStore interface {
	WithAdmin(context.Context, bool, func(AdminTx) error) error
}
type AdminTx interface {
	Directory(context.Context) (AdminDirectory, error)
	Change(context.Context, AdminChange) error
	AppendAudit(context.Context, AuditEvent) error
	ListAudit(context.Context, AuditFilter) ([]AuditEvent, error)
	InviteClaim(context.Context, string) (string, *time.Time, error)
}
type AdminChange struct {
	Action, TeamID, TeamName, Actor, Username, DisplayName, Role, PasswordHash, PasswordAlgorithm string
	InviteDigest                                                                                  string
	StartDisabled                                                                                 bool
}

type DeleteCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type DeletePreflight struct {
	Allowed bool          `json:"allowed"`
	Checks  []DeleteCheck `json:"checks"`
}

func PermanentDeletePreflight(person Identity) DeletePreflight {
	archive := "clear"
	if person.ArchivedAt == nil || person.Active {
		archive = "blocked"
	}
	return DeletePreflight{Checks: []DeleteCheck{
		{Name: "archive", Status: archive},
		{Name: "balance", Status: "unavailable"},
		{Name: "in_flight_jobs", Status: "unavailable"},
		{Name: "pending_reconciliation", Status: "unavailable"},
		{Name: "retention", Status: "unavailable"},
	}}
}

type adminTx struct{ tx *sql.Tx }

func (s *MySQLStore) WithAdmin(ctx context.Context, write bool, fn func(AdminTx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: !write})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if write {
		var key string
		if err = tx.QueryRowContext(ctx, `SELECT import_key FROM account_center_imports WHERE import_key=? AND completed_at IS NOT NULL FOR UPDATE`, legacyImportKey).Scan(&key); err != nil {
			return err
		}
	}
	if err = fn(adminTx{tx}); err != nil {
		return err
	}
	return tx.Commit()
}

func (t adminTx) Directory(ctx context.Context) (d AdminDirectory, err error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT a.username,COALESCE(p.display_name,a.username),COALESCE(p.role,'member'),a.active,p.archived_at FROM accounts a LEFT JOIN account_center_profiles p ON p.username=a.username ORDER BY a.username`)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var p Identity
		if err = rows.Scan(&p.Username, &p.DisplayName, &p.Role, &p.Active, &p.ArchivedAt); err != nil {
			rows.Close()
			return d, err
		}
		d.People = append(d.People, p)
	}
	if err = rows.Err(); err != nil {
		return d, err
	}
	rows, err = t.tx.QueryContext(ctx, `SELECT id,name,manager_username,created_by,archived_at FROM account_center_teams ORDER BY name,id`)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var team Team
		if err = rows.Scan(&team.ID, &team.Name, &team.ManagerUsername, &team.CreatedBy, &team.ArchivedAt); err != nil {
			rows.Close()
			return d, err
		}
		d.Teams = append(d.Teams, team)
	}
	if err = rows.Err(); err != nil {
		return d, err
	}
	// Legacy import ended memberships at the exact moment it archived their team.
	// Include those relationships for inspection and controlled team restore.
	rows, err = t.tx.QueryContext(ctx, `SELECT m.team_id,m.username FROM account_center_memberships m JOIN account_center_teams t ON t.id=m.team_id WHERE m.ended_at IS NULL OR m.ended_at=t.archived_at ORDER BY m.username`)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var m Membership
		if err = rows.Scan(&m.TeamID, &m.Username); err != nil {
			rows.Close()
			return d, err
		}
		d.Memberships = append(d.Memberships, m)
	}
	if err = rows.Err(); err != nil {
		return d, err
	}
	rows, err = t.tx.QueryContext(ctx, `SELECT username, team_id FROM account_center_grants WHERE scope=? AND capability=? AND team_id IS NOT NULL AND revoked_at IS NULL`, GrantScopeTeam, GrantCapabilityTeamManage)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var g Grant
		if err = rows.Scan(&g.Username, &g.TeamID); err != nil {
			rows.Close()
			return d, err
		}
		d.Grants = append(d.Grants, g)
	}
	return d, rows.Err()
}

func (t adminTx) AppendAudit(ctx context.Context, e AuditEvent) error {
	_, err := t.tx.ExecContext(ctx, insertAuditSQL, e.ActorUsername, e.SubjectUsername, e.TeamID, e.ImpersonationID, e.Action, optionalTime(e.CreatedAt))
	return err
}
func (t adminTx) ListAudit(ctx context.Context, f AuditFilter) ([]AuditEvent, error) {
	return listAudit(ctx, t.tx, f)
}

func (t adminTx) InviteClaim(ctx context.Context, digest string) (string, *time.Time, error) {
	var username string
	var at *time.Time
	err := t.tx.QueryRowContext(ctx, `SELECT username, used_at FROM account_center_invite_claims WHERE digest=?`, digest).Scan(&username, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, nil
	}
	return username, at, err
}

func (t adminTx) createAccount(ctx context.Context, c AdminChange) error {
	var reserved bool
	const historicalName = `SELECT EXISTS(
  SELECT 1 FROM account_center_reserved_names WHERE username=?
  UNION ALL SELECT 1 FROM account_center_profiles WHERE username=?
  UNION ALL SELECT 1 FROM account_center_memberships WHERE username=?
  UNION ALL SELECT 1 FROM account_center_teams WHERE manager_username=? OR created_by=?
  UNION ALL SELECT 1 FROM account_center_grants WHERE username=? OR granted_by=?
  UNION ALL SELECT 1 FROM account_center_audit_events WHERE actor_username=? OR subject_username=?
  UNION ALL SELECT 1 FROM account_center_impersonations WHERE actor_username=? OR subject_username=?
  UNION ALL SELECT 1 FROM account_center_archive_states WHERE username=?
  UNION ALL SELECT 1 FROM account_center_invite_claims WHERE username=?
)`
	args := make([]any, strings.Count(historicalName, "?"))
	for i := range args {
		args[i] = c.Username
	}
	if err := t.tx.QueryRowContext(ctx, historicalName, args...).Scan(&reserved); err != nil {
		return err
	}
	if reserved {
		return ErrConflict
	}
	if _, err := t.tx.ExecContext(ctx, `INSERT INTO accounts(username,password_hash,password_algorithm,is_owner,active,created_at,updated_at) VALUES(?,?,?,FALSE,?,CURRENT_TIMESTAMP(6),CURRENT_TIMESTAMP(6))`, c.Username, c.PasswordHash, c.PasswordAlgorithm, !c.StartDisabled); err != nil {
		return recordError(err)
	}
	_, err := t.tx.ExecContext(ctx, `INSERT INTO account_center_profiles(username,display_name,role) VALUES(?,?,?)`, c.Username, c.DisplayName, c.Role)
	return recordError(err)
}

func (t adminTx) Change(ctx context.Context, c AdminChange) error {
	switch c.Action {
	case "create_team":
		if c.PasswordHash != "" {
			if err := t.createAccount(ctx, c); err != nil {
				return err
			}
		}
		if err := insertTeam(ctx, t.tx, Team{ID: c.TeamID, Name: c.TeamName, ManagerUsername: c.Username, CreatedBy: c.Actor}); err != nil {
			return err
		}
		return insertTeamMembership(ctx, t.tx, c.TeamID, c.Username)
	case "create_account", "accept_invite":
		if c.Action == "accept_invite" {
			if _, err := t.tx.ExecContext(ctx, `INSERT INTO account_center_invite_claims(digest,username) VALUES(?,?)`, c.InviteDigest, c.Username); err != nil {
				return recordError(err)
			}
		}
		if err := t.createAccount(ctx, c); err != nil {
			return err
		}
		if c.TeamID == "" {
			return nil
		}
		_, err := t.tx.ExecContext(ctx, `INSERT INTO account_center_memberships(team_id,username) VALUES(?,?)`, c.TeamID, c.Username)
		return recordError(err)
	case "archive_team", "restore_team":
		if c.Action == "archive_team" {
			_, err := t.tx.ExecContext(ctx, `UPDATE account_center_teams SET archived_at=CURRENT_TIMESTAMP(6) WHERE id=?`, c.TeamID)
			return err
		}
		if _, err := t.tx.ExecContext(ctx, `UPDATE account_center_memberships m JOIN account_center_teams t ON t.id=m.team_id SET m.ended_at=NULL WHERE t.id=? AND m.ended_at=t.archived_at`, c.TeamID); err != nil {
			return recordError(err)
		}
		_, err := t.tx.ExecContext(ctx, `UPDATE account_center_teams SET archived_at=NULL WHERE id=?`, c.TeamID)
		return err
	}
	// Accounts created outside the old importer may not have a profile yet.
	if _, err := t.tx.ExecContext(ctx, `INSERT INTO account_center_profiles(username,display_name,role) SELECT username,username,'member' FROM accounts WHERE username=? ON DUPLICATE KEY UPDATE username=account_center_profiles.username`, c.Username); err != nil {
		return err
	}
	var err error
	switch c.Action {
	case "set_member", "set_manager", "set_developer":
		_, err = t.tx.ExecContext(ctx, `UPDATE account_center_profiles SET role=? WHERE username=?`, c.Role, c.Username)
	case "enable", "disable":
		_, err = t.tx.ExecContext(ctx, `UPDATE accounts SET active=? WHERE username=?`, c.Action == "enable", c.Username)
	case "archive":
		_, err = t.tx.ExecContext(ctx, `INSERT INTO account_center_archive_states(username,active_before_archive)
SELECT username,active FROM accounts WHERE username=?
ON DUPLICATE KEY UPDATE active_before_archive=accounts.active`, c.Username)
		if err == nil {
			_, err = t.tx.ExecContext(ctx, `UPDATE account_center_profiles SET archived_at=CURRENT_TIMESTAMP(6) WHERE username=?`, c.Username)
		}
		if err == nil {
			_, err = t.tx.ExecContext(ctx, `UPDATE accounts SET active=FALSE WHERE username=?`, c.Username)
		}
	case "restore":
		_, err = t.tx.ExecContext(ctx, `UPDATE account_center_profiles p JOIN accounts a ON a.username=p.username
LEFT JOIN account_center_archive_states state ON state.username=p.username
SET p.archived_at=NULL,a.active=COALESCE(state.active_before_archive,TRUE) WHERE p.username=?`, c.Username)
	default:
		return ErrInvalid
	}
	if err != nil {
		return err
	}
	if c.Action == "disable" || c.Action == "archive" {
		if _, err = t.tx.ExecContext(ctx, `DELETE FROM auth_sessions WHERE username=?`, c.Username); err != nil {
			return err
		}
	}
	_, err = t.tx.ExecContext(ctx, `UPDATE account_center_impersonations SET revoked_at=CURRENT_TIMESTAMP(6) WHERE (actor_username=? OR subject_username=?) AND revoked_at IS NULL`, c.Username, c.Username)
	return err
}

// Reserve names found in legacy identity and credential history, including
// accounts purged before MySQL governance was introduced.
func (s *MySQLStore) ReserveLegacyNames(ctx context.Context, dir string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const marker = "account-center-reserve-legacy-names-v1"
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_center_imports(import_key) VALUES(?) ON DUPLICATE KEY UPDATE import_key=import_key`, marker); err != nil {
		return err
	}
	var done bool
	if err = tx.QueryRowContext(ctx, `SELECT completed_at IS NOT NULL FROM account_center_imports WHERE import_key=? FOR UPDATE`, marker).Scan(&done); err != nil || done {
		return err
	}
	names := map[string]bool{}
	if err := reserveNamesFromDir(dir, names); err != nil {
		return err
	}
	fallback := filepath.Join(dir, "..", "..", "..", "data", "system")
	if err := reserveNamesFromDir(fallback, names); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for name := range names {
		if _, err = tx.ExecContext(ctx, `INSERT INTO account_center_reserved_names(username) VALUES (?) ON DUPLICATE KEY UPDATE username=username`, name); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE account_center_imports SET completed_at=? WHERE import_key=?`, time.Now().UTC(), marker); err != nil {
		return err
	}
	return tx.Commit()
}

func reserveNamesFromDir(dir string, names map[string]bool) error {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, file := range []string{"member-profiles.json", "member-archives.json", "member-profile-details.json", "account-recovery.json", "passkeys.json", "mfa.json", "team-collaboration.json", "grants.json"} {
		b, e := root.ReadFile(file)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		var v any
		if e = json.Unmarshal(b, &v); e != nil {
			return e
		}
		collectHistoricalNames(v, names)
	}
	return nil
}

func collectHistoricalNames(v any, names map[string]bool) {
	switch x := v.(type) {
	case []any:
		for _, child := range x {
			collectHistoricalNames(child, names)
		}
	case map[string]any:
		for key, child := range x {
			switch strings.ToLower(key) {
			case "username", "managerusername", "boundto", "actor", "actorusername", "subject", "subjectusername", "usedby", "createdby", "grantedby", "deletedby":
				if name, ok := child.(string); ok && name != "" && len(name) <= 32 {
					names[name] = true
				}
			}
			collectHistoricalNames(child, names)
		}
	}
}
