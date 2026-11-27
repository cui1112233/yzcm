package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"qiantie/backend/internal/novelfetchworkshop"
	"testing"
	"time"
)

func TestWorkshopBatchRetryQueuesWithoutCallingProviderAndDeduplicates(t *testing.T) {
	ctx := context.Background()
	store := novelfetchworkshop.NewMemoryStore()
	doc := readyIndependentUploadDocument("retry-book")
	doc.Meta["status"], doc.Meta["aiStatus"], doc.Meta["aiCount"] = "failed", "failed", 2
	doc.Meta["currentBatchId"] = "original-batch"
	doc.Versions["ai1"] = "已经生成的第一版，必须保留。"
	_ = store.PutDocument(ctx, "tester", doc)
	queue := &admissionRecordingOnlyQueue{control: novelfetchworkshop.NewMemoryQueue()}
	calls := 0
	now := time.Unix(1800000000, 0)
	handler := NewRouter(RouterOptions{BridgeSecret: "secret", Now: func() time.Time { return now }, NovelFetchStore: store, NovelFetchQueue: queue,
		NovelFetchRewrite: rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
			calls++
			return "", errors.New("must not call provider in HTTP request")
		})})
	for i := 0; i < 2; i++ {
		response := doNovelFetchRequest(t, handler, "POST", "/api/novel-fetch-workshop/tasks/batch-retry", map[string]any{"ids": []string{"retry-book", "retry-book"}}, now, "secret")
		if response.Code != http.StatusAccepted {
			t.Fatalf("expected async acceptance, got %d %s", response.Code, response.Body)
		}
		var out map[string]any
		_ = json.Unmarshal(response.Body.Bytes(), &out)
		if out["requested"] != float64(1) {
			t.Fatalf("duplicate IDs not normalized: %v", out)
		}
	}
	if calls != 0 {
		t.Fatalf("synchronous provider calls: %d", calls)
	}
	if len(queue.jobs) != 1 {
		t.Fatalf("duplicate queue jobs: %d", len(queue.jobs))
	}
	job := queue.jobs[0]
	if job.BatchID != "original-batch" || len(job.SelectedVersions) != 2 || job.SelectedVersions[1] != "ai2" {
		t.Fatalf("retry lost original batch/version target: %+v", job)
	}
	stored, _ := store.GetDocument(ctx, "tester", doc.BookID)
	if stored.Versions["ai1"] != doc.Versions["ai1"] || stored.Original != doc.Original {
		t.Fatal("retry replaced completed content")
	}
}

func TestWorkshopQueuedRetrySurvivesCallerCancellation(t *testing.T) {
	store := novelfetchworkshop.NewMemoryStore()
	doc := readyIndependentUploadDocument("retry-cancel")
	doc.Meta["aiStatus"] = "failed"
	_ = store.PutDocument(context.Background(), "owner", doc)
	queue := novelfetchworkshop.NewMemoryQueue()
	ctx, cancel := context.WithCancel(context.Background())
	result, err := enqueueWorkshopRetries(ctx, store, queue, "owner", []string{doc.BookID}, false)
	if err != nil || result.Queued != 1 {
		t.Fatalf("not queued: %+v %v", result, err)
	}
	cancel()
	job, err := queue.Claim(context.Background(), time.Millisecond)
	if err != nil || job.BookIDs[0] != doc.BookID {
		t.Fatalf("queued retry lost when request ended: %+v %v", job, err)
	}
}

// Record queue admission without running its consumer. The separate concurrency
// route regression exercises the real MemoryQueue worker and provider result.
type admissionRecordingOnlyQueue struct {
	uniqueRecordingNovelFetchQueue
	control *novelfetchworkshop.MemoryQueue
}

func (q *admissionRecordingOnlyQueue) WithBookAdmission(c context.Context, o, b string, f func(context.Context) error) error {
	return q.control.WithBookAdmission(c, o, b, f)
}
func (q *admissionRecordingOnlyQueue) CancelBooks(c context.Context, o string, b []string) error {
	return q.control.CancelBooks(c, o, b)
}
func (q *admissionRecordingOnlyQueue) ClearCancelledBooks(c context.Context, o string, b []string) error {
	return q.control.ClearCancelledBooks(c, o, b)
}
func (q *admissionRecordingOnlyQueue) IsBookCancelled(c context.Context, o, b string) (bool, error) {
	return q.control.IsBookCancelled(c, o, b)
}
