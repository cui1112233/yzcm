package httpapi

import (
	"context"
	"database/sql"
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
	return sessionChatPromptWithReferences(req, project, active, targets, message, nil)
}
func sessionChatPromptWithReferences(req screenplay.AssistantSessionRequest, project screenplay.Project, active screenplay.Document, targets sessionEditView, message string, refs []screenplay.AssistantReferenceSnapshot) (screenplay.TextRequest, error) {
	system := prompt.ScreenplayAuthoringAssistant + prompt.ScreenplayAuthoringReadTools
	if req.Mode == "readonly" {
		system += prompt.ScreenplayAssistantSessionReadonly
	} else {
		system += prompt.ScreenplayAuthoringAssetEditTools + prompt.ScreenplayAuthoringShotEditTools
	}
	if len(refs) > 0 {
		system += prompt.ScreenplayAssistantReferences
	}
	payload, err := boundedSessionContext(req, project, active, targets, message, system, refs...)
	if err != nil {
		return screenplay.TextRequest{}, err
	}
	request := screenplay.TextRequest{
		Model: project.ModelID, SystemPrompt: system, UserPrompt: payload, JSONResponse: true, SingleAttempt: true, MaxOutputTokens: 16000, Temperature: 0.4,
	}
	if len(refs) > 0 {
		request.RequestByteLimit = sessionPromptBytes
		if err := checkReferenceRequestBudget(request); err != nil {
			return screenplay.TextRequest{}, err
		}
	}
	return request, nil
}

var errSessionReceiptUnavailable = errors.New("当前模型通道无法关联会话请求与供应商回执，暂不能发送")

// A server adapter must persist the reservation token, stage and provider
// receipt as part of dispatch. A generic TextGenerator cannot opt in.
type sessionReceiptGenerator interface {
	GenerateSessionStage(context.Context, screenplay.AssistantSessionRequest, string, screenplay.TextRequest) (screenplay.TextResponse, error)
}

func runSessionChat(ctx context.Context, repo sessionChatRequests, svc screenplay.AuthoringService, generator screenplay.TextGenerator, owner, projectID, actor, session string, expected int64, message string, selectors ...screenplay.AssistantReferenceSelector) (sessionChatResult, error) {
	return runSessionChatWithAttachmentAccess(ctx, repo, svc, generator, nil, owner, projectID, actor, session, expected, message, selectors...)
}
func runSessionChatWithAttachmentAccess(ctx context.Context, repo sessionChatRequests, svc screenplay.AuthoringService, generator screenplay.TextGenerator, attachment *assistantAttachmentAccess, owner, projectID, actor, session string, expected int64, message string, selectors ...screenplay.AssistantReferenceSelector) (out sessionChatResult, resultErr error) {
	hasAttachments := hasAssistantAttachmentSelectors(selectors)
	if hasAttachments {
		if attachment == nil || attachment.ReadGuard == nil || attachment.CommitPrepare == nil || attachment.VerifyPrepare == nil {
			return out, screenplay.ErrAccessDenied
		}
		if err := checkAssistantAttachmentChatScope(ctx, attachment.Scope, owner, projectID, actor, session); err != nil {
			return out, err
		}
		mysql, ok := repo.(*screenplay.MySQLAuthoringRepository)
		if !ok {
			return out, screenplay.ErrAccessDenied
		}
		channel, ok := generator.(sessionChannelGenerator)
		if !ok || channel.repo != mysql || channel.subject != actor {
			return out, errSessionReceiptUnavailable
		}
		frozen, err := freezeAssistantAttachmentSessionGenerator(channel)
		if err != nil {
			return out, err
		}
		generator = frozen
	}

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
	if !hasAttachments {
		if checker, ok := generator.(interface {
			checkSessionModel(context.Context, string) error
		}); ok {
			if err := checker.checkSessionModel(ctx, project.ModelID); err != nil {
				return out, err
			}
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
	resolveReferences := func(view screenplay.AssistantReferenceView) ([]screenplay.AssistantReferenceSnapshot, error) {
		if hasAttachments {
			return screenplay.ResolveAssistantReferencesScoped(ctx, view, assistantReferenceAttachmentRepository(svc.Store), attachment.Scope, selectors, attachment.ReadGuard)
		}
		return view.Resolve(selectors)
	}
	var refs []screenplay.AssistantReferenceSnapshot
	if len(selectors) > 0 {
		view, e := screenplay.LoadAssistantReferenceView(ctx, svc.Store, svc.Repository, owner, projectID)
		if e != nil {
			return out, e
		}
		refs, e = resolveReferences(view)
		if e != nil {
			return out, e
		}
		// Validate full escaped budget before creating a request reservation.
		st, e := svc.Load(ctx, owner, projectID)
		if e != nil {
			return out, e
		}
		preflight := screenplay.AssistantSessionRequest{Chapters: st.Chapters}
		if hasAttachments {
			mysql := repo.(*screenplay.MySQLAuthoringRepository)
			item, history, e := mysql.GetAssistantSession(ctx, owner, projectID, actor, session)
			if e != nil {
				return out, e
			}
			if !item.Active || item.Revision != expected || item.RequestStatus != "" {
				return out, screenplay.ErrRevisionConflict
			}
			preflight.Mode = item.Mode
			preflight.History = history
		}
		preflightRequest, e := sessionChatPromptWithReferences(preflight, project, active, targets, message, refs)
		if e != nil {
			return out, e
		}
		if hasAttachments {
			if e = checkAssistantAttachmentSessionBudget(ctx, generator.(sessionChannelGenerator), preflightRequest); e != nil {
				return out, e
			}
		}
	}
	// Validate the full history using frozen actual model materials before Begin.
	// The original storage/channel admission gate still runs before any slot.
	if hasAttachments {
		if checker, ok := generator.(interface {
			checkSessionModel(context.Context, string) error
		}); ok {
			if err := checker.checkSessionModel(ctx, project.ModelID); err != nil {
				return out, err
			}
		}
	}
	operationID := ""
	if hasAttachments {
		operationID = newAgentID("reference-operation")
	}
	req, err := repo.BeginAssistantRequest(ctx, owner, projectID, actor, session, expected)
	if err != nil {
		return out, err
	}
	// Malformed or forbidden model output has a known provider result. Close
	// the reservation without saving the rejected reply or any proposed action.
	var providerUncertain bool
	var finishAttempted bool
	var dispatched bool
	defer func() {
		if resultErr == nil || finishAttempted {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if len(selectors) > 0 && !dispatched {
			if mysql, ok := repo.(*screenplay.MySQLAuthoringRepository); ok {
				resultErr = errors.Join(resultErr, mysql.ReleaseAssistantReferencesUnsent(cleanup, req))
				return
			}
		}
		original := &req.History
		if providerUncertain {
			original = nil
		}
		resultErr = errors.Join(resultErr, repo.FinishAssistantRequest(cleanup, req, original))
	}()
	if hasAttachments {
		if e := checkAssistantAttachmentChatScope(ctx, attachment.Scope, req.Owner, req.Project, req.Actor, req.SessionID); e != nil {
			return out, e
		}
	}
	if len(refs) > 0 {
		view, e := screenplay.LoadAssistantReferenceView(ctx, svc.Store, svc.Repository, owner, projectID)
		if e != nil {
			return out, e
		}
		refs, e = resolveReferences(view)
		if e != nil {
			return out, e
		}
		// Chapters used by references and the prompt must describe one version.
		view.Chapters = req.Chapters
		if _, e = resolveReferences(view); e != nil {
			return out, e
		}
	}
	request, err := sessionChatPromptWithReferences(req, project, active, targets, message, refs)
	if err != nil {
		return out, err
	}
	if hasAttachments {
		if e := checkAssistantAttachmentSessionBudget(ctx, generator.(sessionChannelGenerator), request); e != nil {
			return out, e
		}
	}
	dispatched = true
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
		data := map[string]any{"tool": wire.Tool, "tool_result": detail, "question": message}
		if len(refs) > 0 {
			data["references"] = refs
		}
		payload, err := json.Marshal(data)
		if err != nil {
			return out, err
		}
		if len(payload)+len(prompt.ScreenplayAuthoringReviewReply) > sessionPromptBytes {
			return out, screenplay.ErrInvalidArgument
		}
		if err = repo.CheckAssistantRequest(ctx, req); err != nil {
			return out, err
		}
		replyRequest := screenplay.TextRequest{Model: project.ModelID, SystemPrompt: prompt.ScreenplayAuthoringReviewReply, UserPrompt: string(payload), JSONResponse: true, SingleAttempt: true, MaxOutputTokens: 16000, Temperature: 0.4}
		if len(refs) > 0 {
			replyRequest.RequestByteLimit = sessionPromptBytes
			replyRequest.SystemPrompt += prompt.ScreenplayAssistantReferences
			if err := checkReferenceRequestBudget(replyRequest); err != nil {
				return out, err
			}
			if hasAttachments {
				if e := checkAssistantAttachmentSessionBudget(ctx, generator.(sessionChannelGenerator), replyRequest); e != nil {
					return out, e
				}
			}
		}
		reply, err := tracked.GenerateSessionStage(ctx, req, "read_reply", replyRequest)
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
	history.Messages = append(history.Messages, screenplay.AssistantMessage{Role: "user", Content: message, References: refs, ReferenceOperationID: operationID}, screenplay.AssistantMessage{Role: "assistant", Content: wire.Reply, Tools: toolRuns, Suggestions: sanitizeAssistantSuggestions(wire.Suggestions)})
	if question := strings.TrimSpace(wire.Question); question != "" {
		history.Messages = append(history.Messages, screenplay.AssistantMessage{Role: "assistant", Kind: screenplay.AssistantMessageKindQuestion, Content: question})
	}
	if err := checkAuthority(); err != nil {
		return out, err
	}
	if len(refs) > 0 {
		view, e := screenplay.LoadAssistantReferenceView(ctx, svc.Store, svc.Repository, owner, projectID)
		if e != nil {
			return out, e
		}
		if _, e = resolveReferences(view); e != nil {
			return out, e
		}
	}
	finishAttempted = true
	// Only the concrete dedicated server adapter can select the SQL-guarded
	// commit. A generic/fake generator cannot announce this capability.
	if mysql, ok := repo.(*screenplay.MySQLAuthoringRepository); ok {
		channel, ok := generator.(sessionChannelGenerator)
		if !ok || channel.commitGuard == nil {
			return out, screenplay.ErrAccessDenied
		}
		if hasAttachments {
			if err := finishAssistantAttachmentSession(ctx, mysql, channel, *attachment, req, &history, operationID, message, refs); err != nil {
				return out, err
			}
		} else {
			guard := channel.commitGuard
			if len(refs) > 0 {
				guard = func(ctx context.Context, tx *sql.Tx) error {
					if err := channel.commitGuard(ctx, tx); err != nil {
						return err
					}
					return screenplay.CheckAssistantReferencesInTx(ctx, tx, owner, projectID, refs)
				}
			}
			if err := mysql.FinishAssistantRequestGuarded(ctx, req, &history, guard); err != nil {
				return out, err
			}
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

func sessionChatHandler(sessionRepo sessionChatRequests, svc screenplay.AuthoringService, decode func(http.ResponseWriter, *http.Request, any) bool, generatorFor func(*http.Request, string) screenplay.TextGenerator, attachmentAuth ...BridgeAuth) func(http.ResponseWriter, *http.Request, string, string) {
	return func(w http.ResponseWriter, r *http.Request, owner, actor string) {
		var input struct {
			Message    string                                  `json:"message"`
			Expected   int64                                   `json:"expected_revision"`
			References []screenplay.AssistantReferenceSelector `json:"references,omitempty"`
		}
		if !decode(w, r, &input) {
			return
		}
		generator := generatorFor(r, actor)
		var attachment *assistantAttachmentAccess
		var err error
		if hasAssistantAttachmentSelectors(input.References) {
			channel, ok := generator.(sessionChannelGenerator)
			if !ok || channel.channel == nil {
				err = errSessionReceiptUnavailable
			} else if len(attachmentAuth) != 1 {
				err = screenplay.ErrAccessDenied
			} else {
				var access assistantAttachmentAccess
				access, err = assistantAttachmentRequestAccess(attachmentAuth[0], r, svc, owner, actor, r.PathValue("session"), true)
				if err == nil {
					attachment = &access
				}
			}
		}
		var result sessionChatResult
		if err == nil {
			result, err = runSessionChatWithAttachmentAccess(r.Context(), sessionRepo, svc, generator, attachment, owner, r.PathValue("id"), actor, r.PathValue("session"), input.Expected, input.Message, input.References...)
		}
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
