package novelfetchworkshop

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
	"time"
)

type conditionalBodyWriter interface {
	PutBodyIfDocumentUnchanged(context.Context, string, BodyRecord, Document) (BodyRef, error)
}

func TestMemoryConditionalBodyRejectsConcurrentStopAndRevision(t *testing.T) {
	for _, change := range []string{"stop", "body"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			s := NewMemoryStore()
			_ = s.PutDocument(ctx, "alice", Document{BookID: "book"})
			_, _ = s.PutBody(ctx, "alice", BodyRecord{BookID: "book", BodyRef: BodyRef{VersionID: "original"}, Content: "old"})
			snapshot, _ := s.GetDocument(ctx, "alice", "book")
			if change == "stop" {
				latest := snapshot
				latest.Meta = map[string]any{"status": "stopped"}
				_ = s.PutDocument(ctx, "alice", latest)
			} else {
				_, _ = s.PutBody(ctx, "alice", BodyRecord{BookID: "book", BodyRef: BodyRef{VersionID: "original"}, Content: "new user body"})
			}
			before, _ := s.GetBody(ctx, "alice", "book", "original")
			writer, ok := any(s).(conditionalBodyWriter)
			if !ok {
				t.Fatal("conditional body writer unavailable")
			}
			if _, err := writer.PutBodyIfDocumentUnchanged(ctx, "alice", BodyRecord{BookID: "book", BodyRef: BodyRef{VersionID: "original"}, Content: "stale model result"}, snapshot); !errors.Is(err, ErrDocumentChanged) {
				t.Fatalf("expected conflict, got %v", err)
			}
			after, _ := s.GetBody(ctx, "alice", "book", "original")
			if after.Revision != before.Revision || after.Content != before.Content {
				t.Fatal("conflicting write changed body")
			}
		})
	}
}
func TestMySQLConditionalBodyRejectsBeforeWriting(t *testing.T) {
	for _, change := range []string{"version", "stop", "body"} {
		t.Run(change, func(t *testing.T) {
			_, mock, s := newBodySQLMock(t)
			now := time.Now().UTC().Truncate(time.Microsecond)
			expected := Document{BookID: "book", UpdatedAt: now.Format(time.RFC3339Nano), BodyRefs: map[string]BodyRef{"original": {VersionID: "original", Revision: 1}}}
			current := cloneDocument(expected)
			stamp := now
			if change == "version" {
				stamp = now.Add(time.Microsecond)
			}
			if change == "stop" {
				current.Meta = map[string]any{"status": "stopped"}
			}
			if change == "body" {
				current.BodyRefs["original"] = BodyRef{VersionID: "original", Revision: 2}
			}
			raw, _ := json.Marshal(current)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT document_json, updated_at FROM novel_fetch_workshop_documents.*FOR UPDATE").WithArgs("alice", "book").WillReturnRows(sqlmock.NewRows([]string{"document_json", "updated_at"}).AddRow(raw, stamp))
			mock.ExpectRollback()
			writer, ok := any(s).(conditionalBodyWriter)
			if !ok {
				t.Fatal("conditional body writer unavailable")
			}
			_, err := writer.PutBodyIfDocumentUnchanged(context.Background(), "alice", BodyRecord{BookID: "book", BodyRef: BodyRef{VersionID: "original"}, Content: "new"}, expected)
			if !errors.Is(err, ErrDocumentChanged) {
				t.Fatalf("expected conflict got %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestMySQLConditionalBodyUsesExistingAtomicReferenceSwitch(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	expected := Document{BookID: "123", UpdatedAt: now.Format(time.RFC3339Nano), BodyRefs: map[string]BodyRef{"original": {VersionID: "original", Revision: 6}}}
	raw, _ := json.Marshal(expected)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT document_json, updated_at FROM novel_fetch_workshop_documents.*FOR UPDATE").WithArgs("alice", "123").WillReturnRows(sqlmock.NewRows([]string{"document_json", "updated_at"}).AddRow(raw, now))
	expectLockedBody(mock, "alice", "123", "original", 6, "gzip", []byte("previous"), "ready", nil, nil)
	mock.ExpectExec("UPDATE novel_fetch_workshop_bodies SET revision").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE novel_fetch_workshop_documents SET document_json=JSON_MERGE_PATCH").WithArgs(bodyPatchSQLArgument{version: "original", chars: 13, revision: 7}, "alice", "123").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	ref, err := s.PutBodyIfDocumentUnchanged(context.Background(), "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "original"}, Content: "现在正文已经修改为更长内容"}, expected)
	if err != nil || ref.Revision != 7 || ref.CharCount != 13 {
		t.Fatalf("ref=%+v err=%v", ref, err)
	}
	assertBodyExpectations(t, mock)
}
func TestMemoryConditionalBodySuccessPreservesOtherVersions(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	_ = s.PutDocument(ctx, "alice", Document{BookID: "book"})
	for _, v := range []string{"original_raw", "original", "ai1"} {
		_, _ = s.PutBody(ctx, "alice", BodyRecord{BookID: "book", BodyRef: BodyRef{VersionID: v}, Content: v})
	}
	d, _ := s.GetDocument(ctx, "alice", "book")
	raw, _ := s.GetBody(ctx, "alice", "book", "original_raw")
	ai, _ := s.GetBody(ctx, "alice", "book", "ai1")
	ref, err := s.PutBodyIfDocumentUnchanged(ctx, "alice", BodyRecord{BookID: "book", BodyRef: BodyRef{VersionID: "original"}, Content: "恢复"}, d)
	if err != nil || ref.Revision != 2 {
		t.Fatalf("ref=%+v err=%v", ref, err)
	}
	for _, want := range []BodyRecord{raw, ai} {
		got, _ := s.GetBody(ctx, "alice", "book", want.VersionID)
		if got.Revision != want.Revision || got.Content != want.Content {
			t.Fatal("unrelated body changed")
		}
	}
}

func TestMemoryConditionalBodyConcurrentWritersOnlyOneWins(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	_ = s.PutDocument(ctx, "alice", Document{BookID: "book"})
	_, _ = s.PutBody(ctx, "alice", BodyRecord{BookID: "book", BodyRef: BodyRef{VersionID: "original"}, Content: "old"})
	expected, _ := s.GetDocument(ctx, "alice", "book")
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, text := range []string{"first", "second"} {
		go func(value string) {
			<-start
			_, err := s.PutBodyIfDocumentUnchanged(ctx, "alice", BodyRecord{BookID: "book", BodyRef: BodyRef{VersionID: "original"}, Content: value}, expected)
			results <- err
		}(text)
	}
	close(start)
	success, conflict := 0, 0
	for range 2 {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, ErrDocumentChanged) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	body, _ := s.GetBody(ctx, "alice", "book", "original")
	if success != 1 || conflict != 1 || body.Revision != 2 {
		t.Fatalf("success=%d conflict=%d revision=%d", success, conflict, body.Revision)
	}
}
func TestConditionalBodyCancelledContextDoesNotWrite(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	_ = s.PutDocument(ctx, "alice", Document{BookID: "book"})
	d, _ := s.GetDocument(ctx, "alice", "book")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.PutBodyIfDocumentUnchanged(cancelled, "alice", BodyRecord{BookID: "book", BodyRef: BodyRef{VersionID: "original"}, Content: "new"}, d); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.GetBody(ctx, "alice", "book", "original"); !errors.Is(err, ErrNotFound) {
		t.Fatal("cancelled write persisted")
	}
}
