package httpapi

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"qiantie/backend/internal/novelfetchworkshop"
)

func assertSelectionRoundTrip(t *testing.T, job novelfetchworkshop.ProcessJob, expected []string) {
	t.Helper()
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatal(err)
	}
	var queued novelfetchworkshop.ProcessJob
	if err = json.Unmarshal(raw, &queued); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(queued.SelectedVersions, expected) {
		t.Fatalf("selection changed across document recovery and queue JSON: got=%#v want=%#v json=%s", queued.SelectedVersions, expected, raw)
	}
	if !queued.AutoSubmit {
		t.Fatal("legacy automatic upload intent was lost")
	}
}

func TestSelectionSnapshotKeepsNilAndEmptyThroughRecovery(t *testing.T) {
	for _, versions := range [][]string{nil, {}, {"ai5"}} {
		ctx := context.Background()
		s := novelfetchworkshop.NewMemoryStore()
		if err := s.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Meta: map[string]any{}}); err != nil {
			t.Fatal(err)
		}
		job := novelfetchworkshop.ProcessJob{Owner: "alice", BookIDs: []string{"123"}, ID: "snapshot", BatchID: "batch", PlatformID: "2", AutoSubmit: true, SelectedVersions: versions}
		if err := saveNovelFetchJobSnapshot(ctx, s, job); err != nil {
			t.Fatal(err)
		}
		doc, err := s.GetDocument(ctx, "alice", "123")
		if err != nil {
			t.Fatal(err)
		}
		assertSelectionRoundTrip(t, novelFetchRecoveryJob(doc, nil, "alice", true), versions)
		delete(doc.Meta, "nextSelectedVersions")
		assertSelectionRoundTrip(t, novelFetchRecoveryJob(doc, nil, "alice", true), versions)
	}
}

func TestSelectionAdmissionSnapshotKeepsNilAndEmptyThroughRecovery(t *testing.T) {
	for _, versions := range [][]string{nil, {}, {"ai5"}} {
		ctx := context.Background()
		s := novelfetchworkshop.NewMemoryStore()
		q := novelfetchworkshop.NewMemoryQueue()
		if err := s.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Meta: map[string]any{}}); err != nil {
			t.Fatal(err)
		}
		doc, _ := s.GetDocument(ctx, "alice", "123")
		job := novelfetchworkshop.ProcessJob{Owner: "alice", BookIDs: []string{"123"}, ID: "admitted", BatchID: "batch", PlatformID: "2", AutoSubmit: true, SelectedVersions: versions}
		admitted, err := stageWorkshopSingleJob(ctx, s, q, doc, job, false, false)
		if err != nil || !admitted.queued {
			t.Fatalf("admission=%+v err=%v", admitted, err)
		}
		doc, err = s.GetDocument(ctx, "alice", "123")
		if err != nil {
			t.Fatal(err)
		}
		assertSelectionRoundTrip(t, novelFetchRecoveryJob(doc, nil, "alice", true), versions)
	}
}

func TestSelectionSnapshotReaderPreservesTypedEmptyAndNull(t *testing.T) {
	for _, value := range []any{[]string{}, []any{}} {
		if got := readNovelFetchSelectedVersions(value); got == nil || len(got) != 0 {
			t.Fatalf("explicit typed empty lost: %#v => %#v", value, got)
		}
	}
	for _, value := range []any{nil, []string(nil), []any(nil)} {
		if got := readNovelFetchSelectedVersions(value); got != nil {
			t.Fatalf("omitted/null became explicit empty: %#v => %#v", value, got)
		}
	}
}
