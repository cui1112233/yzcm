package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/apisecrets"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/novelfetchworkshop"
)

// defaultPetSetting 是后端唯一的宠物默认值，对应前端 petCatalog.js 的
// DEFAULT_PET_ID。用户已保存的 pet 会在 mergeConfigMap 中覆盖它，不会被重置。
var defaultPetSetting = map[string]any{
	"id": "little-companion", "displayName": "小家伙", "description": "小家伙，陪你创作的原创桌面宠物。", "renderer": "multi-sheet", "assetBasePath": "/pets/little-companion", "previewPose": "idle",
}

var defaultUserConfig = func() map[string]any {
	config := map[string]any{
		"provider":                "openai",
		"baseUrl":                 "https://api.openai.com/v1",
		"model":                   "gpt-4o-mini",
		"modelCatalog":            []any{},
		"modelCatalogVersion":     1,
		"storageRoot":             "",
		"productionRetentionDays": 7,
		"image": map[string]any{
			"mode": "openai_compatible", "provider": "openai_compatible", "displayName": "", "baseUrl": "", "model": "",
		},
		"video": map[string]any{
			"provider": "yd_video", "displayName": "中转亚迪",
		},
		"pet":           cloneConfigMap(defaultPetSetting),
		"tts":           map[string]any{"voice": "zh-CN-XiaoxiaoNeural", "style": "general", "speed": 1.8, "pitch": 10},
		"notifications": map[string]any{"soundEnabled": true, "petVisible": true, "soundVolume": 60},
		"avatar":        nil,
		"scriptDefaults": map[string]any{
			"textModelId": "", "imageModelId": "", "videoModelKey": "",
			"imageAspectRatio": "9:16", "videoAspectRatio": "9:16", "videoResolution": "720p",
		},
	}
	for key, value := range novelfetchworkshop.DefaultWorkshopSettings() {
		config[key] = value
	}
	return config
}()

type UserConfigStore struct {
	BaseDir string
	mu      sync.Mutex
	secrets *configSecretProvider
	journal *novelfetchworkshop.MySQLStore
}

func NewUserConfigStore(baseDir string) *UserConfigStore {
	trimmed := strings.TrimSpace(baseDir)
	return &UserConfigStore{BaseDir: trimmed, secrets: newConfigSecretProvider(trimmed)}
}

// Bind the journal before background workers can request effective preferences.
func (s *UserConfigStore) UseLegacySettingJournal(journal *novelfetchworkshop.MySQLStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.journal = journal
}

func (s *UserConfigStore) LegacyModelFileLocker() sync.Locker { return &s.mu }

func cloneConfigMap(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		if nested, ok := value.(map[string]any); ok {
			result[key] = cloneConfigMap(nested)
		} else if list, ok := value.([]any); ok {
			result[key] = append([]any(nil), list...)
		} else {
			result[key] = value
		}
	}
	return result
}

func mergeConfigMap(base, patch map[string]any) map[string]any {
	result := cloneConfigMap(base)
	for key, value := range patch {
		if nested, ok := value.(map[string]any); ok {
			previous, _ := result[key].(map[string]any)
			if previous == nil {
				previous = map[string]any{}
			}
			result[key] = mergeConfigMap(previous, nested)
			continue
		}
		result[key] = value
	}
	return result
}

func (s *UserConfigStore) configPath(username string) (string, error) {
	if s == nil || strings.TrimSpace(s.BaseDir) == "" {
		return "", fmt.Errorf("用户配置存储未配置")
	}
	return UserScopedPath(s.BaseDir, username, "api-config.json")
}

func (s *UserConfigStore) readStandardFile(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		return nil, err
	}
	if saved == nil {
		return nil, errors.New("invalid standard configuration")
	}
	if encryptedConfigSecretCount(saved) > 0 {
		cipher, err := s.secrets.secrets.ReadCipher()
		if err != nil {
			return nil, err
		}
		if cipher == nil {
			return nil, errors.New("missing configuration encryption key")
		}
		transformConfigSecrets(saved, func(value string) string {
			if err != nil || !apisecrets.IsEncrypted(value) {
				return value
			}
			var plain string
			plain, err = cipher.Decrypt(value)
			return plain
		})
		if err != nil {
			return nil, err
		}
	}
	return saved, nil
}
func (s *UserConfigStore) GetConfig(ctx context.Context, username string) (map[string]any, error) {
	if s == nil || s.BaseDir == "" {
		return cloneConfigMap(defaultUserConfig), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.effectiveConfig(ctx, username)
}
func (s *UserConfigStore) effectiveConfig(ctx context.Context, owner string) (map[string]any, error) {
	path, err := s.configPath(owner)
	if err != nil {
		return nil, err
	}
	if _, err := s.secrets.importDeferredSettings(ctx, owner, path, s.journal, true); err != nil {
		return nil, err
	}
	var edits settingsEdits
	var active bool
	if s.journal != nil {
		edits, _, active, err = s.journal.LoadLegacySettingEdits(ctx, owner)
		if err != nil {
			return nil, err
		}
	}
	saved, err := s.readStandardFile(path)
	if err != nil {
		return nil, err
	}
	if saved["ciphertext"] != nil || saved["fingerprint"] != nil {
		if !active && s.journal != nil {
			state, found, err := s.journal.DiscoverLegacyConfigMigration(ctx, owner)
			if err != nil {
				return nil, err
			}
			active = found && state.Phase != novelfetchworkshop.LegacyComplete
		}
		if !active {
			return nil, errors.New("standard configuration contains a migration envelope")
		}
		saved = nil
	}
	config := mergeConfigMap(defaultUserConfig, saved)
	return config, edits.apply(config)
}
func (s *UserConfigStore) write(username string, config map[string]any) error {
	return s.writeConfig(username, config, false)
}

func (s *UserConfigStore) writeAuthenticatedConfig(username string, config map[string]any) error {
	return s.writeConfig(username, config, true)
}

func (s *UserConfigStore) writeConfig(username string, config map[string]any, strict bool) error {
	if s == nil || s.BaseDir == "" {
		return fmt.Errorf("用户配置存储未配置")
	}
	path, err := s.configPath(username)
	if err != nil {
		return fmt.Errorf("用户名不合法")
	}
	// 落盘加密：在深拷贝上转换，避免把调用方内存里的明文替换成密文。
	persisted := config
	if cloned, ok := deepCloneConfigValue(config).(map[string]any); ok {
		if strict {
			if err := s.secrets.encryptStrict(cloned); err != nil {
				return err
			}
		} else {
			s.secrets.encrypt(cloned)
		}
		persisted = cloned
	}
	return writeUserConfigDocument(path, persisted)
}

func writeUserConfigDocument(path string, persisted any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func internalPreferenceField(key string) bool {
	key = apiConfigField(key)
	return strings.HasPrefix(key, "modelcataloglegacy") || strings.HasPrefix(key, "migration") || strings.HasSuffix(key, "ciphertext") || strings.HasSuffix(key, "fingerprint") || key == "revision" || key == "configrevision" || key == "patchenvelope" || key == "candidateenvelope"
}
func stripInternalPreferenceFields(value any) {
	switch v := value.(type) {
	case map[string]any:
		for k, item := range v {
			if internalPreferenceField(k) {
				delete(v, k)
			} else {
				stripInternalPreferenceFields(item)
			}
		}
	case []any:
		for _, item := range v {
			stripInternalPreferenceFields(item)
		}
	}
}
func publicUserConfig(config map[string]any) map[string]any {
	result, _ := deepCloneConfigValue(config).(map[string]any)
	stripInternalPreferenceFields(result)

	// 接口配置只暴露「已配置/未配置」，密钥（含自定义渠道 apiKey）永不下发。
	if stored, ok := result[apiIntegrationsSettingKey].(map[string]any); ok {
		result[apiIntegrationsSettingKey] = apiIntegrationsView(stored)
	}
	apiKey, _ := result["apiKey"].(string)
	delete(result, "apiKey")
	result["hasApiKey"] = strings.TrimSpace(apiKey) != ""
	if image, ok := result["image"].(map[string]any); ok {
		key, _ := image["apiKey"].(string)
		delete(image, "apiKey")
		image["hasApiKey"] = strings.TrimSpace(key) != ""
	}
	if video, ok := result["video"].(map[string]any); ok {
		apiKey, _ := video["apiKey"].(string)
		ydKey, _ := video["ydApiKey"].(string)
		h3Key, _ := video["h3ApiKey"].(string)
		delete(video, "apiKey")
		delete(video, "ydApiKey")
		delete(video, "h3ApiKey")
		video["hasApiKey"] = strings.TrimSpace(apiKey) != "" || strings.TrimSpace(ydKey) != "" || strings.TrimSpace(h3Key) != ""
		video["ydHasApiKey"] = strings.TrimSpace(apiKey) != "" || strings.TrimSpace(ydKey) != ""
		video["h3HasApiKey"] = strings.TrimSpace(h3Key) != ""
	}
	if catalog, ok := result["modelCatalog"].([]any); ok {
		for _, raw := range catalog {
			model, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			key, _ := model["credential"].(string)
			delete(model, "credential")
			model["hasApiKey"] = strings.TrimSpace(key) != ""
			// The current settings page uses the clearer credential name. Keep
			// hasApiKey as a compatibility alias for older pages.
			model["hasCredential"] = strings.TrimSpace(key) != ""
		}
	}
	return result
}

func preferenceOnlyPatch(input map[string]any) map[string]any {
	allowed := map[string]any{}
	for _, key := range []string{"storageRoot", "productionRetentionDays", "pet", "tts", "notifications", "avatar", "scriptDefaults"} {
		if value, ok := input[key]; ok {
			allowed[key] = value
		}
	}
	return allowed
}

func configManagementState(identity BridgeIdentity, profiles *MemberProfileStore, accounts auth.Store) (bool, any) {
	if identity.IsOwner {
		return true, nil
	}
	if profiles == nil || accounts == nil {
		return false, nil
	}
	member := profiles.member(identity.Username, accounts)
	if member == nil {
		return false, nil
	}
	role, _ := member["role"].(string)
	if role == "dev" || role == "manager" {
		return true, nil
	}
	if role == "member" {
		if boundTo, ok := member["boundTo"].(string); ok && strings.TrimSpace(boundTo) != "" {
			return false, boundTo
		}
	}
	return false, nil
}

func mergeManagedModelCatalog(ctx context.Context, config map[string]any, catalogStore novelfetchworkshop.Store, username string) error {
	if catalogStore == nil || strings.TrimSpace(username) == "" {
		return nil
	}
	settings, err := catalogStore.GetConfig(ctx, username)
	if err != nil {
		return err
	}
	if catalog, ok := settings["modelCatalog"].([]any); ok {
		config["modelCatalog"] = catalog
	}
	if version, ok := settings["modelCatalogVersion"]; ok {
		config["modelCatalogVersion"] = version
	}
	return nil
}

// applyManagedTextModelPublic keeps the old top-level config fields useful for
// the settings page while making them reflect the enabled managed text model.
// The catalog remains the source of truth; this is only a compatibility view.
func applyManagedTextModelPublic(config map[string]any) {
	catalog, ok := config["modelCatalog"].([]any)
	if !ok {
		return
	}
	for _, raw := range catalog {
		model, ok := raw.(map[string]any)
		if !ok || model["kind"] != "text" || model["enabled"] != true {
			continue
		}
		modelID := strings.TrimSpace(fmt.Sprint(model["modelId"]))
		baseURL := strings.TrimSpace(fmt.Sprint(model["baseUrl"]))
		if modelID == "" || modelID == "<nil>" || baseURL == "" || baseURL == "<nil>" {
			continue
		}
		config["provider"] = "managed"
		config["baseUrl"] = baseURL
		config["model"] = modelID
		config["hasApiKey"] = model["hasApiKey"] == true
		return
	}
}

func registerUserConfigRoutes(root *http.ServeMux, middleware BridgeAuth, store *UserConfigStore, accounts auth.Store, catalogStore novelfetchworkshop.Store, profiles ...*MemberProfileStore) {
	if root == nil || middleware.AuthStore == nil || store == nil || accounts == nil {
		return
	}
	if journal, ok := catalogStore.(*novelfetchworkshop.MySQLStore); ok {
		store.UseLegacySettingJournal(journal)
	}
	view := func(w http.ResponseWriter, req *http.Request, identity BridgeIdentity, config map[string]any) {
		access := apiConfigPolicy(req.Context(), middleware.AccountCenterStore)
		if !access.custom {
			result := preferenceOnlyPatch(config)
			stripInternalPreferenceFields(result)
			if middleware.AccountCenterStore != nil && access.identity.Subject.Role == accountcenter.RoleMember {
				if owner, err := ResolveAPIConfigOwner(req.Context(), middleware.AccountCenterStore, identity.Username); err == nil {
					result["managedBy"] = owner
				}
			}
			writeJSON(w, http.StatusOK, access.response(redactAPIConfig(result).(map[string]any)))
			return
		}
		delete(config, "modelCatalog")
		if err := mergeManagedModelCatalog(req.Context(), config, catalogStore, identity.Username); err != nil {
			writeAuthError(w, http.StatusServiceUnavailable, "读取模型配置失败")
			return
		}
		result := publicUserConfig(config)
		applyManagedTextModelPublic(result)
		result["managedBy"] = nil
		writeJSON(w, http.StatusOK, access.response(redactAPIConfig(result).(map[string]any)))
		return
	}
	root.Handle("GET /api/config", middleware.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		identity, ok := BridgeIdentityFromContext(req.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		store.mu.Lock()
		config, err := store.effectiveConfig(req.Context(), identity.Username)
		store.mu.Unlock()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "配置读取失败"})
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		view(w, req, identity, config)
	})))
	root.Handle("POST /api/config", middleware.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		identity, ok := BridgeIdentityFromContext(req.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		var patch map[string]any
		if json.NewDecoder(http.MaxBytesReader(w, req.Body, 256<<10)).Decode(&patch) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求内容无效"})
			return
		}
		access := apiConfigPolicy(req.Context(), middleware.AccountCenterStore)
		if !access.custom {
			if len(preferenceOnlyPatch(patch)) != len(patch) || containsAPIConfig(patch) {
				writeAccountCenterForbidden(w)
				return
			}
		}
		if containsConfigField(patch, isManagedModelField) || containsConfigField(patch, internalPreferenceField) {
			writeAccountCenterForbidden(w)
			return
		}
		store.mu.Lock()
		config, err := store.effectiveConfig(req.Context(), identity.Username)
		if err == nil {
			config = mergeConfigMap(config, patch)
			err = store.writeSettingsPatch(req.Context(), identity.Username, config, fieldEdits(nil, patch))
		}
		store.mu.Unlock()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "配置保存失败"})
			return
		}
		view(w, req, identity, config)
	})))
}
