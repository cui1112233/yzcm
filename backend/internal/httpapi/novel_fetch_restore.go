package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
)

func restoreWorkshopOriginal(w http.ResponseWriter, req *http.Request, store novelfetchworkshop.Store, resolve func(*http.Request) (novelfetchworkshop.RewriteProvider, error)) {
	identity, _ := BridgeIdentityFromContext(req.Context())
	bookID := strings.TrimSpace(req.PathValue("bookId"))
	if !novelFetchBookIDPattern.MatchString(bookID) {
		writeJSON(w, 400, map[string]any{"error": "非法的书籍ID"})
		return
	}
	var input struct {
		SensitiveAIEnabled *bool `json:"sensitive_ai_enabled"`
	}
	if req.ContentLength != 0 {
		if err := decodeNovelFetchJSON(w, req, &input); err != nil {
			writeJSON(w, 400, map[string]any{"error": "请求内容无效"})
			return
		}
	}
	document, err := store.GetDocument(req.Context(), identity.Username, bookID)
	if errors.Is(err, novelfetchworkshop.ErrNotFound) {
		writeJSON(w, 404, map[string]any{"error": "任务不存在"})
		return
	}
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "读取任务失败"})
		return
	}
	if novelFetchDocumentStopped(document) {
		writeJSON(w, 409, map[string]any{"error": "任务已停止，请先明确恢复任务"})
		return
	}
	fail := func(cause error) {
		reason := safeSensitiveFailureText(cause.Error())
		// Even a cancelled HTTP request must retain the failed restore's upload gate.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(req.Context()), 5*time.Second)
		defer cancel()
		saveErr := saveNovelFetchSensitiveMetadata(ctx, store, identity.Username, bookID, novelFetchSensitiveMetadataUpdate{Failures: map[string]string{"original": reason}, ExplicitAI: true, Scope: "original"})
		if saveErr != nil {
			reason += "; 保存失败状态失败: " + safeSensitiveFailureText(saveErr.Error())
		}
		status := http.StatusInternalServerError
		if errors.Is(cause, novelfetchworkshop.ErrDocumentChanged) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]any{"error": reason})
	}
	writer, ok := store.(novelfetchworkshop.ConditionalBodyStore)
	if !ok {
		fail(errors.New("正文存储不支持安全恢复，请稍后重试"))
		return
	}
	raw, err := novelfetchworkshop.ReadBodyText(req.Context(), store, identity.Username, document, "original_raw")
	if err != nil {
		fail(fmt.Errorf("读取原始正文失败: %w", err))
		return
	}
	if strings.TrimSpace(raw) == "" {
		writeJSON(w, 400, map[string]any{"error": "没有可恢复的原始正文"})
		return
	}
	settings, err := novelfetchworkshop.ReadWorkflowConfig(req.Context(), store, identity.Username)
	if err != nil {
		fail(fmt.Errorf("读取处理配置失败: %w", err))
		return
	}
	enabled := novelFetchSensitiveAIEnabled(settings)
	if saved, ok := mapOrEmpty(document.Meta["processSnapshot"])["sensitive_ai_enabled"].(bool); ok {
		enabled = saved
	}
	if input.SensitiveAIEnabled != nil {
		enabled = *input.SensitiveAIEnabled
	}
	if model, ok := mapOrEmpty(document.Meta["processSnapshot"])["text_model_id"].(string); ok && strings.TrimSpace(model) != "" {
		req = req.WithContext(novelfetchworkshop.WithProcessTextModel(req.Context(), strings.TrimSpace(model)))
	}
	var provider novelfetchworkshop.RewriteProvider
	if enabled {
		provider, err = resolve(req)
		if err != nil {
			fail(fmt.Errorf("读取敏感词 AI 模型失败: %s", safeSensitiveModelFailure(err)))
			return
		}
	}
	extracted := novelfetchworkshop.ProcessRawText(raw, document.MaxTxt)
	text, trace, items, err := processWorkshopConfiguredText(req.Context(), store, identity.Username, bookID, extracted.ProcessedText, "original", enabled, provider)
	if err == nil && enabled {
		for _, item := range items {
			if item["status"] == "failed" || item["ai_status"] == "failed" {
				err = fmt.Errorf("敏感词 AI 修复失败: %s", safeSensitiveFailureText(firstText(item["error"], "未完成修复")))
				break
			}
		}
	}
	if err != nil {
		fail(fmt.Errorf("恢复原文处理失败: %w", err))
		return
	}
	ref, err := writer.PutBodyIfDocumentUnchanged(req.Context(), identity.Username, novelfetchworkshop.BodyRecord{BookID: bookID, BodyRef: novelfetchworkshop.BodyRef{VersionID: "original", State: "ready"}, Content: text}, document)
	if err != nil {
		fail(fmt.Errorf("保存恢复正文失败: %w", err))
		return
	}
	err = saveNovelFetchSensitiveMetadata(req.Context(), store, identity.Username, bookID, novelFetchSensitiveMetadataUpdate{Changes: []novelFetchSensitiveVersionChange{{VersionID: "original", Ref: ref, Trace: trace, Items: items}}, ExplicitAI: true, Scope: "original", OriginalRestored: true, RestoredRawChars: extracted.OriginalRawChars, RestoredMaxTxt: extracted.MaxTxt})
	if err != nil {
		fail(fmt.Errorf("保存恢复结果失败: %w", err))
		return
	}
	latest, err := store.GetDocument(req.Context(), identity.Username, bookID)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": "读取恢复结果失败"})
		return
	}
	currentRaw, rawErr := novelfetchworkshop.ReadBodyText(req.Context(), store, identity.Username, latest, "original_raw")
	currentText, textErr := novelfetchworkshop.ReadBodyText(req.Context(), store, identity.Username, latest, "original")
	if rawErr != nil || textErr != nil {
		writeJSON(w, 500, map[string]any{"error": "读取恢复正文失败"})
		return
	}
	latest.OriginalRaw = currentRaw
	latest.Original = currentText
	latest.ProcessedText = currentText
	writeJSON(w, 200, map[string]any{"task": latest})
}
