package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	ac "qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
)

func RegisterMemberTeamReadCompat(root *http.ServeMux, middleware BridgeAuth, _ auth.Store, profiles *MemberProfileStore) {
	admin := accountCenterAdmin{auth: middleware}
	register := func(path string, handler http.HandlerFunc) {
		root.Handle("GET "+path, middleware.AccountCenterMiddleware(handler))
	}
	register("/api/member/team", func(w http.ResponseWriter, r *http.Request) {
		admin.withDirectory(w, r, false, func(_ ac.AdminTx, d adminSnapshot) (any, error) {
			directory := d.directory()
			id := r.URL.Query().Get("teamId")
			members := []map[string]any{}
			var teamView *accountAdminTeam
			for _, team := range directory.Teams {
				if id != "" && team.ID != id {
					continue
				}
				if teamView == nil {
					copy := team
					teamView = &copy
				}
				for _, person := range team.Accounts {
					members = append(members, legacyDirectoryAccount(person, team.ManagerUsername))
				}
			}
			if id != "" && teamView == nil {
				return nil, ac.ErrForbidden
			}
			return map[string]any{"team": teamView, "teams": directory.Teams, "members": members, "teamGovernance": nil}, nil
		})
	})
	register("/api/member/team/meta", func(w http.ResponseWriter, r *http.Request) {
		admin.withSubjectDirectory(w, r, false, func(_ ac.AdminTx, d adminSnapshot) (any, error) {
			team := d.accountTeam(d.actor.Username)
			teams := []map[string]any{}
			for _, row := range d.directory().Teams {
				manager := legacyDirectoryAccount(d.row(d.person(row.ManagerUsername), d.team(row.ID)), "")
				teams = append(teams, map[string]any{"team": row, "manager": manager})
			}
			var teamView, manager any
			if team.ID != "" {
				teamView = legacyTeamRow(team)
				manager = legacyDirectoryAccount(d.row(d.person(team.ManagerUsername), team), "")
			}
			return map[string]any{"team": teamView, "manager": manager, "teams": teams}, nil
		})
	})
	register("/api/member/team/members/{username}/usage", func(w http.ResponseWriter, r *http.Request) {
		admin.withSubjectDirectory(w, r, false, func(_ ac.AdminTx, d adminSnapshot) (any, error) {
			target := d.person(r.PathValue("username"))
			team := d.accountTeam(target.Username)
			readsOtherStaff := !strings.EqualFold(d.actor.Username, target.Username) && d.actor.Role != ac.RoleFounder
			if target.Username == "" || (readsOtherStaff && !d.manages(team)) {
				return nil, ac.ErrForbidden
			}
			if readsOtherStaff {
				for _, membership := range d.Memberships {
					if strings.EqualFold(membership.Username, target.Username) && !d.manages(d.team(membership.TeamID)) {
						// The per-account ledger includes periods from other teams.
						return nil, errLegacyGovernanceUnavailable
					}
				}
			}
			if profiles == nil || profiles.UsageReader == nil {
				return nil, errLegacyGovernanceUnavailable
			}
			usage, err := profiles.usage(r.Context(), target.Username)
			return map[string]any{"day": usage["day"], "month": usage["month"], "recent": usage["recent"], "quota": nil}, err
		})
	})
	register("/api/member/team/audit", func(w http.ResponseWriter, r *http.Request) {
		admin.withDirectory(w, r, false, func(tx ac.AdminTx, d adminSnapshot) (any, error) {
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			if limit <= 0 {
				limit = 100
			}
			limit = min(limit, 500)
			teamID := r.URL.Query().Get("teamId")
			switch {
			case teamID != "" && !d.manages(d.team(teamID)):
				return nil, ac.ErrForbidden
			case teamID != "" || d.actor.Role == ac.RoleFounder:
				events, err := tx.ListAudit(r.Context(), ac.AuditFilter{TeamID: teamID, Limit: limit})
				return auditEntries(events), err
			}
			entries := []ac.AuditEvent{}
			for _, team := range d.Teams {
				if !d.manages(team) {
					continue
				}
				events, err := tx.ListAudit(r.Context(), ac.AuditFilter{TeamID: team.ID, Limit: limit})
				if err != nil {
					return nil, err
				}
				entries = append(entries, events...)
			}
			slices.SortFunc(entries, func(a, b ac.AuditEvent) int { return b.CreatedAt.Compare(a.CreatedAt) })
			return auditEntries(entries[:min(len(entries), limit)]), nil
		})
	})
	register("/api/member/team/governance", admin.retired)
	register("/api/member/team/invites", admin.retired)
}

func auditEntries(events []ac.AuditEvent) map[string]any {
	if events == nil {
		events = []ac.AuditEvent{}
	}
	return map[string]any{"entries": events}
}

func legacyTeamRow(team ac.Team) map[string]any {
	return map[string]any{"id": team.ID, "name": team.Name, "managerUsername": team.ManagerUsername, "archivedAt": team.ArchivedAt}
}

func memberCurrentTeam(ctx context.Context, store ac.Store, username string) (manager, team map[string]any, err error) {
	admin, ok := store.(ac.AdminStore)
	if !ok {
		return nil, nil, nil
	}
	err = admin.WithAdmin(ctx, false, func(tx ac.AdminTx) error {
		directory, err := tx.Directory(ctx)
		if err != nil {
			return err
		}
		d := adminSnapshot{AdminDirectory: directory, actor: ac.Identity{Username: username}}
		current := d.accountTeam(username)
		if current.ID != "" {
			team = legacyTeamRow(current)
			manager = legacyDirectoryAccount(d.row(d.person(current.ManagerUsername), current), "")
		}
		return nil
	})
	return manager, team, err
}
