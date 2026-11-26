package scripteditor

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newDeterministicMemoryStore() *MemoryStore {
	s := NewMemoryStore()
	now := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	n := 0
	s.newID = func(prefix string) string {
		n++
		return prefix + "-" + string(rune('0'+n))
	}
	return s
}

func TestStoreKeepsOnePreferredScriptPerEpisode(t *testing.T) {
	ctx := context.Background()
	s := newDeterministicMemoryStore()
	work, err := s.CreateWork(ctx, "alice", CreateWorkInput{Title: "测试", Type: WorkNovel})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateScript(ctx, "alice", work.ID, CreateScriptInput{Episode: 1, Content: "A"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateScript(ctx, "alice", work.ID, CreateScriptInput{Episode: 1, Content: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PreferScript(ctx, "alice", work.ID, a.ID, a.Revision); err != nil {
		t.Fatal(err)
	}
	preferred, err := s.PreferScript(ctx, "alice", work.ID, b.ID, b.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !preferred.Preferred {
		t.Fatal("selected script is not preferred")
	}
	rows, err := s.ListScripts(ctx, "alice", work.ID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.Episode == 1 && row.Preferred {
			count++
			if row.ID != b.ID {
				t.Fatalf("preferred id = %s, want %s", row.ID, b.ID)
			}
		}
	}
	if count != 1 {
		t.Fatalf("preferred count = %d, want 1", count)
	}
}

func TestStoreRejectsForeignAndStaleWrites(t *testing.T) {
	ctx := context.Background()
	s := newDeterministicMemoryStore()
	work, _ := s.CreateWork(ctx, "alice", CreateWorkInput{Title: "甲", Type: WorkNovel})
	chapter, _ := s.CreateChapter(ctx, "alice", work.ID, CreateChapterInput{Title: "第一章", Content: "正文"})
	if _, err := s.GetWork(ctx, "bob", work.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign read error = %v, want not found", err)
	}
	newContent := "新正文"
	if _, err := s.UpdateChapter(ctx, "alice", work.ID, chapter.ID, UpdateChapterInput{Content: &newContent}, chapter.Revision+1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale update error = %v, want conflict", err)
	}
	got, err := s.UpdateChapter(ctx, "alice", work.ID, chapter.ID, UpdateChapterInput{Content: &newContent}, chapter.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "新正文" || got.Revision != chapter.Revision+1 {
		t.Fatalf("updated chapter = %#v", got)
	}
}

func TestStoreCopiesNextVersionAndSoftDeletesWork(t *testing.T) {
	ctx := context.Background()
	s := newDeterministicMemoryStore()
	work, _ := s.CreateWork(ctx, "alice", CreateWorkInput{Title: "甲", Type: WorkScript})
	original, _ := s.CreateScript(ctx, "alice", work.ID, CreateScriptInput{Episode: 2, Content: "原稿"})
	copy, err := s.CopyScript(ctx, "alice", work.ID, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if copy.Version != 2 || copy.Content != "原稿" || copy.GenerationSource != SourceCopy {
		t.Fatalf("copy = %#v", copy)
	}
	if err := s.DeleteWork(ctx, "alice", work.ID, work.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetWork(ctx, "alice", work.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted work read error = %v", err)
	}
}
