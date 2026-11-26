package batchfactoryv11

import (
	"context"
	"testing"
)

func TestComicStyleAssetImagesKeepIndependentPrimary(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()
	batch, err := store.CreateBatch(ctx, "alice", CreateBatchInput{Title: "styles", Books: []CreateBookInput{{Title: "book"}}})
	if err != nil {
		t.Fatal(err)
	}
	book := batch.Books[0]
	asset, err := store.CreateBookAsset(ctx, "alice", batch.ID, book.ID, CreateBookAssetInput{Kind: "character", Name: "person", Prompt: "person description", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	a, b := ComicStyleContext(ctx, "A"), ComicStyleContext(ctx, "B")
	ia, err := store.CreateBookAssetImage(a, "alice", batch.ID, book.ID, asset.ID, CreateBookAssetImageInput{URL: "https://example.test/ink.png"})
	if err != nil {
		t.Fatal(err)
	}
	ib, err := store.CreateBookAssetImage(b, "alice", batch.ID, book.ID, asset.ID, CreateBookAssetImageInput{URL: "https://example.test/photo.png"})
	if err != nil {
		t.Fatal(err)
	}
	if !ia.IsPrimary || !ib.IsPrimary || ia.StyleID != "A" || ib.StyleID != "B" {
		t.Fatal("shared primary across styles")
	}
	if _, err := store.SetPrimaryBookAssetImage(a, "alice", batch.ID, book.ID, asset.ID, ib.ID); err == nil {
		t.Fatal("changed foreign style reference")
	}
	aa, _ := store.ListBookAssetImages(a, "alice", batch.ID, book.ID, asset.ID)
	bb, _ := store.ListBookAssetImages(b, "alice", batch.ID, book.ID, asset.ID)
	if len(aa) != 1 || len(bb) != 1 || aa[0].ID != ia.ID || bb[0].ID != ib.ID {
		t.Fatal("mixed asset images")
	}
}
