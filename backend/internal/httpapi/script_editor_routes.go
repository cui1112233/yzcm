package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/scripteditor"
)

const scriptEditorJSONLimit = 24 << 20

func RegisterScriptEditorRoutes(mux *http.ServeMux, auth BridgeAuth, service *scripteditor.Service, resolvers ...func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error)) {
	if mux == nil || service == nil || service.Store == nil {
		return
	}
	handle := func(pattern string, fn func(http.ResponseWriter, *http.Request, string)) {
		mux.Handle(pattern, auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			identity, ok := BridgeIdentityFromContext(req.Context())
			if !ok || strings.TrimSpace(identity.Username) == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "请先登录"})
				return
			}
			owner := strings.TrimSpace(identity.Username)
			if account, ok := AccountCenterIdentityFromContext(req.Context()); ok && strings.EqualFold(account.Subject.Username, owner) {
				owner = account.Subject.Username
			}
			fn(w, req, owner)
		})))
	}
	decode := func(w http.ResponseWriter, req *http.Request, target any) bool {
		decoder := json.NewDecoder(http.MaxBytesReader(w, req.Body, scriptEditorJSONLimit))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(target); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求内容无效，请检查后再试"})
			return false
		}
		return true
	}
	var resolver func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error)
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	}
	bindingStore, hasBindingStore := service.Store.(scripteditor.BindingStore)
	agentBridge := scripteditor.AgentBridge{Store: service.Store}

	handle("GET /api/script-editor/works", func(w http.ResponseWriter, req *http.Request, owner string) {
		query := req.URL.Query()
		rows, err := service.Store.ListWorks(req.Context(), owner, scripteditor.WorkFilter{
			Type: scripteditor.WorkType(query.Get("type")), Mark: scripteditor.MarkStatus(query.Get("mark")), Query: query.Get("q"), Sort: query.Get("sort"),
		})
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"works": rows})
	})
	handle("POST /api/script-editor/works", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input scripteditor.CreateWorkInput
		if !decode(w, req, &input) {
			return
		}
		work, err := service.Store.CreateWork(req.Context(), owner, input)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"work": work})
	})
	handle("GET /api/script-editor/works/{workID}", func(w http.ResponseWriter, req *http.Request, owner string) {
		detail, err := service.Store.GetWork(req.Context(), owner, req.PathValue("workID"))
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"work": detail})
	})
	handle("PATCH /api/script-editor/works/{workID}", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input struct {
			Title            *string                  `json:"title"`
			Mark             *scripteditor.MarkStatus `json:"markStatus"`
			ExpectedRevision int64                    `json:"expectedRevision"`
		}
		if !decode(w, req, &input) {
			return
		}
		work, err := service.Store.UpdateWork(req.Context(), owner, req.PathValue("workID"), scripteditor.UpdateWorkInput{Title: input.Title, Mark: input.Mark}, input.ExpectedRevision)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"work": work})
	})
	handle("DELETE /api/script-editor/works/{workID}", func(w http.ResponseWriter, req *http.Request, owner string) {
		revision, err := queryRevision(req)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		if err := service.Store.DeleteWork(req.Context(), owner, req.PathValue("workID"), revision); err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	})
	handle("POST /api/script-editor/works/{workID}/copy", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input struct {
			RequestKey string `json:"requestKey"`
		}
		if !decode(w, req, &input) {
			return
		}
		work, err := service.CopyWork(req.Context(), owner, req.PathValue("workID"), input.RequestKey)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"work": work})
	})
	handle("POST /api/script-editor/works/{workID}/agent-context", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input struct {
			References []scripteditor.AgentReference `json:"references"`
		}
		if !decode(w, req, &input) {
			return
		}
		contextValue, err := agentBridge.BuildContext(req.Context(), owner, req.PathValue("workID"), input.References)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"context": contextValue})
	})
	handle("POST /api/script-editor/works/{workID}/agent-drafts/{draftID}/apply", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input struct {
			ScriptID         string `json:"scriptId"`
			Title            string `json:"title"`
			Content          string `json:"content"`
			Mode             string `json:"mode"`
			ExpectedRevision int64  `json:"expectedRevision"`
		}
		if !decode(w, req, &input) {
			return
		}
		result, err := agentBridge.ApplyDraft(req.Context(), owner, scripteditor.AgentDraft{ID: req.PathValue("draftID"), WorkID: req.PathValue("workID"), ScriptID: input.ScriptID, Title: input.Title, Content: input.Content, Mode: input.Mode}, input.ExpectedRevision)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	})
	handle("GET /api/script-editor/works/{workID}/search", func(w http.ResponseWriter, req *http.Request, owner string) {
		hits, err := service.SearchWork(req.Context(), owner, req.PathValue("workID"), req.URL.Query().Get("q"))
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
	})
	handle("POST /api/script-editor/works/{workID}/import", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input scripteditor.ImportIntoWorkInput
		if !decode(w, req, &input) {
			return
		}
		work, err := service.ImportIntoWork(req.Context(), owner, req.PathValue("workID"), input)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"work": work})
	})
	handle("GET /api/script-editor/works/{workID}/bindings/{targetType}", func(w http.ResponseWriter, req *http.Request, owner string) {
		if !hasBindingStore {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "当前运行环境不支持工作区绑定"})
			return
		}
		targetType := scripteditor.BindingTargetType(req.PathValue("targetType"))
		if !targetType.Valid() {
			writeScriptEditorError(w, scripteditor.ErrValidation)
			return
		}
		row, err := bindingStore.WorkBinding(req.Context(), owner, req.PathValue("workID"), targetType)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"binding": row})
	})
	handle("PUT /api/script-editor/works/{workID}/bindings/{targetType}", func(w http.ResponseWriter, req *http.Request, owner string) {
		if !hasBindingStore {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "当前运行环境不支持工作区绑定"})
			return
		}
		var input struct {
			TargetID       string                     `json:"targetId"`
			SourceRevision int64                      `json:"sourceRevision"`
			SyncState      scripteditor.SyncState     `json:"syncState"`
			ConflictState  scripteditor.ConflictState `json:"conflictState"`
		}
		if !decode(w, req, &input) {
			return
		}
		workID := req.PathValue("workID")
		if _, err := service.Store.GetWork(req.Context(), owner, workID); err != nil {
			writeScriptEditorError(w, err)
			return
		}
		row, err := bindingStore.UpsertWorkBinding(req.Context(), scripteditor.WorkBinding{OwnerUsername: owner, WorkID: workID, TargetType: scripteditor.BindingTargetType(req.PathValue("targetType")), TargetID: input.TargetID, SourceRevision: input.SourceRevision, SyncState: input.SyncState, ConflictState: input.ConflictState})
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"binding": row})
	})

	handle("POST /api/script-editor/works/{workID}/chapters", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input scripteditor.CreateChapterInput
		if !decode(w, req, &input) {
			return
		}
		chapter, err := service.Store.CreateChapter(req.Context(), owner, req.PathValue("workID"), input)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"chapter": chapter})
	})
	handle("PATCH /api/script-editor/works/{workID}/chapters/{chapterID}", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input struct {
			Title            *string `json:"title"`
			Content          *string `json:"content"`
			ExpectedRevision int64   `json:"expectedRevision"`
		}
		if !decode(w, req, &input) {
			return
		}
		chapter, err := service.Store.UpdateChapter(req.Context(), owner, req.PathValue("workID"), req.PathValue("chapterID"), scripteditor.UpdateChapterInput{Title: input.Title, Content: input.Content}, input.ExpectedRevision)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"chapter": chapter})
	})
	handle("DELETE /api/script-editor/works/{workID}/chapters/{chapterID}", func(w http.ResponseWriter, req *http.Request, owner string) {
		revision, err := queryRevision(req)
		if err == nil {
			err = service.Store.DeleteChapter(req.Context(), owner, req.PathValue("workID"), req.PathValue("chapterID"), revision)
		}
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	})
	handle("POST /api/script-editor/works/{workID}/chapters/reorder", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input struct {
			ChapterIDs []string `json:"chapterIds"`
		}
		if !decode(w, req, &input) {
			return
		}
		chapters, err := service.Store.ReorderChapters(req.Context(), owner, req.PathValue("workID"), input.ChapterIDs)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"chapters": chapters})
	})

	handle("GET /api/script-editor/works/{workID}/scripts", func(w http.ResponseWriter, req *http.Request, owner string) {
		scripts, err := service.Store.ListScripts(req.Context(), owner, req.PathValue("workID"))
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"scripts": scripts})
	})
	handle("POST /api/script-editor/works/{workID}/scripts", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input scripteditor.CreateScriptInput
		if !decode(w, req, &input) {
			return
		}
		if input.GenerationSource == "" {
			input.GenerationSource = scripteditor.SourceManual
		}
		script, err := service.Store.CreateScript(req.Context(), owner, req.PathValue("workID"), input)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"script": script})
	})
	handle("POST /api/script-editor/works/{workID}/scripts/import", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input struct {
			RequestKey string                           `json:"requestKey"`
			Scripts    []scripteditor.CreateScriptInput `json:"scripts"`
		}
		if !decode(w, req, &input) {
			return
		}
		rows, err := service.ImportScripts(req.Context(), owner, req.PathValue("workID"), input.Scripts, input.RequestKey)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"scripts": rows})
	})
	handle("POST /api/script-editor/works/{workID}/scripts/replace", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input scripteditor.ReplaceScriptsInput
		if !decode(w, req, &input) {
			return
		}
		rows, err := service.ReplaceScripts(req.Context(), owner, req.PathValue("workID"), input)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"scripts": rows})
	})
	handle("PATCH /api/script-editor/works/{workID}/scripts/{scriptID}", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input struct {
			Title            *string `json:"title"`
			Content          *string `json:"content"`
			ExpectedRevision int64   `json:"expectedRevision"`
		}
		if !decode(w, req, &input) {
			return
		}
		script, err := service.Store.UpdateScript(req.Context(), owner, req.PathValue("workID"), req.PathValue("scriptID"), scripteditor.UpdateScriptInput{Title: input.Title, Content: input.Content}, input.ExpectedRevision)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"script": script})
	})
	handle("DELETE /api/script-editor/works/{workID}/scripts/{scriptID}", func(w http.ResponseWriter, req *http.Request, owner string) {
		revision, err := queryRevision(req)
		if err == nil {
			err = service.Store.DeleteScript(req.Context(), owner, req.PathValue("workID"), req.PathValue("scriptID"), revision)
		}
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	})
	handle("POST /api/script-editor/works/{workID}/scripts/{scriptID}/copy", func(w http.ResponseWriter, req *http.Request, owner string) {
		script, err := service.Store.CopyScript(req.Context(), owner, req.PathValue("workID"), req.PathValue("scriptID"))
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"script": script})
	})
	handle("POST /api/script-editor/works/{workID}/scripts/{scriptID}/prefer", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input struct {
			ExpectedRevision int64 `json:"expectedRevision"`
		}
		if !decode(w, req, &input) {
			return
		}
		script, err := service.Store.PreferScript(req.Context(), owner, req.PathValue("workID"), req.PathValue("scriptID"), input.ExpectedRevision)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"script": script})
	})

	handle("POST /api/script-editor/import/preview", func(w http.ResponseWriter, req *http.Request, _ string) {
		req.Body = http.MaxBytesReader(w, req.Body, scriptEditorJSONLimit)
		file, header, err := req.FormFile("file")
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请选择要导入的文件"})
			return
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "文件读取失败，请重新选择"})
			return
		}
		preview, err := scripteditor.PreviewImport(header.Filename, content)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"preview": preview})
	})
	handle("POST /api/script-editor/import/commit", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input scripteditor.ImportCommitInput
		if !decode(w, req, &input) {
			return
		}
		detail, err := service.CommitImport(req.Context(), owner, input)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"work": detail})
	})
	handle("POST /api/script-editor/works/{workID}/generate", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input scripteditor.GenerateScriptInput
		if !decode(w, req, &input) {
			return
		}
		if resolver == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "请先配置文字模型"})
			return
		}
		provider, err := resolver(req.Context(), owner, input.ModelID)
		if err != nil || provider == nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "选择的文字模型不可用，请检查API配置"})
			return
		}
		script, err := service.GenerateScript(req.Context(), owner, req.PathValue("workID"), input, scriptEditorGenerator{provider: provider})
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"script": script})
	})
	handle("POST /api/script-editor/works/{workID}/jobs/script-generation", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input scripteditor.CreateGenerationJobInput
		if !decode(w, req, &input) {
			return
		}
		job, err := service.CreateGenerationJob(req.Context(), owner, req.PathValue("workID"), input)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
	})
	handle("POST /api/script-editor/works/{workID}/jobs/review", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input scripteditor.CreateReviewJobInput
		if !decode(w, req, &input) {
			return
		}
		job, err := service.CreateReviewJob(req.Context(), owner, req.PathValue("workID"), input)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
	})
	handle("GET /api/script-editor/works/{workID}/jobs", func(w http.ResponseWriter, req *http.Request, owner string) {
		jobs, err := service.ListJobs(req.Context(), owner, req.PathValue("workID"))
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
	})
	handle("GET /api/script-editor/works/{workID}/jobs/{jobID}", func(w http.ResponseWriter, req *http.Request, owner string) {
		job, err := service.Store.GetJob(req.Context(), owner, req.PathValue("workID"), req.PathValue("jobID"))
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job})
	})
	handle("POST /api/script-editor/works/{workID}/jobs/{jobID}/cancel", func(w http.ResponseWriter, req *http.Request, owner string) {
		job, err := service.CancelJob(req.Context(), owner, req.PathValue("workID"), req.PathValue("jobID"))
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"job": job})
	})
	handle("POST /api/script-editor/works/{workID}/jobs/{jobID}/retry", func(w http.ResponseWriter, req *http.Request, owner string) {
		job, err := service.RetryJob(req.Context(), owner, req.PathValue("workID"), req.PathValue("jobID"))
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"job": job})
	})
	handle("POST /api/script-editor/works/{workID}/jobs/{jobID}/apply", func(w http.ResponseWriter, req *http.Request, owner string) {
		script, err := service.ApplyJobResult(req.Context(), owner, req.PathValue("workID"), req.PathValue("jobID"))
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"script": script})
	})
	handle("GET /api/script-editor/works/{workID}/export", func(w http.ResponseWriter, req *http.Request, owner string) {
		result, err := service.Export(req.Context(), owner, req.PathValue("workID"), scripteditor.ExportInput{Format: req.URL.Query().Get("format")})
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		disposition := mime.FormatMediaType("attachment", map[string]string{"filename": result.Filename})
		w.Header().Set("Content-Type", result.ContentType)
		w.Header().Set("Content-Disposition", disposition)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(result.Content)
	})
	handle("POST /api/script-editor/works/{workID}/handoffs/comic-screenplay", func(w http.ResponseWriter, req *http.Request, owner string) {
		var input scripteditor.HandoffInput
		if !decode(w, req, &input) {
			return
		}
		handoff, err := service.CreateComicHandoff(req.Context(), owner, req.PathValue("workID"), input)
		if err != nil {
			writeScriptEditorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"handoff": handoff})
	})
}

func queryRevision(req *http.Request) (int64, error) {
	value := strings.TrimSpace(req.URL.Query().Get("expectedRevision"))
	if value == "" {
		return 0, scripteditor.ErrValidation
	}
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision < 0 {
		return 0, scripteditor.ErrValidation
	}
	return revision, nil
}

func writeScriptEditorError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, scripteditor.ErrImportTooLarge):
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "导入文件不能超过20MiB"})
	case errors.Is(err, scripteditor.ErrConflict):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "内容已被其他窗口修改，请重新载入"})
	case errors.Is(err, scripteditor.ErrValidation):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
	case errors.Is(err, scripteditor.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "作品不存在或已删除"})
	case errors.Is(err, scripteditor.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "没有权限访问这个作品"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "剧本编辑服务暂时不可用"})
	}
}
