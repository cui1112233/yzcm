package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
)

type accountCenterIdentityKey struct{}

func AccountCenterIdentityFromContext(ctx context.Context) (accountcenter.RequestIdentity, bool) {
	identity, ok := ctx.Value(accountCenterIdentityKey{}).(accountcenter.RequestIdentity)
	return identity, ok && identity.Actor.Username != ""
}

// AccountCenterMiddleware requires a valid account-center identity in addition
// to ordinary bridge authentication. Existing routes can migrate independently.
func (a BridgeAuth) AccountCenterMiddleware(next http.Handler) http.Handler {
	return a.middleware(next, true)
}

func (a BridgeAuth) accountCenterContext(ctx context.Context, username string) (context.Context, error) {
	if a.AccountCenterStore == nil {
		return ctx, errors.New("account center store unavailable")
	}
	if existing, ok := AccountCenterIdentityFromContext(ctx); ok {
		if strings.EqualFold(existing.Actor.Username, username) {
			return ctx, nil
		}
		ctx = context.WithValue(ctx, accountCenterIdentityKey{}, accountcenter.RequestIdentity{})
	}
	actor, err := a.AccountCenterStore.ResolveIdentity(ctx, username)
	if err != nil {
		return ctx, err
	}
	if !strings.EqualFold(actor.Username, username) || !actor.Active || actor.ArchivedAt != nil {
		return ctx, accountcenter.ErrForbidden
	}
	identity := accountcenter.RequestIdentity{Actor: actor, Subject: actor}
	return context.WithValue(ctx, accountCenterIdentityKey{}, identity), nil
}

// authorizeAccountCenter applies the policy to a server-resolved target. A
// missing target is represented by its zero identity and denied like any other
// unauthorized target.
func authorizeAccountCenter(w http.ResponseWriter, r *http.Request, action accountcenter.Action, target accountcenter.PolicyTarget) bool {
	identity, ok := AccountCenterIdentityFromContext(r.Context())
	if !ok || !(accountcenter.Policy{}).Can(identity.Subject, action, target) {
		writeAccountCenterForbidden(w)
		return false
	}
	return true
}

func writeAccountCenterForbidden(w http.ResponseWriter) {
	writeAuthError(w, http.StatusForbidden, "无权执行此操作")
}

type AuthOptions struct {
	Store              auth.Store
	AccountCenterStore accountcenter.Store
	Profiles           *MemberProfileStore
	Now                func() time.Time
	SessionResolver    func(context.Context, string) (BridgeIdentity, error)
	MFAFile            string
	PasskeyFile        string
	RateLimiter        *authRateLimiter
}

func authAccountPayload(account auth.Account, profiles *MemberProfileStore) map[string]any {
	role := "member"
	displayName := account.Username
	var avatarURL any
	themeID := "system"
	windowBackgroundID := "none"
	var windowBackgroundURL any
	if account.IsOwner {
		role = "dev"
	}
	// Owner 账号同样需要读取成员资料里的 displayName / avatarUrl。旧逻辑把这段
	// 放在 else 分支，导致 owner（页面上的 DEV 账号）的上传头像永远不会出现在
	// 侧边栏与个人中心头部。
	if profiles != nil {
		var profile map[string]any
		for _, row := range profiles.readArray("member-profiles.json") {
			if row["username"] == account.Username {
				profile = row
				break
			}
		}
		if profile != nil {
			if value, ok := profile["role"].(string); ok && !account.IsOwner && (value == "dev" || value == "manager") {
				role = value
			}
			if value, ok := profile["displayName"].(string); ok && strings.TrimSpace(value) != "" {
				displayName = value
			}
			avatarURL = memberAvatarURL(profile)
			if value, ok := profile["themeId"].(string); ok {
				if _, supported := memberThemeIDs[value]; supported {
					themeID = value
				}
			}
			if value, ok := profile["windowBackgroundId"].(string); ok {
				if _, supported := memberWindowBackgroundIDs[value]; supported {
					windowBackgroundID = value
				}
			}
			windowBackgroundURL = profile["windowBackgroundUrl"]
		}
	}
	return map[string]any{"username": account.Username, "active": account.Active, "isOwner": account.IsOwner, "role": role, "displayName": displayName, "avatarUrl": avatarURL, "themeId": themeID, "windowBackgroundId": windowBackgroundID, "windowBackgroundUrl": windowBackgroundURL, "effectivePermissions": []string{}}
}

func RegisterAuthRoutes(root *http.ServeMux, options AuthOptions) {
	accountPayload := func(ctx context.Context, account auth.Account) map[string]any {
		payload := authAccountPayload(account, options.Profiles)
		setFormalAccountFields(ctx, options.AccountCenterStore, payload, accountcenter.Identity{}, false)
		if options.AccountCenterStore == nil {
			return payload
		}
		payload["role"] = "member"
		if identity, err := options.AccountCenterStore.ResolveIdentity(ctx, account.Username); err == nil {
			setFormalAccountFields(ctx, options.AccountCenterStore, payload, identity, false)
		}
		return payload
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	limiter := options.RateLimiter
	if limiter == nil {
		limiter = newAuthRateLimiter(AuthRateLimitConfig{}, now)
	}
	root.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		if options.Store == nil {
			writeAuthError(w, http.StatusServiceUnavailable, "登录服务暂不可用")
			return
		}
		ipKey := authRateLimitIPDimension(r)
		if retryAfter, allowed := limiter.allow(authRateScopeLogin, ipKey); !allowed {
			writeAuthRateLimited(w, retryAfter)
			return
		}
		var input struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Remember bool   `json:"remember"`
			MFACode  string `json:"mfaCode"`
		}
		username := strings.TrimSpace(input.Username)
		userKey := authRateLimitUserDimension(username)
		recordFailure := func() {
			limiter.record(authRateScopeLogin, ipKey)
			if username != "" {
				limiter.record(authRateScopeLogin, userKey)
			}
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil || strings.TrimSpace(input.Username) == "" || input.Password == "" {
			recordFailure()
			writeAuthError(w, http.StatusUnauthorized, "用户名或密码错误")
			return
		}
		username = strings.TrimSpace(input.Username)
		userKey = authRateLimitUserDimension(username)
		if retryAfter, allowed := limiter.allow(authRateScopeLogin, userKey); !allowed {
			writeAuthRateLimited(w, retryAfter)
			return
		}
		account, err := options.Store.FindAccount(r.Context(), username)
		if err != nil && !errors.Is(err, auth.ErrNotFound) {
			writeAuthError(w, http.StatusServiceUnavailable, "登录服务暂不可用")
			return
		}
		if err != nil || !account.Active || !auth.VerifyPassword(input.Password, account.PasswordHash, account.PasswordAlgorithm) {
			recordFailure()
			writeAuthError(w, http.StatusUnauthorized, "用户名或密码错误")
			return
		}
		if strings.TrimSpace(options.MFAFile) != "" {
			required, valid, recoveryUsed, verifyErr := verifyMFAForLogin(options.MFAFile, account.Username, input.MFACode, now())
			if verifyErr != nil {
				writeAuthError(w, http.StatusServiceUnavailable, "MFA 服务暂不可用")
				return
			}
			if required && !valid {
				recordFailure()
				writeJSON(w, http.StatusPreconditionRequired, map[string]string{"error": "请输入 MFA 动态验证码或恢复码", "code": "MFA_REQUIRED"})
				return
			}
			_ = recoveryUsed
		}
		token, err := newSessionToken()
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "登录服务暂不可用")
			return
		}
		issued := now().UTC()
		expires := issued.Add(24 * time.Hour)
		if input.Remember {
			expires = issued.Add(30 * 24 * time.Hour)
		}
		if err := options.Store.CreateSession(r.Context(), auth.Session{Token: token, Username: account.Username, IssuedAt: issued, ExpiresAt: expires, IPHint: r.RemoteAddr}); err != nil {
			writeAuthError(w, http.StatusUnauthorized, "用户名或密码错误")
			return
		}
		limiter.reset(authRateScopeLogin, ipKey)
		limiter.reset(authRateScopeLogin, userKey)
		payload := accountPayload(r.Context(), account)
		payload["token"] = token
		writeJSON(w, http.StatusOK, payload)
	})

	root.HandleFunc("POST /api/login/logout", func(w http.ResponseWriter, r *http.Request) {
		if token := bearerToken(r); token != "" && options.Store != nil {
			_ = options.Store.RevokeSession(r.Context(), token)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	root.HandleFunc("POST /api/login/passkey/options", func(w http.ResponseWriter, r *http.Request) {
		if options.Store == nil || strings.TrimSpace(options.PasskeyFile) == "" {
			writeAuthError(w, http.StatusServiceUnavailable, "Passkey 服务暂不可用")
			return
		}
		var input struct {
			Username string `json:"username"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input) != nil || strings.TrimSpace(input.Username) == "" {
			writeAuthError(w, http.StatusBadRequest, "请输入账号")
			return
		}
		account, err := options.Store.FindAccount(r.Context(), strings.TrimSpace(input.Username))
		if err != nil || !account.Active {
			writeAuthError(w, http.StatusNotFound, "当前账号没有可用 Passkey")
			return
		}
		result, err := passkeyAuthenticationOptions(options.PasskeyFile, account.Username, r)
		if err != nil {
			writeAuthError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	root.HandleFunc("POST /api/login/passkey/verify", func(w http.ResponseWriter, r *http.Request) {
		if options.Store == nil || strings.TrimSpace(options.PasskeyFile) == "" {
			writeAuthError(w, http.StatusServiceUnavailable, "Passkey 服务暂不可用")
			return
		}
		var input map[string]any
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&input) != nil {
			writeAuthError(w, http.StatusBadRequest, "Passkey 登录数据不完整")
			return
		}
		username := strings.TrimSpace(fmt.Sprint(input["username"]))
		account, err := options.Store.FindAccount(r.Context(), username)
		if err != nil || !account.Active {
			writeAuthError(w, http.StatusUnauthorized, "Passkey 登录失败")
			return
		}
		if err := finishPasskeyAuthentication(options.PasskeyFile, account.Username, input, r); err != nil {
			writeAuthError(w, http.StatusUnauthorized, err.Error())
			return
		}
		token, err := newSessionToken()
		if err != nil {
			writeAuthError(w, http.StatusInternalServerError, "登录服务暂不可用")
			return
		}
		issued := now().UTC()
		expires := issued.Add(24 * time.Hour)
		if remember, _ := input["remember"].(bool); remember {
			expires = issued.Add(30 * 24 * time.Hour)
		}
		if err := options.Store.CreateSession(r.Context(), auth.Session{Token: token, Username: account.Username, IssuedAt: issued, ExpiresAt: expires, IPHint: r.RemoteAddr}); err != nil {
			writeAuthError(w, http.StatusUnauthorized, "Passkey 登录失败")
			return
		}
		payload := accountPayload(r.Context(), account)
		payload["token"] = token
		writeJSON(w, http.StatusOK, payload)
	})

	root.HandleFunc("GET /api/login/session", func(w http.ResponseWriter, r *http.Request) {
		if _, switched := switchedBridgeIdentity(r); switched {
			identity, _ := AccountCenterIdentityFromContext(r.Context())
			subject := identity.Subject
			payload := authAccountPayload(auth.Account{Username: subject.Username, Active: true}, options.Profiles)
			setFormalAccountFields(r.Context(), options.AccountCenterStore, payload, subject, true)
			payload["impersonationId"] = identity.ImpersonationID
			writeJSON(w, http.StatusOK, payload)
			return
		}
		if options.Store == nil {
			writeAuthError(w, http.StatusServiceUnavailable, "登录服务暂不可用")
			return
		}
		token := bearerToken(r)
		session, err := options.Store.FindSession(r.Context(), token, now().UTC())
		if err != nil {
			// A legacy Node request may ask Go to validate a Go-issued token.
			// Do not bounce that failed lookup back to Node, or the two services
			// would recursively validate the same invalid token.
			if options.SessionResolver != nil && r.Header.Get("X-Qiantie-Auth-Bridge") != "1" {
				if identity, resolveErr := options.SessionResolver(r.Context(), token); resolveErr == nil {
					writeJSON(w, http.StatusOK, accountPayload(r.Context(), auth.Account{Username: identity.Username, IsOwner: identity.IsOwner, Active: true}))
					return
				}
			}
			w.Header().Set("X-Qiantie-Auth-Failure", "session")
			writeAuthError(w, http.StatusUnauthorized, "登录已失效，请重新登录")
			return
		}
		account, err := options.Store.FindAccount(r.Context(), session.Username)
		if err != nil || !account.Active {
			w.Header().Set("X-Qiantie-Auth-Failure", "session")
			writeAuthError(w, http.StatusUnauthorized, "登录已失效，请重新登录")
			return
		}
		writeJSON(w, http.StatusOK, accountPayload(r.Context(), account))
	})
}

func legacyAccountRole(role string) string {
	switch role {
	case accountcenter.RoleFounder, accountcenter.RoleDeveloper:
		return "dev"
	case accountcenter.RoleManager:
		return "manager"
	default:
		return "member"
	}
}

func setFormalAccountFields(ctx context.Context, store accountcenter.Store, payload map[string]any, identity accountcenter.Identity, switched bool) {
	role := "组员"
	capabilities := []string{}
	switch identity.Role {
	case accountcenter.RoleFounder:
		role = "开发创始"
	case accountcenter.RoleDeveloper:
		role = "开发者"
	case accountcenter.RoleManager:
		role = "管理者"
	}
	if identity.Username != "" {
		payload["role"] = legacyAccountRole(identity.Role)
		payload["displayName"] = firstText(identity.DisplayName, identity.Username)
		if identity.AvatarURL != "" || switched {
			payload["avatarUrl"] = identity.AvatarURL
		}
	}
	if identity.Active && identity.ArchivedAt == nil && !switched {
		switch identity.Role {
		case accountcenter.RoleFounder:
			capabilities = append(capabilities, "team_governance", "account_governance", "platform_admin", "create_team")
		case accountcenter.RoleDeveloper:
			capabilities = append(capabilities, "team_governance", "account_governance")
			if reader, ok := store.(accountcenter.PlatformGrantReader); ok {
				grants, err := reader.PlatformGrants(ctx, identity.Username)
				if err == nil && slices.Contains(grants, accountcenter.GrantCapabilityPlatformAdmin) {
					capabilities = append(capabilities, "platform_admin")
				}
				if err == nil && slices.Contains(grants, accountcenter.GrantCapabilityTeamCreate) {
					capabilities = append(capabilities, "create_team")
				}
			}
		case accountcenter.RoleManager:
			capabilities = append(capabilities, "team_governance")
		}
	}
	payload["canonicalRole"] = role
	payload["capabilities"] = capabilities
}

func bearerToken(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(value) < 8 || !strings.EqualFold(value[:7], "Bearer ") {
		return ""
	}
	return strings.TrimSpace(value[7:])
}

func newSessionToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func writeAuthError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
