package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
	"qiantie/backend/internal/yzcmusage"
)

type failedSessionReservation struct {
	t     *testing.T
	calls int
}

func (m *failedSessionReservation) ReserveSessionAttempt(_ context.Context, r screenplay.AssistantSessionRequest, stage, team, actor, hash string) (string, error) {
	m.calls++
	if r.Owner != "writer" || r.Actor != "member" || stage != "initial" || team != "team-a" || actor != "founder" || hash != yzcmusage.Fingerprint("shared") {
		m.t.Fatal("attribution changed before reservation")
	}
	return "", errors.New("injected prewrite failure")
}
func (m *failedSessionReservation) ClaimSessionAttempt(context.Context, screenplay.AssistantSessionRequest, string, string, string, string) error {
	m.t.Fatal("prewrite failure reached dispatch")
	return errors.New("unexpected claim")
}

func TestSessionChannelFourRolesAndFailedReservation(t *testing.T) {
	accounts := &applicationTestStore{accounts: map[string]auth.Account{"member": {Username: "member", Active: true}, "manager": {Username: "manager", Active: true}}}
	catalog := func(_ context.Context, owner, selection string) (*batchfactoryv11.OpenAICompatibleProvider, error) {
		if owner != "manager" || selection != "chosen" {
			t.Fatal("wrong catalog selection")
		}
		return &batchfactoryv11.OpenAICompatibleProvider{Endpoint: yzcmusage.SessionChatEndpoint, Model: "chosen", APIKey: "shared"}, nil
	}
	var channel *ScreenplaySessionChannel
	repo := &failedSessionReservation{t: t}

	router, store, _ := newImpersonationTestRouter(t, func(opts *RouterOptions) {
		auth := BridgeAuth{Now: opts.Now, AuthStore: &authTestStore{account: auth.Account{Username: "founder", Active: true}, session: auth.Session{Token: "founder", Username: "founder", ExpiresAt: opts.Now().Add(time.Hour)}}, AccountCenterStore: &sessionAuthorityTestCenter{Store: opts.AccountCenterStore, version: "initial"}, SessionResolver: opts.SessionResolver}
		channel = NewScreenplaySessionChannel(accounts, sessionCatalogTeamFixture{opts.AccountCenterStore}, catalog, &yzcmusage.Tracker{})
		opts.RegisterV11 = func(mux *http.ServeMux) {
			mux.HandleFunc("POST "+impersonationBusinessTestPath, func(w http.ResponseWriter, r *http.Request) {
				g := sessionChannelGenerator{channel: channel, repo: repo, subject: "member", verify: sessionAuthorityCheck(auth, r, "member")}
				_, err := g.GenerateSessionStage(r.Context(), screenplay.AssistantSessionRequest{Owner: "writer", Actor: "member"}, "initial", screenplay.TextRequest{Model: "chosen"})
				if !errors.Is(err, yzcmusage.ErrSessionNotSent) {
					t.Fatal(err)
				}
				w.WriteHeader(http.StatusNoContent)
			})
		}
	})
	member := store.identities["member"]
	member.TeamID = "team-a"
	store.identities["member"] = member
	cookie := startImpersonationForTest(t, router, "founder", "member")
	req := httptest.NewRequest("POST", "https://local.test"+impersonationBusinessTestPath, strings.NewReader(`{"actor":"intruder"}`))
	req.Header.Set("Authorization", "Bearer founder")
	req.Header.Set("X-Actor", "intruder")
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent || repo.calls != 1 {
		t.Fatalf("status=%d, reservations=%d", w.Code, repo.calls)
	}
}

// Formal account-center membership determines credential ownership.
type sessionCatalogTeamFixture struct{ accountcenter.Store }

func (s sessionCatalogTeamFixture) ListTeams(context.Context, accountcenter.TeamFilter) ([]accountcenter.TeamSummary, error) {
	return []accountcenter.TeamSummary{{Team: accountcenter.Team{ID: "team-a", ManagerUsername: "manager"}}}, nil
}

func TestSessionChannelRechecksFormalModelOwner(t *testing.T) {
	center := newModelOwnerTestCenter()
	accounts := &applicationTestStore{accounts: map[string]auth.Account{"member": {Username: "member", Active: true}, "manager": {Username: "manager", Active: true}, "other": {Username: "other", Active: true}}}
	owners := []string{}
	catalog := func(_ context.Context, owner, model string) (*batchfactoryv11.OpenAICompatibleProvider, error) {
		owners = append(owners, owner)
		return &batchfactoryv11.OpenAICompatibleProvider{Endpoint: yzcmusage.SessionChatEndpoint, APIKey: "fixture", Model: model}, nil
	}
	channel := NewScreenplaySessionChannel(accounts, center, catalog, &yzcmusage.Tracker{})
	g := sessionChannelGenerator{channel: channel, repo: &failedSessionReservation{t: t}, subject: "member", verify: func(context.Context) error { return nil }}
	ctx := context.WithValue(context.Background(), bridgeIdentityKey{}, BridgeIdentity{Username: "member"})
	if _, err := g.provider(ctx, "chosen"); err != nil {
		t.Fatal(err)
	}
	center.manager = "other"
	if _, err := g.provider(ctx, "chosen"); err != nil {
		t.Fatal(err)
	}
	center.manager = ""
	if _, err := g.provider(ctx, "chosen"); !errors.Is(err, screenplay.ErrAccessDenied) {
		t.Fatal(err)
	}
	if strings.Join(owners, ",") != "manager,other" {
		t.Fatal(owners)
	}
}
