package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"qiantie/backend/internal/batchfactoryv11"
	"strconv"

	"qiantie/backend/internal/screenplay"
)

func checkReferenceRequestBudget(request screenplay.TextRequest) error {
	messages := request.Messages
	if len(messages) == 0 {
		messages = []screenplay.TextMessage{{Role: "system", Content: request.SystemPrompt}, {Role: "user", Content: request.UserPrompt}}
	}
	payload := map[string]any{"model": request.Model, "messages": messages, "temperature": request.Temperature, "max_tokens": request.MaxOutputTokens}
	if request.JSONResponse {
		payload["response_format"] = map[string]string{"type": "json_object"}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(raw) > sessionPromptBytes {
		return screenplay.ErrInvalidArgument
	}
	return nil
}

// Resolve exactly once. The same provider instance whose actual model was
// budgeted is captured by the generator used for the subsequent send.
func generateReferenceText(ctx context.Context, owner string, resolver func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error), request screenplay.TextRequest) (screenplay.TextResponse, error) {
	if resolver == nil {
		return screenplay.TextResponse{}, screenplay.ErrAPIKeyMissing
	}
	provider, err := resolver(ctx, owner, request.Model)
	if err != nil {
		return screenplay.TextResponse{}, err
	}
	if provider == nil {
		return screenplay.TextResponse{}, screenplay.ErrAPIKeyMissing
	}
	p, ok := provider.(*batchfactoryv11.OpenAICompatibleProvider)
	if !ok {
		return screenplay.TextResponse{}, screenplay.ErrInvalidState
	}
	checked := request
	checked.Model = p.Model
	if err = checkReferenceRequestBudget(checked); err != nil {
		return screenplay.TextResponse{}, err
	}
	return (screenplayTextGenerator{owner: owner, resolver: func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) { return provider, nil }}).Generate(ctx, request)
}
func checkReferenceSession(ctx context.Context, svc screenplay.AuthoringService, owner, project, actor, session string) error {
	if session == "" {
		return screenplay.ErrInvalidArgument
	}
	st, err := svc.Load(ctx, owner, project)
	if err != nil {
		return err
	}
	if st.Archived {
		return screenplay.ErrInvalidState
	}
	if session == "default" {
		return nil
	}
	repo, ok := svc.Repository.(*screenplay.MySQLAuthoringRepository)
	if !ok {
		return screenplay.ErrNotFound
	}
	item, _, err := repo.GetAssistantSession(ctx, owner, project, actor, session)
	if err != nil {
		return err
	}
	if !item.Active || item.Archived {
		return screenplay.ErrInvalidState
	}
	return nil
}
func registerAssistantReferenceRoutes(svc screenplay.AuthoringService, handle func(string, string, func(http.ResponseWriter, *http.Request, string, string)), decode func(http.ResponseWriter, *http.Request, any) bool) {
	registerAssistantReferenceShotRoutes(svc, handle, decode)
	base := "/api/comic-screenplays/projects/{id}/assistant/references"
	handle("GET "+base, "read", func(w http.ResponseWriter, r *http.Request, owner, actor string) {
		if err := checkReferenceSession(r.Context(), svc, owner, r.PathValue("id"), actor, r.URL.Query().Get("session_id")); err != nil {
			authoringError(w, err)
			return
		}
		view, err := screenplay.LoadAssistantReferenceView(r.Context(), svc.Store, svc.Repository, owner, r.PathValue("id"))
		if err != nil {
			authoringError(w, err)
			return
		}
		rows := view.Directory()
		filtered := rows[:0]
		kind := r.URL.Query().Get("kind")
		for _, row := range rows {
			if kind == "" || kind == row.Kind {
				filtered = append(filtered, row)
			}
		}
		offset := 0
		if value := r.URL.Query().Get("offset"); value != "" {
			offset, err = strconv.Atoi(value)
			if err != nil || offset < 0 || offset > len(filtered) {
				authoringError(w, screenplay.ErrInvalidArgument)
				return
			}
		}
		end := offset + 100
		if end > len(filtered) {
			end = len(filtered)
		}
		writeJSON(w, 200, map[string]any{"references": filtered[offset:end], "total": len(filtered), "next_offset": end})
	})
	handle("POST "+base+"/resolve", "read", func(w http.ResponseWriter, r *http.Request, owner, actor string) {
		var in struct {
			Session    string                                  `json:"session_id"`
			References []screenplay.AssistantReferenceSelector `json:"references"`
		}
		if !decode(w, r, &in) {
			return
		}
		if err := checkReferenceSession(r.Context(), svc, owner, r.PathValue("id"), actor, in.Session); err != nil {
			authoringError(w, err)
			return
		}
		view, err := screenplay.LoadAssistantReferenceView(r.Context(), svc.Store, svc.Repository, owner, r.PathValue("id"))
		if err != nil {
			authoringError(w, err)
			return
		}
		refs, err := view.Resolve(in.References)
		if err != nil {
			authoringError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"references": refs})
	})
}
