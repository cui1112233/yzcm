package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/apisecrets"
	"qiantie/backend/internal/yzcmmodels"
	"qiantie/backend/internal/yzcmusage"
)

type apiConfigAccess struct {
	identity accountcenter.RequestIdentity
	allowed  bool
	custom   bool
}

func apiConfigPolicy(ctx context.Context, store accountcenter.Store) apiConfigAccess {
	identity, ok := AccountCenterIdentityFromContext(ctx)
	if !ok || store == nil || !identity.Subject.Active || identity.Subject.ArchivedAt != nil {
		return apiConfigAccess{}
	}
	role := identity.Subject.Role
	access := apiConfigAccess{identity: identity, allowed: role == accountcenter.RoleFounder || role == accountcenter.RoleDeveloper || role == accountcenter.RoleManager}
	access.custom = role == accountcenter.RoleFounder
	if role == accountcenter.RoleDeveloper {
		if reader, ok := store.(accountcenter.PlatformGrantReader); ok {
			grants, err := reader.PlatformGrants(ctx, identity.Subject.Username)
			access.custom = err == nil && slices.Contains(grants, accountcenter.GrantCapabilityCustomAPI)
		}
	}
	return access
}

func (a apiConfigAccess) response(payload map[string]any) map[string]any {
	payload["canConfigureAPI"], payload["canManageApi"] = a.allowed, a.allowed
	payload["canAddCustomModel"], payload["canEditAddress"] = a.custom, a.custom
	source, actions := "none", []string{}
	if a.allowed {
		source = "yzcm_saved"
		actions = []string{"select_yzcm_model", "update_model_key", "remove_model"}
		if a.identity.ImpersonationID == "" {
			actions = append(actions, "reveal_model_key")
		} else {
			actions = []string{}
		}
	}
	if a.custom {
		source = "full"
		if a.identity.ImpersonationID == "" {
			actions = append(actions, "add_custom_model", "edit_model_address")
		}
	}
	payload["catalogSource"], payload["actions"] = source, actions
	return payload
}

func (a apiConfigAccess) authorize(w http.ResponseWriter, write, full bool) bool {
	if !a.allowed || full && !a.custom || write && a.identity.ImpersonationID != "" {
		writeAccountCenterForbidden(w)
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	return true
}

// ResolveAPIConfigOwner uses the current formal team relationship for runtime calls.
func ResolveAPIConfigOwner(ctx context.Context, store accountcenter.Store, username string) (string, error) {
	if store == nil {
		return "", accountcenter.ErrForbidden
	}
	identity, err := store.ResolveIdentity(ctx, username)
	if err != nil || !identity.Active || identity.ArchivedAt != nil {
		return "", accountcenter.ErrForbidden
	}
	if identity.Role != accountcenter.RoleMember {
		return identity.Username, nil
	}
	if identity.TeamID == "" {
		return "", accountcenter.ErrForbidden
	}
	teams, err := store.ListTeams(ctx, accountcenter.TeamFilter{Username: identity.Username})
	if err != nil {
		return "", err
	}
	for _, team := range teams {
		if team.ID == identity.TeamID && team.ArchivedAt == nil {
			manager, err := store.ResolveIdentity(ctx, team.ManagerUsername)
			if err == nil && manager.Active && manager.ArchivedAt == nil && manager.Role != accountcenter.RoleMember {
				return manager.Username, nil
			}
		}
	}
	return "", accountcenter.ErrForbidden
}

const managedYZCMSource = "yzcm_saved"

func isManagedYZCMModel(model map[string]any) bool {
	return model["catalogSource"] == managedYZCMSource && model["provider"] == "yzcm" && model["baseUrl"] == yzcmusage.Origin+"/v1"
}

func publicRestrictedModel(model map[string]any) map[string]any {
	return map[string]any{"id": model["id"], "modelId": model["modelId"], "displayName": model["displayName"], "kind": model["kind"],
		"hasCredential": managedModelHasCredential(model), "available": managedModelAvailable(model), "enabled": model["enabled"], "catalogSource": managedYZCMSource}
}

func allowedInputFields(input map[string]any, fields ...string) bool {
	for key := range input {
		if !slices.Contains(fields, key) {
			return false
		}
	}
	return input != nil
}

func apiConfigField(key string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, key)
}

func apiSecretField(key string) bool {
	name := apiConfigField(key)
	return name == "authorization" || name == "headers" || name == "password" || name == "credential" || name == "secret" || name == "token" ||
		strings.HasSuffix(name, "apikey") || strings.HasSuffix(name, "secretkey") || strings.HasSuffix(name, "accesstoken") || strings.HasSuffix(name, "admintoken")
}

func containsAPIConfig(input any) bool {
	switch value := input.(type) {
	case map[string]any:
		for key, item := range value {
			switch apiConfigField(key) {
			case "modelcatalog", "modelcatalogversion", "modelcatalogsecretversion", "apiintegrations", "baseurl", "endpoint", "apiformat", "provider", "tasksendpoint", "resultendpoint", "requesttemplate", "responsemapping", "voiceclonerequesttemplate", "voicecloneresponsemapping", "adapterkind", "credentialref", "parameterschema", "routing", "customchannels":
				return true
			}
			if apiSecretField(key) || containsAPIConfig(item) {
				return true
			}
		}
	case []any:
		for _, item := range value {
			if containsAPIConfig(item) {
				return true
			}
		}
	}
	return false
}

func redactAPIConfig(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			_, boolValue := item.(bool)
			configuredFlag := boolValue && strings.Contains(apiConfigField(key), "hasapikey")
			if (!apiSecretField(key) || configuredFlag) && key != "modelCatalogSecretVersion" {
				out[key] = redactAPIConfig(item)
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(value))
		for _, item := range value {
			out = append(out, redactAPIConfig(item))
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, 0, len(value))
		for _, item := range value {
			out = append(out, redactAPIConfig(item).(map[string]any))
		}
		return out
	case string:
		if apisecrets.IsEncrypted(value) {
			return ""
		}
	}
	return v
}

func withAPIConfigCompatibility(next http.Handler, auth BridgeAuth) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/batch-rewrite/config", "/api/batch-rewrite/bootstrap", "/api/novel-fetch-workshop/config", "/api/shuihuo-production/config":
			ctx := context.WithValue(r.Context(), apiConfigCompatKey{}, auth)
			next.ServeHTTP(w, r.WithContext(ctx))
		default:
			next.ServeHTTP(w, r)
		}
	})
}

func compatibilityAPIConfigAccess(r *http.Request) (apiConfigAccess, bool) {
	ctx := r.Context()
	auth, protected := ctx.Value(apiConfigCompatKey{}).(BridgeAuth)
	if !protected || auth.AccountCenterStore == nil {
		return apiConfigAccess{}, protected
	}
	if _, ok := AccountCenterIdentityFromContext(ctx); !ok {
		if identity, signedIn := BridgeIdentityFromContext(ctx); signedIn {
			ctx, _ = auth.accountCenterContext(ctx, identity.Username)
		}
	}
	return apiConfigPolicy(ctx, auth.AccountCenterStore), true
}

func publicCompatibilityConfig(r *http.Request, config map[string]any) map[string]any {
	access, protected := compatibilityAPIConfigAccess(r)
	if protected && !access.custom {
		return redactRestrictedCompatibilityConfig(config).(map[string]any)
	}
	return redactAPIConfig(config).(map[string]any)
}

func redactRestrictedCompatibilityConfig(value any) any {
	switch entry := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(entry))
		for key, child := range entry {
			switch apiConfigField(key) {
			case "baseurl", "endpoint", "apiformat", "provider", "tasksendpoint", "resultendpoint", "requesttemplate", "responsemapping", "voiceclonerequesttemplate", "voicecloneresponsemapping", "adapterkind", "credentialref", "parameterschema", "workflowid", "routing", "customchannels", "modelcatalogsecretversion", "apiintegrations":
				continue
			}
			if !apiSecretField(key) {
				out[key] = redactRestrictedCompatibilityConfig(child)
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(entry))
		for _, item := range entry {
			out = append(out, redactRestrictedCompatibilityConfig(item))
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, 0, len(entry))
		for _, item := range entry {
			out = append(out, redactRestrictedCompatibilityConfig(item).(map[string]any))
		}
		return out
	default:
		return redactAPIConfig(value)
	}
}

type apiConfigCompatKey struct{}

func rejectCompatibilityAPIConfigWrite(w http.ResponseWriter, r *http.Request, data map[string]any) bool {
	if _, protected := r.Context().Value(apiConfigCompatKey{}).(BridgeAuth); protected && containsAPIConfig(data) {
		writeAccountCenterForbidden(w)
		return true
	}
	return false
}

func modelCredentialPatch(input map[string]any) (string, bool, error) {
	credential := ""
	if raw, present := input["credential"]; present {
		value, ok := raw.(string)
		if !ok || len(value) > 8192 || apisecrets.IsEncrypted(strings.TrimSpace(value)) {
			return "", false, accountcenter.ErrInvalid
		}
		credential = strings.TrimSpace(value)
	}
	clear := false
	if raw, present := input["clearCredential"]; present {
		value, ok := raw.(bool)
		if !ok || (value && credential != "") {
			return "", false, accountcenter.ErrInvalid
		}
		clear = value
	}
	return credential, clear, nil
}

func newRestrictedYZCMModel(ctx context.Context, reader yzcmmodels.Reader, input map[string]any) (map[string]any, error) {
	if !allowedInputFields(input, "kind", "modelId", "credential") {
		return nil, accountcenter.ErrForbidden
	}
	kind, modelID := firstText(input["kind"]), firstText(input["modelId"])
	credential, _, err := modelCredentialPatch(input)
	if err != nil || !modelKinds[kind] || modelID == "" || len(modelID) > 764 {
		return nil, accountcenter.ErrInvalid
	}
	if reader == nil {
		return nil, accountcenter.ErrForbidden
	}
	page, err := reader.Search(ctx, yzcmmodels.Query{Kind: kind, Q: modelID, ExactModelID: modelID, Limit: 1})
	if err != nil || page.Status != "available" {
		return nil, accountcenter.ErrForbidden
	}
	for _, row := range page.Models {
		if row.ModelID == modelID && row.Kind == kind && row.Adapted && row.Allowed {
			return map[string]any{"id": "yzcm:" + kind + ":" + modelID, "modelId": modelID, "kind": kind, "displayName": row.DisplayName,
				"baseUrl": yzcmusage.Origin + "/v1", "provider": "yzcm", "credential": credential, "enabled": true, "catalogSource": managedYZCMSource}, nil
		}
	}
	return nil, accountcenter.ErrForbidden
}

func writeAPIConfigError(w http.ResponseWriter, err error) {
	if errors.Is(err, accountcenter.ErrForbidden) {
		writeAccountCenterForbidden(w)
		return
	}
	writeAuthError(w, http.StatusBadRequest, "模型配置参数无效")
}
