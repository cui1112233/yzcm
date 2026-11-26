package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ac "qiantie/backend/internal/accountcenter"
)

type directoryTestStore struct {
	accountCenterIdentityStore
	directory ac.AdminDirectory
	writes    []ac.AdminChange
	audits    []ac.AuditEvent
	invites   map[string]string
}

func (s *directoryTestStore) WithAdmin(ctx context.Context, write bool, fn func(ac.AdminTx) error) error {
	return fn(s)
}
func (s *directoryTestStore) Directory(context.Context) (ac.AdminDirectory, error) {
	return s.directory, nil
}
func (s *directoryTestStore) Change(_ context.Context, c ac.AdminChange) error {
	s.writes = append(s.writes, c)
	if c.InviteDigest != "" {
		s.invites[c.InviteDigest] = c.Username
	}
	return nil
}
func (s *directoryTestStore) AppendAudit(_ context.Context, e ac.AuditEvent) error {
	s.audits = append(s.audits, e)
	return nil
}
func (s *directoryTestStore) ListAudit(context.Context, ac.AuditFilter) ([]ac.AuditEvent, error) {
	return s.audits, nil
}
func (s *directoryTestStore) InviteClaim(_ context.Context, digest string) (string, *time.Time, error) {
	if username := s.invites[digest]; username != "" {
		now := time.Now()
		return username, &now, nil
	}
	return "", nil, nil
}

func TestAccountCenterAdminRetiredLegacyWrites(t *testing.T) {
	now := time.Now()
	store := &directoryTestStore{directory: ac.AdminDirectory{
		People:      []ac.Identity{{Username: "boss", Role: ac.RoleFounder, Active: true}, {Username: "lead", Role: ac.RoleManager, Active: true}, {Username: "worker", Role: ac.RoleMember, Active: true}},
		Teams:       []ac.Team{{ID: "a", Name: "第一队", ManagerUsername: "lead"}},
		Memberships: []ac.Membership{{TeamID: "a", Username: "worker"}},
	}}
	store.resolve = func(_ context.Context, name string) (ac.Identity, error) {
		return (adminSnapshot{AdminDirectory: store.directory}).person(name), nil
	}
	mux := http.NewServeMux()
	auth := BridgeAuth{Secret: "secret", AccountCenterStore: store, Now: func() time.Time { return now }}
	RegisterAccountAdminReadCompat(mux, auth)
	RegisterAccountTransferCompat(mux, auth)
	RegisterTeamAdminCompat(mux, auth)
	RegisterMemberTeamWriteCompat(mux, auth)
	for _, tc := range []struct {
		actor, method, path string
		status              int
	}{
		{"boss", "POST", "/api/account-admin/accounts/worker/transfer", 410},
		{"boss", "POST", "/api/account-admin/accounts/missing/transfer", 403},
		{"lead", "POST", "/api/account-admin/accounts/worker/transfer", 403},
		{"lead", "PUT", "/api/team-admin/teams/a/members/worker/api-scopes", 410},
		{"lead", "PATCH", "/api/member/team/governance/worker", 410},
		{"lead", "POST", "/api/member/team/members/worker/reset-password", 410},
		{"lead", "PUT", "/api/member/team/members/missing/api-scopes", 403},
	} {
		resp := signedJSONRequest(t, mux, now, tc.actor, tc.method, tc.path, nil)
		if resp.Code != tc.status || tc.status == 410 && !strings.Contains(resp.Body.String(), `"path":"/accounts"`) {
			t.Fatalf("%s %s: %d %s", tc.actor, tc.path, resp.Code, resp.Body)
		}
	}
	if len(store.writes) != 0 || len(store.audits) != 0 {
		t.Fatalf("retired endpoints wrote: changes=%+v audits=%+v", store.writes, store.audits)
	}
}

func TestAccountCenterAdminLegacyInviteWritesCanonicalClaimAndAudit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "team-collaboration.json")
	digest := sha256.Sum256([]byte("synthetic-token"))
	raw := fmt.Sprintf(`{"invites":[{"id":"invite-id","tokenDigest":"%s","teamId":"t","managerUsername":"lead","createdBy":"lead","expiresAt":"%s"}]}`, hex.EncodeToString(digest[:]), time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &directoryTestStore{invites: map[string]string{}, directory: ac.AdminDirectory{
		People: []ac.Identity{{Username: "lead", Role: ac.RoleManager, Active: true}},
		Teams:  []ac.Team{{ID: "t", ManagerUsername: "lead", Name: "第一队"}},
	}}
	mux := http.NewServeMux()
	RegisterPublicTeamInviteCompat(mux, dir, store)
	for _, tc := range []struct {
		method, body string
		status       int
	}{
		{"POST", `{"username":"invitee","password":"sample-test-password"}`, 201},
		{"POST", `{"username":"second","password":"sample-test-password"}`, 409},
		{"GET", "", 200},
	} {
		resp := httptest.NewRecorder()
		mux.ServeHTTP(resp, httptest.NewRequest(tc.method, "/api/login/invite/synthetic-token", strings.NewReader(tc.body)))
		if resp.Code != tc.status || tc.method == "GET" && !strings.Contains(resp.Body.String(), `"available":false`) {
			t.Fatalf("%s = %d %s", tc.method, resp.Code, resp.Body)
		}
	}
	if len(store.writes) != 1 || store.writes[0].Action != "accept_invite" || len(store.audits) != 1 || store.audits[0].SubjectUsername != "invitee" {
		t.Fatalf("claim was not a canonical write: changes=%+v audits=%+v", store.writes, store.audits)
	}
	if bytes, err := os.ReadFile(path); err != nil || string(bytes) != raw {
		t.Fatalf("legacy invite source changed: %v", err)
	}
}

func TestAccountCenterAdminTeamLifecycle(t *testing.T) {
	now := time.Now()
	s := &directoryTestStore{directory: ac.AdminDirectory{People: []ac.Identity{
		{Username: "boss", Role: "founder", Active: true}, {Username: "dev", Role: "developer", Active: true}, {Username: "lead", Role: "manager", Active: true},
		{Username: "worker", Role: "member", ArchivedAt: &now}, {Username: "outsider", Role: "member", Active: true},
	}, Teams: []ac.Team{{ID: "a", Name: "第一队", ManagerUsername: "lead", CreatedBy: "dev"}}, Memberships: []ac.Membership{{TeamID: "a", Username: "worker"}, {TeamID: "a", Username: "lead"}}}}
	s.resolve = func(_ context.Context, name string) (ac.Identity, error) {
		for _, p := range s.directory.People {
			if p.Username == name {
				return p, nil
			}
		}
		return ac.Identity{}, ac.ErrNotFound
	}
	mux := http.NewServeMux()
	registerAccountCenterAdmin(mux, BridgeAuth{Secret: "test", AccountCenterStore: s})
	call := func(actor, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/account-center/admin/"+path, strings.NewReader(body))
		SignBridgeRequest(req, actor, false, time.Now(), "test")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	listed := call("lead", "GET", "teams", "")
	if listed.Code != 200 || !containsAll(listed.Body.String(), `"restore"`, `"balance":null`, `"monthConsumption":null`) ||
		strings.Contains(listed.Body.String(), "outsider") || strings.Contains(listed.Body.String(), "reset_password") {
		t.Fatalf("grouped directory %d %s", listed.Code, listed.Body.String())
	}
	for _, tc := range []struct {
		actor, path, body string
		status            int
	}{
		{"dev", "teams", `{"name":"新队","managerUsername":"newlead","password":"sample-test-password"}`, 200},
		{"dev", "accounts/worker/set_developer", "{}", 403}, {"dev", "accounts/boss/disable", "{}", 403}, {"dev", "accounts/missing/disable", "{}", 403},
		{"lead", "accounts/worker/restore", "{}", 200}, {"lead", "accounts/outsider/disable", "{}", 403},
		{"dev", "accounts/worker/purge_preflight", "{}", 403}, {"boss", "accounts/worker/purge_preflight", "{}", 200},
		{"dev", "teams/a/archive_team", "{}", 200},
		{"boss", "accounts/worker/purge", "{}", 409},
		{"boss", "accounts/worker/not_on_allowlist", "{}", 404},
	} {
		got := call(tc.actor, "POST", tc.path, tc.body)
		if got.Code != tc.status {
			t.Fatalf("%s %s = %d %s", tc.actor, tc.path, got.Code, got.Body.String())
		}
		if got.Code == 403 && !strings.Contains(got.Body.String(), "无权执行此操作") {
			t.Fatalf("non-uniform denial %s", got.Body.String())
		}
		if strings.HasSuffix(tc.path, "purge_preflight") && got.Code == 200 && (!strings.Contains(got.Body.String(), `"allowed":false`) || !strings.Contains(got.Body.String(), "unavailable")) {
			t.Fatalf("unsafe preflight %s", got.Body.String())
		}
	}
	if len(s.writes) != 3 || len(s.audits) < 3 {
		t.Fatalf("changes=%v audits=%v", s.writes, s.audits)
	}
	unknown := call("dev", "POST", "accounts/missing/disable", "{}")
	protected := call("dev", "POST", "accounts/boss/disable", "{}")
	if unknown.Body.String() != protected.Body.String() {
		t.Fatalf("target disclosure: missing=%s protected=%s", unknown.Body, protected.Body)
	}
	if audit := call("boss", "GET", "audit", ""); audit.Code != 200 || !containsAll(audit.Body.String(), `"create_team"`, `"archive_team"`, `"restore"`) {
		t.Fatalf("audit report %d %s", audit.Code, audit.Body)
	}
	for _, path := range []string{"teams", "teams/a/accounts", "teams/a/archive_team", "accounts/worker/restore", "accounts/worker/purge"} {
		if !impersonationBlocks("POST", "/api/account-center/admin/"+path) {
			t.Fatal("unprotected " + path)
		}
	}
}
