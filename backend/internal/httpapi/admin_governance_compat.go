package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"

	ac "qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
)

func RegisterAdminGovernanceCompat(root *http.ServeMux, middleware BridgeAuth, _ auth.Store, profiles *MemberProfileStore) {
	admin := accountCenterAdmin{auth: middleware}
	register := func(pattern string, handler http.HandlerFunc) {
		root.Handle(pattern, middleware.AccountCenterMiddleware(handler))
	}
	register("POST /api/admin/accounts", admin.createUngroupedAccount)
	register("POST /api/admin/accounts/{username}/status", admin.compatibilityStatus)
	register("POST /api/admin/accounts/{username}/reset-password", admin.retired)

	for _, entry := range []struct {
		path string
		file string
		key  string
	}{
		{"/api/admin/applications", "applications.json", "applications"},
		{"/api/admin/grants", "grants.json", "grants"},
	} {
		register("GET "+entry.path, func(w http.ResponseWriter, r *http.Request) {
			admin.withDirectory(w, r, false, func(_ ac.AdminTx, d adminSnapshot) (any, error) {
				if d.actor.Role != ac.RoleFounder {
					return nil, ac.ErrForbidden
				}
				if profiles == nil {
					return nil, errLegacyGovernanceUnavailable
				}
				return map[string]any{entry.key: readAdminGovernanceArray(profiles, entry.file)}, nil
			})
		})
	}
	for _, pattern := range []string{
		"POST /api/admin/applications/{id}/approve",
		"POST /api/admin/applications/{id}/reject",
		"POST /api/admin/grants",
		"DELETE /api/admin/grants/{id}",
	} {
		register(pattern, func(w http.ResponseWriter, r *http.Request) {
			admin.withDirectory(w, r, false, func(_ ac.AdminTx, d adminSnapshot) (any, error) {
				if d.actor.Role != ac.RoleFounder {
					return nil, ac.ErrForbidden
				}
				return nil, errLegacyGovernanceUnavailable
			})
		})
	}
}

func readAdminGovernanceArray(profiles *MemberProfileStore, name string) []map[string]any {
	path := adminSystemFile(profiles.SystemDir, name)
	raw, err := os.ReadFile(path)
	if err != nil {
		return []map[string]any{}
	}
	var rows []map[string]any
	if json.Unmarshal(raw, &rows) != nil || rows == nil {
		return []map[string]any{}
	}
	if name == "applications.json" {
		for _, row := range rows {
			delete(row, "passwordHash")
			delete(row, "passwordAlgorithm")
		}
	}
	return rows
}

func writeAdminGovernanceArray(profiles *MemberProfileStore, name string, rows []map[string]any) error {
	path := adminSystemFile(profiles.SystemDir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readAdminGovernancePrivateArray(path string) []map[string]any {
	raw, err := os.ReadFile(path)
	if err != nil {
		return []map[string]any{}
	}
	var rows []map[string]any
	if json.Unmarshal(raw, &rows) != nil || rows == nil {
		return []map[string]any{}
	}
	return rows
}

func cloneAdminGovernanceMap(input map[string]any) map[string]any {
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}
