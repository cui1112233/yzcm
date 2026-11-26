package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	ac "qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
)

const accountCenterAdminPath = "/api/account-center/admin"

var accountCenterUsername = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{1,31}$`)
var errLegacyGovernanceUnavailable = errors.New("legacy governance unavailable")

type accountCenterAdmin struct {
	auth BridgeAuth
}

type accountAdminRow struct {
	Username         string     `json:"username"`
	DisplayName      string     `json:"displayName"`
	Role             string     `json:"role"`
	Active           bool       `json:"active"`
	ArchivedAt       *time.Time `json:"archivedAt"`
	Balance          *int64     `json:"balance"`
	MonthConsumption *int64     `json:"monthConsumption"`
	Actions          []string   `json:"actions"`
}

type accountAdminTeam struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	ManagerUsername string            `json:"managerUsername"`
	ArchivedAt      *time.Time        `json:"archivedAt"`
	Actions         []string          `json:"actions"`
	Accounts        []accountAdminRow `json:"accounts"`
}

type accountAdminDirectory struct {
	Actions   []string           `json:"actions"`
	Teams     []accountAdminTeam `json:"teams"`
	Ungrouped []accountAdminRow  `json:"ungrouped"`
}

type accountAdminInput struct {
	Name            string `json:"name"`
	ManagerUsername string `json:"managerUsername"`
	BoundTo         string `json:"boundTo"`
	Username        string `json:"username"`
	DisplayName     string `json:"displayName"`
	Password        string `json:"password"`
	Role            string `json:"role"`
	Active          *bool  `json:"active"`
}

type adminSnapshot struct {
	ac.AdminDirectory
	actor ac.Identity
}

func registerAccountCenterAdmin(root *http.ServeMux, auth BridgeAuth) {
	admin := accountCenterAdmin{auth: auth}
	register := func(method, path string, handler http.HandlerFunc) {
		root.Handle(method+" "+accountCenterAdminPath+path, auth.AccountCenterMiddleware(handler))
	}
	register("GET", "/teams", admin.list)
	register("GET", "/audit", admin.audit)
	register("POST", "/teams", admin.createTeam)
	register("POST", "/teams/{teamId}/accounts", admin.createAccount)
	register("POST", "/accounts", admin.createUngroupedAccount)
	for _, action := range []string{"archive_team", "restore_team"} {
		register("POST", "/teams/{teamId}/"+action, admin.teamChange(action))
	}
	for _, action := range []string{"set_member", "set_manager", "set_developer", "enable", "disable", "archive", "restore", "purge_preflight", "purge"} {
		register("POST", "/accounts/{username}/"+action, admin.accountChange(action))
	}
}

func (s accountCenterAdmin) withDirectory(w http.ResponseWriter, r *http.Request, write bool, fn func(ac.AdminTx, adminSnapshot) (any, error)) {
	identity, _ := AccountCenterIdentityFromContext(r.Context())
	if identity.ImpersonationID != "" {
		writeAccountCenterForbidden(w)
		return
	}
	store, ok := s.auth.AccountCenterStore.(ac.AdminStore)
	if !ok {
		writeAccountCenterForbidden(w)
		return
	}
	var result any
	err := store.WithAdmin(r.Context(), write, func(tx ac.AdminTx) error {
		directory, err := tx.Directory(r.Context())
		if err != nil {
			return err
		}
		snapshot := adminSnapshot{AdminDirectory: directory}
		snapshot.actor = snapshot.person(identity.Actor.Username)
		if !snapshot.actor.Active || snapshot.actor.ArchivedAt != nil || snapshot.actor.Role == ac.RoleMember {
			return ac.ErrForbidden
		}
		result, err = fn(tx, snapshot)
		return err
	})
	switch {
	case errors.Is(err, ac.ErrForbidden), errors.Is(err, ac.ErrNotFound):
		writeAccountCenterForbidden(w)
	case errors.Is(err, ac.ErrConflict):
		managedAccountError(w, http.StatusConflict, "操作与现有账号或团队冲突")
	case errors.Is(err, ac.ErrInvalid):
		managedAccountError(w, http.StatusBadRequest, "请求内容无效")
	case errors.Is(err, errLegacyGovernanceUnavailable):
		writeJSON(w, http.StatusGone, map[string]string{"error": "此操作暂不可用，请前往账号与角色页面", "path": "/accounts"})
	case err != nil:
		managedAccountError(w, http.StatusInternalServerError, "账号与团队服务暂不可用")
	default:
		writeJSON(w, http.StatusOK, result)
	}
}

func (s adminSnapshot) person(username string) ac.Identity {
	for _, person := range s.People {
		if strings.EqualFold(person.Username, username) {
			return person
		}
	}
	return ac.Identity{}
}

func (s adminSnapshot) team(id string) ac.Team {
	for _, team := range s.Teams {
		if team.ID == id {
			return team
		}
	}
	return ac.Team{}
}

func (s adminSnapshot) manages(team ac.Team) bool {
	if team.ID == "" {
		return false
	}
	switch s.actor.Role {
	case ac.RoleFounder:
		return true
	case ac.RoleManager:
		return strings.EqualFold(team.ManagerUsername, s.actor.Username)
	case ac.RoleDeveloper:
		if strings.EqualFold(team.CreatedBy, s.actor.Username) || strings.EqualFold(team.ManagerUsername, s.actor.Username) {
			return true
		}
		for _, grant := range s.Grants {
			if grant.TeamID == team.ID && strings.EqualFold(grant.Username, s.actor.Username) {
				return true
			}
		}
	}
	return false
}

func (s adminSnapshot) accountTeam(username string) ac.Team {
	var archived ac.Team
	for _, membership := range s.Memberships {
		if strings.EqualFold(membership.Username, username) {
			team := s.team(membership.TeamID)
			if team.ArchivedAt == nil {
				return team
			}
			archived = team
		}
	}
	return archived
}

func (s adminSnapshot) accountActions(person ac.Identity, team ac.Team) []string {
	actions := []string{}
	policy := ac.Policy{}
	target := ac.PolicyTarget{Identity: person, TeamID: team.ID, InManagedTeam: s.manages(team)}
	if policy.Can(s.actor, ac.ActionPurgeAccount, target) && person.ArchivedAt != nil {
		actions = append(actions, "purge_preflight")
	}
	if team.ArchivedAt != nil {
		return actions
	}
	if policy.Can(s.actor, ac.ActionManageAccount, target) {
		switch {
		case person.ArchivedAt != nil:
			return append(actions, "restore")
		case person.Active:
			actions = append(actions, "disable", "archive")
		default:
			actions = append(actions, "enable", "archive")
		}
	}
	if person.ArchivedAt != nil {
		return actions
	}
	// The manager recorded on the team must remain eligible to manage it.
	for _, role := range []string{ac.RoleMember, ac.RoleManager, ac.RoleDeveloper} {
		if role == person.Role || (role == ac.RoleMember && strings.EqualFold(team.ManagerUsername, person.Username)) {
			continue
		}
		target.NewRole = role
		action := ac.ActionSetRole
		if role == ac.RoleDeveloper || person.Role == ac.RoleDeveloper {
			action = ac.ActionGrantDeveloper
		}
		if policy.Can(s.actor, action, target) {
			actions = append(actions, "set_"+role)
		}
	}
	if policy.Can(s.actor, ac.ActionImpersonate, target) {
		actions = append(actions, "switch_account")
	}
	return actions
}

func (s adminSnapshot) teamActions(team ac.Team) []string {
	actions := []string{}
	policy := ac.Policy{}
	target := ac.PolicyTarget{TeamID: team.ID, InManagedTeam: s.manages(team), NewRole: ac.RoleMember}
	if policy.Can(s.actor, ac.ActionManageTeam, target) {
		if team.ArchivedAt != nil {
			return append(actions, "restore_team")
		}
		actions = append(actions, "archive_team")
	}
	if team.ArchivedAt == nil && policy.Can(s.actor, ac.ActionCreateAccount, target) {
		actions = append(actions, "create_member")
		target.NewRole = ac.RoleManager
		if policy.Can(s.actor, ac.ActionCreateAccount, target) {
			actions = append(actions, "create_manager")
		}
	}
	return actions
}

func (s adminSnapshot) row(person ac.Identity, team ac.Team) accountAdminRow {
	return accountAdminRow{
		Username: person.Username, DisplayName: person.DisplayName, Role: person.Role,
		Active: person.Active, ArchivedAt: person.ArchivedAt, Actions: s.accountActions(person, team),
	}
}

func (s adminSnapshot) directory() accountAdminDirectory {
	result := accountAdminDirectory{Actions: []string{}, Teams: []accountAdminTeam{}, Ungrouped: []accountAdminRow{}}
	if (ac.Policy{}).Can(s.actor, ac.ActionCreateTeam, ac.PolicyTarget{}) {
		result.Actions = append(result.Actions, "create_team")
	}
	if s.actor.Role == ac.RoleFounder {
		result.Actions = append(result.Actions, "create_member", "create_manager", "view_audit")
	}
	groups := make(map[string]int)
	for _, team := range s.Teams {
		if s.manages(team) {
			groups[team.ID] = len(result.Teams)
			result.Teams = append(result.Teams, accountAdminTeam{
				ID: team.ID, Name: team.Name, ManagerUsername: team.ManagerUsername,
				ArchivedAt: team.ArchivedAt, Actions: s.teamActions(team), Accounts: []accountAdminRow{},
			})
		}
	}
	for _, person := range s.People {
		team := s.accountTeam(person.Username)
		if index, visible := groups[team.ID]; visible {
			result.Teams[index].Accounts = append(result.Teams[index].Accounts, s.row(person, team))
		} else if team.ID == "" && s.actor.Role == ac.RoleFounder {
			result.Ungrouped = append(result.Ungrouped, s.row(person, team))
		}
	}
	return result
}

func (s accountCenterAdmin) list(w http.ResponseWriter, r *http.Request) {
	s.withDirectory(w, r, false, func(_ ac.AdminTx, d adminSnapshot) (any, error) {
		return d.directory(), nil
	})
}

func (s accountCenterAdmin) audit(w http.ResponseWriter, r *http.Request) {
	s.withDirectory(w, r, false, func(tx ac.AdminTx, d adminSnapshot) (any, error) {
		teamID := r.URL.Query().Get("teamId")
		if (teamID == "" && d.actor.Role != ac.RoleFounder) || (teamID != "" && !d.manages(d.team(teamID))) {
			return nil, ac.ErrForbidden
		}
		events, err := tx.ListAudit(r.Context(), ac.AuditFilter{TeamID: teamID})
		if events == nil {
			events = []ac.AuditEvent{}
		}
		return map[string]any{"events": events}, err
	})
}

func (s accountCenterAdmin) apply(r *http.Request, tx ac.AdminTx, d adminSnapshot, c ac.AdminChange) (any, error) {
	c.Actor = d.actor.Username
	if err := tx.Change(r.Context(), c); err != nil {
		return nil, err
	}
	err := tx.AppendAudit(r.Context(), ac.AuditEvent{
		ActorUsername: c.Actor, SubjectUsername: c.Username, TeamID: c.TeamID, Action: c.Action,
	})
	return map[string]any{"ok": true, "teamId": c.TeamID}, err
}

func (s accountCenterAdmin) createUngroupedAccount(w http.ResponseWriter, r *http.Request) {
	s.withDirectory(w, r, true, func(tx ac.AdminTx, d adminSnapshot) (any, error) {
		if d.actor.Role != ac.RoleFounder {
			return nil, ac.ErrForbidden
		}
		input, err := decodeAccountAdminInput(w, r)
		if err != nil {
			return nil, err
		}
		if r.URL.Path == "/api/admin/accounts" && input.Role == "" {
			input.Role = ac.RoleMember
		}
		if !(ac.Policy{}).Can(d.actor, ac.ActionCreateAccount, ac.PolicyTarget{NewRole: input.Role}) {
			return nil, ac.ErrForbidden
		}
		change, err := newAccountAdminChange(input)
		if err != nil {
			return nil, err
		}
		change.Action = "create_account"
		if _, err := s.apply(r, tx, d, change); err != nil {
			return nil, err
		}
		person := ac.Identity{Username: change.Username, DisplayName: change.DisplayName, Role: change.Role, Active: input.Active == nil || *input.Active}
		payload := d.row(person, ac.Team{})
		return map[string]any{"ok": true, "account": payload, "member": payload}, nil
	})
}

func decodeAccountAdminInput(w http.ResponseWriter, r *http.Request) (accountAdminInput, error) {
	var input accountAdminInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input); err != nil {
		return input, ac.ErrInvalid
	}
	input.Name, input.Username = strings.TrimSpace(input.Name), strings.TrimSpace(input.Username)
	input.DisplayName, input.ManagerUsername = strings.TrimSpace(input.DisplayName), strings.TrimSpace(input.ManagerUsername)
	return input, nil
}

func newAccountAdminChange(input accountAdminInput) (ac.AdminChange, error) {
	if !accountCenterUsername.MatchString(input.Username) || len(input.Password) < 8 || len(input.Password) > 128 ||
		utf8.RuneCountInString(input.DisplayName) > 191 {
		return ac.AdminChange{}, ac.ErrInvalid
	}
	if input.DisplayName == "" {
		input.DisplayName = input.Username
	}
	hash, algorithm, err := auth.HashPassword(input.Password)
	return ac.AdminChange{Username: input.Username, Role: input.Role, DisplayName: input.DisplayName, PasswordHash: hash, PasswordAlgorithm: algorithm, StartDisabled: input.Active != nil && !*input.Active}, err
}

func (s accountCenterAdmin) createTeam(w http.ResponseWriter, r *http.Request) {
	s.withDirectory(w, r, true, func(tx ac.AdminTx, d adminSnapshot) (any, error) {
		if !(ac.Policy{}).Can(d.actor, ac.ActionCreateTeam, ac.PolicyTarget{}) {
			return nil, ac.ErrForbidden
		}
		input, err := decodeAccountAdminInput(w, r)
		if err != nil || input.Name == "" || utf8.RuneCountInString(input.Name) > 60 {
			return nil, ac.ErrInvalid
		}
		var change ac.AdminChange
		if input.Password == "" {
			// Only the founder can assign an existing, ungrouped manager.
			manager := d.person(input.ManagerUsername)
			if d.actor.Role != ac.RoleFounder || manager.Role != ac.RoleManager ||
				!manager.Active || manager.ArchivedAt != nil || d.accountTeam(manager.Username).ID != "" {
				return nil, ac.ErrForbidden
			}
			change.Username = manager.Username
		} else {
			input.Username, input.Role = input.ManagerUsername, ac.RoleManager
			input.Active = nil
			change, err = newAccountAdminChange(input)
			if err != nil {
				return nil, err
			}
		}
		id, err := newSessionToken()
		if err != nil {
			return nil, err
		}
		change.Action, change.TeamID, change.TeamName = "create_team", id, input.Name
		return s.apply(r, tx, d, change)
	})
}

func (s accountCenterAdmin) createAccount(w http.ResponseWriter, r *http.Request) {
	s.withDirectory(w, r, true, func(tx ac.AdminTx, d adminSnapshot) (any, error) {
		team := d.team(r.PathValue("teamId"))
		if team.ID == "" || team.ArchivedAt != nil || !d.manages(team) {
			return nil, ac.ErrForbidden
		}
		input, err := decodeAccountAdminInput(w, r)
		if err != nil {
			return nil, err
		}
		target := ac.PolicyTarget{TeamID: team.ID, InManagedTeam: true, NewRole: input.Role}
		if !(ac.Policy{}).Can(d.actor, ac.ActionCreateAccount, target) {
			return nil, ac.ErrForbidden
		}
		change, err := newAccountAdminChange(input)
		if err != nil {
			return nil, err
		}
		change.Action, change.TeamID = "create_account", team.ID
		return s.apply(r, tx, d, change)
	})
}

func (s accountCenterAdmin) teamChange(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.withDirectory(w, r, true, func(tx ac.AdminTx, d adminSnapshot) (any, error) {
			team := d.team(r.PathValue("teamId"))
			if !slices.Contains(d.teamActions(team), action) {
				return nil, ac.ErrForbidden
			}
			return s.apply(r, tx, d, ac.AdminChange{Action: action, TeamID: team.ID, Username: team.ManagerUsername})
		})
	}
}

func (s accountCenterAdmin) accountChange(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.withDirectory(w, r, action != "purge_preflight", func(tx ac.AdminTx, d adminSnapshot) (any, error) {
			person := d.person(r.PathValue("username"))
			team := d.accountTeam(person.Username)
			if action == "purge" || action == "purge_preflight" {
				target := ac.PolicyTarget{Identity: person, TeamID: team.ID, InManagedTeam: d.manages(team)}
				if !(ac.Policy{}).Can(d.actor, ac.ActionPurgeAccount, target) {
					return nil, ac.ErrForbidden
				}
				checks := ac.PermanentDeletePreflight(person)
				if action == "purge" {
					// Clearing all financial, job, and retention checks is mandatory.
					return nil, ac.ErrConflict
				}
				return checks, nil
			}
			if !slices.Contains(d.accountActions(person, team), action) {
				return nil, ac.ErrForbidden
			}
			return s.apply(r, tx, d, ac.AdminChange{Action: action, Username: person.Username, TeamID: team.ID, Role: strings.TrimPrefix(action, "set_")})
		})
	}
}

func (s accountCenterAdmin) compatibilityStatus(w http.ResponseWriter, r *http.Request) {
	s.withDirectory(w, r, true, func(tx ac.AdminTx, d adminSnapshot) (any, error) {
		person := d.person(r.PathValue("username"))
		team := d.accountTeam(person.Username)
		target := ac.PolicyTarget{Identity: person, TeamID: team.ID, InManagedTeam: d.manages(team)}
		if team.ArchivedAt != nil || person.ArchivedAt != nil || !(ac.Policy{}).Can(d.actor, ac.ActionManageAccount, target) {
			return nil, ac.ErrForbidden
		}
		input, err := decodeAccountAdminInput(w, r)
		if err != nil || input.Active == nil {
			return nil, ac.ErrInvalid
		}
		if person.Active != *input.Active {
			action := "disable"
			if *input.Active {
				action = "enable"
			}
			if _, err := s.apply(r, tx, d, ac.AdminChange{Action: action, Username: person.Username, TeamID: team.ID}); err != nil {
				return nil, err
			}
		}
		person.Active = *input.Active
		row := d.row(person, team)
		return map[string]any{"ok": true, "account": row, "member": row}, nil
	})
}

func (s accountCenterAdmin) retired(w http.ResponseWriter, r *http.Request) {
	s.withDirectory(w, r, false, func(_ ac.AdminTx, d adminSnapshot) (any, error) {
		teamID := r.PathValue("teamId")
		if strings.HasSuffix(r.URL.Path, "/transfer") || strings.HasSuffix(r.URL.Path, "/transfer-preview") {
			if d.actor.Role == ac.RoleManager {
				return nil, ac.ErrForbidden
			}
		}
		if teamID != "" && !d.manages(d.team(teamID)) {
			return nil, ac.ErrForbidden
		}
		if username := r.PathValue("username"); username != "" {
			person := d.person(username)
			team := d.accountTeam(person.Username)
			target := ac.PolicyTarget{Identity: person, TeamID: team.ID, InManagedTeam: d.manages(team)}
			if (teamID != "" && team.ID != teamID) || !(ac.Policy{}).Can(d.actor, ac.ActionManageAccount, target) {
				return nil, ac.ErrForbidden
			}
		}
		return nil, errLegacyGovernanceUnavailable
	})
}
