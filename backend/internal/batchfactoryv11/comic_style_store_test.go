package batchfactoryv11

import (
	"context"
	"testing"
)

func TestComicStyleStoreIsolatesPagesAndCollections(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	batch, err := store.CreateBatch(ctx, "alice", CreateBatchInput{Title: "styles", Books: []CreateBookInput{{Title: "book", Videos: []CreateVideoInput{{Label: "1"}, {Label: "2"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	book := batch.Books[0]
	a, b := store.ForComicStyle("A"), store.ForComicStyle("B")
	for _, output := range []ComicPageStore{a, b} {
		for _, video := range book.Videos {
			page, err := output.UpsertComicPage(ctx, "alice", batch.ID, book.ID, video.ID, UpsertComicPageInput{ComicPrompt: "story", ImagePrompt: "image"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := output.CreateComicPageImage(ctx, "alice", batch.ID, book.ID, page.ID, CreateComicPageImageInput{URL: "https://example.test/image.png", Source: "provider"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	pagesA, _ := a.ListComicPages(ctx, "alice", batch.ID, book.ID)
	pagesB, _ := b.ListComicPages(ctx, "alice", batch.ID, book.ID)
	if len(pagesA) != 2 || len(pagesB) != 2 || pagesA[0].ID == pagesB[0].ID || pagesA[0].StyleID != "A" || pagesB[0].StyleID != "B" {
		t.Fatalf("style pages not isolated: %+v / %+v", pagesA, pagesB)
	}
	if _, err := a.SetPrimaryComicPageImage(ctx, "alice", batch.ID, book.ID, pagesB[0].ID, pagesB[0].Images[0].ID); err == nil {
		t.Fatal("A changed B primary")
	}
	if _, err := a.CreateComicPageImage(ctx, "alice", batch.ID, book.ID, pagesB[0].ID, CreateComicPageImageInput{URL: "https://example.test/foreign.png", Source: "provider"}); err == nil {
		t.Fatal("A wrote to B")
	}
	for _, output := range []ComicPageStore{a, b} {
		if _, _, err := output.CreateComicCollection(ctx, "alice", batch.ID, book.ID); err != nil {
			t.Fatal(err)
		}
	}
	ca, _ := a.ListComicCollections(ctx, "alice", batch.ID, book.ID)
	cb, _ := b.ListComicCollections(ctx, "alice", batch.ID, book.ID)
	if len(ca) != 1 || len(cb) != 1 || ca[0].ID == cb[0].ID || ca[0].Items[0].ComicPageID != pagesA[0].ID || cb[0].Items[0].ComicPageID != pagesB[0].ID {
		t.Fatal("mixed collections")
	}
	if _, err := a.ListComicPages(ctx, "bob", batch.ID, book.ID); err == nil {
		t.Fatal("foreign owner read")
	}
	legacy, _ := store.ListComicPages(ctx, "alice", batch.ID, book.ID)
	if len(legacy) != 0 {
		t.Fatal("unscoped view mixed styles")
	}
}
