package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/batchfactoryv11/external"
	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/yzcmusage"
)

type impersonationTestStore struct {
	accountcenter.Store
	identities map[string]accountcenter.Identity
	sessions   map[[32]byte]accountcenter.Impersonation
	audits     []accountcenter.AuditEvent
	auditErr   error
	teamEnded  bool
	targetErr  error
}

func (s *impersonationTestStore) ResolveIdentity(_ context.Context, username string) (accountcenter.Identity, error) {
	if username == "member" && s.targetErr != nil {
		return accountcenter.Identity{}, s.targetErr
	}
	if identity, ok := s.identities[strings.ToLower(username)]; ok {
		return identity, nil
	}
	return accountcenter.Identity{}, accountcenter.ErrNotFound
}

func (s *impersonationTestStore) BusinessAllowed(_ context.Context, username string) (bool, error) {
	person, ok := s.identities[username]
	return ok && person.Active && person.ArchivedAt == nil, nil
}

func (s *impersonationTestStore) ListTeams(_ context.Context, filter accountcenter.TeamFilter) ([]accountcenter.TeamSummary, error) {
	if !s.teamEnded && filter.ManagerUsername == "manager" && filter.Username == "member" {
		return []accountcenter.TeamSummary{{Team: accountcenter.Team{ID: "one", ManagerUsername: "manager"}}}, nil
	}
	return nil, nil
}

func (s *impersonationTestStore) CreateImpersonation(_ context.Context, session accountcenter.Impersonation) error {
	s.sessions[session.TokenHash] = session
	return nil
}

func (s *impersonationTestStore) FindImpersonation(_ context.Context, hash [32]byte, actor string, now time.Time) (accountcenter.Impersonation, error) {
	session, found := s.sessions[hash]
	if !found || session.ActorUsername != actor || !now.Before(session.ExpiresAt) {
		return accountcenter.Impersonation{}, accountcenter.ErrNotFound
	}
	return session, nil
}

func (s *impersonationTestStore) RevokeImpersonation(_ context.Context, id, actor string, _ time.Time) error {
	for hash, session := range s.sessions {
		if session.ID == id && session.ActorUsername == actor {
			delete(s.sessions, hash)
			return nil
		}
	}
	return accountcenter.ErrNotFound
}

func (s *impersonationTestStore) AppendAudit(_ context.Context, event accountcenter.AuditEvent) error {
	if s.auditErr != nil {
		return s.auditErr
	}
	s.audits = append(s.audits, event)
	return nil
}

func newImpersonationTestRouter(t *testing.T, configure ...func(*RouterOptions)) (http.Handler, *impersonationTestStore, *time.Time) {
	t.Helper()
	t.Setenv("QIANTIE_ENABLE_LEGACY_V11", "1")
	now := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
	store := &impersonationTestStore{identities: map[string]accountcenter.Identity{}, sessions: map[[32]byte]accountcenter.Impersonation{}}
	for username, role := range map[string]string{
		"founder": accountcenter.RoleFounder, "dev": accountcenter.RoleDeveloper, "dev2": accountcenter.RoleDeveloper,
		"manager": accountcenter.RoleManager, "manager2": accountcenter.RoleManager,
		"member": accountcenter.RoleMember, "member2": accountcenter.RoleMember,
	} {
		store.identities[username] = accountcenter.Identity{Username: username, Role: role, Active: true}
	}
	store.identities["disabled"] = accountcenter.Identity{Username: "disabled", Role: accountcenter.RoleMember}
	archivedAt := now.Add(-time.Hour)
	store.identities["archived"] = accountcenter.Identity{Username: "archived", Role: accountcenter.RoleMember, Active: true, ArchivedAt: &archivedAt}
	novels := novelfetchworkshop.NewMemoryStore()
	for _, owner := range []string{"founder", "manager", "member"} {
		if err := novels.PutConfig(context.Background(), owner, map[string]any{
			"appConfig": map[string]any{"accountTestMarker": "scope-" + owner},
		}); err != nil {
			t.Fatal(err)
		}
	}
	options := RouterOptions{
		BridgeSecret: "test-secret", Now: func() time.Time { return now }, AccountCenterStore: store, NovelFetchStore: novels,
		SessionResolver: func(_ context.Context, token string) (BridgeIdentity, error) {
			username, _, _ := strings.Cut(token, ":")
			if identity, ok := store.identities[username]; ok && identity.Active {
				return BridgeIdentity{Username: username, IsOwner: username == "founder"}, nil
			}
			return BridgeIdentity{}, accountcenter.ErrNotFound
		},
		RegisterV11: func(mux *http.ServeMux) {
			mux.HandleFunc("POST /api/batch-factory/v11/impersonation-test-work", func(w http.ResponseWriter, r *http.Request) {
				bridge, _ := BridgeIdentityFromContext(r.Context())
				identity, _ := AccountCenterIdentityFromContext(r.Context())
				writeJSON(w, http.StatusOK, map[string]any{
					"owner": bridge.Username, "ownerPrivilege": bridge.IsOwner, "actor": identity.Actor.Username,
					"subject": identity.Subject.Username, "impersonationId": identity.ImpersonationID,
				})
			})
		},
	}
	for _, apply := range configure {
		apply(&options)
	}
	return NewRouter(options), store, &now
}

func impersonationRequest(router http.Handler, method, path, token, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://local.test"+path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

const impersonationTestPath = "/api/account-impersonation"
const impersonationCurrentTestPath = impersonationTestPath + "/current"
const impersonationBusinessTestPath = "/api/batch-factory/v11/impersonation-test-work"

func startImpersonationForTest(t *testing.T, router http.Handler, actor, subject string) *http.Cookie {
	t.Helper()
	w := impersonationRequest(router, http.MethodPost, impersonationTestPath, actor, `{"username":"`+subject+`"}`, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("start status = %d %s, want 201", w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Value == "" || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge <= 0 || cookies[0].MaxAge > 30*60 {
		t.Fatalf("invalid cookie attributes: %v", w.Header().Get("Set-Cookie"))
	}
	if strings.Contains(w.Body.String(), cookies[0].Value) {
		t.Fatal("cookie token was exposed in the response body")
	}
	return cookies[0]
}

func TestImpersonationPermissions(t *testing.T) {
	for _, tc := range []struct {
		actor, target string
		want          int
	}{
		{"founder", "dev", 201}, {"founder", "manager", 201}, {"founder", "member2", 201},
		{"founder", "FOUNDER", 403}, {"founder", "disabled", 403}, {"founder", "archived", 403}, {"founder", "missing", 403},
		{"dev", "manager2", 201}, {"dev", "member2", 201}, {"dev", "founder", 403}, {"dev", "dev2", 403},
		{"manager", "member", 201}, {"manager", "member2", 403}, {"manager", "manager2", 403}, {"manager", "dev", 403},
		{"member", "member2", 403}, {"", "member", 401},
	} {
		t.Run(tc.actor+"/"+tc.target, func(t *testing.T) {
			router, store, _ := newImpersonationTestRouter(t)
			w := impersonationRequest(router, http.MethodPost, impersonationTestPath, tc.actor, `{"username":"`+tc.target+`"}`, nil)
			if w.Code != tc.want {
				t.Fatalf("status = %d %s, want %d", w.Code, w.Body, tc.want)
			}
			if tc.want != 201 && (len(w.Result().Cookies()) != 0 || len(store.sessions) != 0) {
				t.Fatal("a denied switch created a session")
			}
		})
	}
}

func TestEnergyAttributionFromAuthenticatedAccountSwitch(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tracker := &yzcmusage.Tracker{Store: yzcmusage.Store{DB: db}, ReceiptDir: t.TempDir()}
	expectActiveUsageReceiptBinding(t, mock, tracker.ReceiptDir)
	const path = "/api/batch-factory/v11/energy-attribution-test"
	requestID := "switch-receipt"
	transport := tracker.Wrap(roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Oneapi-Request-Id": []string{requestID}}}, nil
	}))
	router, store, _ := newImpersonationTestRouter(t, func(options *RouterOptions) {
		options.RegisterV11 = func(mux *http.ServeMux) {
			mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
				ctx := yzcmusage.WithIdentity(r.Context(), "credential-owner", "")
				out, err := http.NewRequestWithContext(ctx, http.MethodPost, yzcmusage.Origin+"/v1/chat/completions", nil)
				if err != nil {
					t.Fatal(err)
				}
				out.Header.Set("Authorization", "Bearer fixture-key")
				if _, err := transport.RoundTrip(out); err != nil {
					t.Fatal(err)
				}
				w.WriteHeader(http.StatusNoContent)
			})
		}
	})
	member := store.identities["member"]
	member.TeamID = "team-a"
	store.identities[member.Username] = member
	cookie := startImpersonationForTest(t, router, "founder", "member")
	mock.ExpectExec("INSERT INTO yzcm_usage_requests").
		WithArgs(requestID, "member", "team-a", "founder", "shuihuo-production", yzcmusage.Fingerprint("fixture-key"), sqlmock.AnyArg(), 200).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if w := impersonationRequest(router, http.MethodPost, path, "founder", "", cookie); w.Code != http.StatusNoContent {
		t.Fatalf("switched route returned %d: %s", w.Code, w.Body)
	}
	requestID = "regular-receipt"
	mock.ExpectExec("INSERT INTO yzcm_usage_requests").
		WithArgs(requestID, "member", "team-a", "member", "shuihuo-production", yzcmusage.Fingerprint("fixture-key"), sqlmock.AnyArg(), 200).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if w := impersonationRequest(router, http.MethodPost, path, "member", "", nil); w.Code != http.StatusNoContent {
		t.Fatalf("regular route returned %d: %s", w.Code, w.Body)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func expectActiveUsageReceiptBinding(t *testing.T, mock sqlmock.Sqlmock, dir string) {
	t.Helper()
	storeID := strings.Repeat("a", 32)
	if runtime.GOOS == "windows" {
		dir = strings.ToLower(dir)
	}
	rows := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"path_digest", "store_id", "state", "origin", "filesystem_identity", "sync_parent_levels"}).
			AddRow(yzcmusage.Fingerprint(dir), storeID, "pending", "upgrade", "", 1)
	}
	mock.ExpectQuery("SELECT path_digest,store_id,state,origin").WithArgs("member-system").WillReturnRows(rows())
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT path_digest,store_id,state,origin.*FOR UPDATE").WithArgs("member-system").WillReturnRows(rows())
	mock.ExpectExec("UPDATE yzcm_usage_receipt_bindings SET filesystem_identity=").WithArgs(sqlmock.AnyArg(), "member-system").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
}

func TestImpersonationUsesSubjectAndAuditsActorUntilExit(t *testing.T) {
	router, store, _ := newImpersonationTestRouter(t)
	cookie := startImpersonationForTest(t, router, "founder", "member")
	status := impersonationRequest(router, http.MethodGet, impersonationCurrentTestPath, "founder", "", cookie)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"active":true`) || !strings.Contains(status.Body.String(), `"canSwitch":false`) {
		t.Fatalf("active status = %d %s", status.Code, status.Body)
	}
	w := impersonationRequest(router, http.MethodPost, impersonationBusinessTestPath, "founder", "", cookie)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != http.StatusOK || result["owner"] != "member" || result["ownerPrivilege"] != false || result["actor"] != "founder" || result["subject"] != "member" || result["impersonationId"] == "" {
		t.Fatalf("switched business identity = %d %s", w.Code, w.Body)
	}
	w = impersonationRequest(router, http.MethodDelete, impersonationCurrentTestPath, "founder", "", cookie)
	cleared := w.Result().Cookies()
	if w.Code != http.StatusOK || len(cleared) != 1 || cleared[0].MaxAge >= 0 || len(store.sessions) != 0 {
		t.Fatalf("exit = %d %s, clear cookie=%t, stored sessions=%d", w.Code, w.Body, len(cleared) == 1, len(store.sessions))
	}
	w = impersonationRequest(router, http.MethodPost, impersonationBusinessTestPath, "founder", "", nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"owner":"founder"`) || !strings.Contains(w.Body.String(), `"impersonationId":""`) {
		t.Fatalf("restored identity = %d %s", w.Code, w.Body)
	}
	if len(store.audits) != 3 || store.audits[0].Action != "impersonation.start" || store.audits[2].Action != "impersonation.stop" {
		t.Fatalf("audit events = %+v", store.audits)
	}
	for _, event := range store.audits {
		if event.ActorUsername != "founder" || event.SubjectUsername != "member" || event.ImpersonationID != result["impersonationId"] {
			t.Fatalf("audit actor/subject/session = %+v", event)
		}
	}
}

func TestImpersonationLoginAndNovelRoutesIgnoreActorHeaders(t *testing.T) {
	router, store, _ := newImpersonationTestRouter(t)
	cookie := startImpersonationForTest(t, router, "founder", "manager")
	if len(store.sessions) != 1 {
		t.Fatalf("stored sessions = %d", len(store.sessions))
	}
	for hash, session := range store.sessions {
		if hash == [32]byte{} || hash == sha256.Sum256([]byte(cookie.Value)) || strings.Contains(session.ID, cookie.Value) {
			t.Fatal("session is represented by a token that is not independently tied to the login")
		}
	}
	login := impersonationRequest(router, http.MethodGet, "/api/login/session", "founder", "", cookie)
	if login.Code != http.StatusOK || !containsAll(login.Body.String(), `"username":"manager"`, `"isOwner":false`, `"impersonationId"`, `"canonicalRole":"管理者"`, `"capabilities":[]`) {
		t.Fatalf("effective session = %d %s", login.Code, login.Body)
	}
	for _, path := range []string{"/api/novel-fetch-workshop/config", "/api/batch-rewrite/config"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", "Bearer founder")
		r.Header.Set(HeaderNovelFetchAccount, "member")
		r.Header.Set(HeaderUsername, "founder")
		r.Header.Set(HeaderIsOwner, "true")
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"accountTestMarker":"scope-manager"`) || strings.Contains(w.Body.String(), "scope-founder") {
			t.Fatalf("switched novel config scope at %s = %d %s", path, w.Code, w.Body)
		}
	}
}

func TestImpersonationRechecksMembershipBeforeEveryBusinessRequest(t *testing.T) {
	router, store, _ := newImpersonationTestRouter(t)
	cookie := startImpersonationForTest(t, router, "manager", "member")
	store.teamEnded = true
	w := impersonationRequest(router, http.MethodPost, impersonationBusinessTestPath, "manager", "", cookie)
	if w.Code != http.StatusConflict || len(store.audits) != 1 {
		t.Fatalf("stale switch mutation = %d %s, audit events = %d", w.Code, w.Body, len(store.audits))
	}
	store.teamEnded = false
	status := impersonationRequest(router, http.MethodGet, impersonationCurrentTestPath, "manager", "", cookie)
	if len(store.sessions) != 0 || !strings.Contains(status.Body.String(), `"active":false`) || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("restored team revived an invalidated session: %d %s, old session records = %d, clear cookie = %t", status.Code, status.Body, len(store.sessions), len(w.Result().Cookies()) != 0)
	}
}

func TestImpersonationStatusRevokesDisallowedTargetButPreservesTemporaryFailure(t *testing.T) {
	router, store, _ := newImpersonationTestRouter(t)
	cookie := startImpersonationForTest(t, router, "founder", "member")
	store.targetErr = context.DeadlineExceeded
	status := impersonationRequest(router, http.MethodGet, impersonationCurrentTestPath, "founder", "", cookie)
	if status.Code != http.StatusServiceUnavailable || len(store.sessions) != 1 || len(status.Result().Cookies()) != 0 {
		t.Fatalf("temporary failure discarded the switch: %d %s", status.Code, status.Body)
	}
	store.targetErr = nil
	member := store.identities["member"]
	disabled := member
	disabled.Active = false
	store.identities["member"] = disabled
	status = impersonationRequest(router, http.MethodGet, impersonationCurrentTestPath, "founder", "", cookie)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"active":false`) || len(store.sessions) != 0 || len(status.Result().Cookies()) != 1 || status.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("disabled account has a live switch: %d %s, sessions = %d", status.Code, status.Body, len(store.sessions))
	}
	store.identities["member"] = member
	status = impersonationRequest(router, http.MethodGet, impersonationCurrentTestPath, "founder", "", cookie)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"active":false`) {
		t.Fatalf("reactivation restored the old switch: %d %s", status.Code, status.Body)
	}
}

func TestImpersonationExpectedIDNeverFallsBackToActor(t *testing.T) {
	router, store, now := newImpersonationTestRouter(t)
	cookie := startImpersonationForTest(t, router, "founder", "member")
	var id string
	for _, session := range store.sessions {
		id = session.ID
	}
	request := func(expect string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, impersonationBusinessTestPath, nil)
		r.Header.Set("Authorization", "Bearer founder")
		if expect != "" {
			r.Header.Set("X-Qiantie-Expected-Actor", "founder")
			r.Header.Set("X-Qiantie-Expected-Impersonation", expect)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := request(id, cookie); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"owner":"member"`) {
		t.Fatalf("expected active switch = %d %s", w.Code, w.Body)
	}
	if w := request("different-switch-id", cookie); w.Code != http.StatusConflict {
		t.Errorf("mismatched switch wrote business data: %d %s", w.Code, w.Body)
	}
	*now = now.Add(31 * time.Minute)
	// The expired cookie is absent, as it would be in a real browser.
	if w := request(id, nil); w.Code != http.StatusConflict {
		t.Errorf("expired page wrote business data: %d %s", w.Code, w.Body)
	}
	if w := request("", nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"owner":"founder"`) {
		t.Errorf("regular actor write = %d %s", w.Code, w.Body)
	}
}

func TestImpersonationRejectsCrossTabActorAndSwitchChanges(t *testing.T) {
	router, store, _ := newImpersonationTestRouter(t)
	status := impersonationRequest(router, http.MethodGet, impersonationCurrentTestPath, "founder", "", nil)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"actorUsername":"founder"`) {
		t.Errorf("initial status did not confirm the real actor: %d %s", status.Code, status.Body)
	}
	cookie := startImpersonationForTest(t, router, "founder", "member")
	status = impersonationRequest(router, http.MethodGet, impersonationCurrentTestPath, "founder", "", cookie)
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"actorUsername":"founder"`) || !strings.Contains(status.Body.String(), `"username":"member"`) {
		t.Errorf("active status did not distinguish actor and effective subject: %d %s", status.Code, status.Body)
	}
	otherActorCookie := startImpersonationForTest(t, router, "dev", "member")
	var otherActorSwitchID string
	for _, session := range store.sessions {
		if session.ActorUsername == "dev" {
			otherActorSwitchID = session.ID
		}
	}
	for _, tc := range []struct {
		name, token, actor, expectation string
		cookie                          *http.Cookie
		want                            int
	}{
		{"another tab signed in as a different actor", "dev", "founder", "none", nil, http.StatusConflict},
		{"a valid switch of another login cannot satisfy actor expectation", "dev", "founder", otherActorSwitchID, otherActorCookie, http.StatusConflict},
		{"another tab started switching", "founder", "founder", "none", cookie, http.StatusConflict},
		{"missing either expectation", "founder", "founder", "", nil, http.StatusConflict},
		{"same actor and no switch", "founder", "founder", "none", nil, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, impersonationBusinessTestPath, nil)
			r.Header.Set("Authorization", "Bearer "+tc.token)
			r.Header.Set("X-Qiantie-Expected-Actor", tc.actor)
			if tc.expectation != "" {
				r.Header.Set("X-Qiantie-Expected-Impersonation", tc.expectation)
			}
			if tc.cookie != nil {
				r.AddCookie(tc.cookie)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Errorf("write = %d %s, want %d", w.Code, w.Body, tc.want)
			}
			if tc.want == http.StatusConflict && strings.Contains(w.Body.String(), `"owner"`) {
				t.Error("a mismatched write reached the business handler")
			}
		})
	}
	if len(store.audits) != 2 {
		t.Errorf("mismatched write reached the switched business audit: %+v", store.audits)
	}
}

func TestImpersonationControlsRejectStalePageWithoutChangingCurrentSession(t *testing.T) {
	router, store, _ := newImpersonationTestRouter(t)
	control := func(method, path, expectedActor, expectedID string, cookie *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(`{"username":"member"}`))
		r.Header.Set("Authorization", "Bearer dev")
		r.Header.Set("X-Qiantie-Expected-Actor", expectedActor)
		r.Header.Set("X-Qiantie-Expected-Impersonation", expectedID)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if w := control(http.MethodPost, impersonationTestPath, "founder", "none", nil); w.Code != http.StatusConflict || len(store.sessions) != 0 || len(w.Result().Cookies()) != 0 {
		t.Errorf("cross-tab start = %d %s, sessions = %d, Set-Cookie headers = %d", w.Code, w.Body, len(store.sessions), len(w.Result().Cookies()))
	}
	router, store, _ = newImpersonationTestRouter(t)
	if w := control(http.MethodPost, impersonationTestPath, "dev", strings.Repeat("A", 43), nil); w.Code != http.StatusConflict || len(store.sessions) != 0 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("start from a switched expectation = %d %s, sessions = %d", w.Code, w.Body, len(store.sessions))
	}
	started := control(http.MethodPost, impersonationTestPath, "dev", "none", nil)
	if started.Code != http.StatusCreated || len(started.Result().Cookies()) != 1 || len(store.sessions) != 1 {
		t.Fatalf("matching start = %d %s", started.Code, started.Body)
	}
	var id string
	for _, session := range store.sessions {
		id = session.ID
	}
	cookie := started.Result().Cookies()[0]
	if w := control(http.MethodDelete, impersonationCurrentTestPath, "dev", id, nil); w.Code != http.StatusConflict || len(store.sessions) != 1 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("stop without the current cookie = %d %s, sessions = %d", w.Code, w.Body, len(store.sessions))
	}
	for _, tc := range []struct{ actor, id string }{{"founder", id}, {"dev", "none"}, {"dev", strings.Repeat("A", 43)}} {
		if w := control(http.MethodDelete, impersonationCurrentTestPath, tc.actor, tc.id, cookie); w.Code != http.StatusConflict || len(store.sessions) != 1 || len(w.Result().Cookies()) != 0 {
			t.Fatalf("stale stop = %d %s, sessions = %d, Set-Cookie headers = %d", w.Code, w.Body, len(store.sessions), len(w.Result().Cookies()))
		}
	}
	if w := control(http.MethodDelete, impersonationCurrentTestPath, "dev", id, cookie); w.Code != http.StatusOK || len(store.sessions) != 0 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("matching stop = %d %s", w.Code, w.Body)
	}
}

func TestImpersonationRejectsBlankOrMalformedExpectationHeaders(t *testing.T) {
	router, _, _ := newImpersonationTestRouter(t)
	for _, tc := range []struct {
		name    string
		headers map[string]string
	}{
		{"actor only empty", map[string]string{"X-Qiantie-Expected-Actor": ""}},
		{"switch only whitespace", map[string]string{"X-Qiantie-Expected-Impersonation": " \t "}},
		{"both blank", map[string]string{"X-Qiantie-Expected-Actor": " ", "X-Qiantie-Expected-Impersonation": ""}},
		{"actor blank", map[string]string{"X-Qiantie-Expected-Actor": "", "X-Qiantie-Expected-Impersonation": "none"}},
		{"switch blank", map[string]string{"X-Qiantie-Expected-Actor": "founder", "X-Qiantie-Expected-Impersonation": " "}},
		{"switch malformed", map[string]string{"X-Qiantie-Expected-Actor": "founder", "X-Qiantie-Expected-Impersonation": strings.Repeat("!", 43)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{impersonationBusinessTestPath, impersonationTestPath, impersonationCurrentTestPath} {
				method := http.MethodPost
				if path == impersonationCurrentTestPath {
					method = http.MethodDelete
				}
				r := httptest.NewRequest(method, path, strings.NewReader(`{"username":"member"}`))
				r.Header.Set("Authorization", "Bearer founder")
				for name, value := range tc.headers {
					r.Header.Set(name, value)
				}
				w := httptest.NewRecorder()
				router.ServeHTTP(w, r)
				if w.Code != http.StatusConflict || len(w.Result().Cookies()) != 0 {
					t.Errorf("%s %s = %d %s, Set-Cookie headers = %d", method, path, w.Code, w.Body, len(w.Result().Cookies()))
				}
			}
		})
	}
}

func TestImpersonationBlocksV11AndV12CredentialWrites(t *testing.T) {
	registry := batchfactoryv11.NewMemoryVideoProviderRegistry()
	credentials := external.NewMemoryStore()
	publish := &external.Service{Credentials: credentials, Enabled: map[external.Provider]bool{external.Provider121: true}, Key: bytes.Repeat([]byte{4}, 32)}
	router, _, _ := newImpersonationTestRouter(t, func(options *RouterOptions) {
		options.Slice = 6
		options.Production = &batchfactoryv11.ProductionService{ProviderRegistry: registry}
		options.External = publish
	})
	cookie := startImpersonationForTest(t, router, "founder", "manager")
	for _, version := range []string{"v11", "v12"} {
		t.Run(version, func(t *testing.T) {
			ctx := context.Background()
			prefix := "/api/batch-factory/" + version
			for _, write := range []struct{ path, original, replacement string }{
				{prefix + "/video-provider/config", `{"provider":"personal_api","apiKey":"previous-test-key"}`, `{"provider":"personal_api","apiKey":"replacement-test-key"}`},
				{prefix + "/publish/121/credential", `{"name":"previous","secret":"previous-test-secret"}`, `{"name":"replacement","secret":"replacement-test-secret"}`},
			} {
				if w := impersonationRequest(router, http.MethodPut, write.path, "manager", write.original, nil); w.Code != http.StatusOK {
					t.Fatalf("unswitched credential write at %s = %d %s", write.path, w.Code, w.Body)
				}
				if w := impersonationRequest(router, http.MethodPut, write.path, "founder", write.replacement, cookie); w.Code != http.StatusForbidden {
					t.Errorf("switched credential write at %s = %d %s", write.path, w.Code, w.Body)
				}
			}
			config, err := registry.Resolve(ctx, "manager", batchfactoryv11.VideoProviderPersonalAPI)
			if err != nil || config.APIKey != "previous-test-key" {
				t.Error("existing video provider key was not preserved")
			}
			stored, err := credentials.GetCredential(ctx, "manager", external.Provider121)
			if err != nil {
				t.Fatal(err)
			}
			secret, err := external.DecryptCredential(publish.Key, stored.Nonce, stored.Ciphertext)
			if err != nil || string(secret) != "previous-test-secret" || stored.Name != "previous" {
				t.Error("existing publish credential was not preserved")
			}
		})
	}
}

func TestImpersonationRejectsMutationsWhenAuditUnavailable(t *testing.T) {
	router, store, _ := newImpersonationTestRouter(t)
	store.auditErr = context.DeadlineExceeded
	w := impersonationRequest(router, http.MethodPost, impersonationTestPath, "founder", `{"username":"member"}`, nil)
	if w.Code != http.StatusServiceUnavailable || len(w.Result().Cookies()) != 0 || len(store.sessions) != 0 {
		t.Fatalf("unaudited start = %d %s, sessions = %d", w.Code, w.Body, len(store.sessions))
	}
	store.auditErr = nil
	cookie := startImpersonationForTest(t, router, "founder", "member")
	store.auditErr = context.DeadlineExceeded
	w = impersonationRequest(router, http.MethodPost, impersonationBusinessTestPath, "founder", "", cookie)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unaudited business write = %d %s", w.Code, w.Body)
	}
}

func TestImpersonationRejectsReusedSessionAndExpires(t *testing.T) {
	router, _, now := newImpersonationTestRouter(t)
	cookie := startImpersonationForTest(t, router, "founder", "member")
	for _, tc := range []struct{ token, method, path, body string }{
		{"founder:another-session", http.MethodGet, impersonationCurrentTestPath, ""},
		{"dev", http.MethodGet, impersonationCurrentTestPath, ""},
		{"founder", http.MethodPost, impersonationTestPath, `{"username":"member2"}`},
	} {
		w := impersonationRequest(router, tc.method, tc.path, tc.token, tc.body, cookie)
		if tc.method == http.MethodGet && (w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"active":false`)) {
			t.Fatalf("cookie worked for a different session: %d %s", w.Code, w.Body)
		}
		if tc.method == http.MethodPost && w.Code != http.StatusConflict {
			t.Fatalf("nested switch = %d %s", w.Code, w.Body)
		}
	}
	*now = now.Add(31 * time.Minute)
	w := impersonationRequest(router, http.MethodGet, impersonationCurrentTestPath, "founder", "", cookie)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"active":false`) || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("expired status = %d %s", w.Code, w.Body)
	}
}

func TestImpersonationRequiresLoginAndEndsOnLogout(t *testing.T) {
	router, store, now := newImpersonationTestRouter(t)
	r := httptest.NewRequest(http.MethodPost, impersonationTestPath, strings.NewReader(`{"username":"member"}`))
	SignBridgeRequest(r, "founder", true, *now, "test-secret")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("signed service request started a switch: %d %s", w.Code, w.Body)
	}
	cookie := startImpersonationForTest(t, router, "founder", "member")
	w = impersonationRequest(router, http.MethodPost, "/api/login/logout", "founder", "", cookie)
	if w.Code != http.StatusNoContent || len(store.sessions) != 0 || len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatalf("logout left switch active: %d %s, sessions = %d", w.Code, w.Body, len(store.sessions))
	}
}

func TestImpersonationBlocksSensitiveRoutes(t *testing.T) {
	router, _, _ := newImpersonationTestRouter(t)
	cookie := startImpersonationForTest(t, router, "founder", "manager")
	for _, tc := range []struct{ method, path string }{
		{"POST", "/api/member/security/password"}, {"POST", "/api/admin/accounts/member/reset-password"},
		{"POST", "/api/account-recovery/password/reset"}, {"POST", "/api/member/security/mfa/setup"},
		{"DELETE", "/api/account-recovery/passkeys/passkey"}, {"PATCH", "/api/account-admin/accounts/member"},
		{"POST", "/api/account-admin/accounts/member/transfer"}, {"PUT", "/api/team-admin/teams/one/co-managers"},
		{"POST", "/api/settings/api-integrations/reveal"}, {"POST", "/api/settings/api-integrations"},
		{"POST", "/api/config"}, {"POST", "/api/member/te%61m/members/member/api"},
		{"PUT", "/api/novel-fetch-workshop/config"}, {"POST", "/api/batch-rewrite/config"},
		{"POST", "/api/account-center/admin/accounts"}, {"POST", "/api/account-center/admin/teams"},
		{"POST", "/api/account-center/admin/teams/one/accounts"}, {"POST", "/api/account-center/admin/teams/one/archive_team"},
		{"POST", "/api/account-center/admin/teams/one/restore_team"}, {"POST", "/api/login/invite/synthetic-token"},
		{"POST", "/api/account-center/admin/accounts/member/set_member"}, {"POST", "/api/account-center/admin/accounts/member/set_manager"},
		{"POST", "/api/account-center/admin/accounts/member/set_developer"}, {"POST", "/api/account-center/admin/accounts/member/enable"},
		{"POST", "/api/account-center/admin/accounts/member/disable"}, {"POST", "/api/account-center/admin/accounts/member/archive"},
		{"POST", "/api/account-center/admin/accounts/member/restore"}, {"POST", "/api/account-center/admin/accounts/member/purge_preflight"},
		{"POST", "/api/account-center/admin/accounts/member/purge"},
		{"POST", "/api/account-center/admin/accounts/member/grant_team_create"}, {"POST", "/api/account-center/admin/accounts/member/revoke_team_create"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := impersonationRequest(router, tc.method, tc.path, "founder", `{}`, cookie)
			if w.Code != http.StatusForbidden {
				t.Fatalf("sensitive route status = %d %s, want 403", w.Code, w.Body)
			}
		})
	}
}
