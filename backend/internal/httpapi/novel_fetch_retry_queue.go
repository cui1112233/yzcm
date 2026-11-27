package httpapi

import (
	"context"
	"fmt"
	"strings"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
)

type workshopRetryQueueResult struct {
	Requested      int               `json:"requested"`
	Queued         int               `json:"queued"`
	Updated        int               `json:"updated"`
	Retried        int               `json:"retried"`
	Failed         int               `json:"failed"`
	Skipped        int               `json:"skipped"`
	AlreadyRunning int               `json:"already_running"`
	TaskIDs        []string          `json:"task_ids"`
	Jobs           []map[string]any  `json:"jobs"`
	Errors         map[string]string `json:"errors"`
	OK             bool              `json:"ok"`
}

// resetNovelFetchAutomationRetryState 在用户主动重试时清掉自动重试留下的
// 人工标记和退避字段。用户点击重试代表重新开始一轮自动处理；如果保留
// manualOnly，这一轮结束失败后扫描器仍然不会接管，任务会一直停住。
func resetNovelFetchAutomationRetryState(document *novelfetchworkshop.Document) bool {
	if document == nil || document.Meta == nil {
		return false
	}
	changed := false
	switch strings.ToLower(firstText(document.Meta["aiStatus"], document.Meta["ai_status"])) {
	case "ai_processing", "processing", "generating", "running":
		document.Meta["aiStatus"] = "pending"
		delete(document.Meta, "aiExecutionJobId")
		changed = true
	}
	if novelFetchStopped(firstText(document.Meta["status"], document.Meta["queueState"])) {
		document.Meta["status"], document.Meta["queueState"] = "input_ready", "input_ready"
		delete(document.Meta, "currentProcessJobId")
		delete(document.Meta, "current_process_job_id")
		changed = true
	}
	for _, key := range []string{"scheduledHold", "manualOnly", "manual_only", "automationEnqueueAttempts", "automationNextRetryAt", "manualOriginalFetchRequired", "originalFetchRetryAttempts", "originalFetchNextRetryAt", "originalFetchLastError", "manualAiRetryRequired", "rewritePersistenceHold", "classificationInFlight", "manual_ai_retry_required", "manual_ai_retry_stage", "manual_ai_retry_error"} {
		if _, ok := document.Meta[key]; ok {
			delete(document.Meta, key)
			changed = true
		}
	}
	// 用户主动重试代表重新开始一轮处理，阶段重试表和退避时间必须一起清掉；
	// 否则扫描器仍会被旧的 next_retry_at 或 manual_only 挡住。
	if len(novelFetchRetrySchedule(document.Meta)) > 0 {
		clearNovelFetchTaskRetry(document.Meta, "")
		changed = true
	}
	return changed
}

// This function only persists work in the existing queue. Provider calls and
// body processing belong to the worker, whose context outlives the HTTP call.
func enqueueWorkshopRetries(ctx context.Context, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, owner string, ids []string, sensitiveAI bool) (workshopRetryQueueResult, error) {
	return enqueueWorkshopRetriesWhere(ctx, store, queue, owner, ids, sensitiveAI, nil)
}

func enqueueWorkshopRetriesWhere(ctx context.Context, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, owner string, ids []string, sensitiveAI bool, eligible func(novelfetchworkshop.Document) bool) (workshopRetryQueueResult, error) {
	ids = uniqueNovelFetchTaskIDs(ids)
	result := workshopRetryQueueResult{Requested: len(ids), TaskIDs: []string{}, Jobs: []map[string]any{}, Errors: map[string]string{}, OK: true}
	settings, err := store.GetConfig(ctx, owner)
	if err != nil {
		return result, err
	}
	if _, ok := queue.(novelfetchworkshop.ProcessBookDedupQueue); !ok {
		return result, fmt.Errorf("重试队列未配置或不支持防重复入队，未执行生成，请检查 Redis 队列")
	}
	for _, bookID := range ids {
		document, err := store.GetDocument(ctx, owner, bookID)
		if err != nil {
			result.Failed++
			result.Errors[bookID] = "读取任务失败"
			continue
		}
		admit := func(locked context.Context) error {
			if eligible != nil && !eligible(document) {
				result.Skipped++
				return nil
			}
			job := novelFetchRecoveryJob(document, settings, owner, novelFetchAutomationEnabled(settings))
			job.ID = fmt.Sprintf("retry_%d", time.Now().UnixNano())
			job.BookIDs = []string{bookID}
			if job.BatchID == "" {
				job.BatchID = "retry-batch"
			}
			if sensitiveAI {
				job.SensitiveAIEnabled = true
			}
			if active, e := workshopActiveBookJob(locked, queue, owner, document, job); e != nil {
				return e
			} else if active != "" {
				result.Skipped++
				result.AlreadyRunning++
				result.Jobs = append(result.Jobs, map[string]any{"id": active, "existing": true})
				return nil
			}
			originalReady := strings.EqualFold(firstText(document.Meta["originalStatus"], document.Meta["original_status"]), "done")
			aiStatus := strings.ToLower(firstText(document.Meta["aiStatus"], document.Meta["ai_status"]))
			status := strings.ToLower(firstText(document.Meta["status"], document.Meta["queueState"]))
			aiDone := aiStatus == "done" || aiStatus == "ai_done" || aiStatus == "completed"
			uploadNeeded := job.AutoSubmit && !novelFetchVersionsSubmitted(document, job.SelectedVersions)
			if originalReady && aiDone && status != "failed" && !uploadNeeded && novelFetchVersionsGenerated(locked, store, owner, document, job.SelectedVersions) {
				result.Skipped++
				return nil
			}
			accepted, e := stageWorkshopSingleJob(locked, store, queue, document, job, eligible == nil, eligible != nil)
			if e != nil {
				return e
			}
			if !accepted.queued {
				result.Skipped++
				result.AlreadyRunning++
				result.Jobs = append(result.Jobs, map[string]any{"id": accepted.existing, "existing": true})
				return nil
			}
			result.Queued++
			result.TaskIDs = append(result.TaskIDs, bookID)
			result.Jobs = append(result.Jobs, map[string]any{"id": job.ID, "book_id": bookID, "status": "queued"})
			return nil
		}
		// Integration callers already hold the same book admission around their
		// eligibility check. Manual retry enters it here and never holds it for AI.
		if eligible != nil {
			err = admit(ctx)
		} else {
			err = withWorkshopBookAdmission(ctx, queue, owner, bookID, admit)
		}
		if err != nil {
			result.Failed++
			result.Errors[bookID] = err.Error()
		}
	}
	result.Updated, result.Retried = result.Queued, result.Queued
	result.OK = result.Failed == 0
	return result, nil
}
