package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
)

const integrationRetryBook = "1234567890"
const integrationOtherBook = "2234567890"

type integrationRetryQueue struct {
	*novelfetchworkshop.MemoryQueue
	mu       sync.Mutex
	attempts map[string]int
	reject   string
}

func (q *integrationRetryQueue) EnqueueUniqueBooks(ctx context.Context, job novelfetchworkshop.ProcessJob, ttl time.Duration) (bool, string, error) {
	q.mu.Lock()
	id := job.BookIDs[0]
	q.attempts[id]++
	rejected := q.reject == id
	q.mu.Unlock()
	if rejected {
		return false, "", errors.New("synthetic queue rejection")
	}
	return q.MemoryQueue.EnqueueUniqueBooks(ctx, job, ttl)
}

type integrationRetryStore struct {
	*novelfetchworkshop.MemoryStore
	failQueueState bool
	advanceWorker  bool
}

func (s *integrationRetryStore) CompareAndSwapDocument(ctx context.Context, owner string, doc novelfetchworkshop.Document) (string, error) {
	if toString(doc.Meta["queueState"]) == "queued" {
		if s.failQueueState {
			s.failQueueState = false
			return "", errors.New("synthetic database interruption")
		}
		if s.advanceWorker {
			s.advanceWorker = false
			doc.Meta["status"], doc.Meta["queueState"] = "processing", "processing"
			if _, err := s.MemoryStore.CompareAndSwapDocument(ctx, owner, doc); err != nil {
				return "", err
			}
			return "", novelfetchworkshop.ErrDocumentChanged
		}
	}
	return s.MemoryStore.CompareAndSwapDocument(ctx, owner, doc)
}

type integrationRetryReceipt struct {
	OK         bool   `json:"ok"`
	Status     string `json:"status"`
	Accepted   int    `json:"accepted"`
	Failed     int    `json:"failed"`
	Skipped    int    `json:"skipped_duplicate"`
	Created    int    `json:"created"`
	Pending    int    `json:"state_pending"`
	Reconciled int    `json:"reconciled"`
}

func integrationRetryRoute(t *testing.T, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, bookIDFetch bool) func(...string) integrationRetryReceipt {
	t.Helper()
	mux := http.NewServeMux()
	now := time.Now()
	RegisterNovelFetchIntegrations(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, store, queue)
	return func(ids ...string) integrationRetryReceipt {
		t.Helper()
		path := "/api/integrations/workbench/push"
		books := make([]map[string]string, 0, len(ids))
		text := ""
		for _, id := range ids {
			books = append(books, map[string]string{"book_id": id})
			text += id + "\n"
		}
		payload := map[string]any{"books": books}
		if bookIDFetch {
			path = "/api/integrations/changdu/bookid-fetch/run"
			payload = map[string]any{"input_text": text}
		}
		resp := signedJSONRequest(t, mux, now, "reader", http.MethodPost, path, payload)
		var receipt integrationRetryReceipt
		if err := json.Unmarshal(resp.Body.Bytes(), &receipt); err != nil {
			t.Errorf("%s: invalid receipt %d %s", path, resp.Code, resp.Body)
		}
		return receipt
	}
}

func TestNovelFetchIntegrationRetryAfterQueueRejection(t *testing.T) {
	for _, route := range []struct {
		name        string
		bookIDFetch bool
	}{{"workbench", false}, {"book-id", true}} {
		t.Run(route.name, func(t *testing.T) {
			store := novelfetchworkshop.NewMemoryStore()
			queue := &integrationRetryQueue{MemoryQueue: novelfetchworkshop.NewMemoryQueue(), attempts: map[string]int{}, reject: integrationRetryBook}
			post := integrationRetryRoute(t, store, queue, route.bookIDFetch)
			if got := post(integrationRetryBook, integrationOtherBook); got.OK || got.Status != "partial_failed" || got.Accepted != 1 || got.Failed != 1 || got.Skipped != 0 {
				t.Errorf("first mixed receipt = %+v", got)
			}
			doc, err := store.GetDocument(context.Background(), "reader", integrationRetryBook)
			if err != nil || novelfetchworkshop.DeriveTaskStatus(doc) != novelfetchworkshop.TaskStatusInputReady {
				t.Fatalf("failed document lost retry state: %+v, %v", doc, err)
			}
			queue.reject = ""
			if got := post(integrationRetryBook, integrationOtherBook); !got.OK || got.Accepted != 1 || got.Failed != 0 || got.Skipped != 1 || got.Created != 0 {
				t.Errorf("retry receipt = %+v", got)
			}
			recovered, err := store.GetDocument(context.Background(), "reader", integrationRetryBook)
			if err != nil || novelfetchworkshop.DeriveTaskStatus(recovered) != novelfetchworkshop.TaskStatusQueued ||
				recovered.Meta["importedAt"] != doc.Meta["importedAt"] {
				t.Errorf("retry replaced or failed to queue document: %+v, %v", recovered, err)
			}
			if queue.attempts[integrationRetryBook] != 2 || queue.attempts[integrationOtherBook] != 1 {
				t.Errorf("unexpected enqueue attempts %+v", queue.attempts)
			}
		})
	}
}

func TestNovelFetchIntegrationRetryRepairsAcceptedQueueState(t *testing.T) {
	for _, route := range []struct {
		name        string
		bookIDFetch bool
	}{{"workbench", false}, {"book-id", true}} {
		t.Run(route.name, func(t *testing.T) {
			ctx := context.Background()
			store := &integrationRetryStore{MemoryStore: novelfetchworkshop.NewMemoryStore(), failQueueState: true}
			queue := &integrationRetryQueue{MemoryQueue: novelfetchworkshop.NewMemoryQueue(), attempts: map[string]int{}}
			post := integrationRetryRoute(t, store, queue, route.bookIDFetch)
			if got := post(integrationRetryBook); got.OK || got.Status != "partial_failed" || got.Accepted != 1 || got.Failed != 1 || got.Pending != 1 {
				t.Errorf("unrecorded queue receipt = %+v", got)
			}
			job, err := queue.Claim(ctx, time.Millisecond)
			if err != nil {
				t.Fatal("the initial queue submission should be real", err)
			}
			if got := post(integrationRetryBook); !got.OK || got.Accepted != 0 || got.Failed != 0 || got.Skipped != 1 || got.Reconciled != 1 {
				t.Errorf("reconciled receipt = %+v", got)
			}
			doc, err := store.GetDocument(ctx, "reader", integrationRetryBook)
			if err != nil || doc.Meta["currentProcessJobId"] != job.ID || novelfetchworkshop.DeriveTaskStatus(doc) != novelfetchworkshop.TaskStatusQueued ||
				queue.attempts[integrationRetryBook] != 1 {
				t.Errorf("queue reconciliation = %+v err=%v attempts=%+v", doc, err, queue.attempts)
			}
		})
	}
}

func TestNovelFetchIntegrationWorkerAdvanceWinsOverQueueState(t *testing.T) {
	ctx := context.Background()
	store := &integrationRetryStore{MemoryStore: novelfetchworkshop.NewMemoryStore(), advanceWorker: true}
	queue := &integrationRetryQueue{MemoryQueue: novelfetchworkshop.NewMemoryQueue(), attempts: map[string]int{}}
	post := integrationRetryRoute(t, store, queue, false)
	if got := post(integrationRetryBook); !got.OK || got.Accepted != 1 || got.Failed != 0 {
		t.Errorf("worker advancement receipt = %+v", got)
	}
	doc, err := store.GetDocument(ctx, "reader", integrationRetryBook)
	if err != nil || novelfetchworkshop.DeriveTaskStatus(doc) != novelfetchworkshop.TaskStatusProcessing {
		t.Fatalf("worker advancement overwritten: %+v, %v", doc, err)
	}
	doc.Meta["status"], doc.Meta["queueState"] = "completed", "completed"
	if err := store.PutDocument(ctx, "reader", doc); err != nil {
		t.Fatal(err)
	}
	job, err := queue.Claim(ctx, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := queue.Ack(ctx, job); err != nil {
		t.Fatal(err)
	}
	if got := post(integrationRetryBook); !got.OK || got.Accepted != 0 || got.Skipped != 1 {
		t.Errorf("worker advancement retry = %+v", got)
	}
	doc, err = store.GetDocument(ctx, "reader", integrationRetryBook)
	if err != nil || novelfetchworkshop.DeriveTaskStatus(doc) != novelfetchworkshop.TaskStatusCompleted || queue.attempts[integrationRetryBook] != 1 {
		t.Errorf("worker state or queue regressed: %+v %v %+v", doc, err, queue.attempts)
	}
}

func TestNovelFetchIntegrationDoesNotResubmitAnotherSourceOrStoppedBook(t *testing.T) {
	ctx := context.Background()
	store := novelfetchworkshop.NewMemoryStore()
	queue := &integrationRetryQueue{MemoryQueue: novelfetchworkshop.NewMemoryQueue(), attempts: map[string]int{}}
	if err := store.PutDocument(ctx, "reader", novelfetchworkshop.Document{
		BookID: integrationRetryBook, Meta: map[string]any{"importSource": "integration", "platformId": "1", "status": "input_ready"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutDocument(ctx, "reader", novelfetchworkshop.Document{
		BookID: integrationOtherBook, Meta: map[string]any{"importSource": "integration", "platformId": "2", "status": "stopped"},
	}); err != nil {
		t.Fatal(err)
	}
	post := integrationRetryRoute(t, store, queue, false)
	if got := post(integrationRetryBook, integrationOtherBook); !got.OK || got.Created != 0 || got.Accepted != 0 || got.Skipped != 2 || len(queue.attempts) != 0 {
		t.Errorf("different source and stopped book receipt = %+v attempts=%+v", got, queue.attempts)
	}
}

func TestNovelFetchIntegrationConcurrentRequestsCreateAndQueueOnce(t *testing.T) {
	store := novelfetchworkshop.NewMemoryStore()
	queue := &integrationRetryQueue{MemoryQueue: novelfetchworkshop.NewMemoryQueue(), attempts: map[string]int{}}
	push := integrationRetryRoute(t, store, queue, false)
	fetch := integrationRetryRoute(t, store, queue, true)
	begin := make(chan struct{})
	results := make(chan integrationRetryReceipt, 12)
	for range 6 {
		go func() { <-begin; results <- push(integrationRetryBook) }()
		go func() { <-begin; results <- fetch(integrationRetryBook) }()
	}
	close(begin)
	created, accepted, skipped := 0, 0, 0
	for range cap(results) {
		got := <-results
		created += got.Created
		accepted += got.Accepted
		skipped += got.Skipped
		if !got.OK {
			t.Errorf("concurrent reply = %+v", got)
		}
	}
	if created != 1 || accepted != 1 || skipped != cap(results)-1 || queue.attempts[integrationRetryBook] != 1 {
		t.Errorf("concurrent results created=%d accepted=%d skipped=%d attempts=%+v", created, accepted, skipped, queue.attempts)
	}
}
