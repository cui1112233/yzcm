package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"unicode"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/apisecrets"
	"qiantie/backend/internal/novelfetchworkshop"
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
	switch identity.Role {
	case accountcenter.RoleFounder, accountcenter.RoleDeveloper, accountcenter.RoleManager:
		return identity.Username, nil
	case accountcenter.RoleMember:
	default:
		return "", accountcenter.ErrForbidden
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
	item := publicRuntimeModel(model)
	item["hasCredential"] = managedModelHasCredential(model) || model["hasCredential"] == true
	item["catalogSource"] = managedYZCMSource
	if model["credentialStatus"] == "needs_reentry" {
		item["credentialStatus"] = "needs_reentry"
	}
	return item
}

func publicRuntimeModel(model map[string]any) map[string]any {
	enabled, set := model["enabled"]
	item := map[string]any{"id": modelString(model, "id"), "modelId": modelString(model, "modelId"),
		"displayName": modelString(model, "displayName"), "kind": modelString(model, "kind"),
		"available": model["available"] == true || managedModelAvailable(model), "enabled": !set || normalizedModelEnabled(enabled)}
	setVideoExecutionAvailability(item, model)
	return item
}

func setVideoExecutionAvailability(result, model map[string]any) {
	reason := videoExecutionUnavailableReason(model)
	if reason == "" && model["unavailableReason"] == "execution_adapter_unavailable" {
		reason = "execution_adapter_unavailable"
	}
	if reason != "" {
		result["available"] = false
		result["unavailableReason"] = reason
	}
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
	return name == "authorization" || name == "headers" || name == "password" || name == "credential" || name == "credentialref" || name == "secret" || name == "token" ||
		strings.HasSuffix(name, "apikey") || strings.HasSuffix(name, "secretkey") || strings.HasSuffix(name, "accesstoken") || strings.HasSuffix(name, "admintoken")
}

func rejectsCredentialReference(input map[string]any) bool {
	for key := range input {
		if apiConfigField(key) == "credentialref" {
			return true
		}
	}
	return false
}

func modelTargetChanged(before, after map[string]any) bool {
	return novelfetchworkshop.ModelCredentialDestination(before) != novelfetchworkshop.ModelCredentialDestination(after) ||
		novelfetchworkshop.ModelCredentialSource(before) != novelfetchworkshop.ModelCredentialSource(after) ||
		firstText(before["modelId"]) != firstText(after["modelId"]) || firstText(before["kind"]) != firstText(after["kind"])
}

func containsConfigField(input any, included func(string) bool) bool {
	switch value := input.(type) {
	case map[string]any:
		for key, item := range value {
			if included(key) || containsConfigField(item, included) {
				return true
			}
		}
	case []any:
		for _, item := range value {
			if containsConfigField(item, included) {
				return true
			}
		}
	}
	return false
}

func isManagedModelField(key string) bool {
	if strings.HasPrefix(apiConfigField(key), "modelcataloglegacy") {
		return true
	}
	switch apiConfigField(key) {
	case "modelcatalog", "modelcatalogversion", "modelcatalogsecretversion", "videoproviders", "videoproviderlegacymigrated", "credentialref":
		return true
	}
	return false
}

func containsAPIConfig(input any) bool {
	return containsConfigField(input, func(key string) bool {
		switch apiConfigField(key) {
		case "apiintegrations", "baseurl", "endpoint", "apiformat", "provider", "tasksendpoint", "resultendpoint", "requesttemplate", "responsemapping", "voiceclonerequesttemplate", "voicecloneresponsemapping", "adapterkind", "parameterschema", "routing", "customchannels":
			return true
		}
		return isManagedModelField(key) || apiSecretField(key)
	})
}

func redactAPIConfig(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			if key == "ciphertext" || key == "fingerprint" {
				continue
			}
			_, boolValue := item.(bool)
			configuredFlag := boolValue && strings.Contains(apiConfigField(key), "hasapikey")
			if (!apiSecretField(key) || configuredFlag) && key != "modelCatalogSecretVersion" && !strings.HasPrefix(key, "modelCatalogLegacy") &&
				key != "videoProviders" && key != "videoProviderLegacyMigrated" {
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
	if !protected {
		return redactAPIConfig(config).(map[string]any)
	}
	return redactCompatibilityConfig(config, access).(map[string]any)
}

func compatibilityModels(value any, access apiConfigAccess) []map[string]any {
	models := modelCatalog(map[string]any{"modelCatalog": value})
	result := make([]map[string]any, 0, len(models))
	for _, model := range models {
		switch {
		case access.custom:
			result = append(result, publicManagedModel(model))
		case access.allowed && isManagedYZCMModel(model):
			result = append(result, publicRestrictedModel(model))
		case access.identity.Subject.Role == accountcenter.RoleMember && (model["available"] == true || managedModelAvailable(model) || videoExecutionUnavailableReason(model) != ""):
			result = append(result, publicRuntimeModel(model))
		}
	}
	return result
}

func redactCompatibilityConfig(value any, access apiConfigAccess) any {
	switch entry := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(entry))
		for key, child := range entry {
			switch apiConfigField(key) {
			case "modelcatalog":
				out[key] = compatibilityModels(child, access)
				continue
			case "modelcatalogsecretversion", "modelcataloglegacyscrubpending", "modelcataloglegacyscrubdone",
				"modelcataloglegacymigrationversion", "modelcataloglegacyscrubentries", "modelcataloglegacyscrubentrydone",
				"videoproviders", "videoproviderlegacymigrated", "importedsensitiverepairbackup", "importedknowledgerepairbackup":
				continue
			case "baseurl", "endpoint", "apiformat", "provider", "tasksendpoint", "resultendpoint", "requesttemplate", "responsemapping", "voiceclonerequesttemplate", "voicecloneresponsemapping", "adapterkind", "credentialref", "parameterschema", "workflowid", "routing", "customchannels", "apiintegrations":
				if !access.custom {
					continue
				}
			}
			_, flags := child.(bool)
			if key == "api_key" || key == "password" {
				out[key+"_configured"] = firstText(child) != ""
			}
			if !apiSecretField(key) || flags && strings.Contains(apiConfigField(key), "hasapikey") {
				out[key] = redactCompatibilityConfig(child, access)
			}
		}
		return out
	case []any:
		out := make([]any, 0, len(entry))
		for _, item := range entry {
			out = append(out, redactCompatibilityConfig(item, access))
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, 0, len(entry))
		for _, item := range entry {
			out = append(out, redactCompatibilityConfig(item, access).(map[string]any))
		}
		return out
	default:
		return redactAPIConfig(value)
	}
}

type apiConfigCompatKey struct{}

func prepareCompatibilitySettingsPatch(r *http.Request, data, current map[string]any) (hasEcho, allowed bool) {
	access, protected := compatibilityAPIConfigAccess(r)
	if !protected {
		return false, !containsAPIConfig(data)
	}
	var stripEcho func(map[string]any) bool
	stripEcho = func(section map[string]any) bool {
		for key, value := range section {
			switch key {
			case "modelCatalog", "modelCatalogVersion":
				hasEcho = true
				if current == nil {
					continue
				}
				expected := current[key]
				if key == "modelCatalog" {
					expected = compatibilityModels(expected, access)
				}
				public, err := json.Marshal(expected)
				var normalized any
				if err != nil || json.Unmarshal(public, &normalized) != nil || !reflect.DeepEqual(value, normalized) {
					return false
				}
				delete(section, key)
			case "settings", "appConfig":
				if child, ok := value.(map[string]any); ok {
					if !stripEcho(child) {
						return false
					}
				} else if containsAPIConfig(value) {
					return false
				}
			case "fetch":
				// The original-book endpoint is a workbench setting and never
				// receives model credentials.
				if fetch, ok := value.(map[string]any); ok {
					copy := cloneAnyMap(fetch)
					delete(copy, "endpoint")
					if containsAPIConfig(copy) {
						return false
					}
				} else if containsAPIConfig(value) {
					return false
				}
			case "ai":
				if ai, ok := value.(map[string]any); ok {
					for field := range ai {
						if name := apiConfigField(field); name == "model" || name == "modelid" || name == "extrabodyjson" {
							return false
						}
					}
				}
				if containsAPIConfig(value) {
					return false
				}
			default:
				if containsAPIConfig(map[string]any{key: value}) {
					return false
				}
			}
		}
		return true
	}
	allowed = stripEcho(data)
	return hasEcho, allowed
}

func compatibilityConfigForEcho(r *http.Request, store novelfetchworkshop.Store, owner string) (map[string]any, error) {
	effective, err := compatibilitySettingsOwner(r, owner)
	if err != nil {
		return nil, err
	}
	// Novel Fetch GET exposes the safe runtime view. Compare its unchanged
	// echo against that same view, including any pending-video restrictions.
	if r.URL.Path == "/api/novel-fetch-workshop/config" {
		if reader, ok := store.(interface {
			ReadRuntimeConfig(context.Context, string) (map[string]any, error)
		}); ok {
			return reader.ReadRuntimeConfig(r.Context(), effective)
		}
	}
	if reader, ok := store.(interface {
		ReadBoundModelConfig(context.Context, string) (map[string]any, error)
	}); ok {
		return reader.ReadBoundModelConfig(r.Context(), effective)
	}
	return store.GetConfig(r.Context(), effective)
}

func compatibilitySettingsOwner(r *http.Request, owner string) (string, error) {
	auth, protected := r.Context().Value(apiConfigCompatKey{}).(BridgeAuth)
	if !protected || auth.AccountCenterStore == nil {
		return "", accountcenter.ErrForbidden
	}
	return ResolveAPIConfigOwner(r.Context(), auth.AccountCenterStore, owner)
}

func formalCompatibilityCatalog(r *http.Request, store novelfetchworkshop.Store, owner string, initial map[string]any) any {
	effective, err := compatibilitySettingsOwner(r, owner)
	if err != nil {
		return []any{}
	}
	if effective != owner || initial["modelCatalog"] == nil {
		initial, err = novelfetchworkshop.ReadRuntimeConfig(r.Context(), store, effective)
		if err != nil {
			return []any{}
		}
	}
	return initial["modelCatalog"]
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
