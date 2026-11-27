package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
)

// Another dispatcher saves each real preparation/dispatch checkpoint just
// before this dispatcher's CAS write. No timers are needed to expose the race.
type competingSchedulePreparationStore struct {
	*novelfetchworkshop.MemoryStore
	checkpoints []string
	conflicts   int
}

func (s *competingSchedulePreparationStore) PutConfig(ctx context.Context, owner string, settings map[string]any) error {
	if s.conflicts >= len(s.checkpoints) {
		return s.MemoryStore.PutConfig(ctx, owner, settings)
	}
	latest, err := s.MemoryStore.GetConfig(ctx, owner)
	if err != nil {
		return err
	}
	incoming := mapOrEmpty(arrayOrEmpty(settings["novel_fetch_schedules"])[0])
	winner := cloneAnyMap(incoming)
	winner["status"] = s.checkpoints[s.conflicts]
	winner["job_id"] = "other-dispatch-checkpoint"
	latest["novel_fetch_schedules"] = []any{winner}
	latest["concurrent_preference"] = "keep"
	if err := s.MemoryStore.PutConfig(ctx, owner, latest); err != nil {
		return err
	}
	s.conflicts++
	return novelfetchworkshop.ErrConfigConflict
}

func TestConcurrentSchedulePreparationAdoptsSavedCheckpoint(t *testing.T) {
	for _, status := range []string{"scheduled", "queued", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			ctx := context.Background()
			now := time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)
			s := &competingSchedulePreparationStore{MemoryStore: novelfetchworkshop.NewMemoryStore(), checkpoints: []string{status, "running", "completed"}}
			if err := s.MemoryStore.PutConfig(ctx, "tester", map[string]any{"novel_fetch_schedules": []any{map[string]any{"id": "schedule-race", "status": "scheduled", "run_at": now.Format(time.RFC3339), "input_text": "12345678901\t并发定时任务", "platform_id": "2", "max_txt": 4000, "selected_versions": []any{"ai1"}}}}); err != nil {
				t.Fatal(err)
			}
			settings, err := s.GetConfig(ctx, "tester")
			if err != nil {
				t.Fatal(err)
			}
			item := mapOrEmpty(arrayOrEmpty(settings["novel_fetch_schedules"])[0])
			if err := prepareNovelFetchScheduleTasks(ctx, s, "tester", item, now); err != nil {
				t.Fatalf("competing prepared checkpoint rejected: %v", err)
			}
			saved, err := s.GetConfig(ctx, "tester")
			if err != nil {
				t.Fatal(err)
			}
			row := mapOrEmpty(arrayOrEmpty(saved["novel_fetch_schedules"])[0])
			if s.conflicts != 1 || item["status"] != status || row["status"] != status || item["job_id"] != "other-dispatch-checkpoint" || saved["concurrent_preference"] != "keep" {
				t.Fatalf("stale preparation overwrote/ignored saved progress: conflicts=%d local_status=%v saved_status=%v local_job=%v", s.conflicts, item["status"], row["status"], item["job_id"])
			}
		})
	}
}

type failedSchedulePreparationWriteStore struct {
	*novelfetchworkshop.MemoryStore
	failure error
}

func (s failedSchedulePreparationWriteStore) PutConfig(context.Context, string, map[string]any) error {
	return s.failure
}
func TestSchedulePreparationKeepsRealWriteFailure(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	base := novelfetchworkshop.NewMemoryStore()
	item := map[string]any{"id": "s", "status": "scheduled", "run_at": now.Format(time.RFC3339), "input_text": "12345678901\t书名", "selected_versions": []any{"ai1"}}
	if err := base.PutConfig(ctx, "tester", map[string]any{"novel_fetch_schedules": []any{item}}); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("database write failed")
	if err := prepareNovelFetchScheduleTasks(ctx, failedSchedulePreparationWriteStore{MemoryStore: base, failure: failure}, "tester", item, now); !errors.Is(err, failure) {
		t.Fatalf("real write failure hidden: %v", err)
	}
}
