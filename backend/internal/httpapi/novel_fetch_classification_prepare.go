package httpapi

import (
	"context"
	"fmt"
	"qiantie/backend/internal/novelfetchworkshop"
	"strings"
)

// Keep resolver failures in the existing safe classification error formatter;
// never substitute another provider or turn configuration errors into labels.
func workshopClassificationProviderForResult(provider novelfetchworkshop.RewriteProvider, err error) novelfetchworkshop.RewriteProvider {
	if err == nil {
		return provider
	}
	return workshopClassificationProvider(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) { return "", err })
}

// Both full processing and generate-only use the same classification gate.
func prepareWorkshopDocumentClassification(ctx context.Context, store novelfetchworkshop.Store, owner, bookID string, judge novelfetchworkshop.AIGenderJudge, provider novelfetchworkshop.RewriteProvider) error {
	doc, err := store.GetDocument(ctx, owner, bookID)
	if err != nil {
		return err
	}
	if novelFetchClassificationResolved(doc) {
		_ = appendWorkshopTaskLog(ctx, store, owner, bookID, "classification_skipped", "已有分类信息，跳过判断")
		return nil
	}
	_ = appendWorkshopTaskLog(ctx, store, owner, bookID, "classification_started", "开始判断男女频和题材风格")
	if err = ensureWorkshopDocumentClassification(ctx, store, owner, bookID, judge, provider); err != nil {
		_ = appendWorkshopTaskLog(ctx, store, owner, bookID, "classification_failed", err.Error())
		return err
	}
	doc, err = store.GetDocument(ctx, owner, bookID)
	if err != nil {
		return err
	}
	if !novelFetchClassificationResolved(doc) {
		return errNovelFetchClassificationPending
	}
	_ = appendWorkshopTaskLog(ctx, store, owner, bookID, "classification_completed", "男女频和题材风格判断完成")
	return nil
}

// A one-step generation must not use prose from an explicitly failed original
// sensitive stage. Missing historical metadata is not evidence of failure.
func holdWorkshopGenerateSensitiveFailure(ctx context.Context, store novelfetchworkshop.Store, owner string, doc novelfetchworkshop.Document) error {
	if err := workshopSensitiveManualRepairHold(doc); err != nil {
		return err
	}
	blocked := strings.EqualFold(firstText(doc.Meta["originalStatus"], doc.Meta["original_status"]), "waiting_sensitive_ai")
	switch strings.ToLower(firstText(doc.Meta["sensitiveStatus"], doc.Meta["sensitive_status"])) {
	case "waiting", "waiting_ai", "waiting_sensitive_ai", "pending", "processing", "running", "failed", "error":
		blocked = true
	}
	if !blocked {
		return nil
	}
	message := "原文敏感处理尚未完成或已失败，请完成敏感处理后手动重试改文"
	doc.Meta["manualOnly"], doc.Meta["error"] = true, message
	if _, err := store.CompareAndSwapDocument(ctx, owner, doc); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", errWorkshopRetryHeld, message)
}

// This marker belongs to explicit sensitive repair. Do not add manualOnly:
// successful repair clears the marker and can resume existing ready work.
func workshopSensitiveManualRepairHold(doc novelfetchworkshop.Document) error {
	if doc.Meta["sensitiveManualRetryRequired"] != true {
		return nil
	}
	return fmt.Errorf("%w: 敏感词 AI 修复尚未完成，请先重新处理失败的正文版本", errWorkshopRetryHeld)
}
