package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	ac "qiantie/backend/internal/accountcenter"
)

func TestAccountCenterPurgeCompatFollowsCanonicalPreflight(t *testing.T) {
	now := time.Unix(100, 0)
	store := &directoryTestStore{directory: ac.AdminDirectory{People: []ac.Identity{
		{Username: "owner", Role: ac.RoleFounder, Active: true},
		{Username: "dev", Role: ac.RoleDeveloper, Active: true},
		{Username: "member", Role: ac.RoleMember, ArchivedAt: &now},
	}}}
	store.resolve = func(_ context.Context, name string) (ac.Identity, error) {
		return (adminSnapshot{AdminDirectory: store.directory}).person(name), nil
	}
	mux := http.NewServeMux()
	auth := BridgeAuth{Secret: "test-secret", AccountCenterStore: store, Now: func() time.Time { return now }}
	RegisterAccountPurgeCompat(mux, auth)
	for actor, status := range map[string]int{"owner": http.StatusConflict, "dev": http.StatusForbidden} {
		request := httptest.NewRequest(http.MethodPost, "/api/account-recovery/purge/member", strings.NewReader("{}"))
		SignBridgeRequest(request, actor, actor == "owner", now, "test-secret")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != status {
			t.Fatalf("%s status=%d body=%s", actor, response.Code, response.Body.String())
		}
	}
	if len(store.writes) != 0 {
		t.Fatal("purge wrote despite unavailable checks")
	}
}
