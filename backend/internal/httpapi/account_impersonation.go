package httpapi

import (
	"context"
	"crypto/sha256"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"qiantie/backend/internal/accountcenter"
)

const (
	impersonationCookieName     = "qiantie_impersonation"
	impersonationPath           = "/api/account-impersonation"
	impersonationCurrent        = impersonationPath + "/current"
	impersonationLifetime       = 15 * time.Minute
	noImpersonationExpected     = "none"
	expectedActorHeader         = "X-Qiantie-Expected-Actor"
	expectedImpersonationHeader = "X-Qiantie-Expected-Impersonation"
)

type accountImpersonation struct{ auth BridgeAuth }

type impersonationContextKey struct{}

type authenticatedImpersonation struct {
	identity  accountcenter.RequestIdentity
	loginHash [32]byte
	teamID    string
	expiresAt time.Time
}

type impersonationStatus struct {
	Active          bool                  `json:"active"`
	CanSwitch       bool                  `json:"canSwitch"`
	ActorUsername   string                `json:"actorUsername"`
	ImpersonationID string                `json:"impersonationId,omitempty"`
	Subject         *impersonationSubject `json:"subject,omitempty"`
	ExpiresInSecs   int                   `json:"expiresInSeconds,omitempty"`
}

type impersonationSubject struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

func (s accountImpersonation) register(root *http.ServeMux) {
	strict := s.auth.SessionOnly().AccountCenterMiddleware
	root.Handle("POST "+impersonationPath, strict(http.HandlerFunc(s.start)))
	root.Handle("GET "+impersonationCurrent, strict(http.HandlerFunc(s.status)))
	root.Handle("DELETE "+impersonationCurrent, strict(http.HandlerFunc(s.stop)))
}

func (s accountImpersonation) now() time.Time {
	if s.auth.Now != nil {
		return s.auth.Now().UTC()
	}
	return time.Now().UTC()
}

func impersonationTokenHash(cookie, login string) [32]byte {
	// The cookie can be used only with the exact login session that created it.
	// The cookie token uses a fixed alphabet that excludes the separator.
	return sha256.Sum256([]byte(cookie + "\x00" + login))
}

func writeImpersonationCookie(w http.ResponseWriter, r *http.Request, value string, expires time.Time, maxAge int) {
	host := r.Host
	if name, _, err := net.SplitHostPort(host); err == nil {
		host = name
	}
	addr := net.ParseIP(strings.Trim(host, "[]"))
	loopback := strings.EqualFold(host, "localhost") || addr != nil && addr.IsLoopback()
	http.SetCookie(w, &http.Cookie{
		Name: impersonationCookieName, Value: value, Path: "/api", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil || !loopback, Expires: expires, MaxAge: maxAge,
	})
}

func clearImpersonationCookie(w http.ResponseWriter, r *http.Request) {
	writeImpersonationCookie(w, r, "", time.Unix(1, 0).UTC(), -1)
}

func writeImpersonationUnavailable(w http.ResponseWriter) {
	writeAuthError(w, http.StatusServiceUnavailable, "账号切换服务暂不可用")
}

func (s accountImpersonation) resolveTarget(ctx context.Context, actor accountcenter.Identity, username string) (accountcenter.Identity, string, error) {
	if strings.TrimSpace(username) == "" || strings.EqualFold(username, actor.Username) || actor.Role == accountcenter.RoleMember {
		return accountcenter.Identity{}, "", accountcenter.ErrForbidden
	}
	subject, err := s.auth.AccountCenterStore.ResolveIdentity(ctx, username)
	if err != nil {
		return accountcenter.Identity{}, "", err
	}
	if !strings.EqualFold(subject.Username, username) {
		return accountcenter.Identity{}, "", accountcenter.ErrForbidden
	}
	teamID := ""
	if actor.Role == accountcenter.RoleManager && subject.Role == accountcenter.RoleMember && subject.Active && subject.ArchivedAt == nil {
		teams, err := s.auth.AccountCenterStore.ListTeams(ctx, accountcenter.TeamFilter{Username: subject.Username, ManagerUsername: actor.Username})
		if err != nil {
			return accountcenter.Identity{}, "", err
		}
		for _, team := range teams {
			if team.ArchivedAt == nil && strings.EqualFold(team.ManagerUsername, actor.Username) {
				teamID = team.ID
				break
			}
		}
	}
	target := accountcenter.PolicyTarget{Identity: subject, InManagedTeam: teamID != ""}
	if !(accountcenter.Policy{}).Can(actor, accountcenter.ActionImpersonate, target) {
		return accountcenter.Identity{}, "", accountcenter.ErrForbidden
	}
	return subject, teamID, nil
}

func (s accountImpersonation) find(r *http.Request, actor string) (accountcenter.Impersonation, error) {
	cookie, err := r.Cookie(impersonationCookieName)
	if err != nil || len(cookie.Value) != 43 {
		return accountcenter.Impersonation{}, accountcenter.ErrNotFound
	}
	return s.auth.AccountCenterStore.FindImpersonation(r.Context(), impersonationTokenHash(cookie.Value, bearerToken(r)), actor, s.now())
}

func (s accountImpersonation) active(r *http.Request, actor accountcenter.Identity) (authenticatedImpersonation, error) {
	session, err := s.find(r, actor.Username)
	if err != nil {
		return authenticatedImpersonation{}, err
	}
	subject, teamID, err := s.resolveTarget(r.Context(), actor, session.SubjectUsername)
	if errors.Is(err, accountcenter.ErrNotFound) || errors.Is(err, accountcenter.ErrForbidden) {
		// A change in the target's account or team cannot make this session usable again.
		if revokeErr := s.auth.AccountCenterStore.RevokeImpersonation(r.Context(), session.ID, actor.Username, s.now()); revokeErr != nil && !errors.Is(revokeErr, accountcenter.ErrNotFound) {
			return authenticatedImpersonation{}, revokeErr
		}
		return authenticatedImpersonation{}, accountcenter.ErrForbidden
	}
	if err != nil {
		return authenticatedImpersonation{}, err
	}
	return authenticatedImpersonation{
		identity:  accountcenter.RequestIdentity{Actor: actor, Subject: subject, ImpersonationID: session.ID},
		loginHash: sha256.Sum256([]byte(bearerToken(r))), teamID: teamID, expiresAt: session.ExpiresAt,
	}, nil
}

func (s accountImpersonation) start(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identity, _ := AccountCenterIdentityFromContext(r.Context())
	if _, err := s.find(r, identity.Actor.Username); err == nil {
		writeAuthError(w, http.StatusConflict, "请先退出当前切换")
		return
	} else if !errors.Is(err, accountcenter.ErrNotFound) {
		writeImpersonationUnavailable(w)
		return
	}
	var input struct {
		Username string `json:"username"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	subject, teamID, err := s.resolveTarget(r.Context(), identity.Actor, strings.TrimSpace(input.Username))
	if errors.Is(err, accountcenter.ErrNotFound) || errors.Is(err, accountcenter.ErrForbidden) {
		writeAccountCenterForbidden(w)
		return
	}
	if err != nil {
		writeImpersonationUnavailable(w)
		return
	}
	cookie, err := newSessionToken()
	if err != nil {
		writeImpersonationUnavailable(w)
		return
	}
	id, err := newSessionToken()
	if err != nil {
		writeImpersonationUnavailable(w)
		return
	}
	now := s.now()
	session := accountcenter.Impersonation{
		ID: id, ActorUsername: identity.Actor.Username, SubjectUsername: subject.Username,
		TokenHash: impersonationTokenHash(cookie, bearerToken(r)), CreatedAt: now, ExpiresAt: now.Add(impersonationLifetime),
	}
	if err := s.auth.AccountCenterStore.CreateImpersonation(r.Context(), session); err != nil {
		writeImpersonationUnavailable(w)
		return
	}
	active := authenticatedImpersonation{
		identity: accountcenter.RequestIdentity{Actor: identity.Actor, Subject: subject, ImpersonationID: id},
		teamID:   teamID, expiresAt: session.ExpiresAt,
	}
	if err := s.audit(r.Context(), active, "impersonation.start"); err != nil {
		// The token has never been sent to the browser, even if cleanup fails.
		_ = s.auth.AccountCenterStore.RevokeImpersonation(r.Context(), id, identity.Actor.Username, now)
		writeImpersonationUnavailable(w)
		return
	}
	writeImpersonationCookie(w, r, cookie, session.ExpiresAt, int(impersonationLifetime.Seconds()))
	writeJSON(w, http.StatusCreated, s.activeStatus(active))
}

func (s accountImpersonation) activeStatus(active authenticatedImpersonation) impersonationStatus {
	subject := active.identity.Subject
	return impersonationStatus{
		Active: true, ActorUsername: active.identity.Actor.Username, ImpersonationID: active.identity.ImpersonationID,
		Subject:       &impersonationSubject{Username: subject.Username, DisplayName: subject.DisplayName},
		ExpiresInSecs: max(1, int(active.expiresAt.Sub(s.now()).Seconds())),
	}
}

func inactiveImpersonationStatus(actor accountcenter.Identity) impersonationStatus {
	canSwitch := actor.Role == accountcenter.RoleFounder || actor.Role == accountcenter.RoleDeveloper || actor.Role == accountcenter.RoleManager
	return impersonationStatus{CanSwitch: canSwitch, ActorUsername: actor.Username}
}

func (s accountImpersonation) status(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identity, _ := AccountCenterIdentityFromContext(r.Context())
	active, err := s.active(r, identity.Actor)
	if err == nil {
		writeJSON(w, http.StatusOK, s.activeStatus(active))
		return
	}
	if !errors.Is(err, accountcenter.ErrNotFound) && !errors.Is(err, accountcenter.ErrForbidden) {
		writeImpersonationUnavailable(w)
		return
	}
	if _, cookieErr := r.Cookie(impersonationCookieName); cookieErr == nil {
		clearImpersonationCookie(w, r)
	}
	writeJSON(w, http.StatusOK, inactiveImpersonationStatus(identity.Actor))
}

func (s accountImpersonation) stop(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	identity, _ := AccountCenterIdentityFromContext(r.Context())
	if err := s.end(r, identity.Actor); err != nil {
		writeImpersonationUnavailable(w)
		return
	}
	clearImpersonationCookie(w, r)
	writeJSON(w, http.StatusOK, inactiveImpersonationStatus(identity.Actor))
}

func (s accountImpersonation) end(r *http.Request, actor accountcenter.Identity) error {
	session, err := s.find(r, actor.Username)
	if errors.Is(err, accountcenter.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := s.auth.AccountCenterStore.RevokeImpersonation(r.Context(), session.ID, actor.Username, s.now()); err != nil {
		if errors.Is(err, accountcenter.ErrNotFound) {
			return nil
		}
		return err
	}
	_, teamID, _ := s.resolveTarget(r.Context(), actor, session.SubjectUsername)
	return s.audit(r.Context(), authenticatedImpersonation{identity: accountcenter.RequestIdentity{
		Actor: actor, Subject: accountcenter.Identity{Username: session.SubjectUsername}, ImpersonationID: session.ID,
	}, teamID: teamID}, "impersonation.stop")
}

func (s accountImpersonation) audit(ctx context.Context, active authenticatedImpersonation, action string) error {
	return s.auth.AccountCenterStore.AppendAudit(ctx, accountcenter.AuditEvent{
		ActorUsername: active.identity.Actor.Username, SubjectUsername: active.identity.Subject.Username,
		ImpersonationID: active.identity.ImpersonationID, TeamID: active.teamID, Action: action, CreatedAt: s.now(),
	})
}

func (s accountImpersonation) middleware(root *http.ServeMux, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == impersonationPath || r.URL.Path == impersonationCurrent {
			next.ServeHTTP(w, r)
			return
		}
		expectedActor, expectedID := "", ""
		if impersonationWriteMethod(r.Method) && r.URL.Path != "/api/login/logout" {
			expectedActor = strings.TrimSpace(r.Header.Get(expectedActorHeader))
			expectedID = strings.TrimSpace(r.Header.Get(expectedImpersonationHeader))
		}
		hasExpectation := expectedActor != "" || expectedID != ""
		if hasExpectation && (expectedActor == "" || expectedID == "" || bearerToken(r) == "") {
			writeImpersonationConflict(w)
			return
		}
		_, cookieErr := r.Cookie(impersonationCookieName)
		if !hasExpectation && (s.auth.AccountCenterStore == nil || bearerToken(r) == "" || cookieErr != nil) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		login := s.auth.SessionOnly()
		actor, err := login.authenticate(r)
		if r.Method == http.MethodPost && r.URL.Path == "/api/login/logout" {
			// Preserve normal logout even if switch storage or audit is unavailable.
			if err == nil {
				logoutActor := accountcenter.Identity{Username: actor.Username}
				if resolved, resolveErr := s.auth.AccountCenterStore.ResolveIdentity(r.Context(), actor.Username); resolveErr == nil {
					logoutActor = resolved
				}
				_ = s.end(r, logoutActor)
			}
			clearImpersonationCookie(w, r)
			next.ServeHTTP(w, r)
			return
		}
		if err != nil {
			w.Header().Set("X-Qiantie-Auth-Failure", "session")
			writeAuthError(w, http.StatusUnauthorized, "登录已失效，请重新登录")
			return
		}
		if hasExpectation && expectedActor != actor.Username {
			writeImpersonationConflict(w)
			return
		}
		if cookieErr != nil {
			if expectedID != noImpersonationExpected {
				writeImpersonationConflict(w)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if s.auth.AccountCenterStore == nil {
			writeImpersonationUnavailable(w)
			return
		}
		ctx, err := login.accountCenterContext(r.Context(), actor.Username)
		if err != nil {
			writeAccountCenterForbidden(w)
			return
		}
		identity, _ := AccountCenterIdentityFromContext(ctx)
		active, err := s.active(r, identity.Actor)
		if err != nil {
			if errors.Is(err, accountcenter.ErrNotFound) || errors.Is(err, accountcenter.ErrForbidden) {
				if errors.Is(err, accountcenter.ErrForbidden) {
					clearImpersonationCookie(w, r)
				}
				if r.Method == http.MethodGet && r.URL.Path == "/api/login/session" {
					next.ServeHTTP(w, r)
					return
				}
				writeImpersonationConflict(w)
			} else {
				writeImpersonationUnavailable(w)
			}
			return
		}
		if hasExpectation && expectedID != active.identity.ImpersonationID {
			writeImpersonationConflict(w)
			return
		}
		if impersonationBlocks(r.Method, r.URL.Path) {
			writeAccountCenterForbidden(w)
			return
		}
		_, pattern := root.Handler(r)
		if pattern != "" && r.URL.Path != "/api/login/session" {
			// Route patterns exclude user-supplied path values, queries and bodies.
			if _, route, explicitMethod := strings.Cut(pattern, " "); explicitMethod {
				pattern = route
			}
			action := "impersonation.request " + r.Method + " " + pattern
			if len(action) > 96 {
				action = "impersonation.request"
			}
			if err := s.audit(r.Context(), active, action); err != nil {
				writeImpersonationUnavailable(w)
				return
			}
		}
		ctx = context.WithValue(ctx, accountCenterIdentityKey{}, active.identity)
		ctx = context.WithValue(ctx, impersonationContextKey{}, active)
		request := r.Clone(ctx)
		request.Header.Del(HeaderNovelFetchAccount)
		next.ServeHTTP(w, request)
	})
}

func writeImpersonationConflict(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusConflict, map[string]string{"code": "IMPERSONATION_CHANGED", "error": "账号切换状态已变化，请刷新后重试"})
}

func switchedBridgeIdentity(r *http.Request) (BridgeIdentity, bool) {
	active, ok := r.Context().Value(impersonationContextKey{}).(authenticatedImpersonation)
	if !ok || active.loginHash != sha256.Sum256([]byte(bearerToken(r))) {
		return BridgeIdentity{}, false
	}
	return BridgeIdentity{Username: active.identity.Subject.Username}, true
}

func allowImpersonationCredentialWrite(w http.ResponseWriter, r *http.Request) bool {
	if _, switched := switchedBridgeIdentity(r); switched {
		writeAccountCenterForbidden(w)
		return false
	}
	return true
}

func impersonationWriteMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func impersonationBlocks(method, path string) bool {
	if !impersonationWriteMethod(method) {
		return false
	}
	for _, prefix := range []string{
		"/api/member/security", "/api/account-recovery", "/api/login/passkey",
		"/api/account-center", "/api/account-admin", "/api/admin/accounts", "/api/admin/grants", "/api/admin/applications",
		"/api/member/team", "/api/team-admin", "/api/settings/api-integrations", "/api/config",
		"/api/shuihuo-production/config", "/api/shuihuo-production/account-ai-config",
		"/api/novel-fetch-workshop/config", "/api/batch-rewrite/config",
	} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}
