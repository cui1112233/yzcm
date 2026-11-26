package accountcenter

import "strings"

type Action string

const (
	ActionCreateTeam      Action = "create_team"
	ActionGrantTeamCreate Action = "grant_team_create"
	ActionManageTeam      Action = "manage_team"
	ActionRestoreTeam     Action = "restore_team"
	ActionCreateAccount   Action = "create_account"
	ActionGrantDeveloper  Action = "grant_developer"
	ActionSetRole         Action = "set_role"
	ActionManageAccount   Action = "manage_account"
	ActionResetPassword   Action = "reset_password"
	ActionImpersonate     Action = "impersonate"
	ActionPurgeAccount    Action = "purge_account"
)

type PolicyTarget struct {
	Identity Identity
	NewRole  string
	TeamID   string
	// InManagedTeam comes from a server-side check of the actor's team scope
	// and the target's membership for account actions. It is never request input.
	InManagedTeam       bool
	TeamCreationGranted bool
}

type Policy struct{}

// Can authorizes one management action. The actor, existing target identity,
// and team scope must come from authenticated, server-resolved records.
func (Policy) Can(actor Identity, action Action, target PolicyTarget) bool {
	if actor.Username == "" || !actor.Active || actor.ArchivedAt != nil {
		return false
	}
	founder := actor.Role == RoleFounder
	developer := actor.Role == RoleDeveloper
	manager := actor.Role == RoleManager
	switch action {
	case ActionCreateTeam:
		return founder || developer && target.TeamCreationGranted
	case ActionManageTeam:
		return target.TeamID != "" && (founder || developer && target.InManagedTeam)
	case ActionRestoreTeam:
		return target.TeamID != "" && (founder || (developer || manager) && target.InManagedTeam)
	case ActionCreateAccount:
		if target.NewRole == RoleManager {
			return founder || developer && target.InManagedTeam
		}
		return target.NewRole == RoleMember && (founder || (developer || manager) && target.InManagedTeam)
	}

	account := target.Identity
	if account.Username == "" || strings.EqualFold(actor.Username, account.Username) {
		return false
	}
	teamAccount := account.Role == RoleManager || account.Role == RoleMember
	if !teamAccount && account.Role != RoleDeveloper {
		return false
	}
	canManageMember := manager && account.Role == RoleMember && target.InManagedTeam
	canSupportAccount := founder || developer && teamAccount || canManageMember
	switch action {
	case ActionGrantTeamCreate:
		return founder && account.Role == RoleDeveloper
	case ActionGrantDeveloper, ActionPurgeAccount:
		return founder
	case ActionSetRole:
		if target.NewRole == RoleDeveloper {
			return founder
		}
		return (target.NewRole == RoleManager || target.NewRole == RoleMember) && (founder || developer && teamAccount && target.InManagedTeam)
	case ActionManageAccount:
		return founder || developer && teamAccount && target.InManagedTeam || canManageMember
	case ActionResetPassword:
		return canSupportAccount
	case ActionImpersonate:
		return canSupportAccount && account.Active && account.ArchivedAt == nil
	default:
		return false
	}
}
