package httpapi

import (
	"net/http"
	"qiantie/backend/internal/screenplay"
)

func registerAssistantSessionManagement(
	repo *screenplay.MySQLAuthoringRepository,
	handle func(string, string, func(http.ResponseWriter, *http.Request, string, string)),
	decode func(http.ResponseWriter, *http.Request, any) bool,
) {
	base := "/api/comic-screenplays/projects/{id}/assistant/sessions"
	handle("GET "+base, "read", func(w http.ResponseWriter, r *http.Request, owner, actor string) {
		sessions, err := repo.ListAssistantSessions(r.Context(), owner, r.PathValue("id"), actor)
		if err != nil {
			authoringError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"sessions": sessions})
	})
	handle("POST "+base, "edit", func(w http.ResponseWriter, r *http.Request, owner, actor string) {
		var input screenplay.SessionCreate
		if !decode(w, r, &input) {
			return
		}
		session, err := repo.CreateAssistantSession(r.Context(), owner, r.PathValue("id"), actor, input)
		if err != nil {
			authoringError(w, err)
			return
		}
		writeJSON(w, 201, map[string]any{"session": session})
	})
	handle("GET "+base+"/{session}", "read", func(w http.ResponseWriter, r *http.Request, owner, actor string) {
		session, history, err := repo.GetAssistantSession(r.Context(), owner, r.PathValue("id"), actor, r.PathValue("session"))
		if err != nil {
			authoringError(w, err)
			return
		}
		writeJSON(w, 200, map[string]any{"session": session, "messages": history.Messages, "proposals": history.Proposals})
	})
	for _, operation := range []string{"rename", "archive", "restore", "stop"} {
		action := operation
		right := "edit"
		if action == "stop" {
			right = "read"
		}
		handle("POST "+base+"/{session}/"+action, right, func(w http.ResponseWriter, r *http.Request, owner, actor string) {
			var input struct {
				Expected int64  `json:"expected_revision"`
				Title    string `json:"title"`
			}
			if !decode(w, r, &input) {
				return
			}
			err := repo.ChangeAssistantSession(r.Context(), owner, r.PathValue("id"), actor, r.PathValue("session"), action, input.Title, input.Expected)
			if err != nil {
				authoringError(w, err)
				return
			}
			writeJSON(w, 200, map[string]any{"revision": input.Expected + 1})
		})
	}
}
