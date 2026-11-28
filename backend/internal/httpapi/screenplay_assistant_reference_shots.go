package httpapi

import (
	"net/http"
	"qiantie/backend/internal/screenplay"
)

func registerAssistantReferenceShotRoutes(svc screenplay.AuthoringService, handle func(string, string, func(http.ResponseWriter, *http.Request, string, string)), decode func(http.ResponseWriter, *http.Request, any) bool) {
	handle("POST /api/comic-screenplays/projects/{id}/assistant/references/storyboard-shots", "read", func(w http.ResponseWriter, r *http.Request, owner, actor string) {
		var input struct {
			SessionID string `json:"session_id"`
			screenplay.AssistantReferenceShotQuery
		}
		if !decode(w, r, &input) {
			return
		}
		if err := checkReferenceSession(r.Context(), svc, owner, r.PathValue("id"), actor, input.SessionID); err != nil {
			authoringError(w, err)
			return
		}
		boards, err := svc.Store.ListStoryboards(r.Context(), owner, r.PathValue("id"), "")
		if err != nil {
			authoringError(w, err)
			return
		}
		page, err := (screenplay.AssistantReferenceView{Boards: boards}).ReferenceShotPage(input.AssistantReferenceShotQuery)
		if err != nil {
			authoringError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, page)
	})
}
