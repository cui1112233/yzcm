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

type governanceAuthStore struct {
	accounts map[string]auth.Account
}

func (s *governanceAuthStore) FindAccount(_ context.Context, username string) (auth.Account, error) {
	account, ok := s.accounts[username]
	if !ok {
		return auth.Account{}, auth.ErrNotFound
	}
	return account, nil
}
func (s *governanceAuthStore) CreateAccount(_ context.Context, account auth.Account) error {
	if _, exists := s.accounts[account.Username]; exists {
		return auth.ErrInvalid
	}
	s.accounts[account.Username] = account
	return nil
}
func (s *governanceAuthStore) UpdatePassword(_ context.Context, username, hash, algorithm string) error {
	account, ok := s.accounts[username]
	if !ok {
		return auth.ErrNotFound
	}
	account.PasswordHash, account.PasswordAlgorithm, account.UpdatedAt = hash, algorithm, time.Now().UTC()
	s.accounts[username] = account
	return nil
}
func (s *governanceAuthStore) SetActive(_ context.Context, username string, active bool) error {
	account, ok := s.accounts[username]
	if !ok {
		return auth.ErrNotFound
	}
	account.Active, account.UpdatedAt = active, time.Now().UTC()
	s.accounts[username] = account
	return nil
}
func (s *governanceAuthStore) CreateSession(context.Context, auth.Session) error { return nil }
func (s *governanceAuthStore) FindSession(context.Context, string, time.Time) (auth.Session, error) {
	return auth.Session{}, auth.ErrNotFound
}
func (s *governanceAuthStore) RevokeSession(context.Context, string) error { return nil }

func TestAdminGovernanceRoutesListApplicationsAndGrantsWithoutLeakingPassword(t *testing.T) {
	now := time.Unix(1700000000, 0)
	profiles := NewMemberProfileStore(t.TempDir())
	if err := os.WriteFile(filepath.Join(profiles.SystemDir, "applications.json"), []byte(`[{"id":"app-1","username":"new-user","passwordHash":"secret-hash","passwordAlgorithm":"scrypt","reason":"测试","status":"pending"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	accounts := &authTestStore{account: auth.Account{Username: "alice", Active: true, IsOwner: true}}
	store := &directoryTestStore{directory: ac.AdminDirectory{People: []ac.Identity{
		{Username: "alice", Role: ac.RoleFounder, Active: true},
	}}}
	store.resolve = func(_ context.Context, username string) (ac.Identity, error) {
		return (adminSnapshot{AdminDirectory: store.directory}).person(username), nil
	}
	api := NewRouter(RouterOptions{BridgeSecret: "secret", Now: func() time.Time { return now }, AuthStore: accounts, MemberProfileStore: profiles, AccountCenterStore: store})

	applications := signedJSONRequest(t, api, now, "alice", "GET", "/api/admin/applications", nil)
	if applications.Code != 200 || !strings.Contains(applications.Body.String(), "app-1") || strings.Contains(applications.Body.String(), "secret-hash") {
		t.Fatalf("applications response = %d %s", applications.Code, applications.Body.String())
	}
	grants := signedJSONRequest(t, api, now, "alice", "GET", "/api/admin/grants", nil)
	if grants.Code != 200 || !strings.Contains(grants.Body.String(), `"grants":[]`) {
		t.Fatalf("grants response = %d %s", grants.Code, grants.Body.String())
	}
}
