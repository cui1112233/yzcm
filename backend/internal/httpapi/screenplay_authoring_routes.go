package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/prompt"
	"qiantie/backend/internal/screenplay"
	"sort"
	"strconv"
	"strings"
)

type assistantWire struct {
	Reply       string                       `json:"reply"`
	Summary     string                       `json:"summary"`
	ActionInput []json.RawMessage            `json:"actions"`
	Actions     []screenplay.AuthoringAction `json:"-"`
	Question    string                       `json:"question"`
	Suggestions []assistantSuggestion        `json:"suggestions"`
	Tool        string                       `json:"tool"`
	ToolArgs    json.RawMessage              `json:"tool_args"`
}

// assistantReadStoryboardFailures 读取最近一次
// 分镜生成尝试的失败场次，不触发新请求、不计费、不改作品。
func assistantReadStoryboardFailures(ctx context.Context, store screenplay.Store, owner, projectID string) (screenplay.AssistantToolRun, map[string]any) {
	run := screenplay.AssistantToolRun{Name: "storyboard_failures", OK: true}
	generations, ok := store.(screenplay.StoryboardGenerationStore)
	if !ok {
		run.OK, run.Summary = false, "当前存储不支持读取分镜尝试"
		return run, map[string]any{"error": run.Summary}
	}
	state, err := generations.LoadStoryboardGenerations(ctx, owner, projectID)
	if err != nil {
		run.OK, run.Summary = false, "分镜尝试读取失败"
		return run, map[string]any{"error": err.Error()}
	}
	var latest *screenplay.StoryboardGenerationAttempt
	for _, attempt := range state.Attempts {
		if attempt != nil && (latest == nil || attempt.CreatedAt.After(latest.CreatedAt)) {
			latest = attempt
		}
	}
	if latest == nil {
		run.Summary = "还没有分镜生成记录"
		return run, map[string]any{"attempts": 0, "failed_scenes": []any{}}
	}
	failures := []map[string]any{}
	for episodeNo, episode := range latest.Episodes {
		if episode == nil {
			continue
		}
		for phase, call := range episode.Calls {
			if call.Status != "invalid" && call.Status != "failed" {
				continue
			}
			if !strings.HasPrefix(phase, "storyboard_scene_") {
				continue
			}
			scene, _ := strconv.Atoi(strings.TrimPrefix(phase, "storyboard_scene_"))
			failures = append(failures, map[string]any{"episode": episodeNo, "scene": scene, "error": call.Error})
		}
	}
	sort.Slice(failures, func(i, j int) bool {
		left, right := failures[i], failures[j]
		if left["episode"] != right["episode"] {
			return left["episode"].(int) < right["episode"].(int)
		}
		return left["scene"].(int) < right["scene"].(int)
	})
	if len(failures) == 0 {
		run.Summary = "最近一次分镜没有失败场景"
	} else {
		parts := make([]string, 0, len(failures))
		for _, failure := range failures {
			parts = append(parts, fmt.Sprintf("第%d集第%d场", failure["episode"], failure["scene"]))
		}
		run.Summary = "最近一次分镜失败场景：" + strings.Join(parts, "、")
	}
	return run, map[string]any{"request_key": latest.RequestKey, "status": latest.Status, "failed_scenes": failures}
}

// ResolveScreenplayAccess never returns an owner's identity before checking a specific right.
func ResolveScreenplayAccess(ctx context.Context, store screenplay.Store, repo screenplay.AuthoringRepository, actor, id, right string) (string, error) {
	return (screenplay.AuthoringService{Store: store, Repository: repo}).Resolve(ctx, actor, id, right)
}
func ScreenplayProjectArchived(ctx context.Context, repo screenplay.AuthoringRepository, o, id string) bool {
	st, e := repo.Load(ctx, o, id)
	return e == nil && st.Archived
}

// mediaStore 为可选：作品助手用它生成链路能力清单（只读媒体状态）。
// 用可变参数是为了兼容既有调用点（测试与 workflow 注册），未提供时媒体环节按未就绪展示。
func RegisterScreenplayAuthoringRoutes(mux *http.ServeMux, auth BridgeAuth, store screenplay.Store, repo screenplay.AuthoringRepository, resolver func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error), mediaStore ...screenplay.MediaStore) {
	registerScreenplayAuthoringRoutesWithReceipts(mux, auth, store, repo, resolver, screenplay.NewMemoryScriptBatchReceipts(), mediaStore...)
}

func registerScreenplayAuthoringRoutesWithReceipts(mux *http.ServeMux, auth BridgeAuth, store screenplay.Store, repo screenplay.AuthoringRepository, resolver func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error), receipts screenplay.ScriptBatchReceipts, mediaStore ...screenplay.MediaStore) {
	registerScreenplayAuthoringSessionRoutes(mux, auth, store, repo, resolver, receipts, nil, mediaStore...)
}

func registerScreenplayAuthoringSessionRoutes(mux *http.ServeMux, auth BridgeAuth, store screenplay.Store, repo screenplay.AuthoringRepository, resolver func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error), receipts screenplay.ScriptBatchReceipts, channel *ScreenplaySessionChannel, mediaStore ...screenplay.MediaStore) {
	var media screenplay.MediaStore
	if len(mediaStore) > 0 {
		media = mediaStore[0]
	}
	if mux == nil || store == nil || repo == nil {
		return
	}
	svc := screenplay.AuthoringService{Store: store, Repository: repo, Receipts: receipts}
	handle := func(pattern, right string, fn func(http.ResponseWriter, *http.Request, string, string)) {
		mux.Handle(pattern, auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ident, ok := BridgeIdentityFromContext(r.Context())
			if !ok {
				writeJSON(w, 401, map[string]string{"error": "unauthorized"})
				return
			}
			owner := ident.Username
			if r.PathValue("id") != "" {
				var e error
				owner, e = svc.Resolve(r.Context(), ident.Username, r.PathValue("id"), right)
				if e != nil {
					authoringError(w, e)
					return
				}
			}
			if r.Method != http.MethodGet && right == "edit" && !strings.HasSuffix(r.URL.Path, "/assistant/apply") {
				err := svc.WithLock(r.Context(), owner, r.PathValue("id"), func() error { fn(w, r, owner, ident.Username); return nil })
				if err != nil {
					authoringError(w, err)
				}
			} else {
				fn(w, r, owner, ident.Username)
			}
		})))
	}
	decode := func(w http.ResponseWriter, r *http.Request, v any) bool {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, screenplayJSONLimit))
		d.DisallowUnknownFields()
		if e := d.Decode(v); e != nil {
			authoringError(w, screenplay.ErrInvalidArgument)
			return false
		}
		return true
	}
	registerAssistantReferenceRoutes(svc, handle, decode)
	if sessionRepo, ok := repo.(*screenplay.MySQLAuthoringRepository); ok {
		generatorFor := func(r *http.Request, actor string) screenplay.TextGenerator {
			if channel == nil {
				return nil
			}
			authority := newSessionAuthority(auth, r, actor)
			return sessionChannelGenerator{channel: channel, repo: sessionRepo, subject: actor, verify: authority.verify, commitGuard: authority.commit}
		}
		registerAssistantSessionManagement(sessionRepo, handle, decode, func(r *http.Request, owner, actor string) bool {
			return sessionChatAvailable(r.Context(), svc, generatorFor(r, actor), owner, r.PathValue("id"), actor)
		})
		registerSessionProposals(svc, sessionRepo, handle, decode)
		handle("POST /api/comic-screenplays/projects/{id}/assistant/sessions/{session}/chat", "generate", sessionChatHandler(sessionRepo, svc, decode, generatorFor))
	}
	respond := func(w http.ResponseWriter, st screenplay.AuthoringState, e error, actor string) {
		if e != nil {
			authoringError(w, e)
			return
		}
		access := map[string]any{"owner": st.Owner == actor, "read": true, "edit": st.Owner == actor, "generate": st.Owner == actor}
		for _, g := range st.Members {
			if g.Username == actor {
				access["edit"] = g.Edit
				access["generate"] = g.Generate
			}
		}
		if actor != st.Owner {
			st.Members = nil
		}
		payload := map[string]any{"state": st, "access": access}
		if st.Archived {
			payload["notice"] = "作品已归档，后续生产和定时计划已暂停；恢复作品不会自动重新生成。"
		}
		writeJSON(w, 200, payload)
	}
	base := "/api/comic-screenplays/projects/{id}"
	for _, path := range []string{"chapters", "display", "assistant/chat", "team"} {
		path := path
		handle("GET "+base+"/"+path, "read", func(w http.ResponseWriter, r *http.Request, o, a string) {
			st, e := svc.Load(r.Context(), o, r.PathValue("id"))
			if e == nil && path == "assistant/chat" {
				e = svc.RefreshAssistantContentProposals(r.Context(), &st)
			}
			respond(w, st, e, a)
		})
	}
	for _, path := range []string{"chapters", "display", "team"} {
		path := path
		handle("PUT "+base+"/"+path, "edit", func(w http.ResponseWriter, r *http.Request, o, a string) {
			var in struct {
				Expected int64                    `json:"expected_revision"`
				Chapters []screenplay.Chapter     `json:"chapters"`
				Display  map[string]any           `json:"display"`
				Members  []screenplay.MemberGrant `json:"members"`
			}
			if !decode(w, r, &in) {
				return
			}
			st, e := svc.Load(r.Context(), o, r.PathValue("id"))
			if e != nil {
				authoringError(w, e)
				return
			}
			switch path {
			case "chapters":
				if e = screenplay.PreserveScriptGeneration(in.Chapters, st.Chapters); e != nil {
					authoringError(w, e)
					return
				}
				st.Chapters = in.Chapters
			case "display":
				if e = validateAuthoringDisplay(in.Display); e != nil {
					authoringError(w, e)
					return
				}
				st.Display = in.Display
			case "team":
				if o != a {
					authoringError(w, screenplay.ErrAccessDenied)
					return
				}
				seen := map[string]bool{}
				for _, g := range in.Members {
					if g.Username == o || seen[g.Username] || g.Username == "" || (!g.Read && (g.Edit || g.Generate)) {
						authoringError(w, screenplay.ErrInvalidArgument)
						return
					}
					seen[g.Username] = true
					if auth.AuthStore == nil {
						authoringError(w, fmt.Errorf("%w: 账号校验服务未配置", screenplay.ErrInvalidState))
						return
					}
					account, er := auth.AuthStore.FindAccount(r.Context(), g.Username)
					if er != nil || !account.Active {
						authoringError(w, fmt.Errorf("%w: 分享账号不存在或未启用", screenplay.ErrInvalidArgument))
						return
					}
				}
				st.Members = in.Members
			}
			if path == "chapters" {
				for _, chapter := range st.Chapters {
					if strings.TrimSpace(chapter.ScriptText) != "" {
						if e = screenplay.ValidateChapterCollection(st.Chapters); e != nil {
							authoringError(w, e)
							return
						}
						break
					}
				}
			}
			st, e = svc.Save(r.Context(), o, st.ProjectID, st, in.Expected)
			if e == nil && path == "chapters" && screenplay.ScriptCollectionReady(st.Chapters) {
				for _, c := range st.Chapters {
					if strings.TrimSpace(c.ScriptText) != "" {
						_, e = screenplay.PublishChapterCollection(r.Context(), store, o, st.ProjectID, st.Chapters)
						break
					}
				}
				if e == nil {
					e = screenplay.InvalidateAuthoringContent(r.Context(), store, o, st.ProjectID, "chapter")
				}
			}
			respond(w, st, e, a)
		})
	}
	for _, path := range []string{"archive", "restore"} {
		path := path
		handle("POST "+base+"/"+path, "edit", func(w http.ResponseWriter, r *http.Request, o, a string) {
			if o != a {
				authoringError(w, screenplay.ErrAccessDenied)
				return
			}
			var in struct {
				Expected int64 `json:"expected_revision"`
			}
			if !decode(w, r, &in) {
				return
			}
			st, e := svc.Load(r.Context(), o, r.PathValue("id"))
			if e == nil {
				if st.Revision != in.Expected {
					authoringError(w, screenplay.ErrRevisionConflict)
					return
				}
				if path == "archive" {
					if e = screenplay.PauseArchivedProduction(r.Context(), store, o, st.ProjectID); e != nil {
						authoringError(w, e)
						return
					}
				}
				st.Archived = path == "archive"
				st, e = svc.Save(r.Context(), o, st.ProjectID, st, in.Expected)
			}
			respond(w, st, e, a)
		})
	}
	for _, path := range []string{"archived", "shared"} {
		path := path
		handle("GET /api/comic-screenplays/"+path, "read", func(w http.ResponseWriter, r *http.Request, o, a string) {
			rows, e := repo.List(r.Context())
			if e != nil {
				authoringError(w, e)
				return
			}
			projects := []screenplay.Project{}
			for _, st := range rows {
				want := path == "archived" && st.Owner == a && st.Archived
				if path == "shared" && !st.Archived && st.Owner != a {
					resolved, er := svc.Resolve(r.Context(), a, st.ProjectID, "read")
					want = er == nil && resolved == st.Owner
				}
				if want {
					p, er := store.GetProject(r.Context(), st.Owner, st.ProjectID)
					if er == nil {
						projects = append(projects, p)
					}
				}
			}
			writeJSON(w, 200, map[string]any{"projects": projects})
		})
	}
	// Read permissions suffice: this inspector never resolves or calls a model.
	handle("POST "+base+"/assistant/read-tool", "read", func(w http.ResponseWriter, r *http.Request, o, a string) {
		var in struct {
			Tool string          `json:"tool"`
			Args json.RawMessage `json:"tool_args"`
		}
		if !decode(w, r, &in) {
			return
		}
		run, data, e := assistantReadScopedTool(r.Context(), svc, o, r.PathValue("id"), in.Tool, in.Args)
		if e != nil {
			authoringError(w, e)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"tool_run": run, "data": data})
	})
	handle("POST "+base+"/assistant/prepare-asset-edit", "edit", func(w http.ResponseWriter, r *http.Request, o, a string) {
		var in struct {
			Expected int64           `json:"expected_revision"`
			Tool     string          `json:"tool"`
			Args     json.RawMessage `json:"tool_args"`
		}
		if !decode(w, r, &in) {
			return
		}
		st, e := svc.Load(r.Context(), o, r.PathValue("id"))
		if e != nil {
			authoringError(w, e)
			return
		}
		if st.Revision != in.Expected {
			authoringError(w, screenplay.ErrRevisionConflict)
			return
		}
		action, summary, e := screenplay.PrepareAssistantAssetEdit(r.Context(), store, o, st.ProjectID, in.Tool, in.Args)
		if e != nil {
			authoringError(w, e)
			return
		}
		st.Proposals = append(st.Proposals, screenplay.AssistantProposal{ID: newAgentID("proposal"), Summary: summary, Actions: []screenplay.AuthoringAction{action}, Status: "pending", Revision: st.Revision + 1})
		st, e = svc.Save(r.Context(), o, st.ProjectID, st, in.Expected)
		respond(w, st, e, a)
	})
	handle("POST "+base+"/assistant/prepare-shot-edit", "edit", func(w http.ResponseWriter, r *http.Request, o, a string) {
		var in struct {
			Expected int64           `json:"expected_revision"`
			Tool     string          `json:"tool"`
			Args     json.RawMessage `json:"tool_args"`
		}
		if !decode(w, r, &in) {
			return
		}
		st, e := svc.Load(r.Context(), o, r.PathValue("id"))
		if e != nil {
			authoringError(w, e)
			return
		}
		if st.Revision != in.Expected {
			authoringError(w, screenplay.ErrRevisionConflict)
			return
		}
		action, summary, e := screenplay.PrepareAssistantShotEdit(r.Context(), store, o, st.ProjectID, in.Tool, in.Args)
		if e != nil {
			authoringError(w, e)
			return
		}
		st.Proposals = append(st.Proposals, screenplay.AssistantProposal{ID: newAgentID("proposal"), Summary: summary, Actions: []screenplay.AuthoringAction{action}, Status: "pending", Revision: st.Revision + 1})
		st, e = svc.Save(r.Context(), o, st.ProjectID, st, in.Expected)
		respond(w, st, e, a)
	})
	handle("POST "+base+"/assistant/prepare-content-edit", "edit", func(w http.ResponseWriter, r *http.Request, o, actor string) {
		ctx := r.Context()
		p := r.PathValue("id")
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, screenplayJSONLimit))
		if err != nil {
			authoringError(w, screenplay.ErrInvalidArgument)
			return
		}
		owner, err := svc.Resolve(ctx, actor, p, "edit")
		if err != nil {
			authoringError(w, err)
			return
		}
		if owner != o {
			authoringError(w, screenplay.ErrAccessDenied)
			return
		}
		st, err := svc.Load(ctx, o, p)
		if err != nil {
			authoringError(w, err)
			return
		}
		project, err := store.GetProject(ctx, o, p)
		if err != nil {
			authoringError(w, err)
			return
		}
		active, err := screenplay.ReadAssistantActiveDocument(ctx, store, o, p)
		if err != nil {
			authoringError(w, err)
			return
		}
		action, summary, err := screenplay.PrepareAssistantContentEdit(project, st, active, body, false)
		if err != nil {
			authoringError(w, err)
			return
		}
		st.Proposals = append(st.Proposals, screenplay.AssistantProposal{ID: newAgentID("proposal"), Summary: summary, Actions: []screenplay.AuthoringAction{action}, Status: "pending", Revision: st.Revision + 1})
		st, err = svc.Save(ctx, o, p, st, st.Revision)
		respond(w, st, err, actor)
	})
	handle("POST "+base+"/assistant/apply", "edit", func(w http.ResponseWriter, r *http.Request, o, a string) {
		var in struct {
			Expected   int64  `json:"expected_revision"`
			ProposalID string `json:"proposal_id"`
			Confirmed  bool   `json:"confirmed"`
		}
		if !decode(w, r, &in) {
			return
		}
		st, e := svc.ApplyAs(r.Context(), a, o, r.PathValue("id"), in.ProposalID, in.Expected, in.Confirmed)
		respond(w, st, e, a)
	})
	for _, path := range []string{"chapters/plan", "chapters/{chapter}/script", "assistant/chat"} {
		path := path
		handle("POST "+base+"/"+path, "generate", func(w http.ResponseWriter, r *http.Request, o, a string) {
			var in struct {
				Expected    int64                                   `json:"expected_revision"`
				Instruction string                                  `json:"instruction"`
				Message     string                                  `json:"message"`
				Action      string                                  `json:"action"`
				Confirmed   bool                                    `json:"confirmed"`
				References  []screenplay.AssistantReferenceSelector `json:"references,omitempty"`
			}
			if !decode(w, r, &in) {
				return
			}
			if path != "assistant/chat" && len(in.References) > 0 {
				authoringError(w, screenplay.ErrInvalidArgument)
				return
			}
			var authority sessionAuthorityCallbacks
			if len(in.References) > 0 {
				authority = newSessionAuthority(auth, r, a)
				if err := authority.verify(r.Context()); err != nil {
					authoringError(w, err)
					return
				}
			}
			if path == "chapters/{chapter}/script" {
				authoring, err := svc.Load(r.Context(), o, r.PathValue("id"))
				if err != nil {
					authoringError(w, err)
					return
				}
				hasGeneration := false
				for _, chapter := range authoring.Chapters {
					if chapter.ID == r.PathValue("chapter") && chapter.ScriptGeneration != nil {
						hasGeneration = true
					}
				}
				if hasGeneration {
					batchSvc := svc
					batchSvc.Generator = screenplayTextGenerator{owner: o, resolver: resolver}
					st, err := batchSvc.GenerateScriptBatch(r.Context(), o, r.PathValue("id"), r.PathValue("chapter"), in.Action, in.Instruction, in.Expected, in.Confirmed)
					respond(w, st, err, a)
					return
				}
				project, err := store.GetProject(r.Context(), o, r.PathValue("id"))
				if err != nil {
					authoringError(w, err)
					return
				}
				id := project.PromptID
				if project.CreativeConfig != nil && project.CreativeConfig.ScriptPromptID != "" {
					id = project.CreativeConfig.ScriptPromptID
				}
				if id != "" {
					selected, err := screenplay.ResolveProjectPrompt(project, id, "script")
					if err != nil {
						authoringError(w, err)
						return
					}
					id = selected.ID
				}
				if id == screenplay.ScriptBatchPromptID {
					batchSvc := svc
					batchSvc.Generator = screenplayTextGenerator{owner: o, resolver: resolver}
					st, err := batchSvc.GenerateScriptBatch(r.Context(), o, project.ID, r.PathValue("chapter"), in.Action, in.Instruction, in.Expected, in.Confirmed)
					respond(w, st, err, a)
					return
				}
				if in.Action != "" && in.Action != "start" {
					authoringError(w, screenplay.ErrInvalidArgument)
					return
				}
			}
			var out screenplay.AuthoringState
			e := svc.WithLock(r.Context(), o, r.PathValue("id"), func() error {
				st, e := svc.Load(r.Context(), o, r.PathValue("id"))
				if e != nil {
					return e
				}
				if st.Revision != in.Expected {
					return screenplay.ErrRevisionConflict
				}
				project, e := store.GetProject(r.Context(), o, st.ProjectID)
				if e != nil {
					return e
				}
				var references []screenplay.AssistantReferenceSnapshot
				if len(in.References) > 0 {
					view, err := screenplay.LoadAssistantReferenceView(r.Context(), store, repo, o, st.ProjectID)
					if err != nil {
						return err
					}
					references, err = view.Resolve(in.References)
					if err != nil {
						return err
					}
				}
				system := prompt.ScreenplayAuthoringAssistant
				user := in.Message
				jsonResponse := true
				var chapterIndex = -1
				// 有引用的历史保留原消息和快照；模型单独使用压缩视图。
				// 从未含引用的旧会话继续沿用原有持久化折叠行为。
				hasReferenceHistory := false
				for _, message := range st.Messages {
					if len(message.References) > 0 {
						hasReferenceHistory = true
						break
					}
				}
				conversation, compacted := screenplay.CompactAssistantMessages(st.Messages, screenplay.AssistantCompactConfig{})
				if compacted && path == "assistant/chat" && !hasReferenceHistory {
					st.Messages = conversation
				}
				modelConversation := append([]screenplay.AssistantMessage(nil), conversation...)
				for i := range modelConversation {
					modelConversation[i].References = nil
				}
				contextData := map[string]any{"project": project, "chapters": st.Chapters, "conversation": modelConversation}
				var shots []screenplay.Shot
				active, activeErr := screenplay.ReadAssistantActiveDocument(r.Context(), store, o, st.ProjectID)
				if activeErr != nil && path == "assistant/chat" {
					return activeErr
				}
				hasDoc := active.ID != ""
				if hasDoc && activeErr == nil {
					contextData["document"] = active
					shots, e = store.ListShots(r.Context(), o, st.ProjectID, active.ID)
					if e != nil {
						return e
					}
					contextData["shots"] = shots
				}
				assets, er := store.ListAssets(r.Context(), o, st.ProjectID, "")
				if er != nil {
					return er
				}
				contextData["assets"] = assets
				if path == "assistant/chat" {
					caps, cov, e := buildAssistantCapabilities(r.Context(), store, media, o, st.ProjectID, project, st, assets, shots, hasDoc)
					if e != nil {
						return e
					}
					contextData["capabilities"] = caps
					contextData["coverage"] = cov
					system += "\n\n" + screenplay.FormatCapabilitiesForPrompt(caps, cov) + prompt.ScreenplayAuthoringReadTools + prompt.ScreenplayAuthoringAssetEditTools + prompt.ScreenplayAuthoringShotEditTools
				}
				switch path {
				case "chapters/plan":
					system = prompt.ScreenplayAuthoringPlan
					user = in.Instruction
				case "chapters/{chapter}/script":
					system = prompt.ScreenplayAuthoringChapter
					user = in.Instruction
					jsonResponse = false
					for i, c := range st.Chapters {
						if c.ID == r.PathValue("chapter") {
							chapterIndex = i
							contextData["selected_chapter"] = c
						}
					}
					if chapterIndex < 0 {
						return screenplay.ErrNotFound
					}
				default:
					if strings.TrimSpace(user) == "" {
						return screenplay.ErrEmptyContent
					}
				}
				b, _ := json.Marshal(contextData)
				request := screenplay.TextRequest{Model: project.ModelID, SystemPrompt: system, UserPrompt: string(b) + "\n用户要求：" + user, JSONResponse: jsonResponse, MaxOutputTokens: 16000, Temperature: 0.4}
				if len(references) > 0 {
					// Reuse the bounded whitelist for reference-bearing default chats.
					targets, err := captureSessionEditView(r.Context(), store, o, st.ProjectID)
					if err != nil {
						return err
					}
					request, err = sessionChatPromptWithReferences(screenplay.AssistantSessionRequest{Mode: "confirm", Chapters: st.Chapters, History: screenplay.AssistantSessionHistory{Messages: conversation, Proposals: st.Proposals}}, project, active, targets, user, references)
					if err != nil {
						return err
					}
				}
				if chapterIndex >= 0 {
					request, e = screenplay.BuildChapterTextRequest(project, st.Chapters[chapterIndex], user, string(b))
					if e != nil {
						return e
					}
				}
				var result screenplay.TextResponse
				if len(references) > 0 {
					result, e = generateReferenceText(r.Context(), o, resolver, request)
				} else {
					result, e = (screenplayTextGenerator{owner: o, resolver: resolver}).Generate(r.Context(), request)
				}
				if e != nil {
					return e
				}
				if e = r.Context().Err(); e != nil {
					return e
				}
				raw := strings.TrimSpace(result.Content)
				if raw == "" {
					return screenplay.ErrAIEmptyContent
				}
				raw = strings.TrimPrefix(raw, "```json")
				raw = strings.TrimSuffix(raw, "```")
				switch path {
				case "chapters/plan":
					var parsed struct {
						Chapters []screenplay.Chapter `json:"chapters"`
					}
					if json.Unmarshal([]byte(raw), &parsed) != nil || len(parsed.Chapters) == 0 {
						return screenplay.ErrAIInvalidJSON
					}
					st.Chapters = parsed.Chapters
				case "chapters/{chapter}/script":
					if e = screenplay.ApplyChapterGeneration(&st.Chapters[chapterIndex], result.Content); e != nil {
						return e
					}
					if _, e = screenplay.PublishChapterCollection(r.Context(), store, o, st.ProjectID, st.Chapters); e != nil {
						return e
					}
				default:
					var parsed assistantWire
					if json.Unmarshal([]byte(raw), &parsed) != nil || strings.TrimSpace(parsed.Reply) == "" {
						return screenplay.ErrAIInvalidJSON
					}
					toolRuns := []screenplay.AssistantToolRun{}
					if strings.TrimSpace(parsed.Tool) != "" && !screenplay.IsAssistantAssetEditTool(parsed.Tool) && !screenplay.IsAssistantShotEditTool(parsed.Tool) {
						if len(parsed.ActionInput) > 0 {
							return screenplay.ErrInvalidArgument
						}
						run, detail, toolErr := assistantReadScopedTool(r.Context(), svc, o, st.ProjectID, parsed.Tool, parsed.ToolArgs)
						if toolErr != nil {
							return toolErr
						}
						toolRuns = append(toolRuns, run)
						review := screenplay.IsAssistantReviewReadTool(parsed.Tool)
						responseSystem := system
						responseData := contextData
						if review {
							responseSystem = prompt.ScreenplayAuthoringReviewReply
							responseData = map[string]any{"review_tool": parsed.Tool}
						}
						responseData["tool_result"] = detail
						if len(references) > 0 {
							responseData = map[string]any{"tool_result": detail, "references": references}
							responseSystem += prompt.ScreenplayAssistantReferences
						}
						payload, err := json.Marshal(responseData)
						if err != nil {
							return err
						}
						retryRequest := screenplay.TextRequest{Model: project.ModelID, SystemPrompt: responseSystem, UserPrompt: string(payload) + "\n用户要求：" + user, JSONResponse: true, MaxOutputTokens: 16000, Temperature: 0.4}
						if len(references) > 0 {
							retryRequest.RequestByteLimit = sessionPromptBytes
							if err := checkReferenceRequestBudget(retryRequest); err != nil {
								return err
							}
						}
						var retry screenplay.TextResponse
						var retryErr error
						if len(references) > 0 {
							retry, retryErr = generateReferenceText(r.Context(), o, resolver, retryRequest)
						} else {
							retry, retryErr = (screenplayTextGenerator{owner: o, resolver: resolver}).Generate(r.Context(), retryRequest)
						}
						if retryErr != nil {
							return retryErr
						}
						raw = strings.TrimSpace(retry.Content)
						raw = strings.TrimPrefix(raw, "```json")
						raw = strings.TrimSuffix(raw, "```")
						parsed = assistantWire{}
						if json.Unmarshal([]byte(raw), &parsed) != nil || strings.TrimSpace(parsed.Reply) == "" {
							return screenplay.ErrAIInvalidJSON
						}
						if review && (parsed.Tool != "" || len(parsed.ActionInput) > 0) {
							return fmt.Errorf("%w: 审核数据只供回答，本轮不能继续调用工具或准备修改", screenplay.ErrInvalidArgument)
						}
						if strings.TrimSpace(parsed.Tool) != "" && !screenplay.IsAssistantAssetEditTool(parsed.Tool) && !screenplay.IsAssistantShotEditTool(parsed.Tool) {
							return fmt.Errorf("%w: 本轮工具读取后必须直接回答", screenplay.ErrInvalidArgument)
						}
					}
					if screenplay.IsAssistantAssetEditTool(parsed.Tool) {
						if len(parsed.ActionInput) > 0 {
							return screenplay.ErrInvalidArgument
						}
						action, summary, editErr := screenplay.PrepareAssistantAssetEdit(r.Context(), store, o, st.ProjectID, parsed.Tool, parsed.ToolArgs)
						if editErr != nil {
							return editErr
						}
						parsed = assistantWire{Reply: "已准备" + summary + "的方案，请核对后确认应用；当前资产尚未修改。", Summary: summary, Actions: []screenplay.AuthoringAction{action}}
					}

					if screenplay.IsAssistantShotEditTool(parsed.Tool) {
						if len(parsed.ActionInput) > 0 {
							return screenplay.ErrInvalidArgument
						}
						action, summary, editErr := screenplay.PrepareAssistantShotEdit(r.Context(), store, o, st.ProjectID, parsed.Tool, parsed.ToolArgs)
						if editErr != nil {
							return editErr
						}
						parsed = assistantWire{Reply: "已准备" + summary + "的方案，请核对后确认应用；当前分镜尚未修改。", Summary: summary, Actions: []screenplay.AuthoringAction{action}}
					}
					if len(parsed.ActionInput) > 0 {
						if len(parsed.ActionInput) != 1 {
							return screenplay.ErrInvalidArgument
						}
						owner, err := svc.Resolve(r.Context(), a, st.ProjectID, "edit")
						if err != nil {
							return err
						}
						if owner != o {
							return screenplay.ErrAccessDenied
						}
						action, summary, err := screenplay.PrepareAssistantContentEdit(project, st, active, parsed.ActionInput[0], true)
						if err != nil {
							return err
						}
						if err = screenplay.CheckAssistantContentContext(r.Context(), store, project, active); err != nil {
							return err
						}
						parsed = assistantWire{Reply: "修改方案已准备，请核对后确认应用。", Summary: summary, Actions: []screenplay.AuthoringAction{action}}
					}
					if len(parsed.Actions) > 0 {
						if e = screenplay.ValidateAuthoringActions(parsed.Actions); e != nil {
							return e
						}
						st.Proposals = append(st.Proposals, screenplay.AssistantProposal{ID: newAgentID("proposal"), Summary: parsed.Summary, Actions: parsed.Actions, Status: "pending", Revision: st.Revision + 1})
					}
					assistantContent := parsed.Reply
					reply := screenplay.AssistantMessage{Role: "assistant", Content: assistantContent, Tools: toolRuns}
					if strings.TrimSpace(parsed.Question) != "" {
						// 主动追问落为独立消息，前端据此渲染追问卡片；不改任何作品数据。
						st.Messages = append(st.Messages,
							screenplay.AssistantMessage{Role: "user", Content: user, References: references},
							reply,
							screenplay.AssistantMessage{Role: "assistant", Content: strings.TrimSpace(parsed.Question), Kind: screenplay.AssistantMessageKindQuestion})
					} else {
						st.Messages = append(st.Messages,
							screenplay.AssistantMessage{Role: "user", Content: user, References: references},
							reply)
					}
					// 导航建议：校验入口标识后附加到最近一条普通助手消息，供前端点击跳转。
					if safe := sanitizeAssistantSuggestions(parsed.Suggestions); len(safe) > 0 {
						for i := len(st.Messages) - 1; i >= 0; i-- {
							if st.Messages[i].Kind == "" && st.Messages[i].Role == "assistant" {
								st.Messages[i].Suggestions = safe
								break
							}
						}
					}
				}
				if len(references) > 0 {
					mysql, ok := repo.(*screenplay.MySQLAuthoringRepository)
					if !ok {
						return screenplay.ErrInvalidState
					}
					out, e = mysql.SaveAssistantReferences(r.Context(), st, a, in.Expected, references, authority.commit)
				} else {
					out, e = svc.Save(r.Context(), o, st.ProjectID, st, in.Expected)
				}
				if e == nil && path == "chapters/{chapter}/script" {
					e = screenplay.InvalidateAuthoringContent(r.Context(), store, o, st.ProjectID, "chapter")
				}
				return e
			})
			respond(w, out, e, a)
		})
	}
	handle("POST "+base+"/storyboards/{engine}/{episode}/shots/batch", "edit", func(w http.ResponseWriter, r *http.Request, o, a string) {
		var in screenplay.StoryboardBatchEdit
		if !decode(w, r, &in) {
			return
		}
		episode, e := strconv.Atoi(r.PathValue("episode"))
		if e != nil {
			authoringError(w, screenplay.ErrInvalidArgument)
			return
		}
		out, e := screenplay.EditStoryboardBatch(r.Context(), store, o, r.PathValue("id"), episode, screenplay.StoryboardEngine(r.PathValue("engine")), in)
		if e != nil {
			authoringError(w, e)
			return
		}
		if e = screenplay.InvalidateAuthoringContent(r.Context(), store, o, r.PathValue("id"), "storyboard"); e != nil {
			authoringError(w, e)
			return
		}
		writeJSON(w, 200, map[string]any{"storyboard": out})
	})
}
func authoringError(w http.ResponseWriter, e error) {
	status := 500
	switch {
	case errors.Is(e, screenplay.ErrAccessDenied):
		status = 403
	case errors.Is(e, screenplay.ErrNotFound), errors.Is(e, screenplay.ErrNoActiveScript):
		status = 404
	case errors.Is(e, screenplay.ErrRevisionConflict), errors.Is(e, screenplay.ErrLockBusy):
		status = 409
	case errors.Is(e, screenplay.ErrInvalidArgument), errors.Is(e, screenplay.ErrEmptyContent), errors.Is(e, screenplay.ErrArtifactInvalid), errors.Is(e, screenplay.ErrAIInvalidJSON):
		status = 400
	case errors.Is(e, screenplay.ErrAPIKeyMissing), errors.Is(e, screenplay.ErrInvalidState):
		status = 422
	}
	writeJSON(w, status, map[string]string{"error": e.Error()})
}
func validateAuthoringDisplay(d map[string]any) error {
	for k, v := range d {
		switch k {
		case "editor_version":
			if v != "classic" && v != "studio" {
				return screenplay.ErrInvalidArgument
			}
		case "active_tab":
			ok := false
			for _, s := range []string{"source", "chapters", "assistant", "assets", "storyboard", "media", "production"} {
				if v == s {
					ok = true
				}
			}
			if !ok {
				return screenplay.ErrInvalidArgument
			}
		case "sidebar_collapsed", "show_descriptions":
			if _, ok := v.(bool); !ok {
				return screenplay.ErrInvalidArgument
			}
		case "font_size":
			n, ok := v.(float64)
			if !ok || n < 12 || n > 22 {
				return screenplay.ErrInvalidArgument
			}
		default:
			return screenplay.ErrInvalidArgument
		}
	}
	return nil
}
