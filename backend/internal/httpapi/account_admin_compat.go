package httpapi

import (
	"net/http"
	"strings"

	ac "qiantie/backend/internal/accountcenter"
)

var managedUsernamePattern = accountCenterUsername

func managedAccountError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// RegisterAccountAdminReadCompat projects the canonical directory to old readers.
func RegisterAccountAdminReadCompat(root *http.ServeMux, middleware BridgeAuth) {
	admin := accountCenterAdmin{auth: middleware}
	root.Handle("GET /api/account-admin/accounts", middleware.AccountCenterMiddleware(http.HandlerFunc(admin.compatibilityAccounts)))
	root.Handle("POST /api/account-admin/accounts", middleware.AccountCenterMiddleware(http.HandlerFunc(admin.createUngroupedAccount)))
	root.Handle("PATCH /api/account-admin/accounts/{username}", middleware.AccountCenterMiddleware(http.HandlerFunc(admin.retired)))
}

func (admin accountCenterAdmin) compatibilityAccounts(w http.ResponseWriter, r *http.Request) {
	admin.withDirectory(w, r, false, func(_ ac.AdminTx, d adminSnapshot) (any, error) {
		if d.actor.Role == ac.RoleManager {
			return nil, ac.ErrForbidden
		}
		result := []map[string]any{}
		role := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role")))
		query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
		directory := d.directory()
		appendRow := func(person accountAdminRow, manager string) {
			oldRole := legacyAccountRole(person.Role)
			if role != "" && role != oldRole || query != "" && !strings.Contains(strings.ToLower(person.Username+" "+person.DisplayName), query) {
				return
			}
			if r.URL.Query().Get("active") == "true" && (!person.Active || person.ArchivedAt != nil) {
				return
			}
			result = append(result, legacyDirectoryAccount(person, manager))
		}
		for _, team := range directory.Teams {
			for _, person := range team.Accounts {
				if d.accountTeam(person.Username).ID == team.ID && (!person.Historical || team.ArchivedAt != nil) {
					appendRow(person, team.ManagerUsername)
				}
			}
		}
		for _, person := range directory.Ungrouped {
			appendRow(person, "")
		}
		return map[string]any{"accounts": result}, nil
	})
}

func legacyDirectoryAccount(person accountAdminRow, manager string) map[string]any {
	return map[string]any{
		"username": person.Username, "displayName": person.DisplayName, "role": legacyAccountRole(person.Role),
		"active": person.Active && person.ArchivedAt == nil, "boundTo": manager,
		"isOwner": person.Role == ac.RoleFounder, "archivedAt": person.ArchivedAt,
		"balance": person.Balance, "monthConsumption": person.MonthConsumption,
		"historical": person.Historical, "restoreConflict": person.RestoreConflict,
	}
}
