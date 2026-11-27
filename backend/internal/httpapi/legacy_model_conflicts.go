package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/novelfetchworkshop"
)

type legacyModelConflictStore interface {
	ListLegacyModelConflicts(context.Context, string, string) ([]novelfetchworkshop.LegacyModelConflict, string, error)
	ReviewLegacyModelConflict(context.Context, accountcenter.AuditEvent, string, string) (string, error)
}

func registerLegacyModelConflictRoutes(root *http.ServeMux, auth BridgeAuth, catalog novelfetchworkshop.Store) {
	store, ok := catalog.(legacyModelConflictStore)
	if !ok {
		return
	}
	strict := auth.SessionOnly().AccountCenterMiddleware
	owner := func(w http.ResponseWriter, r *http.Request) (apiConfigAccess, accountcenter.Identity, bool) {
		if bearerToken(r) == "" {
			writeAccountCenterForbidden(w)
			return apiConfigAccess{}, accountcenter.Identity{}, false
		}
		access := apiConfigPolicy(r.Context(), auth.AccountCenterStore)
		if !access.authorize(w, true, false) {
			return apiConfigAccess{}, accountcenter.Identity{}, false
		}
		target := access.identity.Subject
		if requested := strings.TrimSpace(r.URL.Query().Get("owner")); requested != "" && !strings.EqualFold(requested, target.Username) {
			if target.Role != accountcenter.RoleFounder {
				writeAccountCenterForbidden(w)
				return apiConfigAccess{}, accountcenter.Identity{}, false
			}
			candidate, err := auth.AccountCenterStore.ResolveIdentity(r.Context(), requested)
			if err != nil || candidate.Username == "" {
				writeAccountCenterForbidden(w)
				return apiConfigAccess{}, accountcenter.Identity{}, false
			}
			target = candidate
		}
		return access, target, true
	}
	const path = "/api/config/model-conflicts"
	root.Handle("GET "+path, strict(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		access, target, allowed := owner(w, r)
		if !allowed {
			return
		}
		after := r.URL.Query().Get("after")
		if after != "" && !novelfetchworkshop.ValidLegacyModelConflictID(after) {
			writeAPIConfigError(w, accountcenter.ErrInvalid)
			return
		}
		records, cursor, err := store.ListLegacyModelConflicts(r.Context(), target.Username, after)
		if err != nil {
			writeAuthError(w, http.StatusServiceUnavailable, "旧密钥恢复暂不可用")
			return
		}
		items := make([]map[string]any, 0, len(records))
		for _, record := range records {
			source := record.Source
			branch := [...]string{"root", "settings", "appConfig", "settings.appConfig"}
			group := ""
			if source.Container >= 0 && source.Container < len(branch) {
				group = branch[source.Container]
			}
			origin, _, _ := strings.Cut(source.Document, ":")
			kind := source.Kind
			if !modelKinds[kind] {
				kind = "unknown"
			}
			item := map[string]any{"id": record.ID, "state": record.State, "kind": kind,
				"origin": origin, "branch": group, "field": source.Field}
			if !source.Video {
				item["index"] = source.Index
			}
			if access.custom {
				item["modelId"] = source.ModelID
				item["rowId"] = source.RowID
			}
			items = append(items, item)
		}
		writeJSON(w, http.StatusOK, map[string]any{"conflicts": items, "nextCursor": cursor})
	})))
	for _, action := range []string{"reveal", "confirm", "discard"} {
		root.Handle("POST "+path+"/{id}/"+action, strict(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			access, target, allowed := owner(w, r)
			if !allowed {
				return
			}
			id := r.PathValue("id")
			if !novelfetchworkshop.ValidLegacyModelConflictID(id) {
				writeAccountCenterForbidden(w)
				return
			}
			value, err := store.ReviewLegacyModelConflict(r.Context(), accountcenter.AuditEvent{
				ActorUsername: access.identity.Actor.Username, SubjectUsername: target.Username, TeamID: target.TeamID,
			}, id, action)
			switch {
			case errors.Is(err, novelfetchworkshop.ErrNotFound), errors.Is(err, accountcenter.ErrForbidden):
				writeAccountCenterForbidden(w)
			case errors.Is(err, novelfetchworkshop.ErrConfigConflict):
				writeAuthError(w, http.StatusConflict, "旧文件清理仍待确认，当前不能丢弃备份")
			case err != nil:
				writeAuthError(w, http.StatusServiceUnavailable, "旧密钥恢复或审计暂不可用")
			default:
				response := map[string]any{"id": id}
				if action == "reveal" {
					response["value"] = value
				}
				writeJSON(w, http.StatusOK, response)
			}
		})))
	}
}
