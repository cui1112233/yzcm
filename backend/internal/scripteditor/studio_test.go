package scripteditor

import (
	"context"
	"errors"
	"testing"
)

func TestCopyWorkCopiesContentWithoutDownstreamBindings(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)
	source, err := store.CreateWork(ctx, "alice", CreateWorkInput{Title: "长夜", Type: WorkNovel})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := store.CreateChapter(ctx, "alice", source.ID, CreateChapterInput{Title: "第一章", Content: "月台相遇", Order: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateScript(ctx, "alice", source.ID, CreateScriptInput{Episode: 1, Title: "第一集", Content: "开场", SourceChapterIDs: []string{chapter.ID}}); err != nil {
		t.Fatal(err)
	}

	copied, err := service.CopyWork(ctx, "alice", source.ID, "copy-once")
	if err != nil {
		t.Fatal(err)
	}
	if copied.Title != "长夜 副本" || len(copied.Chapters) != 1 || len(copied.Scripts) != 1 {
		t.Fatalf("copy = %#v", copied)
	}
	if copied.Chapters[0].ID == chapter.ID || copied.Scripts[0].SourceChapterIDs[0] != copied.Chapters[0].ID {
		t.Fatalf("copy did not remap stable chapter ids: %#v", copied)
	}
	if _, err = store.WorkBinding(ctx, "alice", copied.ID, BindingCMCanvas); !errors.Is(err, ErrNotFound) {
		t.Fatalf("copied binding error = %v, want ErrNotFound", err)
	}
	again, err := service.CopyWork(ctx, "alice", source.ID, "copy-once")
	if err != nil || again.ID != copied.ID {
		t.Fatalf("idempotent copy = %#v, %v", again, err)
	}
}

func TestMemoryStoreWorkBindingIsOwnerScoped(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	work, _ := store.CreateWork(ctx, "alice", CreateWorkInput{Title: "作品", Type: WorkNovel})
	row, err := store.UpsertWorkBinding(ctx, WorkBinding{OwnerUsername: "alice", WorkID: work.ID, TargetType: BindingCMCanvas, TargetID: "task-1", SyncState: SyncPending, ConflictState: ConflictNone})
	if err != nil {
		t.Fatal(err)
	}
	if row.ID == "" {
		t.Fatal("binding id missing")
	}
	if _, err = store.WorkBinding(ctx, "bob", work.ID, BindingCMCanvas); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign binding error = %v", err)
	}
}
