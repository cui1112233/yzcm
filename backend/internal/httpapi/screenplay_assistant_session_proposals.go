package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"qiantie/backend/internal/screenplay"
)

func registerSessionProposals(svc screenplay.AuthoringService, repo *screenplay.MySQLAuthoringRepository, handle func(string, string, func(http.ResponseWriter, *http.Request, string, string)), decode func(http.ResponseWriter, *http.Request, any) bool) {
	const root = "/api/comic-screenplays/projects/{id}/assistant/sessions/{session}"
	handle("POST "+root+"/prepare", "edit", func(w http.ResponseWriter, r *http.Request, owner, actor string) {
		var in struct {
			Expected int64           `json:"expected_revision"`
			Tool     string          `json:"tool"`
			Args     json.RawMessage `json:"tool_args"`
			Action   json.RawMessage `json:"action"`
		}
		if !decode(w, r, &in) {
			return
		}
		ctx, p, id := r.Context(), r.PathValue("id"), r.PathValue("session")
		session, _, err := repo.GetAssistantSession(ctx, owner, p, actor, id)
		if err != nil {
			authoringError(w, err)
			return
		}
		if session.Mode == "readonly" {
			authoringError(w, screenplay.ErrAccessDenied)
			return
		}
		if session.Revision != in.Expected {
			authoringError(w, screenplay.ErrRevisionConflict)
			return
		}
		var a screenplay.AuthoringAction
		var summary string
		switch {
		case in.Tool != "" && len(in.Action) == 0:
			a, summary, err = prepareSessionTool(ctx, svc.Store, owner, p, in.Tool, in.Args)
		case in.Tool == "" && len(in.Args) == 0 && len(in.Action) > 0:
			var st screenplay.AuthoringState
			var project screenplay.Project
			var doc screenplay.Document
			if st, err = svc.Load(ctx, owner, p); err == nil {
				project, err = svc.Store.GetProject(ctx, owner, p)
			}
			if err == nil {
				doc, err = screenplay.ReadAssistantActiveDocument(ctx, svc.Store, owner, p)
			}
			if err == nil {
				a, summary, err = screenplay.PrepareSessionContent(project, st, doc, in.Action)
			}
		default:
			err = screenplay.ErrInvalidArgument
		}
		if err != nil {
			authoringError(w, err)
			return
		}
		h, err := repo.SaveSessionProposal(ctx, owner, p, actor, id, in.Expected, screenplay.AssistantProposal{ID: newAgentID("proposal"), Status: "pending", Summary: summary, Actions: []screenplay.AuthoringAction{a}})
		if err != nil {
			authoringError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, sessionChatResult{in.Expected + 1, h})
	})
	handle("POST "+root+"/apply", "edit", func(w http.ResponseWriter, r *http.Request, owner, actor string) {
		var in struct {
			Expected   int64  `json:"expected_revision"`
			ProposalID string `json:"proposal_id"`
			Confirmed  bool   `json:"confirmed"`
		}
		if !decode(w, r, &in) {
			return
		}
		if !in.Confirmed {
			authoringError(w, screenplay.ErrInvalidArgument)
			return
		}
		result, err := applySessionAndInvalidate(r.Context(), repo.ApplySessionProposal, func(ctx context.Context, owner, project, kind string) error {
			return screenplay.InvalidateAuthoringContent(ctx, svc.Store, owner, project, kind)
		}, owner, r.PathValue("id"), actor, r.PathValue("session"), in.ProposalID, in.Expected)
		if err != nil {
			authoringError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
}

type sessionApplyResult struct {
	sessionChatResult
	Warning string `json:"warning,omitempty"`
}

func applySessionAndInvalidate(ctx context.Context, commit func(context.Context, string, string, string, string, string, int64) (screenplay.AssistantSessionHistory, error), invalidate func(context.Context, string, string, string) error, owner, project, actor, id, proposalID string, expected int64) (sessionApplyResult, error) {
	history, err := commit(ctx, owner, project, actor, id, proposalID, expected)
	if err != nil {
		return sessionApplyResult{}, err
	}
	out := sessionApplyResult{sessionChatResult: sessionChatResult{expected + 1, history}}
	for _, proposal := range history.Proposals {
		if proposal.ID != proposalID || proposal.Status != "applied" || len(proposal.Actions) != 1 {
			continue
		}
		kind := proposal.Actions[0].Kind
		switch kind {
		case "asset", "document", "chapter":
		case "creative_shot_structure":
			kind = "storyboard"
		default:
			return out, nil
		}
		follow, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if err = invalidate(follow, owner, project, kind); err != nil {
			out.Warning = "修改已应用，但下游任务状态更新失败。请核对制作任务。"
		}
		return out, nil
	}
	return out, nil
}
