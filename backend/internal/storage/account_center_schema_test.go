package storage

import (
	"strings"
	"testing"
)

func TestAccountCenterMigrationsContainAuthorizationBoundaries(t *testing.T) {
	migrations := AccountCenterMigrations()
	if len(migrations) != 3 || migrations[0].Version != 8800403 || migrations[1].Version != 8800404 || migrations[2].Version != 8800405 {
		t.Fatalf("unexpected account center migrations: %+v", migrations)
	}
	joined := strings.ToLower(strings.Join(migrations[0].SQL, "\n"))
	for _, want := range []string{
		"account_center_profiles", "account_center_teams", "account_center_memberships",
		"account_center_grants", "account_center_audit_events", "account_center_impersonations",
		"unique key uq_account_center_founder (founder_slot)",
		"unique key uq_account_center_active_membership (active_username)",
		"key idx_account_center_audit_team_time (team_id, created_at, id)",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if !strings.Contains(strings.Join(migrations[1].SQL, "\n"), "account_center_imports") {
		t.Fatal("missing legacy import marker table")
	}
	adminSQL := strings.Join(migrations[2].SQL, "\n")
	for _, table := range []string{"account_center_archive_states", "account_center_reserved_names", "account_center_invite_claims"} {
		if !strings.Contains(adminSQL, table) {
			t.Fatalf("missing governance table %s", table)
		}
	}
	app := AppMigrations()
	for _, want := range migrations {
		found := false
		for _, registered := range app {
			if registered.Version == want.Version && ChecksumFor(registered) == ChecksumFor(want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("app migration list is missing account center migration %d", want.Version)
		}
	}
}
