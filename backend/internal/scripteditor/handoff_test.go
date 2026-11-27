package scripteditor

import (
	"context"
	"testing"

	"qiantie/backend/internal/screenplay"
)

func TestComicHandoffIsIdempotentAndPreservesEpisodes(t *testing.T) {
	ctx := context.Background()
	store := newDeterministicMemoryStore()
	work, _ := store.CreateWork(ctx, "alice", CreateWorkInput{Title: "测试作品", Type: WorkNovel})
	one, _ := store.CreateScript(ctx, "alice", work.ID, CreateScriptInput{Episode: 1, Content: "第1集\n\n第一集正文"})
	two, _ := store.CreateScript(ctx, "alice", work.ID, CreateScriptInput{Episode: 2, Content: "第二集正文"})
	one, _ = store.PreferScript(ctx, "alice", work.ID, one.ID, one.Revision)
	two, _ = store.PreferScript(ctx, "alice", work.ID, two.ID, two.Revision)
	comicStore := screenplay.NewMemoryStore()
	service := NewService(store)
	service.ComicStore = comicStore
	service.ComicAuthoring = screenplay.NewMemoryAuthoringRepository()

	input := HandoffInput{RequestKey: "handoff-1", Episodes: []int{1, 2}, Title: "测试漫剧"}
	first, err := service.CreateComicHandoff(ctx, "alice", work.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateComicHandoff(ctx, "alice", work.ID, input)
	if err != nil || first.ScreenplayProjectID != second.ScreenplayProjectID {
		t.Fatalf("first=%#v second=%#v err=%v", first, second, err)
	}
	projects, _ := comicStore.ListProjects(ctx, "alice")
	if len(projects) != 1 {
		t.Fatalf("projects=%d", len(projects))
	}
	doc, err := comicStore.ActiveDocument(ctx, "alice", first.ScreenplayProjectID)
	if err != nil || doc.EpisodeCount != 2 || doc.Content != "# 第1集\n\n第一集正文\n\n# 第2集\n\n第二集正文" {
		t.Fatalf("document=%#v err=%v", doc, err)
	}
	state, err := service.ComicAuthoring.Load(ctx, "alice", first.ScreenplayProjectID)
	if err != nil || len(state.Chapters) != 2 || state.Chapters[0].ScriptText != "第一集正文" || state.Chapters[1].ScriptText != "第二集正文" {
		t.Fatalf("authoring=%#v err=%v", state, err)
	}
}

func TestComicHandoffRequiresPreferredVersion(t *testing.T) {
	ctx := context.Background()
	store := newDeterministicMemoryStore()
	work, _ := store.CreateWork(ctx, "alice", CreateWorkInput{Title: "测试作品", Type: WorkNovel})
	_, _ = store.CreateScript(ctx, "alice", work.ID, CreateScriptInput{Episode: 1, Content: "草稿"})
	service := NewService(store)
	service.ComicStore = screenplay.NewMemoryStore()
	service.ComicAuthoring = screenplay.NewMemoryAuthoringRepository()
	if _, err := service.CreateComicHandoff(ctx, "alice", work.ID, HandoffInput{RequestKey: "x", Episodes: []int{1}, Title: "漫剧"}); err == nil {
		t.Fatal("expected preferred-version validation error")
	}
}
