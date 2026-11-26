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
