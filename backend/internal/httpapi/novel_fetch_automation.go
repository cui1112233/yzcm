package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
)

const novelFetchAutomationConfigKey = "novel_fetch_automation"

func novelFetchAutomationEnabled(settings map[string]any) bool {
	return mapOrEmpty(settings[novelFetchAutomationConfigKey])["enabled"] == true
}

func novelFetchSensitiveAIEnabled(settings map[string]any) bool {
	if settings == nil {
		return false
	}
	if enabled, ok := settings["sensitive_ai_enabled"].(bool); ok {
		return enabled
	}
	if enabled, ok := settings["sensitiveAiEnabled"].(bool); ok {
		return enabled
	}
	ai := mapOrEmpty(settings["sensitive_ai"])
	if enabled, ok := ai["enabled"].(bool); ok {
		return enabled
	}
	return mapOrEmpty(mapOrEmpty(settings["appConfig"])["sensitive_ai"])["enabled"] == true
}

func saveNovelFetchAutomationEnabled(ctx context.Context, store novelfetchworkshop.Store, owner string, enabled bool) error {
	settings, err := novelfetchworkshop.ReadWorkflowConfig(ctx, store, owner)
	if err != nil {
		return err
	}
	if settings == nil {
		settings = map[string]any{}
	}
	automation := mapOrEmpty(settings[novelFetchAutomationConfigKey])
	automation["enabled"] = enabled
	automation["updated_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	settings[novelFetchAutomationConfigKey] = automation
	return novelfetchworkshop.WriteWorkflowConfig(ctx, store, owner, settings)
}

func novelFetchAutomationVersions(settings map[string]any) []string {
	automation := mapOrEmpty(settings[novelFetchAutomationConfigKey])
	versions := cleanStrings(anyStrings(automation["selected_versions"]))
	if len(versions) == 0 {
		web := mapOrEmpty(mapOrEmpty(settings["appConfig"])["web_submit"])
		versions = cleanStrings(anyStrings(web["submit_versions"]))
	}
	if len(versions) == 0 {
		versions = []string{"ai1"}
	}
	return versions
}

func novelFetchOriginalRetryDelay(settings map[string]any, attempt int) time.Duration {
	minutes := intValue(mapOrEmpty(settings["fetch"])["original_fetch_retry_interval_minutes"])
	if minutes <= 0 {
		minutes = intValue(mapOrEmpty(settings[novelFetchAutomationConfigKey])["original_fetch_retry_interval_minutes"])
	}
	if minutes > 0 {
		return time.Duration(min(minutes, 24*60)) * time.Minute
	}
	delays := [...]time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour}
	return delays[min(max(attempt, 1), len(delays))-1]
}

const novelFetchOriginalFetchMaxRetries = 5
const novelFetchOriginalConfigurationMessage = "原文获取配置不可用，请检查配置后手动重试"

// novelFetchAIManualRetryRequired 判断错误是否属于模型确定性失败。参考实现
// 对这类错误写 manual_ai_retry_required 并禁止自动重试：同一段超长文本反复
// 提交只会重复截断，既消耗额度也不可能自行恢复。
func novelFetchAIManualRetryRequired(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, batchfactoryv11.ErrTextOutputTruncated) || errors.Is(err, batchfactoryv11.ErrTextRefused) || errors.Is(err, batchfactoryv11.ErrTextResultUnknown) {
		return true
	}
	if novelFetchRetryResultUnknown(err) || novelFetchRetryConfigurationFailure(err) {
		return true
	}
	// 模型输出格式错误属于本次输出问题，参考实现允许立即重试而不是转人工。
	if novelFetchAIErrorIsOutputRejection(err) {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, keyword := range []string{"文本模型输出被长度截断", "长度截断", "content policy", "内容审核", "模型拒绝", "refusal", "ai请求在自动尝试", "ai服务暂不可用", "ai凭证暂时熔断", "ai凭证正在执行恢复探测"} {
		if strings.Contains(message, keyword) {
			return true
		}
	}
	return false
}

// markNovelFetchAIManualRetry 把模型确定性失败落成人工状态：写 manualOnly、
// manualAiRetryRequired 和可读原因，让扫描器不再自动接管。用户点批量重试时
// 由 resetNovelFetchAutomationRetryState 清除这些标记。
func markNovelFetchAIManualRetry(ctx context.Context, store novelfetchworkshop.Store, owner, bookID string, failure error, now time.Time) error {
	document, err := store.GetDocument(ctx, owner, bookID)
	if err != nil {
		return err
	}
	if document.Meta == nil {
		document.Meta = map[string]any{}
	}
	reason := "文本模型无法完成本书，请更换输出上限更大的模型或减小单次文本量后重试：" + failure.Error()
	document.Meta["status"] = "failed"
	document.Meta["queueState"] = "failed"
	document.Meta["manualOnly"] = true
	document.Meta["manualAiRetryRequired"] = true
	document.Meta["error"] = reason
	delete(document.Meta, "automationNextRetryAt")
	// 同步写入阶段重试表，保持与参考实现一致的 manual_only 记录，前端和
	// 运维可以直接看到卡在改文阶段。
	scheduleNovelFetchTaskRetry(document.Meta, novelFetchRetryStageForError(failure), failure, 0, now)
	appendNovelFetchDocumentLog(&document, "queue_failed", reason, now)
	return store.PutDocument(ctx, owner, document)
}

type novelFetchOriginalFailure struct {
	Class   novelfetchworkshop.UpstreamFailureClass `json:"class"`
	Message string                                  `json:"message"`
	At      time.Time                               `json:"at"`
}

func markNovelFetchOriginalFetchRetry(ctx context.Context, store novelfetchworkshop.Store, owner, bookID string, fetchErr error, now time.Time) error {
	class, _ := novelfetchworkshop.ClassifyUpstreamFailure(fetchErr)
	message := ""
	if fetchErr != nil {
		message = strings.TrimSpace(fetchErr.Error())
	}
	_, err := saveNovelFetchOriginalFailure(ctx, store, owner, bookID, "", novelFetchOriginalFailure{Class: class, Message: message, At: now})
	return err
}

func saveNovelFetchOriginalFailure(ctx context.Context, store novelfetchworkshop.Store, owner, bookID, jobID string, failure novelFetchOriginalFailure) (string, error) {
	document, err := store.GetDocument(ctx, owner, bookID)
	if err != nil {
		return "", err
	}
	if document.Meta == nil {
		document.Meta = map[string]any{}
	}
	meta := document.Meta
	if jobID != "" {
		if meta["originalFetchFailureJobId"] == jobID {
			return firstText(meta["status"]), nil
		}
		// A write can commit even if the worker received an error.
		meta["originalFetchFailureJobId"] = jobID
	}
	attempts := intValue(meta["originalFetchRetryAttempts"]) + 1
	meta["originalFetchRetryAttempts"] = attempts
	now, message := failure.At, failure.Message
	meta["originalFetchLastError"] = message
	previous := novelFetchRetrySchedule(meta)[novelFetchRetryStageOriginalFetch]
	firstAttempt := firstText(previous["first_attempt_at"])
	if attempts == 1 && !now.IsZero() {
		firstAttempt = now.UTC().Format(time.RFC3339Nano)
	}
	roundExpired := false
	if firstAttempt != "" {
		first, ok := novelFetchParseRetryTime(firstAttempt)
		roundExpired = !ok || first.After(now) || !now.Before(first.Add(novelFetchRetryRoundLifetime))
	}

	switch failure.Class {
	case novelfetchworkshop.UpstreamFailurePermanent:
		markNovelFetchOriginalManual(&document, "failed", "原文来源无效，请检查书籍来源后手动重试", now)
	case novelfetchworkshop.UpstreamFailureConfiguration:
		markNovelFetchOriginalManual(&document, "waiting_configuration", novelFetchOriginalConfigurationMessage, now)
	default:
		if attempts > novelFetchOriginalFetchMaxRetries || roundExpired {
			markNovelFetchOriginalManual(&document, "failed", "原文连续获取失败已停止自动重试，请检查书籍来源是否有效后手动重试", now)
			break
		}
		settings, err := novelfetchworkshop.ReadWorkflowConfig(ctx, store, owner)
		if err != nil {
			return "", err
		}
		next := now.Add(novelFetchOriginalRetryDelay(settings, attempts)).UTC().Format(time.RFC3339Nano)
		meta["status"] = "waiting_retry"
		meta["queueState"] = "waiting_retry"
		meta["originalStatus"] = "waiting_fetch_retry"
		meta["originalFetchNextRetryAt"] = next
		meta["error"] = message

		schedule := novelFetchRetrySchedule(meta)
		schedule[novelFetchRetryStageOriginalFetch] = map[string]any{
			"attempt":          attempts,
			"next_retry_at":    next,
			"error":            message,
			"updated_at":       now.UTC().Format(time.RFC3339Nano),
			"first_attempt_at": firstAttempt,
		}
		meta["retry_schedule"] = schedule
		refreshNovelFetchTaskRetrySummary(meta)
	}
	return firstText(meta["status"]), store.PutDocument(ctx, owner, document)
}

func markNovelFetchOriginalManual(document *novelfetchworkshop.Document, status, reason string, now time.Time) {
	meta := document.Meta
	if cause := firstText(meta["originalFetchLastError"]); cause != "" {
		reason += "：" + cause
	}
	meta["status"] = status
	meta["queueState"] = status
	meta["originalStatus"] = "waiting_manual_fetch"
	meta["manualOnly"] = true
	meta["manualOriginalFetchRequired"] = true
	meta["error"] = reason
	for _, key := range []string{"originalFetchNextRetryAt", "original_fetch_next_retry_at", "automationNextRetryAt", "automation_next_retry_at"} {
		delete(meta, key)
	}
	// A permanent original-fetch failure stops this round, but must not erase
	// another stage's failures or uncertain paid-request evidence.
	schedule := novelFetchRetrySchedule(meta)
	state := copyAnyMap(schedule[novelFetchRetryStageOriginalFetch])
	state["attempt"] = intValue(meta["originalFetchRetryAttempts"])
	state["manual_only"], state["next_retry_at"] = true, ""
	state["error"], state["updated_at"] = reason, now.UTC().Format(time.RFC3339Nano)
	if intValue(state["attempt"]) == 1 && firstText(state["first_attempt_at"]) == "" && !now.IsZero() {
		state["first_attempt_at"] = now.UTC().Format(time.RFC3339Nano)
	}
	schedule[novelFetchRetryStageOriginalFetch] = state
	meta["retry_schedule"] = schedule
	refreshNovelFetchTaskRetrySummary(meta)
	refreshNovelFetchAIManualRetryHold(meta)
	appendNovelFetchDocumentLog(document, "queue_"+status, reason, now)
}

func clearNovelFetchOriginalFetchRetry(ctx context.Context, store novelfetchworkshop.Store, owner, bookID string) error {
	document, err := store.GetDocument(ctx, owner, bookID)
	if err != nil {
		return err
	}
	if document.Meta == nil {
		return nil
	}
	for _, key := range []string{"originalFetchRetryAttempts", "originalFetchNextRetryAt", "originalFetchLastError", "originalFetchFailureJobId"} {
		delete(document.Meta, key)
	}
	clearNovelFetchTaskRetry(document.Meta, novelFetchRetryStageOriginalFetch)
	return store.PutDocument(ctx, owner, document)
}

func novelFetchAutomationEligible(document novelfetchworkshop.Document, now time.Time) bool {
	meta := document.Meta
	if meta == nil {
		meta = map[string]any{}
	}
	status := strings.ToLower(firstText(meta["status"], meta["queueState"], meta["taskStatus"]))
	if meta["scheduledHold"] == true || status == "scheduled_waiting" {
		return false
	}
	if status == "stopped" || status == "cancelled" || status == "canceled" || status == "waiting_configuration" {
		return false
	}
	// 自动重试预算耗尽后必须由人工介入，不能因为 status=failed 又被扫描器
	// 立刻捡回队列，否则“停止自动重试”实际上没有任何效果。
	if meta["manualOnly"] == true || meta["manual_only"] == true || meta["sensitiveManualRetryRequired"] == true {
		return false
	}
	// 阶段重试表里的 manual_only 与 manualOnly 等价：模型确定性失败、AI
	// 服务持续不可用都要等人工处理，不能因为 status=failed 又被捡回队列。
	if novelFetchRetryScheduleHasManualHold(meta) {
		return false
	}
	// 退避未到期前不重复入队；不同阶段各自维护到期时间。
	if !novelFetchTaskRetryDue(meta, "", now) {
		return false
	}
	if nextRaw := firstText(meta["originalFetchNextRetryAt"], meta["original_fetch_next_retry_at"], meta["automationNextRetryAt"], meta["automation_next_retry_at"]); nextRaw != "" {
		if next, ok := novelFetchParseRetryTime(nextRaw); !ok || next.After(now) {
			return false
		}
	}
	if meta["rewriteValidationHold"] == true {
		return false
	}
	submitStatus := strings.ToLower(firstText(meta["siteSubmitStatus"], meta["site_submit_status"]))
	if submitStatus == "waiting_configuration" || submitStatus == "waiting_login" {
		return false
	}
	if status == "queued" || status == "running" || status == "processing" {
		return false
	}
	if status == "waiting_classification" {
		return novelFetchClassificationResolved(document)
	}
	// Raw text is persisted before sensitive-word AI runs. If that second
	// stage fails, the task must be picked up again without fetching upstream
	// a second time.
	if strings.EqualFold(firstText(meta["originalStatus"], meta["original_status"]), "waiting_sensitive_ai") &&
		(document.OriginalRawAvailable || strings.TrimSpace(document.OriginalRaw) != "" || document.BodyRefs["original_raw"].CharCount > 0) {
		return true
	}
	if submitStatus == "pending_upload" || submitStatus == "pending_confirmation" || submitStatus == "failed" || submitStatus == "error" {
		return true
	}
	// Older/manual batches can finish rewriting before automatic submission is
	// enabled. They have a completed process state but no submission state, so
	// recover them when automation is turned on instead of leaving them pending.
	if status == "completed" && submitStatus == "" &&
		strings.EqualFold(firstText(meta["originalStatus"], meta["original_status"]), "done") &&
		(strings.EqualFold(firstText(meta["aiStatus"], meta["ai_status"]), "ai_done") ||
			strings.EqualFold(firstText(meta["aiStatus"], meta["ai_status"]), "done") ||
			novelFetchDocumentHasGeneratedAI(document)) {
		return true
	}
	return status == "" || status == "input_ready" || status == "failed" || status == "waiting_retry"
}

func novelFetchAutomationTimeout(settings map[string]any) time.Duration {
	seconds := intValue(mapOrEmpty(settings["workflow"])["auto_queue_task_timeout_seconds"])
	if seconds <= 0 {
		// AI rewriting of long text can legitimately run for several minutes
		// without touching the document; a short default falsely re-queues a
		// live job and duplicates AI calls. Keep 30 minutes as the floor.
		seconds = 1800
	}
	if seconds < 300 {
		seconds = 300
	}
	if seconds > 24*60*60 {
		seconds = 24 * 60 * 60
	}
	return time.Duration(seconds) * time.Second
}

// recoverStaleNovelFetchTasks turns orphaned processing states into retryable
// work. A worker that disappears after marking a document processing must not
// leave that document excluded from automation forever.
func recoverStaleNovelFetchTasks(ctx context.Context, store novelfetchworkshop.Store, owner string, settings map[string]any, documents []novelfetchworkshop.Document, now time.Time, queues ...novelfetchworkshop.ProcessQueue) {
	cutoff := now.Add(-novelFetchAutomationTimeout(settings))
	staleBatches := map[string]bool{}
	var recoveryQueue novelfetchworkshop.ProcessStaleJobRecoveryQueue
	if len(queues) > 0 {
		recoveryQueue, _ = queues[0].(novelfetchworkshop.ProcessStaleJobRecoveryQueue)
	}
	for index, document := range documents {
		if ctx.Err() != nil {
			return
		}
		status := strings.ToLower(firstText(document.Meta["status"], document.Meta["queueState"], document.Meta["taskStatus"]))
		if status != "processing" && status != "running" && status != "ai_processing" && status != "queued" {
			continue
		}
		updated, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(document.UpdatedAt))
		if err != nil || updated.After(cutoff) {
			continue
		}
		// Candidates may omit every body and log. Only hydrate a stale book,
		// and refuse to overwrite a document that changed since the scan.
		latest, readErr := store.GetDocument(ctx, owner, document.BookID)
		if readErr != nil || latest.UpdatedAt != document.UpdatedAt {
			continue
		}
		document = latest
		if status == "queued" {
			// Age alone does not prove a queued job is orphaned. Never replace a
			// job which Redis still owns, or retry when Redis cannot be checked.
			jobID := firstText(document.Meta["currentProcessJobId"], document.Meta["current_process_job_id"])
			if jobID != "" {
				if len(queues) == 0 {
					continue
				}
				checker, ok := queues[0].(interface {
					ProcessJobExists(context.Context, string) (bool, error)
				})
				if !ok {
					continue
				}
				exists, checkErr := checker.ProcessJobExists(ctx, jobID)
				if checkErr != nil || exists {
					continue
				}
			}
			// jobID 为空说明没有任何 Redis 任务持有这本书（常见于早期版本或
			// 入队前中断），属于彻底的孤儿状态；继续跳过会让它永远显示“排队中”，
			// 因此和超时任务一样转入等待重试，交给自动扫描重新入队。
		}
		paidAIUnknown := status == "ai_processing" || strings.EqualFold(firstText(document.Meta["aiStatus"], document.Meta["ai_status"]), "ai_processing")
		if recoveryQueue != nil && status != "queued" && !paidAIUnknown {
			staleJobID := firstText(document.Meta["currentProcessJobId"], document.Meta["current_process_job_id"])
			if staleJobID == "" {
				batchID := firstText(document.Meta["currentBatchId"], document.Meta["current_batch_id"])
				if runs, runsErr := store.ListRuns(ctx, owner, batchID); runsErr == nil {
					for _, run := range runs {
						if run.Stage == "novel_fetch_process" && strings.EqualFold(strings.TrimSpace(run.Status), "running") {
							staleJobID = run.RunID
							break
						}
					}
				}
			}
			if staleJobID != "" {
				if _, recoverErr := recoveryQueue.RecoverStaleJob(ctx, staleJobID); recoverErr != nil {
					continue
				}
				if checker, ok := queues[0].(interface {
					ProcessJobExists(context.Context, string) (bool, error)
				}); ok {
					exists, checkErr := checker.ProcessJobExists(ctx, staleJobID)
					if checkErr != nil || exists {
						continue
					}
					// A worker may have saved the verdict and released the job
					// since this scan read the document.
					latest, readErr := store.GetDocument(ctx, owner, document.BookID)
					if readErr != nil || latest.UpdatedAt != document.UpdatedAt {
						continue
					}
				}
			}
		}
		if paidAIUnknown {
			// A missing worker is not evidence that a paid request failed. A
			// positive/unknown queue lookup must not be treated as an orphan.
			if id := firstText(document.Meta["currentProcessJobId"], document.Meta["current_process_job_id"]); id != "" {
				if len(queues) == 0 {
					continue
				}
				checker, ok := queues[0].(interface {
					ProcessJobExists(context.Context, string) (bool, error)
				})
				if !ok {
					continue
				}
				exists, err := checker.ProcessJobExists(ctx, id)
				if err != nil || exists {
					continue
				}
			}
		}
		if document.Meta == nil {
			document.Meta = map[string]any{}
		}
		message := "任务超过执行超时时间，已转入等待重试"
		document.Meta["status"] = "waiting_retry"
		document.Meta["queueState"] = "waiting_retry"
		document.Meta["error"] = message
		document.Meta["automationNextRetryAt"] = now.UTC().Format(time.RFC3339Nano)
		if paidAIUnknown {
			message = "AI执行结果未知，请先核对结果后手动重试"
			document.Meta["status"], document.Meta["queueState"] = "failed", "failed"
			document.Meta["manualOnly"] = true
			document.Meta["error"] = message
			delete(document.Meta, "automationNextRetryAt")
			scheduleNovelFetchTaskRetry(document.Meta, novelFetchRetryStagePipeline, fmt.Errorf("%s", message), 0, now)
		}
		if paidAIUnknown {
			appendNovelFetchDocumentLog(&document, "process_waiting_manual", message, now)
		} else {
			appendNovelFetchDocumentLog(&document, "process_recovered", "超时任务已转入等待重试", now)
			appendNovelFetchDocumentLog(&document, "process_waiting_retry", message, now)
		}
		if version, err := store.CompareAndSwapDocument(ctx, owner, document); err == nil {
			document.UpdatedAt = version
			documents[index] = document
			staleBatches[firstText(document.Meta["currentBatchId"], document.Meta["current_batch_id"])] = true
		}
	}
	if len(staleBatches) == 0 {
		return
	}
	runs, err := store.ListRuns(ctx, owner, "")
	if err != nil {
		return
	}
	for _, run := range runs {
		if !staleBatches[run.BookID] || strings.ToLower(strings.TrimSpace(run.Status)) != "running" {
			continue
		}
		updated, parseErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(run.UpdatedAt))
		if parseErr != nil || updated.After(cutoff) {
			continue
		}
		run.Status = "waiting_retry"
		run.ErrorMessage = "任务超过执行超时时间，已转入等待重试"
		run.FinishedAt = now.UTC().Format(time.RFC3339Nano)
		_ = store.PutRun(ctx, owner, run)
	}
}

// Older workers could persist an AI body and leave aiStatus at "未开始" or
// "ai_processing". The stored body is the authoritative signal for automatic
// submission; an empty version must never be treated as ready.
func novelFetchDocumentHasGeneratedAI(document novelfetchworkshop.Document) bool {
	for index := 1; index <= novelfetchworkshop.MaxAIVersions; index++ {
		versionID := fmt.Sprintf("ai%d", index)
		if value, ok := document.Versions[versionID]; ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return true
			}
		}
		if ref, ok := document.BodyRefs[versionID]; ok && ref.CharCount > 0 && strings.EqualFold(strings.TrimSpace(ref.State), "ready") {
			return true
		}
	}
	return false
}

// novelFetchAutomationConfigurationBlock identifies failures that cannot be
// corrected by waiting. Retrying these every few seconds hides the actionable
// problem and leaves the automatic queue permanently busy.
func novelFetchAutomationConfigurationBlock(document novelfetchworkshop.Document, submitErr error) string {
	messages := []string{firstText(submitErr)}
	for _, message := range mapOrEmpty(document.Meta["siteSubmitFailedErrors"]) {
		messages = append(messages, firstText(message))
	}
	for _, message := range messages {
		normalized := strings.ToLower(strings.TrimSpace(message))
		if normalized == "" {
			continue
		}
		if strings.Contains(normalized, "风格") || strings.Contains(normalized, "性别") ||
			strings.Contains(normalized, "正文复检未通过") ||
			strings.Contains(normalized, "未找到正文版本") || strings.Contains(normalized, "正文少于") ||
			strings.Contains(normalized, "未配置上传") || strings.Contains(normalized, "未登录") ||
			strings.Contains(normalized, "登录已失效") {
			return message
		}
	}
	return ""
}

func dispatchNovelFetchAutomationOwner(ctx context.Context, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, owner string, now time.Time) (*novelfetchworkshop.ProcessJob, error) {
	if queue == nil {
		return nil, nil
	}
	if reaper, ok := queue.(interface {
		ReapStaleProcessing(context.Context) (int, error)
	}); ok {
		if reaped, reapErr := reaper.ReapStaleProcessing(ctx); reapErr != nil {
			log.Printf("novel fetch automation: reap stale processing owner=%s: %v", owner, reapErr)
		} else if reaped > 0 {
			log.Printf("novel fetch automation: reaped %d stale processing jobs owner=%s", reaped, owner)
		}
	}
	settings, err := novelfetchworkshop.ReadWorkflowConfig(ctx, store, owner)
	if err != nil {
		return nil, err
	}
	automationEnabled := novelFetchAutomationEnabled(settings)
	// 自动队列只接管当前批次。旧批次里没跑完的书属于那一天的批次，
	// 不能在今天被无声拉起；用户切到对应日期手动重跑即可。
	currentBatchID := strings.TrimSpace(firstText(settings["novel_fetch_current_batch_id"], settings["novelFetchCurrentBatchId"]))
	documents, err := listNovelFetchAutomationCandidates(ctx, store, owner, currentBatchID)
	if err != nil {
		return nil, err
	}
	recoverStaleNovelFetchTasks(ctx, store, owner, settings, documents, now, queue)
	bookIDs := make([]string, 0, 50)
	batchID := ""
	platformID := "2"
	maxTxt := 4000
	var selectedJob novelfetchworkshop.ProcessJob
	groupKey := ""
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(bookIDs) >= 50 {
			break
		}
		if currentBatchID != "" && novelFetchDocumentBatchID(document) != currentBatchID {
			continue
		}
		cancelled, cancelErr := reconcileNovelFetchCancellation(ctx, store, queue, owner, document)
		if cancelErr != nil {
			return nil, cancelErr
		}
		if cancelled {
			continue
		}
		if held, holdErr := persistNovelFetchRetrySafetyHold(ctx, store, owner, document, now); holdErr != nil {
			return nil, holdErr
		} else if held {
			continue
		}
		waitingFetchRetry := strings.EqualFold(firstText(document.Meta["originalStatus"], document.Meta["original_status"]), "waiting_fetch_retry")
		// A fetch failure has already created an explicit retry deadline. It must
		// survive a paused global automation switch and a service restart; only
		// ordinary pending work is gated by the switch.
		if !automationEnabled && !waitingFetchRetry {
			continue
		}
		eligible := novelFetchAutomationEligible(document, now)
		if !eligible && strings.EqualFold(firstText(document.Meta["status"], document.Meta["queueState"]), "completed") &&
			firstText(document.Meta["siteSubmitStatus"], document.Meta["site_submit_status"]) == "" &&
			document.Meta["manualOnly"] != true && document.Meta["manual_only"] != true &&
			!novelFetchRetryScheduleHasManualHold(document.Meta) && novelFetchTaskRetryDue(document.Meta, "", now) {
			// Body text is stored separately from the lightweight document JSON.
			// Hydrate refs before deciding whether a stale legacy task is ready
			// for automatic submission.
			if refs, refsErr := store.ListBodyRefs(ctx, owner, document.BookID); refsErr == nil && len(refs) > 0 {
				if document.BodyRefs == nil {
					document.BodyRefs = map[string]novelfetchworkshop.BodyRef{}
				}
				for _, ref := range refs {
					document.BodyRefs[ref.VersionID] = ref
				}
				eligible = novelFetchAutomationEligible(document, now)
			}
		}
		if !eligible {
			continue
		}
		candidate := novelFetchRecoveryJob(document, settings, owner, true)
		candidateKey := novelFetchRecoveryGroup(candidate)
		if groupKey != "" && candidateKey != groupKey {
			continue
		}
		if groupKey == "" {
			selectedJob = candidate
			groupKey = candidateKey
			batchID = candidate.BatchID
			platformID = candidate.PlatformID
			maxTxt = candidate.MaxTxt
		}
		bookIDs = append(bookIDs, document.BookID)
	}
	if len(bookIDs) == 0 {
		return nil, nil
	}
	return enqueueNovelFetchGroupJob(ctx, store, queue, owner, selectedJob, bookIDs, batchID, platformID, maxTxt, now, settings)
}

// enqueueNovelFetchGroupJob 复用自动队列与手动入队的同一套去重和状态落库逻辑。
func enqueueNovelFetchGroupJob(ctx context.Context, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, owner string, selectedJob novelfetchworkshop.ProcessJob, bookIDs []string, batchID, platformID string, maxTxt int, now time.Time, settings map[string]any) (*novelfetchworkshop.ProcessJob, error) {
	stamp := now.UnixNano()
	if batchID == "" {
		batchID = fmt.Sprintf("auto-batch-%d", stamp)
	}
	job := novelfetchworkshop.ProcessJob{
		ID: fmt.Sprintf("queue_%d_%x", now.UnixMilli(), stamp&0xffffffff), Owner: owner, BatchID: batchID,
		BookIDs: bookIDs, PlatformID: platformID, MaxTxt: maxTxt, SensitiveAIEnabled: novelFetchSensitiveAIEnabled(settings), AutoSubmit: true,
		SelectedVersions: novelFetchAutomationVersions(settings), TextModelID: firstText(settings["text_model_id"], settings["textModelId"]),
	}
	job.ScheduleID = selectedJob.ScheduleID
	job.AutoSubmit = selectedJob.AutoSubmit
	job.SelectedVersions = selectedJob.SelectedVersions
	job.RewriteMethods = selectedJob.RewriteMethods
	job.TextModelID = selectedJob.TextModelID
	job.SensitiveAIEnabled = selectedJob.SensitiveAIEnabled
	// EnqueueUniqueBooks is all-or-nothing. Drop books owned by another live
	// job first, otherwise a single busy book blocks this whole automatic batch
	// on every sweep until its dedupe key expires.
	if conflictQueue, ok := queue.(novelfetchworkshop.ProcessBookConflictQueue); ok {
		conflicts, conflictErr := conflictQueue.ConflictingBookIDs(ctx, job)
		if conflictErr != nil {
			return nil, conflictErr
		}
		if len(conflicts) > 0 {
			filtered := make([]string, 0, len(bookIDs))
			for _, bookID := range bookIDs {
				if _, taken := conflicts[bookID]; taken {
					continue
				}
				filtered = append(filtered, bookID)
			}
			if len(filtered) == 0 {
				return nil, nil
			}
			bookIDs = filtered
			job.BookIDs = filtered
		}
	}
	if err := novelFetchSnapshotBeforeQueue(ctx, store, job); err != nil {
		return nil, err
	}
	var enqueueErr error
	if uniqueQueue, ok := queue.(novelfetchworkshop.ProcessBookDedupQueue); ok {
		var queued bool
		queued, _, enqueueErr = uniqueQueue.EnqueueUniqueBooks(ctx, job, 2*time.Hour)
		if enqueueErr == nil && !queued {
			return nil, nil
		}
	} else if uniqueQueue, ok := queue.(novelfetchworkshop.ProcessDedupQueue); ok {
		var queued bool
		queued, enqueueErr = uniqueQueue.EnqueueUnique(ctx, owner+":"+strings.Join(bookIDs, ","), job, 2*time.Hour)
		if enqueueErr == nil && !queued {
			return nil, nil
		}
	} else {
		enqueueErr = queue.Enqueue(ctx, job)
	}
	if enqueueErr != nil {
		log.Printf("novel fetch automation: enqueue failed owner=%s batch=%s books=%d: %v", owner, batchID, len(bookIDs), enqueueErr)
		markNovelFetchAutomationEnqueueRetry(ctx, store, owner, bookIDs, now, enqueueErr)
		return nil, enqueueErr
	}
	for _, bookID := range bookIDs {
		if err := markNovelFetchTaskQueued(ctx, store, owner, bookID, job.ID, batchID, now); err != nil {
			return nil, err
		}
	}
	if err := store.PutRun(ctx, owner, novelfetchworkshop.RunRecord{RunID: job.ID, BookID: batchID, Stage: "novel_fetch_process", Status: "queued", TextModelID: job.TextModelID, ModelID: "novel-fetch-workshop", ModelDisplayName: "小说获取全自动队列", StartedAt: now.UTC().Format(time.RFC3339Nano)}); err != nil {
		return nil, err
	}
	return &job, nil
}

// dispatchNovelFetchManualQueue 处理 /api/process/queue/start：只入队显式选中的
// 书籍（或 failed/all 解析结果），不会拉起整批自动队列。
func dispatchNovelFetchManualQueue(ctx context.Context, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, owner string, ids []string, now time.Time) ([]string, []string, error) {
	if queue == nil || len(ids) == 0 {
		return nil, ids, nil
	}
	settings, err := novelfetchworkshop.ReadWorkflowConfig(ctx, store, owner)
	if err != nil {
		return nil, nil, err
	}
	documents, err := store.ListDocuments(ctx, owner)
	if err != nil {
		return nil, nil, err
	}
	byID := make(map[string]novelfetchworkshop.Document, len(documents))
	for _, document := range documents {
		byID[document.BookID] = document
	}
	groups := map[string][]string{}
	templates := map[string]novelfetchworkshop.ProcessJob{}
	order := []string{}
	skipped := []string{}
	seen := map[string]bool{}
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		document, exists := byID[id]
		if !exists {
			skipped = append(skipped, id)
			continue
		}
		// 正文在独立存储里，先补齐 refs 才能判断任务是否真的可入队。
		if refs, refsErr := store.ListBodyRefs(ctx, owner, id); refsErr == nil && len(refs) > 0 {
			if document.BodyRefs == nil {
				document.BodyRefs = map[string]novelfetchworkshop.BodyRef{}
			}
			for _, ref := range refs {
				document.BodyRefs[ref.VersionID] = ref
			}
		}
		if !novelFetchAutomationEligible(document, now) {
			skipped = append(skipped, id)
			continue
		}
		candidate := novelFetchRecoveryJob(document, settings, owner, true)
		key := novelFetchRecoveryGroup(candidate)
		if _, exists := templates[key]; !exists {
			templates[key] = candidate
			order = append(order, key)
		}
		groups[key] = append(groups[key], id)
	}
	jobIDs := []string{}
	for _, key := range order {
		template := templates[key]
		job, enqueueErr := enqueueNovelFetchGroupJob(ctx, store, queue, owner, template, groups[key], template.BatchID, template.PlatformID, template.MaxTxt, now, settings)
		if enqueueErr != nil {
			return jobIDs, skipped, enqueueErr
		}
		if job != nil {
			jobIDs = append(jobIDs, job.ID)
		}
	}
	return jobIDs, skipped, nil
}

// novelFetchEnqueueMaxAttempts 是入队失败的自动重试上限。参考实现里重试
// 预算耗尽后会把任务标记为需要人工处理，而不是无限等待；这里保持同样的
// 语义，避免 Redis 长期不可用时任务永远停在“等待重试”。
const novelFetchEnqueueMaxAttempts = 20

// markNovelFetchAutomationEnqueueRetry 在入队失败时写入退避时间。Redis 持续
// 不可用时扫描器每分钟都会重试同一批书；没有退避既会放大数据库写入，也会
// 让“最后一次失败”永远停留在当前秒，无法判断是否已经恢复。
func markNovelFetchAutomationEnqueueRetry(ctx context.Context, store novelfetchworkshop.Store, owner string, bookIDs []string, now time.Time, enqueueErr error) {
	for _, bookID := range bookIDs {
		document, err := store.GetDocument(ctx, owner, bookID)
		if err != nil {
			continue
		}
		if document.Meta == nil {
			document.Meta = map[string]any{}
		}
		attempts := intValue(document.Meta["automationEnqueueAttempts"]) + 1
		document.Meta["automationEnqueueAttempts"] = attempts
		if attempts >= novelFetchEnqueueMaxAttempts {
			document.Meta["status"] = "failed"
			document.Meta["queueState"] = "failed"
			document.Meta["manualOnly"] = true
			document.Meta["error"] = fmt.Sprintf("任务入队连续失败 %d 次，已停止自动重试：%s", attempts, firstText(enqueueErr))
			delete(document.Meta, "automationNextRetryAt")
			appendNovelFetchDocumentLog(&document, "queue_failed",
				fmt.Sprintf("入队连续失败 %d 次，已转人工处理", attempts), now)
			_ = store.PutDocument(ctx, owner, document)
			continue
		}
		delay := 30 * time.Second
		for step := 1; step < attempts && delay < 15*time.Minute; step++ {
			delay *= 2
		}
		if delay > 15*time.Minute {
			delay = 15 * time.Minute
		}
		document.Meta["status"] = "waiting_retry"
		document.Meta["queueState"] = "waiting_retry"
		document.Meta["error"] = firstText(enqueueErr)
		document.Meta["automationNextRetryAt"] = now.Add(delay).UTC().Format(time.RFC3339Nano)
		appendNovelFetchDocumentLog(&document, "process_waiting_retry", "入队失败："+firstText(enqueueErr), now)
		_ = store.PutDocument(ctx, owner, document)
	}
}

func markNovelFetchAutomationSubmitResult(ctx context.Context, store novelfetchworkshop.Store, owner string, bookIDs []string, submitErr error) {
	now := time.Now().UTC()
	for _, bookID := range bookIDs {
		document, err := store.GetDocument(ctx, owner, bookID)
		if err != nil {
			continue
		}
		if document.Meta == nil {
			document.Meta = map[string]any{}
		}
		status := strings.ToLower(firstText(document.Meta["siteSubmitStatus"], document.Meta["site_submit_status"]))
		if submitErr == nil && status == "submitted" {
			clearNovelFetchTaskRetry(document.Meta, novelFetchRetryStageUpload)
			delete(document.Meta, "automationSubmitAttempts")
			delete(document.Meta, "automationNextRetryAt")
			delete(document.Meta, "automationSubmitError")
		} else if blockedStatus := workshopSubmitBlockedStatus(submitErr); blockedStatus != "" {
			document.Meta["siteSubmitStatus"] = blockedStatus
			document.Meta["status"], document.Meta["queueState"] = blockedStatus, blockedStatus
			document.Meta["automationSubmitError"] = submitErr.Error()
			delete(document.Meta, "automationSubmitAttempts")
			delete(document.Meta, "automationNextRetryAt")
		} else if blocked := novelFetchAutomationConfigurationBlock(document, submitErr); blocked != "" {
			document.Meta["siteSubmitStatus"] = "waiting_configuration"
			document.Meta["status"] = "waiting_configuration"
			document.Meta["queueState"] = "waiting_configuration"
			document.Meta["automationSubmitError"] = blocked
			delete(document.Meta, "automationSubmitAttempts")
			delete(document.Meta, "automationNextRetryAt")
		} else {
			// Preserve a legacy counter rather than resetting its budget when the
			// staged schedule is first introduced or another job is created.
			schedule := novelFetchRetrySchedule(document.Meta)
			if schedule[novelFetchRetryStageUpload] == nil && intValue(document.Meta["automationSubmitAttempts"]) > 0 {
				schedule[novelFetchRetryStageUpload] = map[string]any{"attempt": intValue(document.Meta["automationSubmitAttempts"])}
				document.Meta["retry_schedule"] = schedule
			}
			failure := submitErr
			if failure == nil {
				failure = fmt.Errorf("上传尚未确认")
			}
			// pending_confirmation is handled by the connector's readback path;
			// never downgrade its status to a fresh upload.
			state := scheduleNovelFetchTaskRetry(document.Meta, novelFetchRetryStageUpload, failure, novelFetchAIRetryDelayHintSeconds(failure), now)
			if !strings.EqualFold(firstText(document.Meta["siteSubmitStatus"]), "pending_confirmation") {
				document.Meta["siteSubmitStatus"] = "pending_upload"
			}
			document.Meta["automationSubmitAttempts"] = intValue(state["attempt"])
			if novelFetchRetryStateIsManual(state) {
				document.Meta["manualOnly"] = true
				document.Meta["status"], document.Meta["queueState"] = "failed", "failed"
				delete(document.Meta, "automationNextRetryAt")
			} else {
				document.Meta["automationNextRetryAt"] = firstText(state["next_retry_at"])
			}
			document.Meta["automationSubmitError"] = firstText(submitErr, document.Meta["siteSubmitFailedErrors"], "上传尚未确认")
		}
		_ = store.PutDocument(ctx, owner, document)
	}
}

// novelFetchQueueScope 表示队列统计的批次范围。实时状态必须按当前批次或
// 选定日期统计：未完成的旧批次如果混进来，用户看到的“排队 80 个”就会跟
// 当天实际开始的书单完全对不上。
type novelFetchQueueScope struct {
	BatchID string
	Date    string
}

func (scope novelFetchQueueScope) empty() bool {
	return strings.TrimSpace(scope.BatchID) == "" && strings.TrimSpace(scope.Date) == ""
}

var novelFetchBeijingZone = time.FixedZone("CST", 8*60*60)

func novelFetchLocalDateKey(at time.Time) string {
	return at.In(novelFetchBeijingZone).Format("2006-01-02")
}

// novelFetchBatchTime 从批次 ID 还原创建时间。本服务的批次 ID 形如
// batch-<UnixNano>、auto-batch-<UnixNano>；定时批次使用哈希后缀，无法
// 还原时间，只能回退到文档里记录的批次时间。
func novelFetchBatchTime(batchID string) time.Time {
	text := strings.TrimSpace(batchID)
	for _, prefix := range []string{"auto-batch-", "batch-"} {
		if !strings.HasPrefix(text, prefix) {
			continue
		}
		raw := strings.TrimPrefix(text, prefix)
		if idx := strings.Index(raw, "-"); idx > 0 {
			raw = raw[:idx]
		}
		if ns, err := strconv.ParseInt(raw, 10, 64); err == nil && ns > 0 {
			return time.Unix(0, ns)
		}
	}
	return time.Time{}
}

func novelFetchDocumentBatchID(document novelfetchworkshop.Document) string {
	if document.Meta == nil {
		return ""
	}
	return strings.TrimSpace(firstText(document.Meta["currentBatchId"], document.Meta["current_batch_id"]))
}

func novelFetchDocumentBatchTime(document novelfetchworkshop.Document) time.Time {
	if at := novelFetchBatchTime(novelFetchDocumentBatchID(document)); !at.IsZero() {
		return at
	}
	if document.Meta != nil {
		for _, key := range []string{"batchCreatedAt", "currentBatchAt", "createdAt"} {
			raw := strings.TrimSpace(firstText(document.Meta[key]))
			if raw == "" {
				continue
			}
			for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
				if parsed, err := time.Parse(layout, raw); err == nil {
					return parsed
				}
			}
		}
	}
	if raw := strings.TrimSpace(document.UpdatedAt); raw != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, raw); err == nil {
				return parsed
			}
		}
	}
	return time.Time{}
}

func novelFetchDocumentInScope(document novelfetchworkshop.Document, scope novelFetchQueueScope) bool {
	if batchID := strings.TrimSpace(scope.BatchID); batchID != "" {
		return novelFetchDocumentBatchID(document) == batchID
	}
	if date := strings.TrimSpace(scope.Date); date != "" {
		at := novelFetchDocumentBatchTime(document)
		return !at.IsZero() && novelFetchLocalDateKey(at) == date
	}
	return true
}

func novelFetchScopedJobMatches(job map[string]any, scope novelFetchQueueScope) bool {
	if batchID := strings.TrimSpace(scope.BatchID); batchID != "" {
		return strings.TrimSpace(firstText(job["batch_id"], job["batchId"])) == batchID
	}
	if date := strings.TrimSpace(scope.Date); date != "" {
		raw := strings.TrimSpace(firstText(job["started_at"], job["startedAt"], job["updated_at"], job["updatedAt"]))
		if raw == "" {
			return false
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, raw); err == nil {
				return novelFetchLocalDateKey(parsed) == date
			}
		}
	}
	return true
}

func novelFetchScopedRunMatches(run novelfetchworkshop.RunRecord, scope novelFetchQueueScope) bool {
	if batchID := strings.TrimSpace(scope.BatchID); batchID != "" {
		return strings.TrimSpace(run.BookID) == batchID
	}
	if date := strings.TrimSpace(scope.Date); date != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if parsed, err := time.Parse(layout, strings.TrimSpace(run.StartedAt)); err == nil {
				return novelFetchLocalDateKey(parsed) == date
			}
		}
	}
	return true
}

func novelFetchAutomationStatus(ctx context.Context, store novelfetchworkshop.Store, registry *workshopProcessRegistry, owner string, queues ...novelfetchworkshop.ProcessQueue) (map[string]any, error) {
	return novelFetchAutomationStatusScoped(ctx, store, registry, owner, novelFetchQueueScope{}, queues...)
}

func novelFetchAutomationStatusScoped(ctx context.Context, store novelfetchworkshop.Store, registry *workshopProcessRegistry, owner string, scope novelFetchQueueScope, queues ...novelfetchworkshop.ProcessQueue) (map[string]any, error) {
	var settings map[string]any
	var documents []novelfetchworkshop.Document
	var err error
	if reader, ok := store.(interface {
		AutomationStatusInputs(context.Context, string) (map[string]any, []novelfetchworkshop.Document, error)
	}); ok {
		settings, documents, err = reader.AutomationStatusInputs(ctx, owner)
	} else {
		settings, err = novelfetchworkshop.ReadWorkflowConfig(ctx, store, owner)
		if err == nil {
			documents, err = store.ListDocuments(ctx, owner)
		}
	}
	if err != nil {
		return nil, err
	}
	currentBatchID := strings.TrimSpace(firstText(settings["novel_fetch_current_batch_id"], settings["novelFetchCurrentBatchId"]))
	scope.BatchID = strings.TrimSpace(scope.BatchID)
	scope.Date = strings.TrimSpace(scope.Date)
	// 默认只统计当前批次；显式传日期时统计该日期创建的批次。
	if scope.empty() && currentBatchID != "" {
		scope.BatchID = currentBatchID
	}
	counts := map[string]int{"running": 0, "queued": 0, "waiting_retry": 0, "waiting_configuration": 0, "waiting_login": 0, "failed": 0, "stopped": 0}
	matched := 0
	for _, document := range documents {
		if !novelFetchDocumentInScope(document, scope) {
			continue
		}
		matched++
		task := legacyNovelFetchTask(document)
		status := strings.ToLower(firstText(document.Meta["status"], document.Meta["queueState"], document.Meta["taskStatus"], task["ai_status"], document.Meta["aiStatus"]))
		switch {
		case status == "running" || status == "processing":
			counts["running"]++
		case status == "queued":
			counts["queued"]++
		case status == "waiting_retry":
			counts["waiting_retry"]++
		case status == "waiting_configuration":
			counts["waiting_configuration"]++
		case status == "waiting_login":
			counts["waiting_login"]++
		case status == "failed" || status == "error":
			counts["failed"]++
		case status == "stopped" || status == "cancelled":
			counts["stopped"]++
		}
	}
	latest := map[string]any{}
	if !scope.empty() {
		for _, job := range registry.latest(owner, 50) {
			if novelFetchScopedJobMatches(job, scope) {
				latest = job
				break
			}
		}
		if len(latest) == 0 {
			if runs, runErr := store.ListRuns(ctx, owner, ""); runErr == nil {
				for _, run := range runs {
					if run.Stage == "novel_fetch_process" && novelFetchScopedRunMatches(run, scope) {
						latest = persistedWorkshopRun(run)
						break
					}
				}
			}
		}
	} else if jobs := registry.latest(owner, 1); len(jobs) > 0 {
		latest = jobs[0]
	} else if runs, runErr := store.ListRuns(ctx, owner, ""); runErr == nil {
		for _, run := range runs {
			if run.Stage == "novel_fetch_process" {
				latest = persistedWorkshopRun(run)
				break
			}
		}
	}
	if len(queues) > 0 {
		latest = verifiedWorkshopJobView(ctx, store, queues[0], owner, latest)
	}
	activeJobID := ""
	status := strings.ToLower(firstText(latest["status"], latest["queue_state"]))
	if status == "queued" || status == "running" || status == "waiting_retry" {
		activeJobID = firstText(latest["id"])
	}
	return map[string]any{
		"ok": true, "enabled": novelFetchAutomationEnabled(settings), "auto_queue_enabled": novelFetchAutomationEnabled(settings),
		"active_job_id": activeJobID, "counts": counts, "processing_count": counts["running"],
		"pending_count": counts["queued"] + counts["waiting_retry"], "latest_job": novelFetchJobStatusSummary(latest),
		"scope":      map[string]any{"batch_id": scope.BatchID, "date": scope.Date, "current_batch_id": currentBatchID, "matched_documents": matched},
		"updated_at": time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}
