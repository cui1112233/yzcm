package httpapi

import (
	"net/http"

	ac "qiantie/backend/internal/accountcenter"
)

func RegisterTeamAdminCompat(root *http.ServeMux, middleware BridgeAuth) {
	admin := accountCenterAdmin{auth: middleware}
	root.Handle("GET /api/team-admin/teams", middleware.AccountCenterMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin.withDirectory(w, r, false, func(_ ac.AdminTx, d adminSnapshot) (any, error) {
			teams := []map[string]any{}
			for _, team := range d.directory().Teams {
				manager := d.person(team.ManagerUsername)
				teams = append(teams, map[string]any{
					"team": team, "manager": d.row(manager, d.team(team.ID)), "actions": team.Actions,
					"memberCount": len(team.Accounts), "coManagers": []string{},
				})
			}
			return map[string]any{"teams": teams}, nil
		})
	})))
	root.Handle("GET /api/team-admin/teams/{teamId}/members", middleware.AccountCenterMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin.withDirectory(w, r, false, func(_ ac.AdminTx, d adminSnapshot) (any, error) {
			for _, team := range d.directory().Teams {
				if team.ID == r.PathValue("teamId") {
					return map[string]any{"team": team, "members": team.Accounts}, nil
				}
			}
			return nil, ac.ErrForbidden
		})
	})))
	for _, pattern := range []string{
		"PATCH /api/team-admin/teams/{teamId}/governance",
		"PUT /api/team-admin/teams/{teamId}/co-managers",
		"PUT /api/team-admin/teams/{teamId}/members/{username}/api-scopes",
		"POST /api/team-admin/teams/{teamId}/members/{username}/status",
		"POST /api/team-admin/teams/{teamId}/members/{username}/reset-password",
	} {
		root.Handle(pattern, middleware.AccountCenterMiddleware(http.HandlerFunc(admin.retired)))
	}
}
