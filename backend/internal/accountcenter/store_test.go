package accountcenter_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/storage"
)

func TestAccountCenterLegacyImportPreservesFormalRolesAndTeam(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := accountcenter.NewMySQLStore(db)
	dir := t.TempDir()
	profiles := `[{"username":"owner","role":"dev","displayName":"开发创始"},{"username":"dev","role":"dev"},{"username":"lead","role":"manager"},{"username":"worker","role":"member","boundTo":"lead","displayName":"组员甲"},{"username":"archived","role":"member","boundTo":"lead"}]`
	writeLegacyFile(t, dir, "member-profiles.json", profiles)
	writeLegacyFile(t, dir, "team-collaboration.json", `{"teams":[{"id":"existing-team","name":"原团队","managerUsername":"lead"}]}`)
	writeLegacyFile(t, dir, "member-archives.json", `[{"username":"archived","archivedAt":"2026-11-25T00:00:00Z"}]`)
	writeLegacyFile(t, dir, "grants.json", `[{"id":"legacy-grant","actor":"owner","subject":"worker","capability":"preset:draft","scope":"script"}]`)
	if err := store.ImportLegacy(ctx, dir); err != nil {
		t.Fatal(err)
	}
	for _, want := range []accountcenter.Identity{
		{Username: "owner", DisplayName: "开发创始", Role: accountcenter.RoleFounder, Active: true},
		{Username: "dev", DisplayName: "dev", Role: accountcenter.RoleDeveloper, Active: true},
		{Username: "lead", DisplayName: "lead", Role: accountcenter.RoleManager, Active: true},
		{Username: "worker", DisplayName: "组员甲", Role: accountcenter.RoleMember, Active: true},
		{Username: "archived", DisplayName: "archived", Role: accountcenter.RoleMember},
		{Username: "other", DisplayName: "other", Role: accountcenter.RoleMember, Active: true},
	} {
		got, err := store.ResolveIdentity(ctx, want.Username)
		if err != nil || got.Username != want.Username || got.DisplayName != want.DisplayName || got.Role != want.Role || got.Active != want.Active {
			t.Fatalf("identity %s = %+v, %v, want %+v", want.Username, got, err, want)
		}
	}
	teams, err := store.ListTeams(ctx, accountcenter.TeamFilter{Username: "worker"})
	if err != nil || len(teams) != 1 || teams[0].ID != "existing-team" || teams[0].MemberCount != 2 {
		t.Fatalf("worker teams = %+v, %v", teams, err)
	}
	grants, err := store.ListGrants(ctx, "worker")
	if err != nil || len(grants) != 1 || grants[0].ID != "legacy-grant" {
		t.Fatalf("worker grants = %+v, %v", grants, err)
	}
	writeLegacyFile(t, dir, "member-profiles.json", `[{"username":"worker","role":"dev"}]`)
	if err := store.ImportLegacy(ctx, dir); err != nil {
		t.Fatal(err)
	}
	worker, err := store.ResolveIdentity(ctx, "worker")
	if err != nil || worker.Role != accountcenter.RoleMember || worker.DisplayName != "组员甲" {
		t.Fatalf("restart overwrote imported worker = %+v, %v", worker, err)
	}
}

func TestAccountCenterLegacyImportRollsBackAndRetries(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := accountcenter.NewMySQLStore(db)
	if _, err := db.Exec(`UPDATE accounts SET is_owner = TRUE WHERE username = 'worker'`); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := store.ImportLegacy(ctx, dir); !errors.Is(err, accountcenter.ErrConflict) {
		t.Fatalf("import with two owners = %v", err)
	}
	var profiles, markers int
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM account_center_profiles), (SELECT COUNT(*) FROM account_center_imports)`).Scan(&profiles, &markers); err != nil || profiles != 0 || markers != 0 {
		t.Fatalf("partial import left %d profiles and %d markers, %v", profiles, markers, err)
	}
	if _, err := db.Exec(`UPDATE accounts SET is_owner = FALSE WHERE username = 'worker'`); err != nil {
		t.Fatal(err)
	}
	if err := store.ImportLegacy(ctx, dir); err != nil {
		t.Fatalf("retry after repairing source: %v", err)
	}
	identity, err := store.ResolveIdentity(ctx, "owner")
	if err != nil || identity.Role != accountcenter.RoleFounder {
		t.Fatalf("retried import owner = %+v, %v", identity, err)
	}
}

func TestAccountCenterSetRoleProtectsFounderAndDeveloper(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := accountcenter.NewMySQLStore(db)
	if err := store.ImportLegacy(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRole(ctx, "owner", "dev", accountcenter.RoleDeveloper); err != nil {
		t.Fatalf("founder grants developer: %v", err)
	}
	if err := store.SetRole(ctx, "dev", "worker", accountcenter.RoleDeveloper); !errors.Is(err, accountcenter.ErrForbidden) {
		t.Fatalf("developer grants developer: %v", err)
	}
	if err := store.SetRole(ctx, "dev", "owner", accountcenter.RoleMember); !errors.Is(err, accountcenter.ErrForbidden) {
		t.Fatalf("developer changes founder: %v", err)
	}
	if err := store.SetRole(ctx, "dev", "worker", accountcenter.RoleManager); err != nil {
		t.Fatalf("developer creates manager: %v", err)
	}
	if err := store.SetRole(ctx, "owner", "dev", accountcenter.RoleMember); err != nil {
		t.Fatalf("founder revokes developer: %v", err)
	}
}

func TestAccountCenterMySQLRejectsDuplicateFounder(t *testing.T) {
	db := accountCenterDatabase(t)
	const insert = `INSERT INTO account_center_profiles (username, display_name, role) VALUES (?, ?, 'founder')`
	if _, err := db.Exec(insert, "owner", "开发创始"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(insert, "other", "重复开发创始"); !isDuplicateKey(err) {
		t.Fatalf("second founder error = %v, want MySQL duplicate key", err)
	}
}

func TestAccountCenterMySQLRejectsCrossTeamActiveMembership(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := accountcenter.NewMySQLStore(db)
	if err := store.ImportLegacy(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTeam(ctx, accountcenter.Team{ID: "first", Name: "测试团队一", ManagerUsername: "owner", CreatedBy: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTeam(ctx, accountcenter.Team{ID: "second", Name: "测试团队二", ManagerUsername: "lead", CreatedBy: "owner"}); err != nil {
		t.Fatal(err)
	}
	membership := accountcenter.Membership{TeamID: "first", Username: "worker"}
	if err := store.AddMembership(ctx, membership); err != nil {
		t.Fatal(err)
	}
	membership.TeamID = "second"
	if err := store.AddMembership(ctx, membership); !errors.Is(err, accountcenter.ErrConflict) || !isDuplicateKey(err) {
		t.Fatalf("cross-team membership error = %v, want typed and MySQL duplicate key errors", err)
	}
	if err := store.EndMembership(ctx, "first", "worker", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.AddMembership(ctx, membership); err != nil {
		t.Fatalf("join second team after ending first membership: %v", err)
	}
	memberships, err := store.ListMemberships(ctx, "second")
	if err != nil || len(memberships) != 2 || memberships[0].Username != "lead" || memberships[1].Username != "worker" {
		t.Fatalf("active memberships = %+v, %v", memberships, err)
	}
}

func TestAccountCenterCreateTeamRejectsOwnerWithActiveMembership(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := accountcenter.NewMySQLStore(db)
	first := accountcenter.Team{ID: "first", Name: "测试团队一", ManagerUsername: "owner", CreatedBy: "owner"}
	if err := store.CreateTeam(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := accountcenter.Team{ID: "second", Name: "测试团队二", ManagerUsername: "owner", CreatedBy: "owner"}
	if err := store.CreateTeam(ctx, second); !errors.Is(err, accountcenter.ErrConflict) || !isDuplicateKey(err) {
		t.Fatalf("team owner with active membership error = %v", err)
	}
	teams, err := store.ListTeams(ctx, accountcenter.TeamFilter{IncludeArchived: true})
	if err != nil || len(teams) != 1 || teams[0].ID != first.ID {
		t.Fatalf("team creation was not rolled back: %+v, %v", teams, err)
	}
	if err := store.EndMembership(ctx, "first", "owner", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTeam(ctx, second); err != nil {
		t.Fatalf("create second team after ending first membership: %v", err)
	}
}

func TestAccountCenterLegacyImportPreservesSecondaryAndArchivedTeams(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := accountcenter.NewMySQLStore(db)
	dir := t.TempDir()
	writeLegacyFile(t, dir, "member-profiles.json", `[{"username":"lead","role":"manager"},{"username":"dev","role":"dev"},{"username":"worker","role":"member","boundTo":"lead"}]`)
	writeLegacyFile(t, dir, "team-collaboration.json", `{"teams":[
		{"id":"secondary","name":"额外团队","managerUsername":"lead"},
		{"id":"dev-team","name":"开发团队","managerUsername":"dev","coManagers":["lead","lead"]},
		{"id":"go-team-lead","name":"主团队","managerUsername":"lead"},
		{"id":"historical","name":"历史团队","managerUsername":"lead","coManagers":["dev"],"archivedAt":"2026-11-25T00:00:00Z"}
	]}`)
	if err := store.ImportLegacy(ctx, dir); err != nil {
		t.Fatal(err)
	}
	for _, username := range []string{"lead", "worker"} {
		teams, err := store.ListTeams(ctx, accountcenter.TeamFilter{Username: username, IncludeArchived: true})
		if err != nil || len(teams) != 1 || teams[0].ID != "go-team-lead" {
			t.Fatalf("%s active teams = %+v, %v", username, teams, err)
		}
	}
	grants, err := store.ListGrants(ctx, "lead")
	if err != nil || len(grants) != 1 || grants[0].TeamID != "dev-team" || grants[0].Capability != "team:manage" || grants[0].Scope != "team" {
		t.Fatalf("co-manager grants = %+v, %v", grants, err)
	}
	grants, err = store.ListGrants(ctx, "dev")
	if err != nil || len(grants) != 0 {
		t.Fatalf("archived team left active grants = %+v, %v", grants, err)
	}
	var historicalGrants int
	if err := db.QueryRow(`SELECT COUNT(*) FROM account_center_grants WHERE username = 'dev' AND team_id = 'historical' AND revoked_at IS NOT NULL`).Scan(&historicalGrants); err != nil || historicalGrants != 1 {
		t.Fatalf("archived team preserved %d historical co-manager grants, %v", historicalGrants, err)
	}
	teams, err := store.ListTeams(ctx, accountcenter.TeamFilter{ManagerUsername: "lead", IncludeArchived: true})
	if err != nil || len(teams) != 3 {
		t.Fatalf("owned teams including history = %+v, %v", teams, err)
	}
	for _, team := range teams {
		if team.ID == "go-team-lead" {
			continue
		}
		if team.ArchivedAt == nil || team.MemberCount != 0 {
			t.Fatalf("team history lost or active: %+v", team)
		}
		var oldMembers int
		if err := db.QueryRow(`SELECT COUNT(*) FROM account_center_memberships WHERE team_id = ? AND ended_at IS NOT NULL AND username IN ('lead', 'worker')`, team.ID).Scan(&oldMembers); err != nil || oldMembers != 2 {
			t.Fatalf("team %s preserved %d historical memberships, %v", team.ID, oldMembers, err)
		}
		if team.ID == "historical" && !team.ArchivedAt.Equal(time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("old archive time changed: %s", team.ArchivedAt)
		}
	}
	audit, err := store.ListAudit(ctx, accountcenter.AuditFilter{TeamID: "secondary"})
	if err != nil || len(audit) != 1 || audit[0].Action != "legacy_import.team_archived_duplicate_owner" || audit[0].SubjectUsername != "lead" {
		t.Fatalf("secondary team archive audit = %+v, %v", audit, err)
	}
}

func TestAccountCenterLegacyImportRetriesWhenSourceDirectoryReturns(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := accountcenter.NewMySQLStore(db)
	dir := filepath.Join(t.TempDir(), "legacy-system")
	for _, source := range []string{"", dir} {
		if err := store.ImportLegacy(ctx, source); err == nil {
			t.Fatalf("unavailable source %q was accepted", source)
		}
		var markers int
		if err := db.QueryRow(`SELECT COUNT(*) FROM account_center_imports`).Scan(&markers); err != nil || markers != 0 {
			t.Fatalf("unavailable source left %d markers, %v", markers, err)
		}
	}
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeLegacyFile(t, dir, "member-profiles.json", `[{"username":"worker","role":"manager","displayName":"恢复的管理者"}]`)
	if err := store.ImportLegacy(ctx, dir); err != nil {
		t.Fatal(err)
	}
	worker, err := store.ResolveIdentity(ctx, "worker")
	if err != nil || worker.Role != accountcenter.RoleManager || worker.DisplayName != "恢复的管理者" {
		t.Fatalf("recovered worker = %+v, %v", worker, err)
	}
}

func TestAccountCenterLegacyImportAcceptsExistingEmptyDirectory(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := accountcenter.NewMySQLStore(db)
	if err := store.ImportLegacy(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	var imported bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM account_center_imports WHERE completed_at IS NOT NULL)`).Scan(&imported); err != nil || !imported {
		t.Fatalf("empty source completion = %t, %v", imported, err)
	}
}

func TestAccountCenterRoleAndArchiveSameValueRequestsAreIdempotent(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := accountcenter.NewMySQLStore(db)
	if err := store.ImportLegacy(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := store.SetRole(ctx, "owner", "worker", accountcenter.RoleDeveloper); err != nil {
			t.Fatalf("founder sets developer role: %v", err)
		}
	}
	if err := store.SetRole(ctx, "worker", "worker", accountcenter.RoleDeveloper); !errors.Is(err, accountcenter.ErrForbidden) {
		t.Fatalf("unauthorized same role update = %v", err)
	}
	if err := store.SetRole(ctx, "owner", "missing", accountcenter.RoleMember); !errors.Is(err, accountcenter.ErrNotFound) {
		t.Fatalf("authorized role update with missing target = %v", err)
	}
	archivedAt := time.Date(2026, 11, 26, 1, 0, 0, 0, time.UTC)
	for _, at := range []*time.Time{&archivedAt, &archivedAt, nil, nil} {
		if err := store.SetArchived(ctx, "worker", at); err != nil {
			t.Fatalf("update worker archive to %v: %v", at, err)
		}
	}
	if err := store.SetArchived(ctx, "owner", nil); !errors.Is(err, accountcenter.ErrForbidden) {
		t.Fatalf("change founder archive = %v", err)
	}
	if err := store.SetArchived(ctx, "missing", nil); !errors.Is(err, accountcenter.ErrNotFound) {
		t.Fatalf("change missing archive = %v", err)
	}
}

func TestAccountCenterMySQLStoresGrantsAuditsAndImpersonations(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := accountcenter.NewMySQLStore(db)
	if err := store.ImportLegacy(ctx, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 26, 1, 0, 0, 0, time.UTC)
	grant := accountcenter.Grant{ID: "grant-1", Username: "worker", Capability: "team_read", Scope: "team", GrantedBy: "owner", GrantedAt: now}
	if err := store.CreateGrant(ctx, grant); err != nil {
		t.Fatal(err)
	}
	grants, err := store.ListGrants(ctx, "worker")
	if err != nil || len(grants) != 1 || grants[0].GrantedBy != grant.GrantedBy || grants[0].Capability != grant.Capability {
		t.Fatalf("grants = %+v, %v", grants, err)
	}
	if err := store.RevokeGrant(ctx, grant.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	grants, err = store.ListGrants(ctx, "worker")
	if err != nil || len(grants) != 0 {
		t.Fatalf("active grants after revocation = %+v, %v", grants, err)
	}
	session := accountcenter.Impersonation{ID: "switch-1", TokenHash: [32]byte{1}, ActorUsername: "owner", SubjectUsername: "worker", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateImpersonation(ctx, session); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindImpersonation(ctx, session.TokenHash, "other", now); !errors.Is(err, accountcenter.ErrNotFound) {
		t.Fatalf("mismatched actor error = %v", err)
	}
	active, err := store.FindImpersonation(ctx, session.TokenHash, "owner", now)
	if err != nil || active.SubjectUsername != "worker" || active.ID != session.ID {
		t.Fatalf("active impersonation = %+v, %v", active, err)
	}
	if _, err := store.FindImpersonation(ctx, session.TokenHash, "owner", session.ExpiresAt); !errors.Is(err, accountcenter.ErrNotFound) {
		t.Fatalf("expired impersonation error = %v", err)
	}
	if err := store.RevokeImpersonation(ctx, session.ID, "owner", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.FindImpersonation(ctx, session.TokenHash, "owner", now.Add(time.Minute)); !errors.Is(err, accountcenter.ErrNotFound) {
		t.Fatalf("revoked impersonation error = %v", err)
	}
	event := accountcenter.AuditEvent{ActorUsername: "owner", SubjectUsername: "worker", TeamID: "team", ImpersonationID: session.ID, Action: "impersonation.stop", CreatedAt: now}
	if err := store.AppendAudit(ctx, event); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListAudit(ctx, accountcenter.AuditFilter{TeamID: "team"})
	if err != nil || len(events) != 1 || events[0].ActorUsername != "owner" || events[0].SubjectUsername != "worker" || events[0].ImpersonationID != session.ID {
		t.Fatalf("audit events = %+v, %v", events, err)
	}
}

func accountCenterDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("QIANTIE_TEST_MYSQL84_DSN")
	if dsn == "" {
		t.Skip("QIANTIE_TEST_MYSQL84_DSN is required for MySQL storage verification")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	name := fmt.Sprintf("account_center_test_%d", time.Now().UnixNano())
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.ExecContext(ctx, "DROP DATABASE "+name); err != nil {
			t.Error(err)
		}
	})
	cfg.DBName, cfg.ParseTime = name, true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	migrations := append(storage.AuthMigrations(), storage.AccountCenterMigrations()...)
	for range 2 {
		if err := storage.RunMigrations(ctx, db, migrations); err != nil {
			t.Fatal(err)
		}
	}
	for _, username := range []string{"owner", "dev", "lead", "worker", "archived", "other"} {
		const seed = `INSERT INTO accounts (username, password_hash, password_algorithm, is_owner, active, created_at, updated_at) VALUES (?, 'test-hash', 'test', ?, TRUE, CURRENT_TIMESTAMP(6), CURRENT_TIMESTAMP(6))`
		if _, err := db.ExecContext(ctx, seed, username, username == "owner"); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func writeLegacyFile(t *testing.T, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func isDuplicateKey(err error) bool {
	var mysqlErr *mysql.MySQLError
	return errors.As(err, &mysqlErr) && mysqlErr.Number == 1062
}
