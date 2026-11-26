package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	ac "qiantie/backend/internal/accountcenter"
)

var publicInviteUsernamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{3,32}$`)

type legacyTeamInvite struct {
	ID              string     `json:"id"`
	Digest          string     `json:"tokenDigest"`
	TeamID          string     `json:"teamId"`
	ManagerUsername string     `json:"managerUsername"`
	CreatedBy       string     `json:"createdBy"`
	CreatedAt       time.Time  `json:"createdAt"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	UsedAt          *time.Time `json:"usedAt"`
	UsedBy          string     `json:"usedBy"`
}

func RegisterPublicTeamInviteCompat(root *http.ServeMux, systemDir string, store ac.Store) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if !allowImpersonationCredentialWrite(w, r) {
			return
		}
		admin, ok := store.(ac.AdminStore)
		if !ok {
			managedAccountError(w, http.StatusServiceUnavailable, "团队邀请暂不可用")
			return
		}
		raw, err := os.ReadFile(adminSystemFile(systemDir, "team-collaboration.json"))
		if errors.Is(err, os.ErrNotExist) {
			managedAccountError(w, http.StatusNotFound, "邀请不存在")
			return
		}
		var source struct {
			Invites []legacyTeamInvite `json:"invites"`
		}
		if err != nil || json.Unmarshal(raw, &source) != nil {
			managedAccountError(w, http.StatusServiceUnavailable, "团队邀请暂不可用")
			return
		}
		digest := sha256.Sum256([]byte(strings.TrimSpace(r.PathValue("token"))))
		key := hex.EncodeToString(digest[:])
		for _, invite := range source.Invites {
			if invite.Digest == key {
				servePublicTeamInvite(w, r, admin, invite)
				return
			}
		}
		managedAccountError(w, http.StatusNotFound, "邀请不存在")
	}
	root.HandleFunc("GET /api/login/invite/{token}", handler)
	root.HandleFunc("POST /api/login/invite/{token}", handler)
}

func servePublicTeamInvite(w http.ResponseWriter, r *http.Request, store ac.AdminStore, invite legacyTeamInvite) {
	write := r.Method == http.MethodPost
	var change ac.AdminChange
	if write {
		input, err := decodeAccountAdminInput(w, r)
		if err != nil || !publicInviteUsernamePattern.MatchString(input.Username) {
			managedAccountError(w, http.StatusBadRequest, "登录账号需为 3-32 位字母、数字、下划线或短横线")
			return
		}
		input.Role = ac.RoleMember
		input.Active = nil
		change, err = newAccountAdminChange(input)
		if err != nil {
			managedAccountError(w, http.StatusBadRequest, "账号资料或密码无效")
			return
		}
	}
	ctx := r.Context()
	var response any
	err := store.WithAdmin(ctx, write, func(tx ac.AdminTx) error {
		directory, err := tx.Directory(ctx)
		if err != nil {
			return err
		}
		d := adminSnapshot{AdminDirectory: directory}
		team := d.team(invite.TeamID)
		manager := d.person(team.ManagerUsername)
		d.actor = d.person(invite.CreatedBy)
		if d.actor.Username == "" && invite.CreatedBy == "" {
			d.actor = manager
		}
		target := ac.PolicyTarget{TeamID: team.ID, InManagedTeam: d.manages(team), NewRole: ac.RoleMember}
		if team.ID == "" || team.ArchivedAt != nil || !strings.EqualFold(team.ManagerUsername, invite.ManagerUsername) ||
			!manager.Active || manager.ArchivedAt != nil || !(ac.Policy{}).Can(d.actor, ac.ActionCreateAccount, target) {
			return ac.ErrForbidden
		}
		usedBy, usedAt, err := tx.InviteClaim(ctx, invite.Digest)
		if err != nil {
			return err
		}
		if usedAt == nil {
			usedBy, usedAt = invite.UsedBy, invite.UsedAt
		}
		expired := !time.Now().Before(invite.ExpiresAt)
		teamView := map[string]string{"id": team.ID, "name": team.Name, "managerUsername": manager.Username}
		if !write {
			response = map[string]any{
				"invite": map[string]any{
					"id": invite.ID, "teamId": team.ID, "teamName": team.Name, "managerUsername": manager.Username,
					"createdAt": invite.CreatedAt, "expiresAt": invite.ExpiresAt, "usedAt": usedAt, "usedBy": usedBy,
					"available": !expired && usedAt == nil && usedBy == "", "expired": expired,
				},
				"manager": map[string]string{"username": manager.Username, "displayName": manager.DisplayName},
			}
			return nil
		}
		if expired || usedAt != nil || usedBy != "" {
			return ac.ErrConflict
		}
		change.Action, change.TeamID, change.InviteDigest = "accept_invite", team.ID, invite.Digest
		if err := tx.Change(ctx, change); err != nil {
			return err
		}
		if err := tx.AppendAudit(ctx, ac.AuditEvent{ActorUsername: d.actor.Username, SubjectUsername: change.Username, TeamID: team.ID, Action: change.Action}); err != nil {
			return err
		}
		response = map[string]any{"member": map[string]any{
			"username": change.Username, "displayName": change.DisplayName, "role": ac.RoleMember, "active": true,
		}, "team": teamView}
		return nil
	})
	switch {
	case errors.Is(err, ac.ErrForbidden):
		managedAccountError(w, http.StatusForbidden, "邀请所属团队当前不可用")
	case errors.Is(err, ac.ErrConflict):
		managedAccountError(w, http.StatusConflict, "邀请已过期、已使用或账号已存在")
	case err != nil:
		managedAccountError(w, http.StatusServiceUnavailable, "团队邀请暂不可用")
	case write:
		writeJSON(w, http.StatusCreated, response)
	default:
		writeJSON(w, http.StatusOK, response)
	}
}
