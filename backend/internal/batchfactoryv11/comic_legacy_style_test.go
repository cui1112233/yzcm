package batchfactoryv11

import (
	"context"
	"testing"
)

func TestComicLegacyStyleAdoptionPreservesIDsAndDoesNotRebind(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	batch, err := store.CreateBatch(ctx, "alice", CreateBatchInput{Title: "old", Books: []CreateBookInput{{Title: "book", Videos: []CreateVideoInput{{Label: "one"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	book := batch.Books[0]
	video := book.Videos[0]
	page, err := store.UpsertComicPage(ctx, "alice", batch.ID, book.ID, video.ID, UpsertComicPageInput{ComicPrompt: "old story", ImagePrompt: "old image"})
	if err != nil {
		t.Fatal(err)
	}
	image, err := store.CreateComicPageImage(ctx, "alice", batch.ID, book.ID, page.ID, CreateComicPageImageInput{URL: "https://example.test/old.png"})
	if err != nil {
		t.Fatal(err)
	}
	style := ComicStyleOutput{PresetSnapshot: PresetSnapshot{ID: "old-style", Name: "old"}}
	if err := store.AdoptLegacyComicStyle(ctx, "alice", batch.ID, book.ID, style); err != nil {
		t.Fatal(err)
	}
	// The provider still holds the pre-switch physical scope and revision.
	if _, err := store.UpsertComicPage(ctx, "alice", batch.ID, book.ID, video.ID, UpsertComicPageInput{ComicPrompt: "old story", ImagePrompt: "completed image", ExpectedRevision: page.Revision + 1}); err != nil {
		t.Fatalf("lost in-flight legacy result: %v", err)
	}
	pages, err := store.ForComicStyle("old-style").ListComicPages(ctx, "alice", batch.ID, book.ID)
	if err != nil || len(pages) != 1 || pages[0].ID != page.ID || pages[0].Images[0].ID != image.ID || !pages[0].Images[0].IsPrimary {
		t.Fatal("legacy image replaced")
	}
	style.ID = "new-style"
	if err := store.AdoptLegacyComicStyle(ctx, "alice", batch.ID, book.ID, style); err != nil {
		t.Fatal(err)
	}
	newer, _ := store.ForComicStyle("new-style").ListComicPages(ctx, "alice", batch.ID, book.ID)
	if len(newer) != 0 {
		t.Fatal("rebound existing output to new style")
	}
}
