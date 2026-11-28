package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/screenplay"
)

type sessionHistoryRecorder struct {
	lateSessionRequests
	saved int
}

func (r *sessionHistoryRecorder) FinishAssistantRequest(_ context.Context, _ screenplay.AssistantSessionRequest, h *screenplay.AssistantSessionHistory) error {
	if h != nil {
		r.saved += len(h.Messages) + len(h.Proposals)
	}
	return nil
}

type authoritySlowSessionGenerator struct {
	verify           func(context.Context) error
	started, release chan struct{}
	second           bool
	receipts         atomic.Int32
}

func (g *authoritySlowSessionGenerator) Generate(context.Context, screenplay.TextRequest) (screenplay.TextResponse, error) {
	return screenplay.TextResponse{}, errors.New("generic dispatch forbidden")
}
func (g *authoritySlowSessionGenerator) checkSessionAuthority(ctx context.Context) error {
	return g.verify(ctx)
}
func (g *authoritySlowSessionGenerator) GenerateSessionStage(ctx context.Context, _ screenplay.AssistantSessionRequest, stage string, _ screenplay.TextRequest) (screenplay.TextResponse, error) {
	if g.second && stage == "initial" {
		return screenplay.TextResponse{Content: `{"reply":"read","tool":"storyboard_failures","tool_args":{}}`}, nil
	}
	close(g.started)
	select {
	case <-g.release:
	case <-ctx.Done():
		return screenplay.TextResponse{}, ctx.Err()
	}
	g.receipts.Add(1) // receipt persists independently of permission to save content
	return screenplay.TextResponse{Content: `{"reply":"late answer"}`}, nil
}

func TestSessionLateReplyRejectsRevokedOriginalLogin(t *testing.T) {
	for _, stage := range []string{"initial", "read_reply"} {
		for _, restore := range []bool{false, true} {
			t.Run(stage+map[bool]string{false: "/disabled", true: "/reenabled"}[restore], func(t *testing.T) {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				login := &authTestStore{account: auth.Account{Username: "owner", Active: true}, session: auth.Session{Token: "original", Username: "owner", ExpiresAt: time.Now().Add(time.Hour)}}
				req := httptest.NewRequest(http.MethodPost, "https://local.test/session", nil)
				req.Header.Set("Authorization", "Bearer original")
				verify := sessionAuthorityCheck(BridgeAuth{AuthStore: login}, req, "owner")
				if err := verify(ctx); err != nil {
					t.Fatal(err)
				}
				store := screenplay.NewMemoryStore()
				if _, err := store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "owner"}); err != nil {
					t.Fatal(err)
				}
				repo := &sessionHistoryRecorder{}
				g := &authoritySlowSessionGenerator{verify: verify, started: make(chan struct{}), release: make(chan struct{}), second: stage == "read_reply"}
				done := make(chan error, 1)
				go func() {
					_, err := runSessionChat(ctx, repo, screenplay.AuthoringService{Store: store, Repository: screenplay.NewMemoryAuthoringRepository()}, g, "owner", "p", "owner", "first", 1, "hello")
					done <- err
				}()
				select {
				case <-g.started:
				case err := <-done:
					t.Fatalf("never dispatched: %v", err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				login.account.Active = false
				if err := login.RevokeSession(ctx, "original"); err != nil {
					t.Fatal(err)
				}
				if restore {
					login.account.Active = true
				}
				close(g.release)
				select {
				case err := <-done:
					if !errors.Is(err, screenplay.ErrAccessDenied) || repo.saved != 0 || g.receipts.Load() != 1 {
						t.Fatalf("late reply admitted: error=%v saved=%d receipts=%d", err, repo.saved, g.receipts.Load())
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			})
		}
	}
}

func TestSessionAuthorityRequiresRevocableOriginalLogin(t *testing.T) {
	now := time.Now()
	req := httptest.NewRequest(http.MethodPost, "https://local.test/session", nil)
	// A valid trusted bridge identity has no revocable auth session.

	SignBridgeRequest(req, "owner", true, now, "secret")
	authn := BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}
	if _, err := authn.authenticate(req); err != nil {
		t.Fatalf("valid legacy HMAC fixture rejected: %v", err)
	}
	verify := sessionAuthorityCheck(authn, req, "owner")
	if err := verify(context.Background()); !errors.Is(err, screenplay.ErrAccessDenied) {
		t.Fatal(err)
	}
}

func TestSessionLateReplyRejectsEndedOriginalImpersonation(t *testing.T) {
	for _, stage := range []string{"initial", "read_reply"} {
		t.Run(stage, func(t *testing.T) {
			var authn BridgeAuth
			router, center, now := newImpersonationTestRouter(t, func(opts *RouterOptions) {
				authn = BridgeAuth{Now: opts.Now, AccountCenterStore: &sessionAuthorityTestCenter{Store: opts.AccountCenterStore, version: "initial"}, AuthStore: &authTestStore{account: auth.Account{Username: "founder", Active: true}, session: auth.Session{Token: "founder", Username: "founder", ExpiresAt: opts.Now().Add(time.Hour)}}}
			})
			member := center.identities["member"]
			member.TeamID = "team-a"
			center.identities["member"] = member
			cookie := startImpersonationForTest(t, router, "founder", "member")
			originalID := ""
			for _, item := range center.sessions {
				originalID = item.ID
			}
			frozen := accountcenter.RequestIdentity{Actor: center.identities["founder"], Subject: member, ImpersonationID: originalID}
			ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), accountCenterIdentityKey{}, frozen), 5*time.Second)
			defer cancel()
			request := httptest.NewRequest("POST", "https://local.test/session", nil).WithContext(ctx)
			request.Header.Set("Authorization", "Bearer founder")
			request.AddCookie(cookie)
			verify := sessionAuthorityCheck(authn, request, "member")
			if err := verify(ctx); err != nil {
				t.Fatal(err)
			}
			store := screenplay.NewMemoryStore()
			if _, err := store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "member"}); err != nil {
				t.Fatal(err)
			}
			repo := &sessionHistoryRecorder{}
			g := &authoritySlowSessionGenerator{verify: verify, started: make(chan struct{}), release: make(chan struct{}), second: stage == "read_reply"}
			done := make(chan error, 1)
			go func() {
				_, err := runSessionChat(ctx, repo, screenplay.AuthoringService{Store: store, Repository: screenplay.NewMemoryAuthoringRepository()}, g, "member", "p", "member", "first", 1, "hello")
				done <- err
			}()
			select {
			case <-g.started:
			case err := <-done:
				t.Fatalf("not dispatched: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if err := center.RevokeImpersonation(ctx, originalID, "founder", *now); err != nil {
				t.Fatal(err)
			}
			// Even a newly granted impersonation cannot revive the original cookie/id.
			_ = startImpersonationForTest(t, router, "founder", "member")
			close(g.release)
			select {
			case err := <-done:
				if !errors.Is(err, screenplay.ErrAccessDenied) || repo.saved != 0 || g.receipts.Load() != 1 {
					t.Fatalf("ended impersonation admitted: error=%v saved=%d receipts=%d", err, repo.saved, g.receipts.Load())
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

type sessionAuthorityTestCenter struct {
	accountcenter.Store
	version     string
	snapshotErr error
}

func (s *sessionAuthorityTestCenter) SessionAuthorityVersion(context.Context, string, string, string) (string, error) {
	return s.version, s.snapshotErr
}
func (s *sessionAuthorityTestCenter) BusinessAllowed(ctx context.Context, username string) (bool, error) {
	return s.Store.(accountcenter.BusinessAccessReader).BusinessAllowed(ctx, username)
}

func TestSessionLateReplyRejectsRestoredBusinessRelationship(t *testing.T) {
	for _, change := range []string{"membership_end_rejoin_same_team", "team_archive_restore", "grant_revoke_regrant", "snapshot_read_failure"} {
		t.Run(change, func(t *testing.T) {
			center := &sessionAuthorityTestCenter{Store: &impersonationTestStore{identities: map[string]accountcenter.Identity{"owner": {Username: "owner", Role: accountcenter.RoleMember, Active: true, TeamID: "team-a"}}}, version: "membership:1/audit:1"}
			login := &authTestStore{account: auth.Account{Username: "owner", Active: true}, session: auth.Session{Token: "original", Username: "owner", ExpiresAt: time.Now().Add(time.Hour)}}
			person, _ := center.ResolveIdentity(context.Background(), "owner")
			frozen := accountcenter.RequestIdentity{Actor: person, Subject: person}
			ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), accountCenterIdentityKey{}, frozen), 5*time.Second)
			defer cancel()
			original := httptest.NewRequest("POST", "https://local.test/session", nil).WithContext(ctx)
			original.Header.Set("Authorization", "Bearer original")
			verify := sessionAuthorityCheck(BridgeAuth{AuthStore: login, AccountCenterStore: center}, original, "owner")
			if err := verify(ctx); err != nil {
				t.Fatal(err)
			}
			store := screenplay.NewMemoryStore()
			if _, err := store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "owner"}); err != nil {
				t.Fatal(err)
			}
			repo := &sessionHistoryRecorder{}
			g := &authoritySlowSessionGenerator{verify: verify, started: make(chan struct{}), release: make(chan struct{})}
			done := make(chan error, 1)
			go func() {
				_, err := runSessionChat(ctx, repo, screenplay.AuthoringService{Store: store, Repository: screenplay.NewMemoryAuthoringRepository()}, g, "owner", "p", "owner", "first", 1, "hello")
				done <- err
			}()
			select {
			case <-g.started:
			case err := <-done:
				t.Fatalf("not dispatched: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			switch change {
			case "membership_end_rejoin_same_team":
				center.version = "membership:2/audit:1"
			case "team_archive_restore":
				center.version = "membership:1/audit:3"
			case "grant_revoke_regrant":
				center.version = "membership:1/grant:replacement/audit:1"
			case "snapshot_read_failure":
				center.snapshotErr = errors.New("authority storage unavailable")
			}
			// Current Active, TeamID, BusinessAllowed and original login are unchanged.
			close(g.release)
			select {
			case err := <-done:
				if !errors.Is(err, screenplay.ErrAccessDenied) || repo.saved != 0 || g.receipts.Load() != 1 {
					t.Fatalf("restored business relationship admitted old reply: %v saved=%d receipts=%d", err, repo.saved, g.receipts.Load())
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func TestSessionFinalCommitRechecksOriginalAuthority(t *testing.T) {
	ctx := context.Background()
	store := screenplay.NewMemoryStore()
	if _, err := store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "owner"}); err != nil {
		t.Fatal(err)
	}
	checks := 0
	verify := func(context.Context) error {
		checks++
		if checks == 3 {
			return screenplay.ErrAccessDenied
		}
		return nil
	}
	repo := &sessionHistoryRecorder{}
	g := &authoritySlowSessionGenerator{verify: verify, started: make(chan struct{}), release: make(chan struct{})}
	close(g.release)
	_, err := runSessionChat(ctx, repo, screenplay.AuthoringService{Store: store, Repository: screenplay.NewMemoryAuthoringRepository()}, g, "owner", "p", "owner", "first", 1, "hello")
	if !errors.Is(err, screenplay.ErrAccessDenied) || checks != 3 || repo.saved != 0 || g.receipts.Load() != 1 {
		t.Fatalf("final authority check/cleanup failed: %v checks=%d saved=%d receipts=%d", err, checks, repo.saved, g.receipts.Load())
	}
}
