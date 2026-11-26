package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
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
