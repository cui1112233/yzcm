package httpapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
)

func novelFetchRetrySafetyHoldStages(meta map[string]any, now time.Time) []string {
	stages := []string{}
	for stage, state := range novelFetchRetrySchedule(meta) {
		if novelFetchRetryStateIsManual(state) {
			continue
		}
		_, valid := novelFetchParseRetryTime(firstText(state["next_retry_at"]))
		if !valid || novelFetchRetryStateExhausted(stage, state, now) {
			stages = append(stages, stage)
		}
	}
	return stages
}

// Convert legacy exhausted/corrupt schedules into explicit persisted holds.
// Summaries never enter PutDocument: hydrate and verify the exact snapshot first.
func persistNovelFetchRetrySafetyHold(ctx context.Context, store novelfetchworkshop.Store, owner string, summary novelfetchworkshop.Document, now time.Time) (bool, error) {
	status := strings.ToLower(firstText(summary.Meta["status"], summary.Meta["queueState"]))
	if summary.Meta["manualOnly"] == true || summary.Meta["manual_only"] == true || status == "waiting_configuration" || status == "waiting_login" ||
		strings.EqualFold(firstText(summary.Meta["siteSubmitStatus"], summary.Meta["site_submit_status"]), "submitted") {
		return false, nil
	}
	if status == "queued" || status == "running" || status == "processing" || status == "ai_processing" {
		return false, nil
	}
	if len(novelFetchRetrySafetyHoldStages(summary.Meta, now)) == 0 {
		return false, nil
	}
	doc, err := store.GetDocument(ctx, owner, summary.BookID)
	if err != nil {
		return true, err
	}
	if doc.UpdatedAt != summary.UpdatedAt {
		return true, nil
	}
	stages := novelFetchRetrySafetyHoldStages(doc.Meta, now)
	if len(stages) == 0 {
		return false, nil
	}
	schedule := novelFetchRetrySchedule(doc.Meta)
	reason := "自动重试次数或时长已达上限，或重试时间无效，请核对后手动重试"
	for _, stage := range stages {
		state := schedule[stage]
		state["manual_only"], state["next_retry_at"] = true, ""
		state["error"] = strings.TrimSpace(firstText(state["error"]) + "；" + reason)
		state["updated_at"] = now.UTC().Format(time.RFC3339Nano)
	}
	doc.Meta["retry_schedule"] = schedule
	doc.Meta["manualOnly"] = true
	doc.Meta["status"], doc.Meta["queueState"] = "failed", "failed"
	doc.Meta["error"] = reason
	delete(doc.Meta, "automationNextRetryAt")
	refreshNovelFetchTaskRetrySummary(doc.Meta)
	refreshNovelFetchAIManualRetryHold(doc.Meta)
	_, err = store.CompareAndSwapDocument(ctx, owner, doc)
	if errors.Is(err, novelfetchworkshop.ErrDocumentChanged) {
		return true, nil
	}
	return true, err
}

// Read only metadata pages; selected books are hydrated by existing enqueue
// persistence. Recovery hydrates each stale book before modifying its document.
func listNovelFetchAutomationCandidates(ctx context.Context, store novelfetchworkshop.Store, owner, batch string) ([]novelfetchworkshop.Document, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	candidates, ok := store.(novelfetchworkshop.AutomationCandidateStore)
	if !ok {
		return store.ListDocuments(ctx, owner)
	}
	result := []novelfetchworkshop.Document{}
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, err := candidates.ListAutomationCandidates(ctx, owner, batch, after, 250)
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
		if len(page) < 250 {
			return result, nil
		}
		after = page[len(page)-1].BookID
	}
}
