package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
)

func TestNovelFetchWrappedValidationGetsOnlyOneDelayedRepair(t *testing.T) {
	now := time.Now().UTC()
	meta := map[string]any{}
	failure := fmt.Errorf("quality validation failed: %w", novelfetchworkshop.ErrRewriteValidation)
	first := scheduleNovelFetchTaskRetry(meta, novelFetchRetryStageRewrite, failure, 0, now)
	due, ok := novelFetchParseRetryTime(firstText(first["next_retry_at"]))
	if !ok || due.Before(now.Add(30*time.Second)) || intValue(first["output_rejections"]) != 1 || novelFetchRetryStateIsManual(first) {
		t.Fatalf("first validation did not get one delayed repair: %#v", first)
	}
	persisted, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	meta = map[string]any{}
	if err := json.Unmarshal(persisted, &meta); err != nil {
		t.Fatal(err)
	}
	second := scheduleNovelFetchTaskRetry(meta, novelFetchRetryStageRewrite, failure, 0, due)
	if !novelFetchRetryStateIsManual(second) || intValue(second["attempt"]) != 2 || intValue(second["output_rejections"]) != 2 || firstText(second["next_retry_at"]) != "" {
		t.Fatalf("second validation remained retryable: %#v", second)
	}
	unknownSave := fmt.Errorf("quality result persistence: %w", errors.Join(novelfetchworkshop.ErrRewriteValidation, novelfetchworkshop.ErrRewritePersistence))
	manual := scheduleNovelFetchTaskRetry(map[string]any{}, novelFetchRetryStageRewrite, unknownSave, 0, now)
	if !novelFetchRetryStateIsManual(manual) || firstText(manual["next_retry_at"]) != "" {
		t.Fatalf("unknown save incorrectly scheduled repair: %#v", manual)
	}
}

func TestNovelFetchRetryBudgetSurvivesReloadAndExhausts(t *testing.T) {
	now := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	meta := map[string]any{}
	for attempt := 1; attempt <= 3; attempt++ {
		state := scheduleNovelFetchTaskRetry(meta, novelFetchRetryStageRewrite, errors.New("HTTP 429 rate limit"), 0, now)
		if intValue(state["attempt"]) != attempt || (state["manual_only"] == true) != (attempt == 3) {
			t.Fatalf("attempt %d: %v", attempt, state)
		}
		data, _ := json.Marshal(meta)
		meta = map[string]any{}
		_ = json.Unmarshal(data, &meta)
	}
	if novelFetchTaskRetryDue(meta, "", now.Add(time.Hour)) {
		t.Fatal("exhausted budget became due")
	}
}

func TestNovelFetchOutputRetryIsDelayedAndLimited(t *testing.T) {
	now := time.Now().UTC()
	meta := map[string]any{}
	first := scheduleNovelFetchTaskRetry(meta, novelFetchRetryStageRewrite, errors.New("ai_output_rejected invalid_json"), 0, now)
	due, ok := novelFetchParseRetryTime(firstText(first["next_retry_at"]))
	if !ok || due.Before(now.Add(30*time.Second)) {
		t.Fatalf("unsafe output retry: %v", first)
	}
	second := scheduleNovelFetchTaskRetry(meta, novelFetchRetryStageRewrite, errors.New("ai_output_rejected invalid_json"), 0, due)
	if second["manual_only"] != true {
		t.Fatalf("second malformed output retries again: %v", second)
	}
}

func TestNovelFetchRetryUnknownAndClassifyAreManual(t *testing.T) {
	for _, tc := range []struct{ stage, message string }{{novelFetchRetryStageRewrite, "模型请求超时"}, {novelFetchRetryStageRewrite, "connection reset by peer"}, {novelFetchRetryStageClassify, "HTTP 429"}} {
		state := scheduleNovelFetchTaskRetry(map[string]any{}, tc.stage, errors.New(tc.message), 0, time.Now())
		if state["manual_only"] != true {
			t.Fatalf("unsafe retry: %v", state)
		}
	}
}

func TestNovelFetchRetryDeadlineAndCorruptDatesNeverBecomeDue(t *testing.T) {
	now := time.Now().UTC()
	for _, state := range []map[string]any{
		{"attempt": 1, "first_attempt_at": now.Add(-25 * time.Hour).Format(time.RFC3339Nano), "next_retry_at": now.Add(-time.Hour).Format(time.RFC3339Nano)},
		{"attempt": 1, "next_retry_at": "not-a-date"},
		{"attempt": 3, "next_retry_at": now.Add(-time.Hour).Format(time.RFC3339Nano)},
	} {
		meta := map[string]any{"retry_schedule": map[string]map[string]any{novelFetchRetryStageRewrite: state}}
		if novelFetchTaskRetryDue(meta, "", now) {
			t.Fatalf("unsafe legacy retry: %v", state)
		}
	}
}

func TestNovelFetchUploadBudgetAndUnknownResultsHold(t *testing.T) {
	for _, message := range []string{"HTTP 429 rate limit", "上传连接暂时超时"} {
		store := novelfetchworkshop.NewMemoryStore()
		ctx := context.Background()
		_ = store.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Meta: map[string]any{"siteSubmitStatus": "pending_upload"}})
		for i := 0; i < 3; i++ {
			markNovelFetchAutomationSubmitResult(ctx, store, "alice", []string{"123"}, errors.New(message))
		}
		doc, _ := store.GetDocument(ctx, "alice", "123")
		if novelFetchAutomationEligible(doc, time.Now().Add(time.Hour)) {
			t.Fatalf("upload repeats indefinitely: %v", doc.Meta)
		}
	}
}

func TestNovelFetchStalePaidAIHoldsInsteadOfRequeue(t *testing.T) {
	ctx := context.Background()
	store := novelfetchworkshop.NewMemoryStore()
	now := time.Now().UTC()
	_ = store.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Meta: map[string]any{"status": "processing", "aiStatus": "ai_processing", "currentProcessJobId": "missing"}})
	docs, _ := store.ListDocuments(ctx, "alice")
	// The stale scanner's input is a historical summary; keep stored timestamp
	// consistent so the fresh-document concurrency guard is exercised.
	now = now.Add(2 * time.Hour)
	queue := &budgetAbsentQueue{MemoryQueue: novelfetchworkshop.NewMemoryQueue()}
	recoverStaleNovelFetchTasks(ctx, store, "alice", nil, docs, now, queue)
	doc, _ := store.GetDocument(ctx, "alice", "123")
	if doc.Meta["manualOnly"] != true || novelFetchAutomationEligible(doc, now.Add(time.Hour)) {
		t.Fatalf("uncertain paid AI requeued: %v", doc.Meta)
	}
}

func TestNovelFetchScannerPersistsExpiredBudgetHold(t *testing.T) {
	ctx := context.Background()
	store := novelfetchworkshop.NewMemoryStore()
	now := time.Now().UTC()
	_ = store.PutConfig(ctx, "alice", map[string]any{novelFetchAutomationConfigKey: map[string]any{"enabled": true}})
	_ = store.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Meta: map[string]any{"status": "failed", "retry_schedule": map[string]map[string]any{novelFetchRetryStageRewrite: {"attempt": 1, "first_attempt_at": now.Add(-25 * time.Hour).Format(time.RFC3339Nano), "next_retry_at": now.Add(-time.Hour).Format(time.RFC3339Nano)}}}})
	job, err := dispatchNovelFetchAutomationOwner(ctx, store, novelfetchworkshop.NewMemoryQueue(), "alice", now)
	if err != nil || job != nil {
		t.Fatalf("job=%v err=%v", job, err)
	}
	doc, _ := store.GetDocument(ctx, "alice", "123")
	if doc.Meta["manualOnly"] != true || !novelFetchRetryStateIsManual(novelFetchRetrySchedule(doc.Meta)[novelFetchRetryStageRewrite]) {
		t.Fatalf("expired round has no persisted manual hold: %#v", doc.Meta)
	}
}

func TestNovelFetchOriginalSuccessPreservesOtherStageBudget(t *testing.T) {
	ctx := context.Background()
	store := novelfetchworkshop.NewMemoryStore()
	now := time.Now().UTC()
	meta := map[string]any{"originalFetchRetryAttempts": 1}
	scheduleNovelFetchTaskRetry(meta, novelFetchRetryStageOriginalFetch, errors.New("fetch failed"), time.Minute, now)
	scheduleNovelFetchTaskRetry(meta, novelFetchRetryStageSensitive, errors.New("HTTP 429 rate limit"), time.Minute, now)
	_ = store.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Meta: meta})
	if err := clearNovelFetchOriginalFetchRetry(ctx, store, "alice", "123"); err != nil {
		t.Fatal(err)
	}
	doc, _ := store.GetDocument(ctx, "alice", "123")
	schedule := novelFetchRetrySchedule(doc.Meta)
	if schedule[novelFetchRetryStageOriginalFetch] != nil || intValue(schedule[novelFetchRetryStageSensitive]["attempt"]) != 1 {
		t.Fatalf("original success reset another stage: %#v", schedule)
	}
}

func TestNovelFetchSensitiveLegacyDeadlineIsNotBypassed(t *testing.T) {
	now := time.Now().UTC()
	for _, deadline := range []string{"corrupt", now.Add(time.Hour).Format(time.RFC3339Nano)} {
		doc := novelfetchworkshop.Document{BookID: "123", OriginalRaw: "saved raw", Meta: map[string]any{"status": "failed", "originalStatus": "waiting_sensitive_ai", "automationNextRetryAt": deadline}}
		if novelFetchAutomationEligible(doc, now) {
			t.Fatalf("sensitive stage bypassed deadline %q", deadline)
		}
	}
}

type budgetAbsentQueue struct {
	*novelfetchworkshop.MemoryQueue
}

func (*budgetAbsentQueue) RecoverStaleJob(context.Context, string) (bool, error)  { return true, nil }
func (*budgetAbsentQueue) ProcessJobExists(context.Context, string) (bool, error) { return false, nil }
