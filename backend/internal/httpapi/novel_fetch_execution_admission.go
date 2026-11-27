package httpapi

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
)

func withWorkshopBookAdmission(ctx context.Context, queue novelfetchworkshop.ProcessQueue, owner, id string, fn func(context.Context) error) error {
	admission, ok := queue.(novelfetchworkshop.ProcessBookAdmissionQueue)
	if !ok {
		return errors.New("任务队列无法核验入队状态，请稍后重试")
	}
	return admission.WithBookAdmission(ctx, owner, id, fn)
}

// Queue evidence is authoritative; an unavailable check is not an empty queue.
func workshopActiveBookJob(ctx context.Context, queue novelfetchworkshop.ProcessQueue, owner string, doc novelfetchworkshop.Document, job novelfetchworkshop.ProcessJob) (string, error) {
	conflicts, ok := queue.(novelfetchworkshop.ProcessBookConflictQueue)
	if !ok {
		return "", errors.New("无法核验实际队列，请稍后重试")
	}
	active, err := conflicts.ConflictingBookIDs(ctx, job)
	if err != nil {
		return "", fmt.Errorf("无法核验实际队列: %w", err)
	}
	if id := active[doc.BookID]; id != "" {
		return id, nil
	}
	if current := firstText(doc.Meta["currentProcessJobId"], doc.Meta["current_process_job_id"]); current != "" {
		if checker, ok := queue.(interface {
			ProcessJobExists(context.Context, string) (bool, error)
		}); ok {
			exists, e := checker.ProcessJobExists(ctx, current)
			if e != nil {
				return "", fmt.Errorf("无法核验实际队列: %w", e)
			}
			if exists {
				return current, nil
			}
		}
	}
	return "", nil
}

type workshopSingleAdmission struct {
	queued   bool
	existing string
	document novelfetchworkshop.Document
}

// The caller holds the same short admission used by stop. Document writes use
// CAS so an update from outside that lock also wins over an earlier retry read.
func stageWorkshopSingleJob(ctx context.Context, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, before novelfetchworkshop.Document, job novelfetchworkshop.ProcessJob, explicitRetry, deferQueueState bool) (workshopSingleAdmission, error) {
	out := workshopSingleAdmission{document: before}
	unique, ok := queue.(novelfetchworkshop.ProcessBookDedupQueue)
	if !ok {
		return out, errors.New("任务队列不支持防重复入队")
	}
	if existing, err := workshopActiveBookJob(ctx, queue, job.Owner, before, job); err != nil {
		return out, err
	} else if existing != "" {
		out.existing = existing
		return out, nil
	}
	cancellation, ok := queue.(novelfetchworkshop.ProcessCancellationQueue)
	if !ok {
		return out, errors.New("任务队列无法核验停止状态")
	}
	wasCancelled, err := cancellation.IsBookCancelled(ctx, job.Owner, before.BookID)
	if err != nil {
		return out, err
	}
	if !explicitRetry && (wasCancelled || novelFetchDocumentStopped(before)) {
		return out, novelfetchworkshop.ErrBookCancelled
	}
	doc, err := store.GetDocument(ctx, job.Owner, before.BookID)
	if err != nil {
		return out, err
	}
	if doc.UpdatedAt != before.UpdatedAt {
		return out, novelfetchworkshop.ErrDocumentChanged
	}
	if doc.Meta == nil {
		doc.Meta = map[string]any{}
	}
	if explicitRetry {
		resetNovelFetchAutomationRetryState(&doc)
	}
	if !deferQueueState {
		doc.Meta["status"], doc.Meta["queueState"], doc.Meta["currentProcessJobId"] = "queued", "queued", job.ID
	}
	doc.Meta["processSnapshot"] = map[string]any{"batch_id": job.BatchID, "schedule_id": job.ScheduleID, "platform_id": job.PlatformID, "max_txt": job.MaxTxt, "selected_versions": copyNovelFetchSelectedVersions(job.SelectedVersions), "rewrite_methods": job.RewriteMethods, "text_model_id": job.TextModelID, "sensitive_ai_enabled": job.SensitiveAIEnabled, "auto_submit": job.AutoSubmit, "generate_only": job.GenerateOnly}
	doc.Meta["nextSelectedVersions"] = copyNovelFetchSelectedVersions(job.SelectedVersions)
	delete(doc.Meta, "error")
	appendNovelFetchDocumentLog(&doc, "queue_queued", "任务已进入后台队列", time.Now().UTC())
	written, err := store.CompareAndSwapDocument(ctx, job.Owner, doc)
	if err != nil {
		return out, err
	}
	doc.UpdatedAt = written
	rollback := func() {
		original := before
		original.UpdatedAt = written
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		_, _ = store.CompareAndSwapDocument(rollbackCtx, job.Owner, original)
	}
	if wasCancelled && explicitRetry {
		if err = cancellation.ClearCancelledBooks(ctx, job.Owner, []string{before.BookID}); err != nil {
			rollback()
			return out, err
		}
	}
	started := time.Now().UTC().Format(time.RFC3339Nano)
	if err = store.PutRun(ctx, job.Owner, novelfetchworkshop.RunRecord{RunID: job.ID, BookID: job.BatchID, Stage: "novel_fetch_process", Status: "queued", TextModelID: job.TextModelID, ModelID: "novel-fetch-workshop", ModelDisplayName: "批量测书后台处理", StartedAt: started}); err != nil {
		rollback()
		return out, err
	}
	queued, existing, err := unique.EnqueueUniqueBooks(ctx, job, 24*time.Hour)
	if err != nil {
		rollback()
		_ = updateWorkshopRun(store, job, "failed", "任务入队失败", started, true)
		return out, err
	}
	if !queued {
		rollback()
		_ = updateWorkshopRun(store, job, "duplicate_skipped", "同一书籍已有后台任务", started, true)
		out.existing = existing
		return out, nil
	}
	out.queued = true
	out.document = doc
	return out, nil
}

func queueWorkshopGenerate(w http.ResponseWriter, req *http.Request, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, count int) {
	owner, _ := BridgeIdentityFromContext(req.Context())
	id := strings.TrimSpace(req.PathValue("bookId"))
	var out workshopSingleAdmission
	var job novelfetchworkshop.ProcessJob
	err := withWorkshopBookAdmission(req.Context(), queue, owner.Username, id, func(ctx context.Context) error {
		doc, e := store.GetDocument(ctx, owner.Username, id)
		if e != nil {
			return e
		}
		cfg, e := novelfetchworkshop.ReadWorkflowConfig(ctx, store, owner.Username)
		if e != nil {
			return e
		}
		targets := []string{}
		for i := 1; i <= workshopAIRequestedCount(doc, cfg, count); i++ {
			targets = append(targets, fmt.Sprintf("ai%d", i))
		}
		versions, e := missingWorkshopAIBodies(ctx, store, owner.Username, doc, targets)
		if e != nil {
			return e
		}
		out.document = doc
		if len(versions) == 0 {
			return nil
		}
		job = novelFetchRecoveryJob(doc, cfg, owner.Username, false)
		job.ID = fmt.Sprintf("generate_%d", time.Now().UnixNano())
		job.BookIDs = []string{id}
		job.GenerateOnly = true
		job.AutoSubmit = false
		job.ScheduleID = ""
		job.SelectedVersions = versions
		if job.BatchID == "" {
			job.BatchID = "generate-batch"
		}
		out, e = stageWorkshopSingleJob(ctx, store, queue, doc, job, false, false)
		return e
	})
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, novelfetchworkshop.ErrNotFound) {
			status = http.StatusNotFound
		} else if manualImportConflict(err) {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if out.existing != "" {
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "job_id": out.existing, "error": "该书已有后台任务，请等待完成"})
		return
	}
	if !out.queued {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "queued": false, "task": out.document, "message": "所需 AI 版本已齐，无需重复生成"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "queued": true, "job_id": job.ID, "task": out.document, "message": "已进入后台生成队列"})
}

// Claim's normal blocking empty timeout needs no second wait. Transport errors
// receive bounded jittered backoff, cancelable without tying up another job.
func workshopClaimBackoff(failures int) time.Duration {
	delay := 100 * time.Millisecond
	for i := 1; i < failures && delay < 30*time.Second; i++ {
		delay *= 2
	}
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	return delay/2 + time.Duration(rand.Int64N(int64(delay/2)+1))
}
func waitWorkshopClaimBackoff(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func stopWorkshopBook(ctx context.Context, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, owner, id string, skipCompleted bool) (bool, error) {
	updated := false
	stop := func(locked context.Context) error {
		for attempts := 0; attempts < 4; attempts++ {
			doc, err := store.GetDocument(locked, owner, id)
			if err != nil {
				return err
			}
			status := firstText(doc.Meta["status"], doc.Meta["queueState"])
			if skipCompleted && (status == "completed" || status == "done" || status == "success") {
				return nil
			}
			if queue != nil {
				cancel, ok := queue.(novelfetchworkshop.ProcessCancellationQueue)
				if !ok {
					return errors.New("任务队列不支持停止")
				}
				if err := cancel.CancelBooks(locked, owner, []string{id}); err != nil {
					return err
				}
			}
			if doc.Meta == nil {
				doc.Meta = map[string]any{}
			}
			doc.Meta["status"], doc.Meta["queueState"] = "stopped", "stopped"
			appendNovelFetchDocumentLog(&doc, "stopped", "任务已停止", time.Now().UTC())
			if _, err = store.CompareAndSwapDocument(locked, owner, doc); !errors.Is(err, novelfetchworkshop.ErrDocumentChanged) {
				updated = err == nil
				return err
			}
		}
		return novelfetchworkshop.ErrDocumentChanged
	}
	if queue == nil {
		err := stop(ctx)
		return updated, err
	}
	err := withWorkshopBookAdmission(ctx, queue, owner, id, stop)
	return updated, err
}
func cancelWorkshopBookMarkers(ctx context.Context, queue novelfetchworkshop.ProcessQueue, owner string, ids []string) error {
	for _, id := range uniqueNovelFetchTaskIDs(ids) {
		if err := withWorkshopBookAdmission(ctx, queue, owner, id, func(locked context.Context) error {
			c, ok := queue.(novelfetchworkshop.ProcessCancellationQueue)
			if !ok {
				return errors.New("任务队列不支持停止")
			}
			return c.CancelBooks(locked, owner, []string{id})
		}); err != nil {
			return err
		}
	}
	return nil
}

func missingWorkshopAIBodies(ctx context.Context, store novelfetchworkshop.Store, owner string, doc novelfetchworkshop.Document, versions []string) ([]string, error) {
	missing := []string{}
	for _, version := range selectedNovelFetchAIVersions(versions) {
		ready, err := novelfetchworkshop.AIRewriteVersionReady(ctx, store, owner, doc, version)
		if err != nil {
			return nil, err
		}
		if !ready {
			missing = append(missing, version)
		}
	}
	return missing, nil
}

// A short metadata write guard shares stop's admission; provider calls execute
// outside the lock. Returned bodies may be retained, but cannot revive a stop.
type workshopBookExecutionStore struct {
	novelfetchworkshop.Store
	queue         novelfetchworkshop.ProcessQueue
	owner, bookID string
}

func (s workshopBookExecutionStore) withWrite(ctx context.Context, fn func(context.Context) error) error {
	check := func(locked context.Context) error {
		if cancellation, ok := s.queue.(novelfetchworkshop.ProcessCancellationQueue); ok {
			cancelled, err := cancellation.IsBookCancelled(locked, s.owner, s.bookID)
			if err != nil {
				return err
			}
			if cancelled {
				return novelfetchworkshop.ErrBookCancelled
			}
		}
		doc, err := s.Store.GetDocument(locked, s.owner, s.bookID)
		if err != nil {
			return err
		}
		if novelFetchDocumentStopped(doc) {
			return novelfetchworkshop.ErrBookCancelled
		}
		return fn(locked)
	}
	if _, ok := s.queue.(novelfetchworkshop.ProcessBookAdmissionQueue); ok {
		return withWorkshopBookAdmission(ctx, s.queue, s.owner, s.bookID, check)
	}
	return check(ctx)
}
func (s workshopBookExecutionStore) PutDocument(ctx context.Context, owner string, doc novelfetchworkshop.Document) error {
	return s.withWrite(ctx, func(locked context.Context) error { return s.Store.PutDocument(locked, owner, doc) })
}
func (s workshopBookExecutionStore) CompareAndSwapDocument(ctx context.Context, owner string, doc novelfetchworkshop.Document) (string, error) {
	var version string
	err := s.withWrite(ctx, func(locked context.Context) error {
		var err error
		version, err = s.Store.CompareAndSwapDocument(locked, owner, doc)
		return err
	})
	return version, err
}
func (s workshopBookExecutionStore) PutBody(ctx context.Context, owner string, body novelfetchworkshop.BodyRecord) (novelfetchworkshop.BodyRef, error) {
	if cancellation, ok := s.queue.(novelfetchworkshop.ProcessCancellationQueue); ok {
		cancelled, err := cancellation.IsBookCancelled(ctx, s.owner, s.bookID)
		if err != nil {
			return novelfetchworkshop.BodyRef{}, err
		}
		if cancelled {
			return novelfetchworkshop.BodyRef{}, novelfetchworkshop.ErrBookCancelled
		}
	}
	return s.Store.PutBody(ctx, owner, body)
}
func reserveWorkshopAI(ctx context.Context, store novelfetchworkshop.Store, job novelfetchworkshop.ProcessJob, bookID string) error {
	doc, err := store.GetDocument(ctx, job.Owner, bookID)
	if err != nil {
		return err
	}
	if doc.Meta["rewritePersistenceHold"] == true || doc.Meta["manualOnly"] == true || novelFetchRetryScheduleHasManualHold(doc.Meta) {
		return errors.New("AI 执行结果未知或已暂停，请核对后手动重试")
	}
	switch strings.ToLower(firstText(doc.Meta["aiStatus"], doc.Meta["ai_status"])) {
	case "ai_processing", "processing", "generating", "running":
		return errors.New("AI 上次执行结果未知，请核对已保存版本后手动重试")
	}
	if doc.Meta == nil {
		doc.Meta = map[string]any{}
	}
	doc.Meta["aiStatus"] = "ai_processing"
	doc.Meta["aiExecutionJobId"] = job.ID
	_, err = store.CompareAndSwapDocument(ctx, job.Owner, doc)
	return err
}
func clearWorkshopSuccessfulRetryStages(ctx context.Context, store novelfetchworkshop.Store, owner, bookID string, stages ...string) error {
	doc, err := store.GetDocument(ctx, owner, bookID)
	if err != nil {
		return err
	}
	if len(novelFetchRetrySchedule(doc.Meta)) == 0 {
		return nil
	}
	for _, stage := range stages {
		clearNovelFetchTaskRetry(doc.Meta, stage)
	}
	return store.PutDocument(ctx, owner, doc)
}

const workshopStageSaveCheckpointPrefix = "stage_save_checkpoint:"

type workshopStageSaveFailure struct {
	Stage     string                     `json:"stage"`
	Message   string                     `json:"message"`
	Manual    bool                       `json:"manual"`
	Original  *novelFetchOriginalFailure `json:"original,omitempty"`
	JobID     string                     `json:"job_id,omitempty"`
	FailureID string                     `json:"failure_id,omitempty"`
}

func saveWorkshopStageFailure(ctx context.Context, store novelfetchworkshop.Store, owner, bookID string, failure workshopStageSaveFailure) error {
	if failure.Original != nil {
		_, err := saveNovelFetchOriginalFailure(ctx, store, owner, bookID, failure.JobID, *failure.Original)
		return err
	}
	doc, err := store.GetDocument(ctx, owner, bookID)
	if err != nil {
		return err
	}
	if doc.Meta == nil {
		doc.Meta = map[string]any{}
	}
	previous := novelFetchRetrySchedule(doc.Meta)[failure.Stage]
	if failure.FailureID != "" && firstText(previous["failure_id"]) == failure.FailureID {
		return nil
	}
	cause := errors.New(failure.Message)
	now := time.Now().UTC()
	state := scheduleNovelFetchTaskRetry(doc.Meta, failure.Stage, cause, novelFetchAIRetryDelayHintSeconds(cause), now)
	state["failure_id"] = failure.FailureID
	schedule := novelFetchRetrySchedule(doc.Meta)
	schedule[failure.Stage] = state
	doc.Meta["retry_schedule"] = schedule
	manual := failure.Manual || novelFetchRetryStateIsManual(state)
	if manual {
		doc.Meta["status"], doc.Meta["queueState"] = "failed", "failed"
	} else {
		doc.Meta["status"], doc.Meta["queueState"] = "waiting_retry", "waiting_retry"
	}
	if failure.Manual {
		doc.Meta["manualOnly"] = true
		doc.Meta["manualAiRetryRequired"] = true
	}
	doc.Meta["error"] = failure.Message
	refreshNovelFetchTaskRetrySummary(doc.Meta)
	refreshNovelFetchAIManualRetryHold(doc.Meta)
	appendNovelFetchDocumentLog(&doc, "process_"+firstText(doc.Meta["status"]), failure.Message, now)
	_, err = store.CompareAndSwapDocument(ctx, owner, doc)
	return err
}

var errWorkshopRetryDeferred = errors.New("尚未到重试时间，等待到期后继续")
var errWorkshopRetryHeld = errors.New("任务已暂停，核对后手动重试")

func checkWorkshopRetryBudget(ctx context.Context, store novelfetchworkshop.Store, owner string, doc novelfetchworkshop.Document, at ...time.Time) error {
	now := time.Now().UTC()
	if len(at) > 0 && !at[0].IsZero() {
		now = at[0].UTC()
	}
	schedule := novelFetchRetrySchedule(doc.Meta)
	changed := false
	for stage, state := range schedule {
		if !novelFetchRetryStateIsManual(state) && novelFetchRetryStateExhausted(stage, state, now) {
			state["manual_only"] = true
			state["next_retry_at"] = ""
			state["updated_at"] = now.Format(time.RFC3339Nano)
			schedule[stage] = state
			changed = true
		}
	}
	if changed {
		doc.Meta["retry_schedule"] = schedule
		doc.Meta["status"], doc.Meta["queueState"] = "failed", "failed"
		doc.Meta["error"] = "本轮重试次数或24小时期限已用尽，请核对后手动重试"
		refreshNovelFetchTaskRetrySummary(doc.Meta)
		refreshNovelFetchAIManualRetryHold(doc.Meta)
		if _, err := store.CompareAndSwapDocument(ctx, owner, doc); err != nil {
			return err
		}
		return errWorkshopRetryHeld
	}
	if !novelFetchTaskRetryDue(doc.Meta, "", now) {
		return errWorkshopRetryDeferred
	}
	return nil
}

func (s workshopBookExecutionStore) GetWorkflowConfig(ctx context.Context, owner string) (map[string]any, error) {
	return novelfetchworkshop.ReadWorkflowConfig(ctx, s.Store, owner)
}

func (s workshopBookExecutionStore) PutWorkflowConfig(ctx context.Context, owner string, cfg map[string]any) error {
	return novelfetchworkshop.WriteWorkflowConfig(ctx, s.Store, owner, cfg)
}
