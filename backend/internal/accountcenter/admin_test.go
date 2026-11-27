package accountcenter_test

import (
	"context"
	"errors"
	"testing"

	ac "qiantie/backend/internal/accountcenter"
)

func TestAccountCenterAdminMySQLRestoresArchivedTeamAndMember(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := ac.NewMySQLStore(db)
	dir, _ := legacySourceDirectories(t)
	writeLegacyFile(t, dir, "member-profiles.json", `[{"username":"lead","role":"manager"},{"username":"worker","role":"member","boundTo":"lead"}]`)
	writeLegacyFile(t, dir, "team-collaboration.json", `{"teams":[{"id":"legacy","name":"历史团队","managerUsername":"lead","archivedAt":"2026-11-25T00:00:00Z"}]}`)
	if err := store.ImportLegacy(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if allowed, err := store.BusinessAllowed(ctx, "worker"); err != nil || allowed {
		t.Fatalf("archived team allowed new work: %t, %v", allowed, err)
	}

	change := func(action, team, username string) {
		t.Helper()
		err := store.WithAdmin(ctx, true, func(tx ac.AdminTx) error {
			if err := tx.Change(ctx, ac.AdminChange{Action: action, TeamID: team, Username: username}); err != nil {
				return err
			}
			return tx.AppendAudit(ctx, ac.AuditEvent{ActorUsername: "owner", SubjectUsername: username, TeamID: team, Action: action})
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	var previousMembership bool
	if err := store.WithAdmin(ctx, false, func(tx ac.AdminTx) error {
		d, err := tx.Directory(ctx)
		for _, membership := range d.Memberships {
			if membership.TeamID == "legacy" && membership.Username == "worker" {
				previousMembership = true
			}
		}
		return err
	}); err != nil || !previousMembership {
		t.Fatalf("archived membership unavailable for restore: %t, %v", previousMembership, err)
	}
	change("restore_team", "legacy", "lead")
	if allowed, err := store.BusinessAllowed(ctx, "worker"); err != nil || !allowed {
		t.Fatalf("restored team rejected new work: %t, %v", allowed, err)
	}
	members, err := store.ListMemberships(ctx, "legacy")
	if err != nil || len(members) != 2 {
		t.Fatalf("restored members = %+v, %v", members, err)
	}
	change("disable", "legacy", "worker")
	change("archive", "legacy", "worker")
	change("restore", "legacy", "worker")
	person, err := store.ResolveIdentity(ctx, "worker")
	if err != nil || person.Active || person.ArchivedAt != nil {
		t.Fatalf("restore did not preserve the disabled status: %+v, %v", person, err)
	}
	events, err := store.ListAudit(ctx, ac.AuditFilter{TeamID: "legacy"})
	if err != nil || len(events) != 4 {
		t.Fatalf("committed audits = %+v, %v", events, err)
	}
}

func TestAccountCenterAdminMySQLPlatformGrantsAndCurrentMembership(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := ac.NewMySQLStore(db)
	dir, _ := legacySourceDirectories(t)
	writeLegacyFile(t, dir, "member-profiles.json", `[{"username":"dev","role":"dev"},{"username":"lead","role":"manager"},{"username":"worker","role":"member","boundTo":"lead"}]`)
	writeLegacyFile(t, dir, "team-collaboration.json", `{"teams":[{"id":"archived","name":"旧团队","managerUsername":"lead","archivedAt":"2026-11-25T00:00:00Z"}]}`)
	if err := store.ImportLegacy(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTeam(ctx, ac.Team{ID: "current", Name: "现行团队", ManagerUsername: "owner", CreatedBy: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddMembership(ctx, ac.Membership{TeamID: "current", Username: "worker"}); err != nil {
		t.Fatal(err)
	}
	change := func(action, username, team, id string) error {
		return store.WithAdmin(ctx, true, func(tx ac.AdminTx) error {
			if err := tx.Change(ctx, ac.AdminChange{Action: action, TeamID: team, Actor: "owner", Username: username, GrantID: id}); err != nil {
				return err
			}
			return tx.AppendAudit(ctx, ac.AuditEvent{ActorUsername: "owner", SubjectUsername: username, TeamID: team, Action: action})
		})
	}
	if err := change("restore_team", "lead", "archived", ""); !errors.Is(err, ac.ErrConflict) {
		t.Fatalf("restore with current membership: %v", err)
	}
	if err := change("grant_team_create", "dev", "", "test-team-create-grant"); err != nil {
		t.Fatal(err)
	}
	grants, err := store.PlatformGrants(ctx, "dev")
	if err != nil || len(grants) != 1 || grants[0] != ac.GrantCapabilityTeamCreate {
		t.Fatalf("persisted team creation grant = %+v, %v", grants, err)
	}
	if err := change("revoke_team_create", "dev", "", ""); err != nil {
		t.Fatal(err)
	}
	grants, err = store.PlatformGrants(ctx, "dev")
	if err != nil || len(grants) != 0 {
		t.Fatalf("revoked team creation grant = %+v, %v", grants, err)
	}
	if err := store.CreateGrant(ctx, ac.Grant{ID: "test-platform-developer", Username: "dev", Capability: ac.GrantCapabilityPlatformBusiness, Scope: ac.GrantScopePlatform, GrantedBy: "owner"}); err != nil {
		t.Fatal(err)
	}
	for username, want := range map[string]bool{"owner": true, "dev": true, "worker": true, "lead": false, "other": false} {
		if allowed, err := store.BusinessAllowed(ctx, username); err != nil || allowed != want {
			t.Fatalf("business access %s = %t, %v want %t", username, allowed, err, want)
		}
	}
}

func TestAccountCenterAdminMySQLRejectsHistoricalNamesAndRollsBackAudits(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := ac.NewMySQLStore(db)
	dir, _ := legacySourceDirectories(t)
	writeLegacyFile(t, dir, "account-recovery.json", `{"deletedAccounts":[{"username":"purged","deletedBy":"owner"}]}`)
	if err := store.ImportLegacy(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveLegacyNames(ctx, dir); err != nil {
		t.Fatal(err)
	}
	create := func(username string, auditFailure error) error {
		return store.WithAdmin(ctx, true, func(tx ac.AdminTx) error {
			if err := tx.Change(ctx, ac.AdminChange{
				Action: "create_team", TeamID: "new-team", TeamName: "团队", Actor: "owner", Username: username,
				DisplayName: username, Role: ac.RoleManager, PasswordHash: "test-hash", PasswordAlgorithm: "test",
			}); err != nil {
				return err
			}
			return auditFailure
		})
	}
	if err := create("purged", nil); !errors.Is(err, ac.ErrConflict) {
		t.Fatalf("historical name recreation = %v", err)
	}
	failedAudit := errors.New("audit unavailable")
	if err := create("newlead", failedAudit); !errors.Is(err, failedAudit) {
		t.Fatalf("create with missing audit = %v", err)
	}
	if _, err := store.ResolveIdentity(ctx, "newlead"); !errors.Is(err, ac.ErrNotFound) {
		t.Fatalf("account survived transaction rollback: %v", err)
	}
}

func TestAccountCenterAdminMySQLInviteClaimAndRollback(t *testing.T) {
	ctx := context.Background()
	db := accountCenterDatabase(t)
	store := ac.NewMySQLStore(db)
	dir, _ := legacySourceDirectories(t)
	if err := store.ImportLegacy(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTeam(ctx, ac.Team{ID: "invite-team", Name: "团队", ManagerUsername: "lead", CreatedBy: "owner"}); err != nil {
		t.Fatal(err)
	}
	claim := func(username, digest string, failure error) error {
		return store.WithAdmin(ctx, true, func(tx ac.AdminTx) error {
			if err := tx.Change(ctx, ac.AdminChange{
				Action: "accept_invite", TeamID: "invite-team", Username: username, DisplayName: username, Role: ac.RoleMember,
				InviteDigest: digest, PasswordHash: "test-hash", PasswordAlgorithm: "test",
			}); err != nil {
				return err
			}
			if err := tx.AppendAudit(ctx, ac.AuditEvent{ActorUsername: "lead", SubjectUsername: username, TeamID: "invite-team", Action: "accept_invite"}); err != nil {
				return err
			}
			return failure
		})
	}
	if err := claim("first-invitee", "test-digest-one", nil); err != nil {
		t.Fatalf("first valid claim failed: %v", err)
	}
	if err := claim("repeat-invitee", "test-digest-one", nil); !errors.Is(err, ac.ErrConflict) {
		t.Fatalf("duplicate claim: %v", err)
	}
	failure := errors.New("failed after writing audit")
	if err := claim("failed-invitee", "test-digest-two", failure); !errors.Is(err, failure) {
		t.Fatalf("failed claim: %v", err)
	}
	for _, username := range []string{"repeat-invitee", "failed-invitee"} {
		if _, err := store.ResolveIdentity(ctx, username); !errors.Is(err, ac.ErrNotFound) {
			t.Fatalf("failed invite account %s was persisted: %v", username, err)
		}
	}
	if err := claim("retry-invitee", "test-digest-two", nil); err != nil {
		t.Fatalf("failed claim consumed invite: %v", err)
	}
	events, err := store.ListAudit(ctx, ac.AuditFilter{TeamID: "invite-team"})
	if err != nil || len(events) != 2 {
		t.Fatalf("invite audits = %+v, %v", events, err)
	}
}
