package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
)

func admissionTestStore(t *testing.T) *novelfetchworkshop.MemoryStore {
	t.Helper()
	s := novelfetchworkshop.NewMemoryStore()
	ctx := context.Background()
	if e := s.PutConfig(ctx, "alice", map[string]any{"appConfig": map[string]any{"rewrite": map[string]any{"default_template_id": "plain", "process_line_count": 1}}, "knowledgeLibraries": map[string]any{"rewrite_templates": map[string]any{"profiles": []any{map[string]any{"id": "plain", "prompt": "保持原文", "response_mode": "plain_text"}}}}}); e != nil {
		t.Fatal(e)
	}
	if e := s.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Original: "首行原文", Gender: "女频", Meta: map[string]any{"style": "现代甜文", "status": "failed", "originalStatus": "done", "aiStatus": "failed", "aiCount": 1}}); e != nil {
		t.Fatal(e)
	}
	return s
}
func admissionGenerate(mux http.Handler) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "/api/novel-fetch-workshop/tasks/123/generate-ai", strings.NewReader(`{}`))
	r = r.WithContext(context.WithValue(r.Context(), bridgeIdentityKey{}, BridgeIdentity{Username: "alice"}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

type admissionRecordingQueue struct {
	*novelfetchworkshop.MemoryQueue
	jobs chan novelfetchworkshop.ProcessJob
}

func (q *admissionRecordingQueue) EnqueueUniqueBooks(ctx context.Context, job novelfetchworkshop.ProcessJob, ttl time.Duration) (bool, string, error) {
	accepted, id, err := q.MemoryQueue.EnqueueUniqueBooks(ctx, job, ttl)
	if accepted {
		q.jobs <- job
	}
	return accepted, id, err
}
func TestExecutionAdmissionConcurrentGenerateQueuesOneWorkerCall(t *testing.T) {
	s := admissionTestStore(t)
	q := &admissionRecordingQueue{MemoryQueue: novelfetchworkshop.NewMemoryQueue(), jobs: make(chan novelfetchworkshop.ProcessJob, 2)}
	var calls atomic.Int32
	release := make(chan struct{})
	var released sync.Once
	t.Cleanup(func() { released.Do(func() { close(release) }) })
	p := rewriteProviderFunc(func(ctx context.Context, _ novelfetchworkshop.RewriteRequest) (string, error) {
		calls.Add(1)
		select {
		case <-release:
			return "改后首行", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	})
	mux := http.NewServeMux()
	registerNovelFetchWorkshopRoutes(mux, s, q, nil, nil, nil, p, nil, "", nil, nil, nil)
	responses := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() { responses <- admissionGenerate(mux) }()
	}
	accepted := 0
	for i := 0; i < 2; i++ {
		var r *httptest.ResponseRecorder
		select {
		case r = <-responses:
		case <-time.After(time.Second):
			t.Fatal("HTTP waited for model completion")
		}
		if r.Code == 200 {
			var out map[string]any
			json.Unmarshal(r.Body.Bytes(), &out)
			if out["queued"] != true || out["job_id"] == "" {
				t.Fatalf("not queued: %s", r.Body.String())
			}
			accepted++
		} else if r.Code != 409 {
			t.Fatalf("unexpected %d %s", r.Code, r.Body.String())
		}
	}
	if accepted != 1 {
		t.Fatalf("duplicate admission: accepted=%d", accepted)
	}
	var job novelfetchworkshop.ProcessJob
	select {
	case job = <-q.jobs:
	default:
		t.Fatal("no durable queue job")
	}
	cfg, err := novelfetchworkshop.ReadWorkflowConfig(context.Background(), s, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if job.SensitiveAIEnabled != novelFetchSensitiveAIEnabled(cfg) || job.AutoSubmit {
		t.Fatalf("direct generation lost configured sensitive setting or enabled upload: %+v", job)
	}
	raw, _ := json.Marshal(job)
	if !strings.Contains(string(raw), `"generateOnly":true`) {
		t.Fatalf("lost generate-only intent %s", raw)
	}
	released.Do(func() { close(release) })
	deadline := time.Now().Add(time.Second)
	for {
		d, _ := s.GetDocument(context.Background(), "alice", "123")
		if body, err := s.GetBody(context.Background(), "alice", "123", "ai1"); err == nil && body.Content == "改后首行" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("missing queued worker result: %+v", d)
		}
		time.Sleep(time.Millisecond)
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicate rewrite or generate-only classification: calls=%d", calls.Load())
	}
}

type admissionStopRaceStore struct {
	*novelfetchworkshop.MemoryStore
	reads int
	q     *novelfetchworkshop.MemoryQueue
}

func (s *admissionStopRaceStore) GetDocument(c context.Context, o, b string) (novelfetchworkshop.Document, error) {
	d, e := s.MemoryStore.GetDocument(c, o, b)
	s.reads++
	if s.reads == 2 {
		latest, _ := s.MemoryStore.GetDocument(c, o, b)
		latest.Meta["status"] = "stopped"
		latest.Meta["queueState"] = "stopped"
		s.MemoryStore.PutDocument(c, o, latest)
		s.q.CancelBooks(c, o, []string{b})
	}
	return d, e
}
func TestExecutionAdmissionNewStopWinsManualRetry(t *testing.T) {
	base := admissionTestStore(t)
	ctx := context.Background()
	d, _ := base.GetDocument(ctx, "alice", "123")
	d.Meta["manualOnly"] = true
	base.PutDocument(ctx, "alice", d)
	q := novelfetchworkshop.NewMemoryQueue()
	s := &admissionStopRaceStore{MemoryStore: base, q: q}
	out, e := enqueueWorkshopRetries(ctx, s, q, "alice", []string{"123"}, false)
	if e != nil {
		t.Fatal(e)
	}
	saved, _ := base.GetDocument(ctx, "alice", "123")
	cancelled, _ := q.IsBookCancelled(ctx, "alice", "123")
	if out.Queued != 0 || !cancelled || saved.Meta["status"] != "stopped" {
		t.Fatalf("stop lost: %+v cancelled=%v meta=%v", out, cancelled, saved.Meta)
	}
}
func TestExecutionAdmissionStaleQueuedDocumentCanRetry(t *testing.T) {
	s := admissionTestStore(t)
	ctx := context.Background()
	d, _ := s.GetDocument(ctx, "alice", "123")
	d.Meta["status"] = "queued"
	d.Meta["queueState"] = "queued"
	d.Meta["currentProcessJobId"] = "missing-old-job"
	s.PutDocument(ctx, "alice", d)
	q := novelfetchworkshop.NewMemoryQueue()
	out, e := enqueueWorkshopRetries(ctx, s, q, "alice", []string{"123"}, false)
	if e != nil || out.Queued != 1 || out.AlreadyRunning != 0 {
		t.Fatalf("stale queue blocked retry: %+v %v", out, e)
	}
}

type admissionClaimErrorQueue struct {
	calls      atomic.Int32
	claimError error
}

func (q *admissionClaimErrorQueue) Claim(context.Context, time.Duration) (novelfetchworkshop.ProcessJob, error) {
	q.calls.Add(1)
	if q.claimError != nil {
		return novelfetchworkshop.ProcessJob{}, q.claimError
	}
	return novelfetchworkshop.ProcessJob{}, errors.New("redis unavailable")
}
func (*admissionClaimErrorQueue) Enqueue(context.Context, novelfetchworkshop.ProcessJob) error {
	return nil
}
func (*admissionClaimErrorQueue) Ack(context.Context, novelfetchworkshop.ProcessJob) error {
	return nil
}
func (*admissionClaimErrorQueue) Fail(context.Context, novelfetchworkshop.ProcessJob, bool) error {
	return nil
}
func TestExecutionAdmissionClaimErrorsBackOffAndCancelPromptly(t *testing.T) {
	q := &admissionClaimErrorQueue{}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() { (&workshopProcessWorker{queue: q}).run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("backoff ignored cancellation")
	}
	if n := q.calls.Load(); n > 3 || n == 0 {
		t.Fatalf("claim errors busy-looped %d calls", n)
	}
}

type admissionUnavailableQueue struct {
	*novelfetchworkshop.MemoryQueue
}

func (q admissionUnavailableQueue) ConflictingBookIDs(context.Context, novelfetchworkshop.ProcessJob) (map[string]string, error) {
	return nil, errors.New("queue cannot be verified")
}
func TestExecutionAdmissionQueueReadFailureBlocksRetry(t *testing.T) {
	s := admissionTestStore(t)
	q := admissionUnavailableQueue{novelfetchworkshop.NewMemoryQueue()}
	out, e := enqueueWorkshopRetries(context.Background(), s, q, "alice", []string{"123"}, false)
	if e != nil {
		t.Fatal(e)
	}
	if out.Queued != 0 || out.Failed != 1 || !strings.Contains(out.Errors["123"], "无法核验") {
		t.Fatalf("unverified queue treated as empty %+v", out)
	}
}
func TestExecutionAdmissionExplicitPriorStopCanResume(t *testing.T) {
	s := admissionTestStore(t)
	ctx := context.Background()
	q := novelfetchworkshop.NewMemoryQueue()
	d, _ := s.GetDocument(ctx, "alice", "123")
	d.Meta["status"], d.Meta["queueState"] = "stopped", "stopped"
	d.Meta["classificationInFlight"] = true
	s.PutDocument(ctx, "alice", d)
	q.CancelBooks(ctx, "alice", []string{"123"})
	out, e := enqueueWorkshopRetries(ctx, s, q, "alice", []string{"123"}, false)
	if e != nil || out.Queued != 1 {
		t.Fatalf("cannot explicitly resume prior stop %+v %v", out, e)
	}
	cancelled, _ := q.IsBookCancelled(ctx, "alice", "123")
	saved, _ := s.GetDocument(ctx, "alice", "123")
	if saved.Meta["classificationInFlight"] != nil {
		t.Fatal("explicit retry retained prior classification reservation")
	}
	if cancelled {
		t.Fatal("prior cancellation retained")
	}
}
func TestExecutionAdmissionGenerateOnlySnapshotSurvivesManualRetry(t *testing.T) {
	s := admissionTestStore(t)
	ctx := context.Background()
	q := novelfetchworkshop.NewMemoryQueue()
	d, _ := s.GetDocument(ctx, "alice", "123")
	d.Meta["processSnapshot"] = map[string]any{"generate_only": true, "auto_submit": true, "selected_versions": []string{"ai2"}, "schedule_id": "old-schedule"}
	s.PutDocument(ctx, "alice", d)
	out, e := enqueueWorkshopRetries(ctx, s, q, "alice", []string{"123"}, false)
	if e != nil || out.Queued != 1 {
		t.Fatalf("not queued %+v %v", out, e)
	}
	job, e := q.Claim(ctx, time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	if !job.GenerateOnly || job.AutoSubmit || job.ScheduleID != "" || len(job.SelectedVersions) != 1 || job.SelectedVersions[0] != "ai2" {
		t.Fatalf("generate-only choice lost %+v", job)
	}
	saved, _ := s.GetDocument(ctx, "alice", "123")
	if mapOrEmpty(saved.Meta["processSnapshot"])["generate_only"] != true {
		t.Fatal("snapshot lost flag")
	}
}

func TestExecutionAdmissionScheduledRewriteRetryReleasesWorkerSlot(t *testing.T) {
	s := admissionTestStore(t)
	q := novelfetchworkshop.NewMemoryQueue()
	ctx := context.Background()
	job := novelfetchworkshop.ProcessJob{ID: "scheduled-stage", BatchID: "scheduled-batch", Owner: "alice", BookIDs: []string{"123"}, PlatformID: "2", SelectedVersions: []string{"ai1"}, GenerateOnly: true}
	if accepted, _, e := q.EnqueueUniqueBooks(ctx, job, time.Hour); e != nil || !accepted {
		t.Fatalf("enqueue %v %v", accepted, e)
	}
	claimed, e := q.Claim(ctx, time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	w := workshopProcessWorker{store: s, queue: q, registry: newWorkshopProcessRegistry(), resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
			return "", errors.New("HTTP 429 rate limit")
		}), nil
	}}
	if e := w.process(ctx, claimed); e != nil {
		t.Fatal(e)
	}
	if _, e := q.Claim(ctx, time.Millisecond); !errors.Is(e, novelfetchworkshop.ErrQueueEmpty) {
		t.Fatalf("scheduled retry immediately occupied worker again: %v", e)
	}
	d, _ := s.GetDocument(ctx, "alice", "123")
	if len(novelFetchRetrySchedule(d.Meta)) == 0 || novelFetchTaskRetryDue(d.Meta, "", time.Now()) {
		t.Fatalf("missing persisted future retry: %v", d.Meta)
	}
}

func TestExecutionAdmissionFastClaimDeadlineAlsoBacksOff(t *testing.T) {
	q := &admissionClaimErrorQueue{claimError: context.DeadlineExceeded}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Millisecond)
	defer cancel()
	(&workshopProcessWorker{queue: q}).run(ctx)
	if n := q.calls.Load(); n > 3 || n == 0 {
		t.Fatalf("fast transport deadline busy-looped %d calls", n)
	}
}

func TestExecutionAdmissionRecoveredAIInFlightDoesNotCallAgain(t *testing.T) {
	s := admissionTestStore(t)
	ctx := context.Background()
	d, _ := s.GetDocument(ctx, "alice", "123")
	d.Meta["aiStatus"] = "ai_processing"
	s.PutDocument(ctx, "alice", d)
	q := novelfetchworkshop.NewMemoryQueue()
	calls := 0
	w := workshopProcessWorker{store: s, queue: q, resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
			calls++
			return "改后首行", nil
		}), nil
	}}
	err := w.execute(ctx, novelfetchworkshop.ProcessJob{ID: "reclaimed-job", Owner: "alice", BookIDs: []string{"123"}, GenerateOnly: true, SelectedVersions: []string{"ai1"}})
	if calls != 0 || err == nil || !novelFetchRetryResultUnknown(err) {
		t.Fatalf("reclaimed unknown model result repeated: calls=%d err=%v", calls, err)
	}
}
func TestExecutionAdmissionStopDuringModelPreventsNextVersionAndMetadataOverwrite(t *testing.T) {
	s := admissionTestStore(t)
	q := novelfetchworkshop.NewMemoryQueue()
	ctx := context.Background()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var calls atomic.Int32
	w := workshopProcessWorker{store: s, queue: q, resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
			calls.Add(1)
			entered <- struct{}{}
			<-release
			return "改后首行", nil
		}), nil
	}}
	done := make(chan error, 1)
	go func() {
		done <- w.execute(ctx, novelfetchworkshop.ProcessJob{ID: "stop-during-model", Owner: "alice", BookIDs: []string{"123"}, GenerateOnly: true, SelectedVersions: []string{"ai1", "ai2"}})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("model not entered")
	}
	if stopped, e := stopWorkshopBook(ctx, s, q, "alice", "123", false); e != nil || !stopped {
		t.Fatalf("stop failed %v %v", stopped, e)
	}
	close(release)
	select {
	case e := <-done:
		if !errors.Is(e, novelfetchworkshop.ErrBookCancelled) {
			t.Fatalf("worker did not observe stop: %v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("worker stuck")
	}
	saved, _ := s.GetDocument(ctx, "alice", "123")
	if calls.Load() != 1 || saved.Meta["status"] != "stopped" || saved.Meta["queueState"] != "stopped" {
		t.Fatalf("new stop overwritten or second model called: calls=%d meta=%v", calls.Load(), saved.Meta)
	}
}

type admissionStageCommitErrorStore struct {
	*novelfetchworkshop.MemoryStore
	failed bool
}

func (s *admissionStageCommitErrorStore) PutDocument(ctx context.Context, owner string, doc novelfetchworkshop.Document) error {
	if !s.failed && len(novelFetchRetrySchedule(doc.Meta)) > 0 {
		s.failed = true
		if err := s.MemoryStore.PutDocument(ctx, owner, doc); err != nil {
			return err
		}
		return errors.New("commit result unknown")
	}
	return s.MemoryStore.PutDocument(ctx, owner, doc)
}
func (s *admissionStageCommitErrorStore) CompareAndSwapDocument(ctx context.Context, owner string, doc novelfetchworkshop.Document) (string, error) {
	if !s.failed && len(novelFetchRetrySchedule(doc.Meta)) > 0 {
		s.failed = true
		version, err := s.MemoryStore.CompareAndSwapDocument(ctx, owner, doc)
		if err != nil {
			return version, err
		}
		return version, errors.New("commit result unknown")
	}
	return s.MemoryStore.CompareAndSwapDocument(ctx, owner, doc)
}
func TestExecutionAdmissionFailureCheckpointDoesNotRepeatModelOrCount(t *testing.T) {
	ctx := context.Background()
	s := &admissionStageCommitErrorStore{MemoryStore: admissionTestStore(t)}
	q := novelfetchworkshop.NewMemoryQueue()
	calls := 0
	job := novelfetchworkshop.ProcessJob{ID: "checkpoint-stage", BatchID: "checkpoint-batch", Owner: "alice", BookIDs: []string{"123"}, PlatformID: "2", SelectedVersions: []string{"ai1"}, GenerateOnly: true}
	w := workshopProcessWorker{store: s, queue: q, registry: newWorkshopProcessRegistry(), resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
			calls++
			return "", errors.New("HTTP 429 rate limit")
		}), nil
	}}
	q.EnqueueUniqueBooks(ctx, job, time.Hour)
	claimed, e := q.Claim(ctx, time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	if e = w.process(ctx, claimed); e != nil {
		t.Fatal(e)
	}
	checkpoint, e := q.Claim(ctx, time.Millisecond)
	if e != nil || !strings.HasPrefix(checkpoint.LastError, workshopStageSaveCheckpointPrefix) {
		t.Fatalf("missing save-only recovery %+v %v", checkpoint, e)
	}
	beforeCalls := calls
	if e = w.process(ctx, checkpoint); e != nil {
		t.Fatal(e)
	}
	d, _ := s.GetDocument(ctx, "alice", "123")
	state := novelFetchRetrySchedule(d.Meta)[novelFetchRetryStageRewrite]
	if calls != beforeCalls || intValue(state["attempt"]) != 1 {
		t.Fatalf("checkpoint repeated model/count calls=%d->%d state=%v", beforeCalls, calls, state)
	}
}
func TestExecutionAdmissionPaidOutputSaveFailureHoldsAutomaticReplay(t *testing.T) {
	ctx := context.Background()
	s := &rulesBodyFaultStore{MemoryStore: admissionTestStore(t), writeErrors: map[string]error{"ai1": errors.New("body save unavailable")}}
	q := novelfetchworkshop.NewMemoryQueue()
	calls := 0
	job := novelfetchworkshop.ProcessJob{ID: "paid-save", BatchID: "paid-save-batch", Owner: "alice", BookIDs: []string{"123"}, PlatformID: "2", SelectedVersions: []string{"ai1"}, GenerateOnly: true}
	w := workshopProcessWorker{store: s, queue: q, registry: newWorkshopProcessRegistry(), resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
			calls++
			return "改后首行", nil
		}), nil
	}}
	q.EnqueueUniqueBooks(ctx, job, time.Hour)
	claimed, e := q.Claim(ctx, time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	if e = w.process(ctx, claimed); e != nil {
		t.Fatal(e)
	}
	if _, e = q.Claim(ctx, time.Millisecond); !errors.Is(e, novelfetchworkshop.ErrQueueEmpty) {
		t.Fatalf("paid output save failure immediately retried %v", e)
	}
	if e = w.execute(ctx, job); e == nil {
		t.Fatal("reclaimed held execution was not blocked")
	}
	if calls != 1 {
		t.Fatalf("paid output save failure called model %d times", calls)
	}
	d, _ := s.GetDocument(ctx, "alice", "123")
	if !novelFetchRetryScheduleHasManualHold(d.Meta) || d.Meta["rewritePersistenceHold"] != true {
		t.Fatalf("missing persistence/manual hold %v", d.Meta)
	}
}

func TestExecutionAdmissionReclaimedPayloadHonorsFutureRetryWithoutConsumingBudget(t *testing.T) {
	ctx := context.Background()
	s := admissionTestStore(t)
	d, _ := s.GetDocument(ctx, "alice", "123")
	scheduleNovelFetchTaskRetry(d.Meta, novelFetchRetryStageRewrite, errors.New("HTTP 429 rate limit"), 0, time.Now())
	s.PutDocument(ctx, "alice", d)
	q := novelfetchworkshop.NewMemoryQueue()
	calls := 0
	job := novelfetchworkshop.ProcessJob{ID: "reclaimed-before-due", BatchID: "reclaimed-before-due-batch", Owner: "alice", BookIDs: []string{"123"}, PlatformID: "2", SelectedVersions: []string{"ai1"}, GenerateOnly: true}
	w := workshopProcessWorker{store: s, queue: q, registry: newWorkshopProcessRegistry(), resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
			calls++
			return "改后首行", nil
		}), nil
	}}
	q.EnqueueUniqueBooks(ctx, job, time.Hour)
	claimed, e := q.Claim(ctx, time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	if e = w.process(ctx, claimed); e != nil {
		t.Fatal(e)
	}
	saved, _ := s.GetDocument(ctx, "alice", "123")
	state := novelFetchRetrySchedule(saved.Meta)[novelFetchRetryStageRewrite]
	if calls != 0 || intValue(state["attempt"]) != 1 {
		t.Fatalf("reclaim bypassed wait or consumed attempt: calls=%d state=%v", calls, state)
	}
	if _, e = q.Claim(ctx, time.Millisecond); !errors.Is(e, novelfetchworkshop.ErrQueueEmpty) {
		t.Fatalf("reclaim retained busy worker slot %v", e)
	}
}

func TestExecutionAdmissionFormatBudgetCountsActualPaidCalls(t *testing.T) {
	ctx := context.Background()
	s := admissionTestStore(t)
	q := novelfetchworkshop.NewMemoryQueue()
	calls := 0
	w := workshopProcessWorker{store: s, queue: q, registry: newWorkshopProcessRegistry(), resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) { calls++; return "", nil }), nil
	}}
	for round := 1; round <= 2; round++ {
		if round == 2 {
			d, _ := s.GetDocument(ctx, "alice", "123")
			schedule := novelFetchRetrySchedule(d.Meta)
			schedule[novelFetchRetryStageRewrite]["next_retry_at"] = time.Now().Add(-time.Second).Format(time.RFC3339Nano)
			d.Meta["retry_schedule"] = schedule
			s.PutDocument(ctx, "alice", d)
		}
		job := novelfetchworkshop.ProcessJob{ID: fmt.Sprintf("format-round-%d", round), BatchID: "format-budget", Owner: "alice", BookIDs: []string{"123"}, PlatformID: "2", SelectedVersions: []string{"ai1"}, GenerateOnly: true}
		q.EnqueueUniqueBooks(ctx, job, time.Hour)
		claimed, e := q.Claim(ctx, time.Millisecond)
		if e != nil {
			t.Fatal(e)
		}
		if e = w.process(ctx, claimed); e != nil {
			t.Fatal(e)
		}
		d, _ := s.GetDocument(ctx, "alice", "123")
		state := novelFetchRetrySchedule(d.Meta)[novelFetchRetryStageRewrite]
		if calls != round || intValue(state["output_rejections"]) != round {
			t.Fatalf("nested calls bypassed format budget: round=%d calls=%d state=%v", round, calls, state)
		}
		if round == 1 {
			due, ok := novelFetchParseRetryTime(firstText(state["next_retry_at"]))
			if !ok || time.Until(due) < 29*time.Second || novelFetchRetryStateIsManual(state) {
				t.Fatalf("first format rejection must wait once %v", state)
			}
		} else if !novelFetchRetryStateIsManual(state) {
			t.Fatalf("second format rejection did not hold %v", state)
		}
		if _, e = q.Claim(ctx, time.Millisecond); !errors.Is(e, novelfetchworkshop.ErrQueueEmpty) {
			t.Fatalf("format failure queued immediate extra call %v", e)
		}
	}
}
func TestExecutionAdmissionSensitiveControlledAttemptCallsOnce(t *testing.T) {
	calls := 0
	provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) { calls++; return "", nil })
	settings := map[string]any{"sensitive_ai": map[string]any{"enabled": true, "retries": 2}}
	items := []map[string]any{{"keyword": "坏词", "before": "坏词片段", "after": "替换片段", "line_index": 0}}
	_, out := applySensitiveAIRepair(batchfactoryv11.WithSingleAttempt(context.Background()), provider, settings, "123", "替换片段", items, nil)
	if calls != 1 || out[0]["status"] != "failed" {
		t.Fatalf("sensitive retried inside controlled attempt: calls=%d items=%v", calls, out)
	}
}

func TestExecutionAdmissionUploadOnlyPreservesUnconfirmedRoundBudget(t *testing.T) {
	ctx := context.Background()
	s := admissionTestStore(t)
	q := novelfetchworkshop.NewMemoryQueue()
	first := time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
	d, _ := s.GetDocument(ctx, "alice", "123")
	d.Versions = map[string]any{"ai1": "已生成的版本"}
	d.Meta["aiStatus"] = "done"
	d.Meta["autoSubmitReady"] = true
	d.Meta["autoSubmitReadyVersions"] = []string{"ai1"}
	d.Meta["automationSubmitAttempts"] = 1
	d.Meta["retry_schedule"] = map[string]map[string]any{novelFetchRetryStageUpload: {"attempt": 1, "first_attempt_at": first, "next_retry_at": time.Now().Add(-time.Second).Format(time.RFC3339Nano), "output_rejections": 1}}
	d.Meta["autoSubmitReadyFingerprint"] = novelFetchUploadReadyFingerprint(ctx, s, "alice", d, []string{"ai1"})
	s.PutDocument(ctx, "alice", d)
	calls := 0
	failure := errors.New("HTTP 429 upload rate limit")
	w := workshopProcessWorker{store: s, queue: q, registry: newWorkshopProcessRegistry(), autoSubmit: func(context.Context, novelfetchworkshop.ProcessJob) error {
		calls++
		current, _ := s.GetDocument(ctx, "alice", "123")
		state := novelFetchRetrySchedule(current.Meta)[novelFetchRetryStageUpload]
		if firstText(state["first_attempt_at"]) != first || intValue(state["attempt"]) != calls || intValue(state["output_rejections"]) != 1 {
			t.Errorf("content shortcut erased unconfirmed upload budget before submission: %v", state)
		}
		markNovelFetchAutomationSubmitResult(ctx, s, "alice", []string{"123"}, failure)
		return failure
	}}
	for round := 1; round <= 3; round++ {
		current, _ := s.GetDocument(ctx, "alice", "123")
		states := novelFetchRetrySchedule(current.Meta)
		if state := states[novelFetchRetryStageUpload]; !novelFetchRetryStateIsManual(state) {
			state["next_retry_at"] = time.Now().Add(-time.Second).Format(time.RFC3339Nano)
			current.Meta["retry_schedule"] = states
			s.PutDocument(ctx, "alice", current)
		}
		job := novelfetchworkshop.ProcessJob{ID: fmt.Sprintf("upload-budget-%d", round), BatchID: "upload-budget", Owner: "alice", BookIDs: []string{"123"}, PlatformID: "2", SelectedVersions: []string{"ai1"}, AutoSubmit: true}
		if ok, _, e := q.EnqueueUniqueBooks(ctx, job, time.Hour); e != nil || !ok {
			t.Fatalf("enqueue round%d %v %v", round, ok, e)
		}
		claimed, e := q.Claim(ctx, time.Millisecond)
		if e != nil {
			t.Fatal(e)
		}
		if e = w.process(ctx, claimed); e != nil {
			t.Fatal(e)
		}
		if _, e = q.Claim(ctx, time.Millisecond); !errors.Is(e, novelfetchworkshop.ErrQueueEmpty) {
			t.Fatalf("unconfirmed upload occupied worker before due: %v", e)
		}
	}
	saved, _ := s.GetDocument(ctx, "alice", "123")
	state := novelFetchRetrySchedule(saved.Meta)[novelFetchRetryStageUpload]
	if calls != 2 || intValue(state["attempt"]) != 3 || firstText(state["first_attempt_at"]) != first || !novelFetchRetryStateIsManual(state) {
		t.Fatalf("upload round budget reset: calls=%d state=%v", calls, state)
	}
}

func TestExecutionAdmissionExpiredUploadRoundHoldsWithoutClearingEvidence(t *testing.T) {
	ctx := context.Background()
	s := admissionTestStore(t)
	q := novelfetchworkshop.NewMemoryQueue()
	first := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339Nano)
	d, _ := s.GetDocument(ctx, "alice", "123")
	d.Meta["retry_schedule"] = map[string]map[string]any{novelFetchRetryStageUpload: {"attempt": 1, "first_attempt_at": first, "next_retry_at": time.Now().Add(-time.Second).Format(time.RFC3339Nano), "output_rejections": 1}}
	s.PutDocument(ctx, "alice", d)
	calls := 0
	w := workshopProcessWorker{store: s, queue: q, registry: newWorkshopProcessRegistry(), autoSubmit: func(context.Context, novelfetchworkshop.ProcessJob) error { calls++; return nil }}
	job := novelfetchworkshop.ProcessJob{ID: "expired-upload", BatchID: "expired-upload", Owner: "alice", BookIDs: []string{"123"}, PlatformID: "2", AutoSubmit: true}
	q.EnqueueUniqueBooks(ctx, job, time.Hour)
	claimed, e := q.Claim(ctx, time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	if e = w.process(ctx, claimed); e != nil {
		t.Fatal(e)
	}
	saved, _ := s.GetDocument(ctx, "alice", "123")
	state := novelFetchRetrySchedule(saved.Meta)[novelFetchRetryStageUpload]
	if calls != 0 || intValue(state["attempt"]) != 1 || firstText(state["first_attempt_at"]) != first || intValue(state["output_rejections"]) != 1 || !novelFetchRetryStateIsManual(state) {
		t.Fatalf("expired round not held with original evidence: calls=%d state=%v", calls, state)
	}
}
