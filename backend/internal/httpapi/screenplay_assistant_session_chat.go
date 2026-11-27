package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"qiantie/backend/internal/prompt"
	"qiantie/backend/internal/screenplay"
	"qiantie/backend/internal/yzcmusage"
)

type sessionChatRequests interface {
	CheckAssistantRequest(context.Context, screenplay.AssistantSessionRequest) error
	BeginAssistantRequest(context.Context, string, string, string, string, int64) (screenplay.AssistantSessionRequest, error)
	FinishAssistantRequest(context.Context, screenplay.AssistantSessionRequest, *screenplay.AssistantSessionHistory) error
}

type sessionChatResult struct {
	Revision int64 `json:"revision"`
	screenplay.AssistantSessionHistory
}

// Only these fields enter the model request. The full chapter and document
// snapshots are retained locally for validating a proposed edit.
func sessionChatPrompt(req screenplay.AssistantSessionRequest, project screenplay.Project, active screenplay.Document, targets sessionEditView, message string) (screenplay.TextRequest, error) {
	system := prompt.ScreenplayAuthoringAssistant + prompt.ScreenplayAuthoringReadTools
	if req.Mode == "readonly" {
		system += prompt.ScreenplayAssistantSessionReadonly
	} else {
		system += prompt.ScreenplayAuthoringAssetEditTools + prompt.ScreenplayAuthoringShotEditTools
	}
	payload, err := boundedSessionContext(req, project, active, targets, message, system)
	if err != nil {
		return screenplay.TextRequest{}, err
	}
	return screenplay.TextRequest{
		Model: project.ModelID, SystemPrompt: system, UserPrompt: payload, JSONResponse: true, SingleAttempt: true, MaxOutputTokens: 16000, Temperature: 0.4,
	}, nil
}

var errSessionReceiptUnavailable = errors.New("当前模型通道无法关联会话请求与供应商回执，暂不能发送")

// A server adapter must persist the reservation token, stage and provider
// receipt as part of dispatch. A generic TextGenerator cannot opt in.
type sessionReceiptGenerator interface {
	GenerateSessionStage(context.Context, screenplay.AssistantSessionRequest, string, screenplay.TextRequest) (screenplay.TextResponse, error)
}

func runSessionChat(ctx context.Context, repo sessionChatRequests, svc screenplay.AuthoringService, generator screenplay.TextGenerator, owner, projectID, actor, session string, expected int64, message string) (out sessionChatResult, resultErr error) {
	tracked, ok := generator.(sessionReceiptGenerator)
	if !ok {
		return out, errSessionReceiptUnavailable
	}
	checkAuthority := func() error {
		if checker, ok := generator.(interface{ checkSessionAuthority(context.Context) error }); ok {
			return checker.checkSessionAuthority(ctx)
		}
		return nil
	}
	if err := checkAuthority(); err != nil {
		return out, err
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return out, screenplay.ErrEmptyContent
	}
	project, err := svc.Store.GetProject(ctx, owner, projectID)
	if err != nil {
		return out, err
	}
	if checker, ok := generator.(interface {
		checkSessionModel(context.Context, string) error
	}); ok {
		if err := checker.checkSessionModel(ctx, project.ModelID); err != nil {
			return out, err
		}
	}
	active, err := screenplay.ReadAssistantActiveDocument(ctx, svc.Store, owner, projectID)
	if err != nil {
		return out, err
	}
	targets, err := captureSessionEditView(ctx, svc.Store, owner, projectID)
	if err != nil {
		return out, err
	}
	req, err := repo.BeginAssistantRequest(ctx, owner, projectID, actor, session, expected)
	if err != nil {
		return out, err
	}
	// Malformed or forbidden model output has a known provider result. Close
	// the reservation without saving the rejected reply or any proposed action.
	var providerUncertain bool
	var finishAttempted bool
	defer func() {
		if resultErr == nil || finishAttempted {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		original := &req.History
		if providerUncertain {
			original = nil
		}
		resultErr = errors.Join(resultErr, repo.FinishAssistantRequest(cleanup, req, original))
	}()
	request, err := sessionChatPrompt(req, project, active, targets, message)
	if err != nil {
		return out, err
	}
	response, err := tracked.GenerateSessionStage(ctx, req, "initial", request)
	if err != nil {
		providerUncertain = !errors.Is(err, yzcmusage.ErrSessionNotSent) && !errors.Is(err, screenplay.ErrModelRequestRejected) && !errors.Is(err, screenplay.ErrAPIKeyMissing)
		return out, err
	}
	if err := checkAuthority(); err != nil {
		return out, err
	}
	wire, err := parseSessionReply(response.Content)
	if err != nil {
		return out, err
	}
	var toolRuns []screenplay.AssistantToolRun
	if screenplay.IsAssistantReadTool(wire.Tool) || wire.Tool == "storyboard_failures" {
		if len(wire.ActionInput) > 0 {
			return out, screenplay.ErrInvalidArgument
		}
		if err = repo.CheckAssistantRequest(ctx, req); err != nil {
			return out, err
		}
		run, detail, readErr := assistantReadScopedTool(ctx, svc, owner, projectID, wire.Tool, wire.ToolArgs)
		if readErr != nil {
			return out, readErr
		}
		toolRuns = append(toolRuns, run)
		payload, err := json.Marshal(map[string]any{"tool": wire.Tool, "tool_result": detail, "question": message})
		if err != nil {
			return out, err
		}
		if len(payload)+len(prompt.ScreenplayAuthoringReviewReply) > sessionPromptBytes {
			return out, screenplay.ErrInvalidArgument
		}
		if err = repo.CheckAssistantRequest(ctx, req); err != nil {
			return out, err
		}
		reply, err := tracked.GenerateSessionStage(ctx, req, "read_reply", screenplay.TextRequest{Model: project.ModelID, SystemPrompt: prompt.ScreenplayAuthoringReviewReply, UserPrompt: string(payload), JSONResponse: true, SingleAttempt: true, MaxOutputTokens: 16000, Temperature: 0.4})
		if err != nil {
			providerUncertain = !errors.Is(err, yzcmusage.ErrSessionNotSent) && !errors.Is(err, screenplay.ErrModelRequestRejected) && !errors.Is(err, screenplay.ErrAPIKeyMissing)
			return out, err
		}
		if err := checkAuthority(); err != nil {
			return out, err
		}
		wire, err = parseSessionReply(reply.Content)
		if err != nil {
			return out, err
		}
		if wire.Tool != "" || len(wire.ActionInput) > 0 {
			return out, screenplay.ErrInvalidArgument
		}
	}
	if wire.Tool != "" && len(wire.ActionInput) > 0 {
		return out, screenplay.ErrInvalidArgument
	}
	history := req.History
	if len(wire.ActionInput) > 0 || wire.Tool != "" {
		if req.Mode == "readonly" {
			return out, screenplay.ErrAccessDenied
		}
		if len(wire.ActionInput) > 1 {
			return out, screenplay.ErrInvalidArgument
		}
		resolved, err := svc.Resolve(ctx, actor, projectID, "edit")
		if err != nil {
			return out, err
		}
		if resolved != owner {
			return out, screenplay.ErrAccessDenied
		}
		st := screenplay.AuthoringState{Owner: owner, ProjectID: projectID, Revision: req.AuthorRevision, Chapters: req.Chapters}
		var action screenplay.AuthoringAction
		var summary string
		if wire.Tool != "" {
			action, summary, err = targets.prepare(ctx, owner, projectID, wire.Tool, wire.ToolArgs)
		} else {
			action, summary, err = screenplay.PrepareSessionContent(project, st, active, wire.ActionInput[0])
			if err == nil {
				err = screenplay.CheckAssistantContentContext(ctx, svc.Store, project, active)
			}
		}
		if err != nil {
			return out, err
		}
		wire.Reply = "修改方案已准备，请核对后确认应用。当前作品尚未修改。"
		wire.Question = ""
		wire.Suggestions = nil
		history.Proposals = append(history.Proposals, screenplay.AssistantProposal{ID: newAgentID("proposal"), Summary: summary, Actions: []screenplay.AuthoringAction{action}, Status: "pending", Revision: req.Revision + 1})
	}
	history.Messages = append(history.Messages, screenplay.AssistantMessage{Role: "user", Content: message}, screenplay.AssistantMessage{Role: "assistant", Content: wire.Reply, Tools: toolRuns, Suggestions: sanitizeAssistantSuggestions(wire.Suggestions)})
	if question := strings.TrimSpace(wire.Question); question != "" {
		history.Messages = append(history.Messages, screenplay.AssistantMessage{Role: "assistant", Kind: screenplay.AssistantMessageKindQuestion, Content: question})
	}
	if err := checkAuthority(); err != nil {
		return out, err
	}
	finishAttempted = true
	// Only the concrete dedicated server adapter can select the SQL-guarded
	// commit. A generic/fake generator cannot announce this capability.
	if mysql, ok := repo.(*screenplay.MySQLAuthoringRepository); ok {
		channel, ok := generator.(sessionChannelGenerator)
		if !ok || channel.commitGuard == nil {
			return out, screenplay.ErrAccessDenied
		}
		if err := mysql.FinishAssistantRequestGuarded(ctx, req, &history, channel.commitGuard); err != nil {
			return out, err
		}
	} else if err := repo.FinishAssistantRequest(ctx, req, &history); err != nil {
		return out, err
	}
	return sessionChatResult{req.Revision + 1, history}, nil
}

func parseSessionReply(content string) (assistantWire, error) {
	raw := strings.TrimSpace(content)
	raw = strings.TrimSuffix(strings.TrimPrefix(raw, "```json"), "```")
	var wire assistantWire
	if json.Unmarshal([]byte(raw), &wire) != nil || strings.TrimSpace(wire.Reply) == "" {
		return wire, screenplay.ErrAIInvalidJSON
	}
	return wire, nil
}

func sessionChatHandler(sessionRepo sessionChatRequests, svc screenplay.AuthoringService, decode func(http.ResponseWriter, *http.Request, any) bool, generatorFor func(*http.Request, string) screenplay.TextGenerator) func(http.ResponseWriter, *http.Request, string, string) {
	return func(w http.ResponseWriter, r *http.Request, owner, actor string) {
		var input struct {
			Message  string `json:"message"`
			Expected int64  `json:"expected_revision"`
		}
		if !decode(w, r, &input) {
			return
		}
		generator := generatorFor(r, actor)
		result, err := runSessionChat(r.Context(), sessionRepo, svc, generator, owner, r.PathValue("id"), actor, r.PathValue("session"), input.Expected, input.Message)
		if errors.Is(err, errSessionReceiptUnavailable) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}

		if errors.Is(err, yzcmusage.ErrSessionNotSent) {
			for _, access := range []error{screenplay.ErrAccessDenied, screenplay.ErrRevisionConflict} {
				if errors.Is(err, access) {
					authoringError(w, access)
					return
				}
			}
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": yzcmusage.ErrSessionNotSent.Error()})
			return
		}
		for _, billing := range []error{yzcmusage.ErrSessionReceiptOnDisk, yzcmusage.ErrSessionReceiptUnknown} {
			if errors.Is(err, billing) {
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": billing.Error()})
				return
			}
		}
		if err != nil {
			authoringError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
