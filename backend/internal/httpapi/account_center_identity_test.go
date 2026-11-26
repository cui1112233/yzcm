package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"qiantie/backend/internal/accountcenter"
)

type accountCenterIdentityStore struct {
	accountcenter.Store
	resolve func(context.Context, string) (accountcenter.Identity, error)
}

func (s accountCenterIdentityStore) ResolveIdentity(ctx context.Context, username string) (accountcenter.Identity, error) {
	return s.resolve(ctx, username)
}

func TestAccountCenterIdentityRouterAttachesAuthenticatedActorOnce(t *testing.T) {
	t.Setenv("QIANTIE_ENABLE_LEGACY_V11", "1")
	now := time.Now()
	count := 0
	want := accountcenter.Identity{Username: "alice", Role: accountcenter.RoleManager, Active: true}
	store := accountCenterIdentityStore{resolve: func(_ context.Context, username string) (accountcenter.Identity, error) {
		count++
		if username != want.Username {
			t.Fatalf("account-center lookup for %q, want %q", username, want.Username)
		}
		return want, nil
	}}
	router := NewRouter(RouterOptions{
		BridgeSecret: "test-secret", Now: func() time.Time { return now }, AccountCenterStore: store,
		RegisterV11: func(mux *http.ServeMux) {
			mux.HandleFunc("GET /api/batch-factory/v11/test-account-identity", func(w http.ResponseWriter, r *http.Request) {
				identity, ok := AccountCenterIdentityFromContext(r.Context())
				if !ok || identity.Actor != want || identity.Subject != want || identity.ImpersonationID != "" {
					t.Errorf("account-center identity = %+v, present=%t", identity, ok)
				}
				legacy, ok := BridgeIdentityFromContext(r.Context())
				if !ok || !legacy.IsOwner || legacy.Username != want.Username {
					t.Errorf("legacy bridge identity = %+v, present=%t", legacy, ok)
				}
				w.WriteHeader(http.StatusNoContent)
			})
		},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/batch-factory/v11/test-account-identity", nil)
	// The signed legacy owner flag must never replace the role resolved from the store.
	SignBridgeRequest(request, "alice", true, now, "test-secret")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || count != 1 {
		t.Fatalf("response=%d %s, identity lookups=%d", response.Code, response.Body, count)
	}
}

func TestAccountCenterIdentityStrictRouteRejectsUnavailableIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		user accountcenter.Identity
		err  error
		want int
	}{
		{name: "missing", err: accountcenter.ErrNotFound, want: http.StatusForbidden},
		{name: "disabled", user: accountcenter.Identity{Username: "alice", Role: accountcenter.RoleMember}, want: http.StatusForbidden},
		{name: "wrong username", user: accountcenter.Identity{Username: "carol", Role: accountcenter.RoleMember, Active: true}, want: http.StatusForbidden},
		{name: "database unavailable", err: errors.New("internal database error"), want: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			auth := BridgeAuth{Secret: "test-secret", Now: func() time.Time { return now }, AccountCenterStore: accountCenterIdentityStore{
				resolve: func(context.Context, string) (accountcenter.Identity, error) { return tc.user, tc.err },
			}}
			request := httptest.NewRequest(http.MethodGet, "/account-test", nil)
			SignBridgeRequest(request, "alice", false, now, "test-secret")
			protected := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("strict route received an unresolved identity") })
			response := httptest.NewRecorder()
			auth.AccountCenterMiddleware(protected).ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("strict status = %d %s, want %d", response.Code, response.Body, tc.want)
			}
			legacy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := AccountCenterIdentityFromContext(r.Context()); ok {
					t.Error("unresolved account-center identity was published")
				}
				if identity, ok := BridgeIdentityFromContext(r.Context()); !ok || identity.Username != "alice" {
					t.Errorf("legacy bridge identity = %+v, present=%t", identity, ok)
				}
				w.WriteHeader(http.StatusNoContent)
			})
			response = httptest.NewRecorder()
			auth.Middleware(legacy).ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("legacy status = %d %s", response.Code, response.Body)
			}
		})
	}
}

func TestAccountCenterIdentityPolicyDenialsHideTargetExistence(t *testing.T) {
	now := time.Now()
	lookups := 0
	auth := BridgeAuth{Secret: "test-secret", Now: func() time.Time { return now }, AccountCenterStore: accountCenterIdentityStore{
		resolve: func(context.Context, string) (accountcenter.Identity, error) {
			lookups++
			return accountcenter.Identity{Username: "alice", Role: accountcenter.RoleManager, Active: true}, nil
		},
	}}
	responseFor := func(target accountcenter.PolicyTarget) *httptest.ResponseRecorder {
		handler := auth.AccountCenterMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if authorizeAccountCenter(w, r, accountcenter.ActionResetPassword, target) {
				w.WriteHeader(http.StatusNoContent)
			}
		}))
		request := httptest.NewRequest(http.MethodPost, "/account-test", nil)
		SignBridgeRequest(request, "alice", false, now, "test-secret")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	target := accountcenter.PolicyTarget{Identity: accountcenter.Identity{Username: "bob", Role: accountcenter.RoleMember, Active: true}}
	forbidden := responseFor(target)
	missing := responseFor(accountcenter.PolicyTarget{})
	if forbidden.Code != http.StatusForbidden || missing.Code != http.StatusForbidden || forbidden.Body.String() != missing.Body.String() {
		t.Fatalf("existing forbidden = %d %s, missing = %d %s", forbidden.Code, forbidden.Body, missing.Code, missing.Body)
	}
	target.InManagedTeam = true
	if allowed := responseFor(target); allowed.Code != http.StatusNoContent {
		t.Fatalf("permitted target = %d %s", allowed.Code, allowed.Body)
	}
	if lookups != 3 {
		t.Fatalf("performed %d identity lookups for 3 requests", lookups)
	}
}
