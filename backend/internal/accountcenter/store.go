package accountcenter

import (
	"context"
	"crypto/sha256"
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

	GrantCapabilityTeamManage       = "team:manage"
	GrantScopeTeam                  = "team"
	GrantScopePlatform              = "platform"
	GrantCapabilityTeamCreate       = "team:create"
	GrantCapabilityCustomAPI        = "api:custom"
	GrantCapabilityPlatformBusiness = "business:run"
	GrantCapabilityPlatformAdmin    = "admin:access"
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
	TeamID                      string
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
	Username        string // Matches an active membership.
	ManagerUsername string
	IncludeArchived bool
}

type TeamSummary struct {
	Team
	MemberCount int
}

type Membership struct {
	ID               uint64
	TeamID, Username string
	JoinedAt         time.Time
	EndedAt          *time.Time
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
       a.active AND p.archived_at IS NULL, COALESCE(p.avatar_url, ''), p.archived_at, COALESCE(t.id, '')
FROM accounts a LEFT JOIN account_center_profiles p ON p.username = a.username
LEFT JOIN account_center_memberships m ON m.active_username = a.username
LEFT JOIN account_center_teams t ON t.id = m.team_id AND t.archived_at IS NULL
WHERE a.username = ?`, username).Scan(&identity.Username, &identity.DisplayName, &identity.Role, &identity.Active, &identity.AvatarURL, &identity.ArchivedAt, &identity.TeamID)
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var actorRole string
	var targetRole sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT actor.role, target.role
FROM account_center_profiles actor JOIN accounts a ON a.username = actor.username
LEFT JOIN accounts target_account ON target_account.username = ?
LEFT JOIN account_center_profiles target ON target.username = target_account.username
WHERE actor.username = ? AND a.active = TRUE AND actor.archived_at IS NULL FOR UPDATE`, username, actorUsername).Scan(&actorRole, &targetRole)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if actorRole != RoleFounder && (actorRole != RoleDeveloper || role == RoleDeveloper) {
		return ErrForbidden
	}
	if !targetRole.Valid {
		return ErrNotFound
	}
	if targetRole.String == RoleFounder || (actorRole != RoleFounder && targetRole.String == RoleDeveloper) {
		return ErrForbidden
	}
	if _, err := tx.ExecContext(ctx, `UPDATE account_center_profiles SET role = ? WHERE username = ?`, role, username); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MySQLStore) SetArchived(ctx context.Context, username string, archivedAt *time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var role string
	err = tx.QueryRowContext(ctx, `SELECT p.role FROM account_center_profiles p JOIN accounts a ON a.username = p.username WHERE p.username = ? FOR UPDATE`, username).Scan(&role)
	if err != nil {
		return recordError(err)
	}
	if role == RoleFounder {
		return ErrForbidden
	}
	if _, err := tx.ExecContext(ctx, `UPDATE account_center_profiles SET archived_at = ? WHERE username = ?`, archivedAt, username); err != nil {
		return err
	}
	return tx.Commit()
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
	if err := insertTeamMembership(ctx, tx, team.ID, team.ManagerUsername); err != nil {
		return err
	}
	return tx.Commit()
}

func insertTeam(ctx context.Context, tx *sql.Tx, team Team) error {
	result, err := tx.ExecContext(ctx, `INSERT INTO account_center_teams (id, name, manager_username, created_by, created_at, archived_at)
SELECT ?, ?, username, ?, COALESCE(?, ?, CURRENT_TIMESTAMP(6)), ? FROM accounts WHERE username = ?`,
		team.ID, team.Name, team.CreatedBy, optionalTime(team.CreatedAt), team.ArchivedAt, team.ArchivedAt, team.ManagerUsername)
	return recordChanged(result, err)
}

func insertTeamMembership(ctx context.Context, tx *sql.Tx, teamID, username string) error {
	result, err := tx.ExecContext(ctx, `INSERT INTO account_center_memberships (team_id, username, joined_at, ended_at)
SELECT t.id, a.username, t.created_at, t.archived_at FROM account_center_teams t
JOIN accounts a ON a.username = ? WHERE t.id = ?`, username, teamID)
	return recordChanged(result, err)
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
	_, err := s.db.ExecContext(ctx, insertAuditSQL, event.ActorUsername, event.SubjectUsername, event.TeamID, event.ImpersonationID, event.Action, optionalTime(event.CreatedAt))
	return err
}

const insertAuditSQL = `INSERT INTO account_center_audit_events (actor_username, subject_username, team_id, impersonation_id, action, created_at)
VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, COALESCE(?, CURRENT_TIMESTAMP(6)))`

func (s *MySQLStore) ListAudit(ctx context.Context, filter AuditFilter) ([]AuditEvent, error) {
	return listAudit(ctx, s.db, filter)
}

func listAudit(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, filter AuditFilter) ([]AuditEvent, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.QueryContext(ctx, `SELECT id, actor_username, subject_username, COALESCE(team_id, ''), COALESCE(impersonation_id, ''), action, created_at
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
	systemDir = strings.TrimSpace(systemDir)
	if systemDir == "" {
		return errors.New("legacy account center source directory is empty")
	}
	source, err := os.OpenRoot(systemDir)
	if err != nil {
		return fmt.Errorf("open legacy account center source directory: %w", err)
	}
	defer source.Close()
	accounts, err := legacyAccounts(ctx, tx)
	if err != nil || len(accounts) == 0 {
		return err
	}
	var profiles []legacyProfile
	var archives []legacyArchive
	var collaboration struct{ Teams []legacyTeam }
	var grants []legacyGrant
	fallbackDir := filepath.Join(systemDir, "..", "..", "..", "data", "system")
	for _, file := range []struct {
		name        string
		dest        any
		fallbackDir string
	}{
		{"member-profiles.json", &profiles, ""},
		{"member-archives.json", &archives, ""},
		{"team-collaboration.json", &collaboration, fallbackDir},
		{"grants.json", &grants, fallbackDir},
	} {
		if err := readLegacyFile(source, file.fallbackDir, file.name, file.dest); err != nil {
			return err
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

func readLegacyFile(source *os.Root, fallbackDir, name string, dest any) error {
	_, err := source.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && fallbackDir != "" {
		// The old team and grant readers check the configured directory first.
		fallback, openErr := os.OpenRoot(fallbackDir)
		if errors.Is(openErr, os.ErrNotExist) {
			return nil
		}
		if openErr != nil {
			return fmt.Errorf("open legacy account center fallback directory: %w", openErr)
		}
		defer fallback.Close()
		source = fallback
		_, err = source.Lstat(name)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check legacy %s: %w", name, err)
	}
	data, err := source.ReadFile(name)
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
	primary := primaryLegacyTeamIDs(identities, teams)
	ownedTeams := make(map[string][]string)
	importedAt := time.Now().UTC()
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
		archiveDuplicate := team.ArchivedAt == nil && primary[manager.Username] != team.ID
		if archiveDuplicate {
			team.ArchivedAt = &importedAt
		}
		if err := insertTeam(ctx, tx, team); err != nil {
			return fmt.Errorf("import legacy team: %w", err)
		}
		if err := insertTeamMembership(ctx, tx, team.ID, manager.Username); err != nil {
			return fmt.Errorf("import legacy team owner: %w", err)
		}
		ownedTeams[manager.Username] = append(ownedTeams[manager.Username], team.ID)
		if archiveDuplicate {
			if _, err := tx.ExecContext(ctx, insertAuditSQL, "_legacy_import", manager.Username, team.ID, "", "legacy_import.team_archived_duplicate_owner", importedAt); err != nil {
				return fmt.Errorf("audit duplicate legacy team archive: %w", err)
			}
		}
		if err := importLegacyTeamGrants(ctx, tx, team, identities, row.CoManagers); err != nil {
			return err
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
		if len(ownedTeams[manager.Username]) == 0 {
			team := Team{ID: "go-team-" + manager.Username, Name: manager.DisplayName + "的团队", ManagerUsername: manager.Username, CreatedBy: manager.Username}
			if err := insertTeam(ctx, tx, team); err != nil {
				return fmt.Errorf("import legacy member team: %w", err)
			}
			if err := insertTeamMembership(ctx, tx, team.ID, manager.Username); err != nil {
				return fmt.Errorf("import legacy member team owner: %w", err)
			}
			ownedTeams[manager.Username] = []string{team.ID}
		}
		for _, teamID := range ownedTeams[manager.Username] {
			if err := insertTeamMembership(ctx, tx, teamID, identity.Username); err != nil {
				return fmt.Errorf("import legacy member team relationship: %w", err)
			}
		}
	}
	return nil
}

func primaryLegacyTeamIDs(identities map[string]Identity, teams []legacyTeam) map[string]string {
	primary := make(map[string]string)
	for _, row := range teams {
		if row.ID == "" || !row.ArchivedAt.IsZero() || !canOwnLegacyTeam(identities[row.ManagerUsername]) {
			continue
		}
		manager := row.ManagerUsername
		if primary[manager] == "" || row.ID == "go-team-"+manager {
			primary[manager] = row.ID
		}
	}
	return primary
}

func canOwnLegacyTeam(identity Identity) bool {
	return identity.Role == RoleFounder || identity.Role == RoleDeveloper || identity.Role == RoleManager
}

func importLegacyTeamGrants(ctx context.Context, tx *sql.Tx, team Team, identities map[string]Identity, coManagers []string) error {
	seen := make(map[string]bool, len(coManagers))
	for _, username := range coManagers {
		if username == team.ManagerUsername || seen[username] || !canOwnLegacyTeam(identities[username]) {
			continue
		}
		seen[username] = true
		id := sha256.Sum256([]byte(team.ID + "\x00" + username))
		_, err := tx.ExecContext(ctx, `INSERT INTO account_center_grants (id, username, capability, scope, team_id, granted_by, granted_at, revoked_at)
SELECT ?, ?, ?, ?, id, manager_username, created_at, archived_at FROM account_center_teams WHERE id = ?`,
			fmt.Sprintf("legacy-team-%x", id[:16]), username, GrantCapabilityTeamManage, GrantScopeTeam, team.ID)
		if err != nil {
			return fmt.Errorf("import legacy co-manager grant: %w", recordError(err))
		}
	}
	return nil
}
