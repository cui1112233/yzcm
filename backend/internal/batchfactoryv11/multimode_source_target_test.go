package batchfactoryv11

import (
	"context"
	"testing"
)

func TestMultiModeWorkerOnlyReadsItsBookAndReusesSharedResult(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	group, err := s.CreateMultiModeBatches(ctx, "alice", CreateMultiModeInput{RequestID: "targeted", Title: "group", Books: []CreateBookInput{{BookID: "738579", Title: "ready"}, {BookID: "738580", Title: "slow"}}, Projects: []ModeProjectInput{{Mode: "comic"}, {Mode: "text"}}})
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ := s.ListMultiModeSources(ctx, "alice", group.GroupID)
	firstKey := jobs[0].Key
	for _, job := range jobs {
		if job.Input.BookID == "738579" {
			firstKey = job.Key
		}
	}
	reads := 0
	prepare := func(_ context.Context, input CreateBookInput) (MultiModeSourceSnapshot, error) {
		reads++
		if input.BookID != "738579" {
			t.Fatal("this worker waited for an unrelated book")
		}
		return MultiModeSourceSnapshot{OriginalRaw: "full source"}, nil
	}
	for range 2 {
		if err = ResolveMultiModeSource(ctx, s, "alice", group.GroupID, firstKey, prepare); err != nil {
			t.Fatal(err)
		}
	}
	if reads != 1 {
		t.Fatalf("shared source was read %d times", reads)
	}
	group, _ = s.GetMultiModeCreation(ctx, "alice", group.GroupID)
	for _, batch := range group.Batches {
		if batch.Books[0].TxtText != "full source" || batch.Books[1].SourceMetadata["multiModeSourceState"] != "pending" {
			t.Fatal("ready book did not proceed independently")
		}
	}
}
