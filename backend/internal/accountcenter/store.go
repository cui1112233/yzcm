package accountcenter

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

const (
	RoleFounder   = "founder"
	RoleDeveloper = "developer"
	RoleManager   = "manager"
	RoleMember    = "member"
)

var (
	ErrNotFound  = errors.New("account center record not found")
	ErrConflict  = errors.New("account center record already exists")
	ErrInvalid   = errors.New("invalid account center input")
	ErrForbidden = errors.New("account center operation forbidden")
)

type Identity struct {
	Username, DisplayName, Role string
	Active                      bool
	AvatarURL                   string
	ArchivedAt                  *time.Time
}

type RequestIdentity struct {
	Actor, Subject  Identity
	ImpersonationID string
}

type Team struct {
	ID, Name, ManagerUsername, CreatedBy string
	CreatedAt                            time.Time
	ArchivedAt                           *time.Time
}

type TeamFilter struct {
	Username, ManagerUsername string
	IncludeArchived           bool
}

type TeamSummary struct {
	Team
	MemberCount int
}

type Membership struct {
	ID               uint64
	TeamID, Username string
	JoinedAt         time.Time
}

type Grant struct {
	ID, Username, Capability, Scope, TeamID, GrantedBy string
	GrantedAt                                          time.Time
}

type AuditEvent struct {
	ID                                     uint64
	ActorUsername, SubjectUsername, TeamID string
	ImpersonationID, Action                string
	CreatedAt                              time.Time
}

type AuditFilter struct {
	TeamID       string
	From, Before time.Time
	Limit        int
}

type Impersonation struct {
	ID, ActorUsername, SubjectUsername string
	TokenHash                          [32]byte `json:"-"`
	CreatedAt, ExpiresAt               time.Time
}

// Store persists account center data. Callers authorize team and account
// operations before writing and keep login credentials in the auth store.
type Store interface {
	ResolveIdentity(context.Context, string) (Identity, error)
	CreateIdentity(context.Context, Identity) error
	SetRole(ctx context.Context, actorUsername, username, role string) error
	SetArchived(ctx context.Context, username string, archivedAt *time.Time) error
	CreateTeam(context.Context, Team) error
	ListTeams(context.Context, TeamFilter) ([]TeamSummary, error)
	AddMembership(context.Context, Membership) error
	EndMembership(ctx context.Context, teamID, username string, endedAt time.Time) error
	ListMemberships(ctx context.Context, teamID string) ([]Membership, error)
	CreateGrant(context.Context, Grant) error
	ListGrants(ctx context.Context, username string) ([]Grant, error)
	RevokeGrant(ctx context.Context, id string, revokedAt time.Time) error
	AppendAudit(context.Context, AuditEvent) error
	ListAudit(context.Context, AuditFilter) ([]AuditEvent, error)
	CreateImpersonation(context.Context, Impersonation) error
	FindImpersonation(ctx context.Context, tokenHash [32]byte, actorUsername string, now time.Time) (Impersonation, error)
	RevokeImpersonation(ctx context.Context, id, actorUsername string, revokedAt time.Time) error
}

type MySQLStore struct{ db *sql.DB }

var _ Store = (*MySQLStore)(nil)

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{db: db} }

func (s *MySQLStore) ResolveIdentity(ctx context.Context, username string) (Identity, error) {
	var identity Identity
	err := s.db.QueryRowContext(ctx, `SELECT a.username, COALESCE(p.display_name, a.username), COALESCE(p.role, 'member'),
       a.active AND p.archived_at IS NULL, COALESCE(p.avatar_url, ''), p.archived_at
FROM accounts a LEFT JOIN account_center_profiles p ON p.username = a.username
WHERE a.username = ?`, username).Scan(&identity.Username, &identity.DisplayName, &identity.Role, &identity.Active, &identity.AvatarURL, &identity.ArchivedAt)
	return identity, recordError(err)
}

func (s *MySQLStore) CreateIdentity(ctx context.Context, identity Identity) error {
	if identity.Role != RoleManager && identity.Role != RoleMember {
		return ErrForbidden
	}
	displayName := strings.TrimSpace(identity.DisplayName)
	result, err := s.db.ExecContext(ctx, `INSERT INTO account_center_profiles (username, display_name, avatar_url, role)
SELECT username, COALESCE(NULLIF(?, ''), username), ?, ? FROM accounts WHERE username = ?`, displayName, identity.AvatarURL, identity.Role, identity.Username)
	return recordChanged(result, err)
}

// SetRole checks the durable actor and target roles in the same write. Only
// the founder can grant or revoke developer status.
func (s *MySQLStore) SetRole(ctx context.Context, actorUsername, username, role string) error {
	if role != RoleDeveloper && role != RoleManager && role != RoleMember {
		return ErrForbidden
	}
	result, err := s.db.ExecContext(ctx, `UPDATE account_center_profiles target
JOIN accounts target_account ON target_account.username = target.username
JOIN account_center_profiles actor ON actor.username = ?
JOIN accounts actor_account ON actor_account.username = actor.username
SET target.role = ?
WHERE target.username = ? AND target.role <> 'founder'
  AND actor_account.active = TRUE AND actor.archived_at IS NULL
  AND (actor.role = 'founder' OR (actor.role = 'developer' AND target.role <> 'developer' AND ? <> 'developer'))`, actorUsername, role, username, role)
	if err := recordChanged(result, err); errors.Is(err, ErrNotFound) {
		return ErrForbidden
	} else {
		return err
	}
}

func (s *MySQLStore) SetArchived(ctx context.Context, username string, archivedAt *time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE account_center_profiles SET archived_at = ? WHERE username = ? AND role <> 'founder'`, archivedAt, username)
	return recordChanged(result, err)
}

func (s *MySQLStore) CreateTeam(ctx context.Context, team Team) error {
	if team.ID == "" || team.Name == "" || team.ManagerUsername == "" || team.CreatedBy == "" {
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertTeam(ctx, tx, team); err != nil {
		return err
	}
	return tx.Commit()
}

func insertTeam(ctx context.Context, tx *sql.Tx, team Team) error {
	result, err := tx.ExecContext(ctx, `INSERT INTO account_center_teams (id, name, manager_username, created_by, created_at, archived_at)
SELECT ?, ?, username, ?, COALESCE(?, CURRENT_TIMESTAMP(6)), ? FROM accounts WHERE username = ?`,
		team.ID, team.Name, team.CreatedBy, optionalTime(team.CreatedAt), team.ArchivedAt, team.ManagerUsername)
	if err := recordChanged(result, err); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO account_center_memberships (team_id, username, joined_at)
SELECT id, manager_username, created_at FROM account_center_teams WHERE id = ?`, team.ID)
	return recordError(err)
}

func (s *MySQLStore) ListTeams(ctx context.Context, filter TeamFilter) ([]TeamSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.id, t.name, t.manager_username, t.created_by, t.created_at, t.archived_at,
  (SELECT COUNT(*) FROM account_center_memberships m
   JOIN accounts a ON a.username = m.username
   LEFT JOIN account_center_profiles p ON p.username = m.username
   WHERE m.team_id = t.id AND m.ended_at IS NULL AND p.archived_at IS NULL) AS member_count
FROM account_center_teams t
WHERE (? OR t.archived_at IS NULL) AND (? = '' OR t.manager_username = ?)
  AND (? = '' OR EXISTS (SELECT 1 FROM account_center_memberships m
     JOIN accounts a ON a.username = m.username
     LEFT JOIN account_center_profiles p ON p.username = m.username
     WHERE m.team_id = t.id AND m.active_username = ? AND p.archived_at IS NULL))
ORDER BY t.created_at, t.id`, filter.IncludeArchived, filter.ManagerUsername, filter.ManagerUsername, filter.Username, filter.Username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	teams := []TeamSummary{}
	for rows.Next() {
		var team TeamSummary
		if err := rows.Scan(&team.ID, &team.Name, &team.ManagerUsername, &team.CreatedBy, &team.CreatedAt, &team.ArchivedAt, &team.MemberCount); err != nil {
			return nil, err
		}
		teams = append(teams, team)
	}
	return teams, rows.Err()
}

func (s *MySQLStore) AddMembership(ctx context.Context, membership Membership) error {
	result, err := s.db.ExecContext(ctx, `INSERT INTO account_center_memberships (team_id, username, joined_at)
SELECT t.id, a.username, COALESCE(?, CURRENT_TIMESTAMP(6))
FROM account_center_teams t JOIN accounts a ON a.username = ?
WHERE t.id = ? AND t.archived_at IS NULL`, optionalTime(membership.JoinedAt), membership.Username, membership.TeamID)
	return recordChanged(result, err)
}

func (s *MySQLStore) EndMembership(ctx context.Context, teamID, username string, endedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE account_center_memberships
SET ended_at = COALESCE(?, CURRENT_TIMESTAMP(6)) WHERE team_id = ? AND active_username = ?`, optionalTime(endedAt), teamID, username)
	return recordChanged(result, err)
}

func (s *MySQLStore) ListMemberships(ctx context.Context, teamID string) ([]Membership, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id, m.team_id, m.username, m.joined_at
FROM account_center_memberships m JOIN accounts a ON a.username = m.username
LEFT JOIN account_center_profiles p ON p.username = m.username
WHERE m.team_id = ? AND m.ended_at IS NULL AND p.archived_at IS NULL
ORDER BY m.username`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	memberships := []Membership{}
	for rows.Next() {
		var membership Membership
		if err := rows.Scan(&membership.ID, &membership.TeamID, &membership.Username, &membership.JoinedAt); err != nil {
			return nil, err
		}
		memberships = append(memberships, membership)
	}
	return memberships, rows.Err()
}

func (s *MySQLStore) CreateGrant(ctx context.Context, grant Grant) error {
	if grant.ID == "" || grant.Username == "" || grant.Capability == "" || grant.Scope == "" || grant.GrantedBy == "" {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, insertGrantSQL, grant.ID, grant.Username, grant.Capability, grant.Scope, grant.TeamID, grant.GrantedBy, optionalTime(grant.GrantedAt))
	return recordError(err)
}

const insertGrantSQL = `INSERT INTO account_center_grants (id, username, capability, scope, team_id, granted_by, granted_at)
VALUES (?, ?, ?, ?, NULLIF(?, ''), ?, COALESCE(?, CURRENT_TIMESTAMP(6)))`

func (s *MySQLStore) ListGrants(ctx context.Context, username string) ([]Grant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, username, capability, scope, COALESCE(team_id, ''), granted_by, granted_at
FROM account_center_grants WHERE username = ? AND revoked_at IS NULL ORDER BY granted_at, id`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	grants := []Grant{}
	for rows.Next() {
		var grant Grant
		if err := rows.Scan(&grant.ID, &grant.Username, &grant.Capability, &grant.Scope, &grant.TeamID, &grant.GrantedBy, &grant.GrantedAt); err != nil {
			return nil, err
		}
		grants = append(grants, grant)
	}
	return grants, rows.Err()
}

func (s *MySQLStore) RevokeGrant(ctx context.Context, id string, revokedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE account_center_grants
SET revoked_at = COALESCE(?, CURRENT_TIMESTAMP(6)) WHERE id = ? AND revoked_at IS NULL`, optionalTime(revokedAt), id)
	return recordChanged(result, err)
}

func (s *MySQLStore) AppendAudit(ctx context.Context, event AuditEvent) error {
	if event.ActorUsername == "" || event.SubjectUsername == "" || event.Action == "" {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO account_center_audit_events (actor_username, subject_username, team_id, impersonation_id, action, created_at)
VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, COALESCE(?, CURRENT_TIMESTAMP(6)))`, event.ActorUsername, event.SubjectUsername, event.TeamID, event.ImpersonationID, event.Action, optionalTime(event.CreatedAt))
	return err
}

func (s *MySQLStore) ListAudit(ctx context.Context, filter AuditFilter) ([]AuditEvent, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, actor_username, subject_username, COALESCE(team_id, ''), COALESCE(impersonation_id, ''), action, created_at
FROM account_center_audit_events WHERE (? = '' OR team_id = ?) AND (? IS NULL OR created_at >= ?) AND (? IS NULL OR created_at < ?)
ORDER BY created_at DESC, id DESC LIMIT ?`, filter.TeamID, filter.TeamID, optionalTime(filter.From), optionalTime(filter.From), optionalTime(filter.Before), optionalTime(filter.Before), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []AuditEvent{}
	for rows.Next() {
		var event AuditEvent
		if err := rows.Scan(&event.ID, &event.ActorUsername, &event.SubjectUsername, &event.TeamID, &event.ImpersonationID, &event.Action, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *MySQLStore) CreateImpersonation(ctx context.Context, session Impersonation) error {
	if session.ID == "" || session.TokenHash == [32]byte{} || session.ActorUsername == "" || session.SubjectUsername == "" ||
		session.ActorUsername == session.SubjectUsername || session.CreatedAt.IsZero() || !session.ExpiresAt.After(session.CreatedAt) {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO account_center_impersonations
(id, token_hash, actor_username, subject_username, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		session.ID, session.TokenHash[:], session.ActorUsername, session.SubjectUsername, session.CreatedAt.UTC(), session.ExpiresAt.UTC())
	return recordError(err)
}

func (s *MySQLStore) FindImpersonation(ctx context.Context, tokenHash [32]byte, actorUsername string, now time.Time) (Impersonation, error) {
	var session Impersonation
	err := s.db.QueryRowContext(ctx, `SELECT id, actor_username, subject_username, created_at, expires_at
FROM account_center_impersonations WHERE token_hash = ? AND actor_username = ? AND expires_at > ? AND revoked_at IS NULL`,
		tokenHash[:], actorUsername, now.UTC()).Scan(&session.ID, &session.ActorUsername, &session.SubjectUsername, &session.CreatedAt, &session.ExpiresAt)
	return session, recordError(err)
}

func (s *MySQLStore) RevokeImpersonation(ctx context.Context, id, actorUsername string, revokedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `UPDATE account_center_impersonations
SET revoked_at = COALESCE(?, CURRENT_TIMESTAMP(6)) WHERE id = ? AND actor_username = ? AND revoked_at IS NULL`, optionalTime(revokedAt), id, actorUsername)
	return recordChanged(result, err)
}

func optionalTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

func recordError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
		return fmt.Errorf("%w: %w", ErrConflict, err)
	}
	return err
}

func recordChanged(result sql.Result, err error) error {
	if err != nil {
		return recordError(err)
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return ErrNotFound
	}
	return err
}

const legacyImportKey = "member-center-legacy-v1"

type legacyProfile struct {
	Username, Role, DisplayName, AvatarURL, BoundTo string
}

type legacyArchive struct {
	Username               string
	ArchivedAt, RestoredAt time.Time
}

type legacyTeam struct {
	ID, Name, ManagerUsername, CreatedBy string
	CreatedAt, ArchivedAt                time.Time
	CoManagers                           []string
}

type legacyGrant struct {
	ID, Actor, Subject, Capability, Scope string
	At                                    time.Time
}

// ImportLegacy runs after the auth accounts are imported. The marker is
// locked for the entire import so two app instances cannot both apply it.
func (s *MySQLStore) ImportLegacy(ctx context.Context, systemDir string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO account_center_imports (import_key) VALUES (?)
ON DUPLICATE KEY UPDATE import_key = import_key`, legacyImportKey); err != nil {
		return err
	}
	var completed bool
	if err := tx.QueryRowContext(ctx, `SELECT completed_at IS NOT NULL FROM account_center_imports WHERE import_key = ? FOR UPDATE`, legacyImportKey).Scan(&completed); err != nil || completed {
		return err
	}
	accounts, err := legacyAccounts(ctx, tx)
	if err != nil || len(accounts) == 0 {
		return err
	}
	var profiles []legacyProfile
	var archives []legacyArchive
	var collaboration struct{ Teams []legacyTeam }
	var grants []legacyGrant
	if systemDir = strings.TrimSpace(systemDir); systemDir != "" {
		for _, file := range []struct {
			name string
			dest any
		}{
			{"member-profiles.json", &profiles},
			{"member-archives.json", &archives},
			{"team-collaboration.json", &collaboration},
			{"grants.json", &grants},
		} {
			if err := readLegacyFile(systemDir, file.name, file.dest); err != nil {
				return err
			}
		}
	}
	identities, err := importLegacyProfiles(ctx, tx, accounts, profiles, archives)
	if err != nil {
		return err
	}
	if err := importLegacyTeams(ctx, tx, identities, collaboration.Teams, profiles); err != nil {
		return err
	}
	for _, grant := range grants {
		if identities[grant.Actor].Role != RoleFounder || identities[grant.Subject].Username == "" || grant.ID == "" || !validLegacyGrant(grant) {
			continue
		}
		if _, err := tx.ExecContext(ctx, insertGrantSQL, grant.ID, grant.Subject, grant.Capability, grant.Scope, "", grant.Actor, optionalTime(grant.At)); err != nil {
			return fmt.Errorf("import legacy grant: %w", recordError(err))
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE account_center_imports SET completed_at = CURRENT_TIMESTAMP(6) WHERE import_key = ?`, legacyImportKey); err != nil {
		return err
	}
	return tx.Commit()
}

func validLegacyGrant(grant legacyGrant) bool {
	if grant.Scope != "*" && grant.Scope != "script" {
		return false
	}
	switch grant.Capability {
	case "account:review", "preset:draft", "preset:publish":
		return true
	default:
		return false
	}
}

type legacyAccount struct {
	username string
	isOwner  bool
}

func legacyAccounts(ctx context.Context, tx *sql.Tx) ([]legacyAccount, error) {
	rows, err := tx.QueryContext(ctx, `SELECT username, is_owner FROM accounts ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var accounts []legacyAccount
	for rows.Next() {
		var account legacyAccount
		if err := rows.Scan(&account.username, &account.isOwner); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func readLegacyFile(dir, name string, dest any) error {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if errors.Is(err, os.ErrNotExist) && (name == "team-collaboration.json" || name == "grants.json") {
		// The old team and grant readers try the default data path as well.
		data, err = os.ReadFile(filepath.Join(dir, "..", "..", "..", "data", "system", name))
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read legacy %s: %w", name, err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("decode legacy %s: %w", name, err)
	}
	return nil
}

func importLegacyProfiles(ctx context.Context, tx *sql.Tx, accounts []legacyAccount, profiles []legacyProfile, archives []legacyArchive) (map[string]Identity, error) {
	byUsername := make(map[string]legacyProfile, len(profiles))
	for _, profile := range profiles {
		if _, present := byUsername[profile.Username]; !present {
			byUsername[profile.Username] = profile
		}
	}
	archiveTimes := make(map[string]time.Time, len(archives))
	for _, archive := range archives {
		if archive.ArchivedAt.After(archive.RestoredAt) {
			archiveTimes[archive.Username] = archive.ArchivedAt
		}
	}
	identities := make(map[string]Identity, len(accounts))
	for _, account := range accounts {
		profile := byUsername[account.username]
		identity := Identity{Username: account.username, DisplayName: strings.TrimSpace(profile.DisplayName), AvatarURL: profile.AvatarURL, Role: RoleMember}
		if identity.DisplayName == "" {
			identity.DisplayName = account.username
		}
		switch {
		case account.isOwner:
			identity.Role = RoleFounder
		case profile.Role == "dev":
			identity.Role = RoleDeveloper
		case profile.Role == "manager":
			identity.Role = RoleManager
		}
		if at, archived := archiveTimes[account.username]; archived && identity.Role != RoleFounder {
			identity.ArchivedAt = &at
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO account_center_profiles (username, display_name, avatar_url, role, archived_at)
VALUES (?, ?, ?, ?, ?)`, identity.Username, identity.DisplayName, identity.AvatarURL, identity.Role, identity.ArchivedAt); err != nil {
			return nil, fmt.Errorf("import legacy account center profile: %w", recordError(err))
		}
		identities[identity.Username] = identity
	}
	return identities, nil
}

func importLegacyTeams(ctx context.Context, tx *sql.Tx, identities map[string]Identity, teams []legacyTeam, profiles []legacyProfile) error {
	primary := make(map[string]string)
	archivedFallback := make(map[string]string)
	for _, row := range teams {
		manager := identities[row.ManagerUsername]
		if !canOwnLegacyTeam(manager) || row.ID == "" {
			continue
		}
		team := Team{ID: row.ID, Name: row.Name, ManagerUsername: manager.Username, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt}
		if team.Name == "" {
			team.Name = manager.DisplayName + "的团队"
		}
		if team.CreatedBy == "" {
			team.CreatedBy = manager.Username
		}
		if !row.ArchivedAt.IsZero() {
			team.ArchivedAt = &row.ArchivedAt
		}
		if err := insertTeam(ctx, tx, team); err != nil {
			return fmt.Errorf("import legacy team: %w", err)
		}
		if team.ArchivedAt == nil && (primary[manager.Username] == "" || team.ID == "go-team-"+manager.Username) {
			primary[manager.Username] = team.ID
		}
		if team.ArchivedAt != nil && team.ID == "go-team-"+manager.Username {
			archivedFallback[manager.Username] = team.ID
		}
		for _, username := range row.CoManagers {
			if canOwnLegacyTeam(identities[username]) {
				if err := importLegacyMembership(ctx, tx, team.ID, username); err != nil {
					return err
				}
			}
		}
	}
	seen := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		if seen[profile.Username] {
			continue
		}
		seen[profile.Username] = true
		identity, manager := identities[profile.Username], identities[profile.BoundTo]
		if identity.Role != RoleMember || !canOwnLegacyTeam(manager) {
			continue
		}
		teamID := primary[manager.Username]
		if teamID == "" {
			teamID = archivedFallback[manager.Username]
		}
		if teamID == "" {
			teamID = "go-team-" + manager.Username
			team := Team{ID: teamID, Name: manager.DisplayName + "的团队", ManagerUsername: manager.Username, CreatedBy: manager.Username}
			if err := insertTeam(ctx, tx, team); err != nil {
				return fmt.Errorf("import legacy member team: %w", err)
			}
			primary[manager.Username] = teamID
		}
		if err := importLegacyMembership(ctx, tx, teamID, identity.Username); err != nil {
			return err
		}
	}
	return nil
}

func canOwnLegacyTeam(identity Identity) bool {
	return identity.Role == RoleFounder || identity.Role == RoleDeveloper || identity.Role == RoleManager
}

func importLegacyMembership(ctx context.Context, tx *sql.Tx, teamID, username string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO account_center_memberships (team_id, username)
VALUES (?, ?) ON DUPLICATE KEY UPDATE id = id`, teamID, username)
	return err
}
