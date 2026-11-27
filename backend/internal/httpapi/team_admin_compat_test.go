package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ac "qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
)

type teamAdminTestStore struct {
	accounts map[string]auth.Account
	revoked  int64
}

func (s *teamAdminTestStore) FindAccount(_ context.Context, username string) (auth.Account, error) {
	account, ok := s.accounts[username]
	if !ok {
		return auth.Account{}, auth.ErrNotFound
	}
	return account, nil
}
func (s *teamAdminTestStore) CreateAccount(context.Context, auth.Account) error { return nil }
func (s *teamAdminTestStore) UpdatePassword(_ context.Context, username, hash, algorithm string) error {
	account, ok := s.accounts[username]
	if !ok {
		return auth.ErrNotFound
	}
	account.PasswordHash, account.PasswordAlgorithm = hash, algorithm
	s.accounts[username] = account
	return nil
}
func (s *teamAdminTestStore) SetActive(_ context.Context, username string, active bool) error {
	account, ok := s.accounts[username]
	if !ok {
		return auth.ErrNotFound
	}
	account.Active = active
	s.accounts[username] = account
	return nil
}
func (s *teamAdminTestStore) CreateSession(context.Context, auth.Session) error { return nil }
func (s *teamAdminTestStore) FindSession(context.Context, string, time.Time) (auth.Session, error) {
	return auth.Session{}, auth.ErrNotFound
}
func (s *teamAdminTestStore) RevokeSession(context.Context, string) error { return nil }
func (s *teamAdminTestStore) RevokeOtherSessions(_ context.Context, username, currentToken string) (int64, error) {
	if currentToken != "" {
		return 0, nil
	}
	s.revoked++
	return 1, nil
}
func (s *teamAdminTestStore) ListAccounts(context.Context) ([]auth.Account, error) {
	rows := make([]auth.Account, 0, len(s.accounts))
	for _, account := range s.accounts {
		rows = append(rows, account)
	}
	return rows, nil
}

func TestTeamAdminCompatReadsCanonicalTeamScope(t *testing.T) {
	profiles := NewMemberProfileStore(t.TempDir())
	teamState := `{"teams":[{"id":"team-a","managerUsername":"owner","name":"A队","coManagers":["co1"]}],"invites":[],"notifications":[],"archives":[]}`
	if err := os.WriteFile(filepath.Join(profiles.SystemDir, "team-collaboration.json"), []byte(teamState), 0o600); err != nil {
		t.Fatal(err)
	}
	profilesJSON := `[{"username":"owner","role":"manager"},{"username":"co1","role":"manager"}]`
	if err := os.WriteFile(filepath.Join(profiles.SystemDir, "member-profiles.json"), []byte(profilesJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &teamAdminTestStore{accounts: map[string]auth.Account{
		"owner": {Username: "owner", Active: true},
		"co1":   {Username: "co1", Active: true},
	}}
	now := time.Unix(1700000000, 0)
	directory := &directoryTestStore{directory: ac.AdminDirectory{
		People: []ac.Identity{{Username: "owner", Role: ac.RoleManager, Active: true}, {Username: "co1", Role: ac.RoleManager, Active: true}},
		Teams:  []ac.Team{{ID: "team-a", Name: "A队", ManagerUsername: "owner"}},
	}}
	directory.resolve = func(_ context.Context, name string) (ac.Identity, error) {
		return (adminSnapshot{AdminDirectory: directory.directory}).person(name), nil
	}
	api := NewRouter(RouterOptions{BridgeSecret: "secret", Now: func() time.Time { return now }, AuthStore: store, MemberProfileStore: profiles, AccountCenterStore: directory})

	response := signedJSONRequest(t, api, now, "co1", "GET", "/api/team-admin/teams", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"teams":[]`) {
		t.Fatalf("legacy JSON co-manager cannot read another team's accounts: %d %s", response.Code, response.Body.String())
	}
	response = signedJSONRequest(t, api, now, "owner", "GET", "/api/team-admin/teams", nil)
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"team-a"`) {
		t.Fatalf("canonical manager sees own team: %d %s", response.Code, response.Body.String())
	}
}

func TestTeamAdminCompatDoesNotWriteLegacyPassword(t *testing.T) {
	profiles := NewMemberProfileStore(t.TempDir())
	teamState := `{"teams":[{"id":"team-a","managerUsername":"owner","name":"A队","coManagers":[]}],"invites":[],"notifications":[],"archives":[]}`
	if err := os.WriteFile(filepath.Join(profiles.SystemDir, "team-collaboration.json"), []byte(teamState), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profiles.SystemDir, "member-profiles.json"), []byte(`[{"username":"owner","role":"manager"},{"username":"m1","role":"member","boundTo":"owner"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &teamAdminTestStore{accounts: map[string]auth.Account{
		"owner": {Username: "owner", Active: true},
		"m1":    {Username: "m1", Active: true, PasswordHash: "old", PasswordAlgorithm: "scrypt"},
	}}
	now := time.Unix(1700000000, 0)
	api := NewRouter(RouterOptions{BridgeSecret: "secret", Now: func() time.Time { return now }, AuthStore: store, MemberProfileStore: profiles})

	response := signedJSONRequest(t, api, now, "owner", "POST", "/api/team-admin/teams/team-a/members/m1/reset-password", map[string]any{"password": "new-password-1"})
	if response.Code != 403 || !strings.Contains(response.Body.String(), "无权执行此操作") {
		t.Fatalf("reset password: %d %s", response.Code, response.Body.String())
	}
	if store.revoked != 0 || store.accounts["m1"].PasswordHash != "old" {
		t.Fatalf("legacy reset changed the member credential or sessions")
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
