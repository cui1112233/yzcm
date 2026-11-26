package scripteditor

import (
	"context"
	"strings"
	"testing"
)

type fakeScriptGenerator struct{ system, user string }

func (f *fakeScriptGenerator) Generate(_ context.Context, system, user, _ string) (string, error) {
	f.system, f.user = system, user
	return "第1集\n1-1 夜 内\n女主：我回来了。", nil
}

func TestGenerateScriptSavesANewVersion(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)
	work, _ := store.CreateWork(ctx, "alice", CreateWorkInput{Title: "归途", Type: WorkNovel})
	chapter, _ := store.CreateChapter(ctx, "alice", work.ID, CreateChapterInput{Title: "第一章", Content: "女主雨夜归乡"})
	generator := &fakeScriptGenerator{}
	script, err := service.GenerateScript(ctx, "alice", work.ID, GenerateScriptInput{Episode: 1, ChapterIDs: []string{chapter.ID}, ModelID: "text-main", Extra: "节奏紧凑"}, generator)
	if err != nil {
		t.Fatal(err)
	}
	if script.GenerationSource != SourceAI || script.ModelID != "text-main" || !strings.Contains(generator.system, "短剧") || !strings.Contains(generator.user, "女主雨夜归乡") {
		t.Fatalf("script=%+v system=%q user=%q", script, generator.system, generator.user)
	}
}

func TestGenerateScriptRejectsMissingModelBeforeCall(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store)
	work, _ := store.CreateWork(context.Background(), "alice", CreateWorkInput{Title: "归途", Type: WorkNovel})
	if _, err := service.GenerateScript(context.Background(), "alice", work.ID, GenerateScriptInput{Episode: 1}, &fakeScriptGenerator{}); err == nil {
		t.Fatal("missing model must fail")
	}
}
