package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"testing"
	"time"

	ac "qiantie/backend/internal/accountcenter"
)

func impersonationTargetDirectory(now time.Time) ac.AdminDirectory {
	archivedAt := now.Add(-time.Hour)
	endedAt := now.Add(-2 * time.Hour)
	return ac.AdminDirectory{
		People: []ac.Identity{
			{Username: "founder", DisplayName: "创始人", Role: ac.RoleFounder, Active: true},
			{Username: "developer", DisplayName: "开发者", Role: ac.RoleDeveloper, Active: true},
			{Username: "developer2", DisplayName: "其他开发者", Role: ac.RoleDeveloper, Active: true},
			{Username: "manager-a", DisplayName: "甲队管理者", Role: ac.RoleManager, Active: true},
			{Username: "member-a", DisplayName: "甲队组员", Role: ac.RoleMember, Active: true},
			{Username: "manager-b", DisplayName: "乙队管理者", Role: ac.RoleManager, Active: true},
			{Username: "member-b", DisplayName: "乙队组员", Role: ac.RoleMember, Active: true},
			{Username: "disabled", DisplayName: "停用组员", Role: ac.RoleMember},
			{Username: "archived", DisplayName: "归档组员", Role: ac.RoleMember, Active: true, ArchivedAt: &archivedAt},
			{Username: "historical", DisplayName: "历史组员", Role: ac.RoleMember, Active: true},
		},
		Teams: []ac.Team{
			{ID: "team-a", Name: "甲队", ManagerUsername: "manager-a", CreatedBy: "developer"},
			{ID: "team-b", Name: "乙队", ManagerUsername: "manager-b", CreatedBy: "founder"},
		},
		Memberships: []ac.Membership{
			{ID: 1, TeamID: "team-a", Username: "manager-a", JoinedAt: now.Add(-24 * time.Hour)},
			{ID: 2, TeamID: "team-a", Username: "member-a", JoinedAt: now.Add(-23 * time.Hour)},
			{ID: 3, TeamID: "team-a", Username: "disabled", JoinedAt: now.Add(-22 * time.Hour)},
			{ID: 4, TeamID: "team-a", Username: "archived", JoinedAt: now.Add(-21 * time.Hour)},
			{ID: 5, TeamID: "team-a", Username: "historical", JoinedAt: now.Add(-20 * time.Hour), EndedAt: &endedAt},
			{ID: 6, TeamID: "team-b", Username: "manager-b", JoinedAt: now.Add(-19 * time.Hour)},
			{ID: 7, TeamID: "team-b", Username: "member-b", JoinedAt: now.Add(-18 * time.Hour)},
		},
	}
}

type impersonationTargetsStore struct {
	*directoryTestStore
	sessions map[[32]byte]ac.Impersonation
}

func (s *impersonationTargetsStore) CreateImpersonation(_ context.Context, session ac.Impersonation) error {
	s.sessions[session.TokenHash] = session
	return nil
}

func (s *impersonationTargetsStore) FindImpersonation(_ context.Context, hash [32]byte, actor string, now time.Time) (ac.Impersonation, error) {
	session, ok := s.sessions[hash]
	if !ok || session.ActorUsername != actor || !now.Before(session.ExpiresAt) {
		return ac.Impersonation{}, ac.ErrNotFound
	}
	return session, nil
}

func (s *impersonationTargetsStore) RevokeImpersonation(_ context.Context, id, actor string, _ time.Time) error {
	for hash, session := range s.sessions {
		if session.ID == id && session.ActorUsername == actor {
			delete(s.sessions, hash)
			return nil
		}
	}
	return ac.ErrNotFound
}

func newImpersonationTargetsTestRouter(t *testing.T) (http.Handler, *impersonationTargetsStore) {
	t.Helper()
	now := time.Date(2026, 11, 28, 4, 0, 0, 0, time.UTC)
	directory := &directoryTestStore{directory: impersonationTargetDirectory(now)}
	store := &impersonationTargetsStore{directoryTestStore: directory, sessions: map[[32]byte]ac.Impersonation{}}
	store.resolve = func(_ context.Context, username string) (ac.Identity, error) {
		person := (adminSnapshot{AdminDirectory: store.directory}).person(username)
		if person.Username == "" {
			return ac.Identity{}, ac.ErrNotFound
		}
		return person, nil
	}
	router := NewRouter(RouterOptions{
		BridgeSecret: "target-test", Now: func() time.Time { return now }, AccountCenterStore: store,
		SessionResolver: func(_ context.Context, token string) (BridgeIdentity, error) {
			person, err := store.ResolveIdentity(context.Background(), token)
			if err != nil || !person.Active || person.ArchivedAt != nil {
				return BridgeIdentity{}, ac.ErrNotFound
			}
			return BridgeIdentity{Username: person.Username, IsOwner: person.Role == ac.RoleFounder}, nil
		},
	})
	return router, store
}

func TestImpersonationTargetsRespectActorScope(t *testing.T) {
	router, _ := newImpersonationTargetsTestRouter(t)
	for _, tc := range []struct {
		actor string
		want  []string
	}{
		{"founder", []string{"developer", "developer2", "historical", "manager-a", "manager-b", "member-a", "member-b"}},
		{"developer", []string{"manager-a", "member-a"}},
		{"manager-a", []string{"member-a"}},
	} {
		t.Run(tc.actor, func(t *testing.T) {
			response := impersonationRequest(router, http.MethodGet, impersonationPath+"/targets", tc.actor, "", nil)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d %s", response.Code, response.Body.String())
			}
			var payload struct {
				Groups []struct {
					Accounts []struct {
						Username string `json:"username"`
					} `json:"accounts"`
				} `json:"groups"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			got := []string{}
			for _, group := range payload.Groups {
				for _, account := range group.Accounts {
					got = append(got, account.Username)
				}
			}
			sort.Strings(got)
			sort.Strings(tc.want)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("targets = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestImpersonationTargetsRejectMembers(t *testing.T) {
	router, _ := newImpersonationTargetsTestRouter(t)
	response := impersonationRequest(router, http.MethodGet, impersonationPath+"/targets", "member-a", "", nil)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d %s, want 403", response.Code, response.Body.String())
	}
}

func TestImpersonationTargetsRejectActiveSwitch(t *testing.T) {
	router, _ := newImpersonationTargetsTestRouter(t)
	cookie := startImpersonationForTest(t, router, "founder", "member-a")
	response := impersonationRequest(router, http.MethodGet, impersonationPath+"/targets", "founder", "", cookie)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d %s, want 403", response.Code, response.Body.String())
	}
}
