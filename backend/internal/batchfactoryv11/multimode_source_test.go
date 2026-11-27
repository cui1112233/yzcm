package batchfactoryv11

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestMultiModeSourcesFetchOnceDeliverAndPreserveManualEdits(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	input := CreateMultiModeInput{RequestID: "sources", Title: "group", Books: []CreateBookInput{{BookID: "738579", Title: "one"}, {BookID: "738580", Title: "two"}}}
	for _, mode := range []CreationMode{"video", "comic", "illustrated_text", "text"} {
		input.Projects = append(input.Projects, ModeProjectInput{Mode: mode, Settings: SettingsPatch{}})
	}
	group, err := s.CreateMultiModeBatches(ctx, "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	a := group.Batches[0]
	_, err = s.UpdateBookMetadata(ctx, "alice", a.ID, a.Books[0].ID, UpdateBookMetadataInput{ExpectedRevision: 1, Metadata: map[string]any{"gender": "男频"}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	work := func(_ context.Context, in CreateBookInput) (MultiModeSourceSnapshot, error) {
		calls++
		return MultiModeSourceSnapshot{Title: in.Title, OriginalRaw: "full-" + in.BookID, Metadata: map[string]any{"gender": "女频", "style": "现代言情"}}, nil
	}
	if err = ResolveMultiModeSources(ctx, s, "alice", group.GroupID, work); err != nil {
		t.Fatal(err)
	}
	if err = ResolveMultiModeSources(ctx, s, "alice", group.GroupID, work); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("duplicate source/classification operations: %d", calls)
	}
	result, _ := s.GetMultiModeCreation(ctx, "alice", group.GroupID)
	for i, b := range result.Batches {
		if len(b.Books) != 2 || b.Books[0].TxtText != "full-738579" {
			t.Fatalf("missing full source: %d %+v", i, b.Books)
		}
		want := "女频"
		if i == 0 {
			want = "男频"
		}
		if b.Books[0].SourceMetadata["gender"] != want {
			t.Fatalf("lost manual or shared classification: %d %v", i, b.Books[0].SourceMetadata)
		}
	}
	// Subsequent mode-local edits never propagate to the other three projects.
	before, _ := json.Marshal(result.Batches[1].Books)
	first := result.Batches[0]
	_, err = s.CaptureBookSource(ctx, "alice", first.ID, first.Books[0].ID, CaptureBookSourceInput{ExpectedRevision: first.Books[0].Revision, SourceText: "manual new story", ReplaceSource: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DeliverMultiModeSources(ctx, "alice", group.GroupID); err != nil {
		t.Fatal(err)
	}
	reread, _ := s.GetBatch(ctx, "alice", result.Batches[1].ID)
	after, _ := json.Marshal(reread.Books)
	if string(before) != string(after) {
		t.Fatal("redelivery changed independent target")
	}
}
func TestMultiModeSourceLeaseAndFailureResume(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	group, err := s.CreateMultiModeBatches(ctx, "alice", CreateMultiModeInput{RequestID: "retry", Title: "g", Books: []CreateBookInput{{BookID: "738579", Title: "b"}}, Projects: []ModeProjectInput{{Mode: "text"}, {Mode: "comic"}}})
	if err != nil {
		t.Fatal(err)
	}
	jobs, _ := s.ListMultiModeSources(ctx, "alice", group.GroupID)
	claim, ok, err := s.ClaimMultiModeSource(ctx, "alice", group.GroupID, jobs[0].Key)
	if err != nil || !ok {
		t.Fatal(err)
	}
	if _, ok, err = s.ClaimMultiModeSource(ctx, "alice", group.GroupID, jobs[0].Key); err != nil || ok {
		t.Fatal("duplicate source lease")
	}
	if err = s.CompleteMultiModeSource(ctx, "alice", group.GroupID, jobs[0].Key, claim.Token, MultiModeSourceSnapshot{}, fmt.Errorf("temporary source failure")); err != nil {
		t.Fatal(err)
	}
	calls := 0
	err = ResolveMultiModeSources(ctx, s, "alice", group.GroupID, func(context.Context, CreateBookInput) (MultiModeSourceSnapshot, error) {
		calls++
		return MultiModeSourceSnapshot{OriginalRaw: "raw source"}, nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("resume: %d %v", calls, err)
	}
}

func TestMultiModeControlsSurviveSharedDelivery(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	input := CreateMultiModeInput{RequestID: "controls", Title: "group", Books: []CreateBookInput{{BookID: "one", Title: "one"}}, Projects: []ModeProjectInput{{Mode: "video", Settings: SettingsPatch{"contentRangeLines": json.RawMessage(`12`)}}, {Mode: "comic", Settings: SettingsPatch{"comicAnalysisCharacters": json.RawMessage(`3000`)}}}}
	group, err := s.CreateMultiModeBatches(ctx, "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	err = ResolveMultiModeSources(ctx, s, "alice", group.GroupID, func(context.Context, CreateBookInput) (MultiModeSourceSnapshot, error) {
		return MultiModeSourceSnapshot{OriginalRaw: "full", Metadata: map[string]any{"contentRangeLines": float64(5), "comicAnalysisCharacters": float64(2000)}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	group, _ = s.GetMultiModeCreation(ctx, "alice", group.GroupID)
	if group.Batches[0].Books[0].SourceMetadata["contentRangeLines"] != float64(12) || group.Batches[1].Books[0].SourceMetadata["comicAnalysisCharacters"] != float64(3000) {
		t.Fatalf("mode controls overwritten: %+v", group.Batches)
	}
}
