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

func TestSearchWorkReturnsTitleAndBodyOffsets(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)
	work, _ := store.CreateWork(ctx, "alice", CreateWorkInput{Title: "月台故事", Type: WorkNovel})
	chapter, _ := store.CreateChapter(ctx, "alice", work.ID, CreateChapterInput{Title: "重逢", Content: "他在深夜月台等待列车", Order: 1})
	hits, err := service.SearchWork(ctx, "alice", work.ID, "月台")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %#v", hits)
	}
	if hits[0].ChapterID != chapter.ID || hits[0].Start < 0 || hits[0].End <= hits[0].Start {
		t.Fatalf("body hit = %#v", hits[0])
	}
	if _, err := service.SearchWork(ctx, "bob", work.ID, "月台"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign search error = %v", err)
	}
}

func TestImportIntoWorkAppendsAndReplacesWithRevision(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)
	work, _ := store.CreateWork(ctx, "alice", CreateWorkInput{Title: "作品", Type: WorkNovel})
	chapter, _ := store.CreateChapter(ctx, "alice", work.ID, CreateChapterInput{Title: "旧章", Content: "旧内容", Order: 1})
	sections := []ImportSection{{ID: "s1", Title: "新章", Content: "新内容"}}
	result, err := service.ImportIntoWork(ctx, "alice", work.ID, ImportIntoWorkInput{Mode: ImportAppend, Sections: sections, SelectedSectionIDs: []string{"s1"}})
	if err != nil || len(result.Chapters) != 2 {
		t.Fatalf("append = %#v, %v", result, err)
	}
	result, err = service.ImportIntoWork(ctx, "alice", work.ID, ImportIntoWorkInput{Mode: ImportReplaceChapter, ChapterID: chapter.ID, ExpectedRevision: chapter.Revision, Sections: sections, SelectedSectionIDs: []string{"s1"}})
	if err != nil || result.Chapters[0].Title != "新章" || result.Chapters[0].Content != "新内容" {
		t.Fatalf("replace = %#v, %v", result, err)
	}
	_, err = service.ImportIntoWork(ctx, "alice", work.ID, ImportIntoWorkInput{Mode: ImportReplaceChapter, ChapterID: chapter.ID, ExpectedRevision: chapter.Revision, Sections: sections, SelectedSectionIDs: []string{"s1"}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("stale replace error = %v", err)
	}
}

func TestImportScriptsNeverOverwritesExistingEpisodeVersion(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)
	work, _ := store.CreateWork(ctx, "alice", CreateWorkInput{Title: "剧本", Type: WorkScript})
	_, _ = store.CreateScript(ctx, "alice", work.ID, CreateScriptInput{Episode: 1, Version: 1, Title: "第一集", Content: "旧版"})
	rows, err := service.ImportScripts(ctx, "alice", work.ID, []CreateScriptInput{{Episode: 1, Version: 1, Title: "第一集", Content: "新版"}}, "import-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Version != 2 || rows[0].Content != "新版" {
		t.Fatalf("rows = %#v", rows)
	}
	again, err := service.ImportScripts(ctx, "alice", work.ID, []CreateScriptInput{{Episode: 1, Content: "重复"}}, "import-1")
	if err != nil || len(again) != 1 || again[0].ID != rows[0].ID {
		t.Fatalf("idempotent import = %#v, %v", again, err)
	}
}

func TestReplaceScriptsValidatesAllRevisionsBeforeWriting(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)
	work, _ := store.CreateWork(ctx, "alice", CreateWorkInput{Title: "剧本", Type: WorkScript})
	first, _ := store.CreateScript(ctx, "alice", work.ID, CreateScriptInput{Episode: 1, Content: "甲旧"})
	second, _ := store.CreateScript(ctx, "alice", work.ID, CreateScriptInput{Episode: 1, Content: "乙旧"})
	_, err := service.ReplaceScripts(ctx, "alice", work.ID, ReplaceScriptsInput{Scope: ReplaceEpisode, ScriptID: first.ID, Find: "旧", Replace: "新", ExpectedRevisions: map[string]int64{first.ID: first.Revision, second.ID: 99}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("replace error = %v", err)
	}
	current, _ := store.GetWork(ctx, "alice", work.ID)
	if current.Scripts[0].Content != "甲旧" || current.Scripts[1].Content != "乙旧" {
		t.Fatalf("partial replacement: %#v", current.Scripts)
	}
}
