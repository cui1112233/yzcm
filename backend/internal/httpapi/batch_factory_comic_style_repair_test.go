package httpapi

import (
	"context"
	"encoding/json"
	"qiantie/backend/internal/batchfactoryv11"
	"testing"
)

func TestComicStyleRepairNeverWritesSharedStoryboard(t *testing.T) {
	s := batchfactoryv11.NewMemoryStore()
	ctx := context.Background()
	settings := batchfactoryv11.SettingsPatch{"productionMode": json.RawMessage(`"comic"`), "aiPromptConfig": json.RawMessage(`{"comic":{"style":{"presetId":"A","body":"ink"},"styles":[{"presetId":"A","body":"ink"},{"presetId":"B","body":"photo"}]}}`)}
	batch, err := s.CreateBatch(ctx, "alice", batchfactoryv11.CreateBatchInput{Title: "repair", InitialBatchSettings: settings, Books: []batchfactoryv11.CreateBookInput{{Title: "book", Videos: []batchfactoryv11.CreateVideoInput{{Label: "v", VisualPrompt: "shared visual"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	book := batch.Books[0]
	video := book.Videos[0]
	scoped, err := batchfactoryv11.ResolveComicStyleContext(ctx, s, "alice", batch.ID, book.ID, "A")
	if err != nil {
		t.Fatal(err)
	}
	input := batchfactoryv11.UpsertComicPageInput{ComicPrompt: "shared story", ImagePrompt: "original frame"}
	page, err := s.UpsertComicPage(scoped, "alice", batch.ID, book.ID, video.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	effective, err := batchfactoryv11.ResolveComicStyleSettings(settings, "A")
	if err != nil {
		t.Fatal(err)
	}
	g := comicPageGenerator{store: s}
	_, _, err = g.persistRepairedComicPrompt(scoped, s, "alice", batch, book, video, page, input, effective, "repaired A only", nil)
	if err != nil {
		t.Fatal(err)
	}
	reread, err := s.GetBatch(ctx, "alice", batch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.Books[0].Videos[0].ComicImagePrompt != "" {
		t.Fatal("A repair changed shared video prompt")
	}
	pages, err := s.ForComicStyle("A").ListComicPages(ctx, "alice", batch.ID, book.ID)
	if err != nil || pages[0].ImagePrompt != "repaired A only" {
		t.Fatal("A repair not persisted", err)
	}
	if pages, err = s.ForComicStyle("B").ListComicPages(ctx, "alice", batch.ID, book.ID); err != nil || len(pages) != 0 {
		t.Fatal("A repair created B data", err)
	}
}
