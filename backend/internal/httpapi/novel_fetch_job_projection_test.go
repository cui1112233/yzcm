package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
)

type jobProjectionQueue struct {
	recordingNovelFetchQueue
	exists bool
	err    error
}

func (q *jobProjectionQueue) ProcessJobExists(context.Context, string) (bool, error) {
	return q.exists, q.err
}

// A missing Redis payload must not turn a historical queued run into live work.
func TestNovelFetchHistoricalJobsVerifyQueueBeforeShowingActive(t *testing.T) {
	for _, id := range []string{"queue_old", "go-workshop-old", "go-schedule-old"} {
		for _, state := range []string{"queued", "running", "waiting_retry"} {
			t.Run(id+"/"+state, func(t *testing.T) {
				ctx := context.Background()
				now := time.Unix(1_800_000_000, 0)
				store := novelfetchworkshop.NewMemoryStore()
				if err := store.PutRun(ctx, "tester", novelfetchworkshop.RunRecord{RunID: id, BookID: "batch-old", ModelID: "novel-fetch-workshop", Stage: "novel_fetch_process", Status: state, StartedAt: now.Add(-time.Hour).Format(time.RFC3339Nano)}); err != nil {
					t.Fatal(err)
				}
				q := &jobProjectionQueue{}
				h := NewRouter(RouterOptions{BridgeSecret: "projection", Now: func() time.Time { return now }, NovelFetchStore: store, NovelFetchQueue: q})
				for _, path := range []string{"/api/novel-fetch-workshop/process/jobs/latest?limit=3", "/api/novel-fetch-workshop/process/jobs/" + id, "/api/novel-fetch-workshop/realtime/status"} {
					response := doNovelFetchRequest(t, h, http.MethodGet, path, nil, now, "projection")
					if response.Code != 200 {
						t.Fatalf("%s: %d %s", path, response.Code, response.Body.String())
					}
					if !strings.Contains(response.Body.String(), `"queue_missing"`) {
						t.Errorf("%s still presents orphan as active: %s", path, response.Body.String())
					}
					if strings.Contains(response.Body.String(), `"active_job_id":"`+id+`"`) {
						t.Errorf("orphan is active: %s", response.Body.String())
					}
				}
				runs, _ := store.ListRuns(ctx, "tester", "")
				if len(runs) != 1 || runs[0].Status != state || len(q.jobs) != 0 {
					t.Fatal("read-only projection changed history or queued work")
				}
			})
		}
	}
}

func TestNovelFetchJobProjectionPreservesLiveAndUnverifiableJobs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		exists bool
		err    error
		state  string
	}{
		{"live", true, nil, "queued"}, {"redis unavailable", false, errors.New("redis unavailable"), "running"}, {"completed", false, nil, "done"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(1_800_000_000, 0)
			s := novelfetchworkshop.NewMemoryStore()
			if err := s.PutRun(context.Background(), "tester", novelfetchworkshop.RunRecord{RunID: "queue_checked", BookID: "batch-old", ModelID: "novel-fetch-workshop", Stage: "novel_fetch_process", Status: tc.state}); err != nil {
				t.Fatal(err)
			}
			q := &jobProjectionQueue{exists: tc.exists, err: tc.err}
			h := NewRouter(RouterOptions{BridgeSecret: "projection", Now: func() time.Time { return now }, NovelFetchStore: s, NovelFetchQueue: q})
			r := doNovelFetchRequest(t, h, http.MethodGet, "/api/novel-fetch-workshop/process/jobs/queue_checked", nil, now, "projection")
			var data map[string]any
			if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &data) != nil {
				t.Fatalf("response %d %s", r.Code, r.Body.String())
			}
			if data["status"] != tc.state {
				t.Fatalf("status=%v want %s", data["status"], tc.state)
			}
			if tc.err != nil && data["queue_check_error"] == nil {
				t.Fatal("queue failure not distinguished from verified live state")
			}
		})
	}
}
