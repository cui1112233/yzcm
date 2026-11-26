package httpapi

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"time"

	ac "qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
)

type accountLister interface {
	ListAccounts(context.Context) ([]auth.Account, error)
}

var managedUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,31}$`)

func managedAccountError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func managedProfile(profiles *MemberProfileStore, username string) map[string]any {
	for _, row := range profiles.readArray("member-profiles.json") {
		if row["username"] == username {
			return row
		}
	}
	return nil
}

func saveManagedProfile(profiles *MemberProfileStore, username, displayName, role string, monthly any) error {
	profiles.mu.Lock()
	defer profiles.mu.Unlock()
	rows := profiles.readArray("member-profiles.json")
	profile := managedProfileUnlocked(rows, username)
	if profile == nil {
		profile = map[string]any{"username": username}
		rows = append(rows, profile)
	}
	profile["displayName"], profile["role"] = displayName, role
	if _, exists := profile["boundTo"]; !exists {
		profile["boundTo"] = nil
	}
	profile["monthlyTokenLimit"] = monthly
	profile["updatedAt"] = time.Now().UTC().Format(time.RFC3339)
	return profiles.writeArray("member-profiles.json", rows)
}

func managedProfileUnlocked(rows []map[string]any, username string) map[string]any {
	for _, row := range rows {
		if row["username"] == username {
			return row
		}
	}
	return nil
}

func managedAccountRow(account auth.Account, profile map[string]any) map[string]any {
	displayName, role := account.Username, "member"
	var boundTo, monthly any
	if profile != nil {
		if value, ok := profile["displayName"].(string); ok && strings.TrimSpace(value) != "" {
			displayName = value
		}
		if value, ok := profile["role"].(string); ok && value != "" {
			role = value
		}
		boundTo, monthly = profile["boundTo"], profile["monthlyTokenLimit"]
	}
	if account.IsOwner {
		role = "dev"
	}
	return map[string]any{"username": account.Username, "displayName": displayName, "role": role, "active": account.Active, "isOwner": account.IsOwner, "updatedAt": account.UpdatedAt, "boundTo": boundTo, "teamOwner": nil, "apiScopes": []any{}, "monthlyTokenLimit": monthly, "presence": map[string]any{"online": false}, "usage": map[string]any{"month": map[string]any{}}}
}

// RegisterAccountAdminReadCompat projects the canonical directory to old readers.
func RegisterAccountAdminReadCompat(root *http.ServeMux, middleware BridgeAuth) {
	admin := accountCenterAdmin{auth: middleware}
	root.Handle("GET /api/account-admin/accounts", middleware.AccountCenterMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin.withDirectory(w, r, false, func(_ ac.AdminTx, d adminSnapshot) (any, error) {
			if d.actor.Role == ac.RoleManager {
				return nil, ac.ErrForbidden
			}
			result := []map[string]any{}
			role := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("role")))
			query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("query")))
			directory := d.directory()
			appendRow := func(person accountAdminRow, manager string) {
				oldRole := person.Role
				if oldRole == ac.RoleFounder || oldRole == ac.RoleDeveloper {
					oldRole = "dev"
				}
				if role != "" && role != oldRole || query != "" && !strings.Contains(strings.ToLower(person.Username+" "+person.DisplayName), query) {
					return
				}
				if r.URL.Query().Get("active") == "true" && (!person.Active || person.ArchivedAt != nil) {
					return
				}
				result = append(result, map[string]any{
					"username": person.Username, "displayName": person.DisplayName, "role": oldRole,
					"active": person.Active && person.ArchivedAt == nil, "boundTo": manager,
					"isOwner": person.Role == ac.RoleFounder, "archivedAt": person.ArchivedAt,
					"balance": person.Balance, "monthConsumption": person.MonthConsumption,
				})
			}
			for _, team := range directory.Teams {
				for _, person := range team.Accounts {
					appendRow(person, team.ManagerUsername)
				}
			}
			for _, person := range directory.Ungrouped {
				appendRow(person, "")
			}
			return map[string]any{"accounts": result}, nil
		})
	})))
	root.Handle("POST /api/account-admin/accounts", middleware.AccountCenterMiddleware(http.HandlerFunc(admin.createUngroupedAccount)))
	root.Handle("PATCH /api/account-admin/accounts/{username}", middleware.AccountCenterMiddleware(http.HandlerFunc(admin.retired)))
}
