package httpapi

import (
	"net/http"
	"path"
	"strings"

	ac "qiantie/backend/internal/accountcenter"
)

func accountCenterBusinessWrite(r *http.Request) bool {
	if !impersonationWriteMethod(r.Method) {
		return false
	}
	cleaned := path.Clean(r.URL.Path)
	apiPath, api := strings.CutPrefix(cleaned, "/api/")
	if !api {
		return false
	}
	namespace, _, _ := strings.Cut(apiPath, "/")
	switch namespace {
	case "account-center", "account-impersonation", "account-admin", "team-admin", "account-recovery",
		"login", "member", "admin", "applications", "client-errors", "settings", "config",
		"local-executor", "giant-material-executor", "integrations":
		return false
	}
	// Merge media links are short-lived access to existing artifacts.
	if namespace == "batch-factory" && r.Method == http.MethodPost {
		parts := strings.Split(apiPath, "/")
		if len(parts) == 7 && (parts[1] == "v11" || parts[1] == "v12") &&
			parts[2] == "batches" && parts[4] == "merge-media" && parts[6] == "token" {
			return false
		}
	}
	return true
}

func (a BridgeAuth) allowBusinessSubject(w http.ResponseWriter, r *http.Request, username string) bool {
	access, ok := a.AccountCenterStore.(ac.BusinessAccessReader)
	if !ok {
		writeAuthError(w, http.StatusServiceUnavailable, "团队归属服务暂不可用")
		return false
	}
	allowed, err := access.BusinessAllowed(r.Context(), username)
	if err != nil {
		writeAuthError(w, http.StatusServiceUnavailable, "团队归属服务暂不可用")
		return false
	}
	if !allowed {
		writeAuthError(w, http.StatusForbidden, "当前账号没有可用的团队归属")
		return false
	}
	return true
}

func withAccountCenterBusinessAccess(next http.Handler, auth BridgeAuth) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth.AccountCenterStore == nil || !accountCenterBusinessWrite(r) {
			next.ServeHTTP(w, r)
			return
		}
		if identity, ok := AccountCenterIdentityFromContext(r.Context()); ok {
			if auth.allowBusinessSubject(w, r, identity.Subject.Username) {
				next.ServeHTTP(w, r)
			}
			return
		}
		bridge, err := auth.authenticate(r)
		if err != nil {
			if bearerToken(r) != "" {
				w.Header().Set("X-Qiantie-Auth-Failure", "session")
			}
			writeAuthError(w, http.StatusUnauthorized, "登录已失效，请重新登录")
			return
		}
		ctx, err := auth.accountCenterContext(r.Context(), bridge.Username)
		if err != nil {
			writeAccountCenterForbidden(w)
			return
		}
		if auth.allowBusinessSubject(w, r, bridge.Username) {
			next.ServeHTTP(w, r.WithContext(ctx))
		}
	})
}
