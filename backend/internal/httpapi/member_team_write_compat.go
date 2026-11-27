package httpapi

import (
	"net/http"
	"strings"

	ac "qiantie/backend/internal/accountcenter"
)

func RegisterMemberTeamWriteCompat(root *http.ServeMux, middleware BridgeAuth) {
	admin := accountCenterAdmin{auth: middleware}
	register := func(pattern string, handler http.HandlerFunc) {
		root.Handle(pattern, middleware.AccountCenterMiddleware(handler))
	}
	register("POST /api/member/team/members", admin.createLegacyTeamMember)
	register("POST /api/member/team/members/{username}/status", admin.compatibilityStatus)
	register("POST /api/member/team/members/{username}/archive", admin.accountChange("archive"))
	register("POST /api/member/team/members/{username}/restore", admin.accountChange("restore"))
	for _, pattern := range []string{
		"PATCH /api/member/team/meta/{teamId}",
		"POST /api/member/team/invites",
		"PATCH /api/member/team/governance/{username}",
		"PATCH /api/member/team/members/{username}",
		"POST /api/member/team/members/{username}/reset-password",
		"POST /api/member/team/members/{username}/api",
		"PUT /api/member/team/members/{username}/api-scopes",
	} {
		register(pattern, admin.retired)
	}
}

func (s accountCenterAdmin) createLegacyTeamMember(w http.ResponseWriter, r *http.Request) {
	s.withDirectory(w, r, true, func(tx ac.AdminTx, d adminSnapshot) (any, error) {
		input, err := decodeAccountAdminInput(w, r)
		if err != nil {
			return nil, err
		}
		manager := strings.TrimSpace(input.BoundTo)
		if d.actor.Role == ac.RoleManager {
			manager = d.actor.Username
		}
		var team ac.Team
		for _, candidate := range d.Teams {
			if candidate.ArchivedAt == nil && strings.EqualFold(candidate.ManagerUsername, manager) && d.manages(candidate) {
				team = candidate
				break
			}
		}
		if team.ID == "" || !(ac.Policy{}).Can(d.actor, ac.ActionCreateAccount, ac.PolicyTarget{TeamID: team.ID, NewRole: ac.RoleMember, InManagedTeam: true}) {
			return nil, ac.ErrForbidden
		}
		input.Role = ac.RoleMember
		change, err := newAccountAdminChange(input)
		if err != nil {
			return nil, err
		}
		change.Action, change.TeamID = "create_account", team.ID
		if _, err := s.apply(r, tx, d, change); err != nil {
			return nil, err
		}
		person := ac.Identity{Username: change.Username, DisplayName: change.DisplayName, Role: change.Role, Active: !change.StartDisabled}
		return map[string]any{"ok": true, "member": d.row(person, team)}, nil
	})
}
