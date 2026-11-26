//go:build mysql84

package batchfactoryv11_test

import (
	"context"
	"database/sql"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"os"
	bf "qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/storage"
	"regexp"
	"testing"
	"time"
)

func TestComicStyleMySQLIsolationAndUpgrade(t *testing.T) {
	dsn := os.Getenv("QIANTIE_TEST_COMIC_STYLES_DSN")
	if dsn == "" {
		t.Skip("requires a disposable comic-style MySQL database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	template := os.Getenv("QIANTIE_TEST_COMIC_STYLES_TEMPLATE_DB")
	if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(template) {
		t.Fatal("requires a verified template schema identifier")
	}
	// Copy only empty schemas: replaying every historical migration is unrelated
	// to this upgrade and is prohibitively slow on the Windows Docker filesystem.
	for _, suffix := range []string{"batches", "batch_records", "books", "book_records", "videos", "video_records", "comic_pages", "comic_page_images", "comic_collections", "comic_collection_items", "book_assets", "book_asset_images"} {
		table := "batch_factory_v12_" + suffix
		if _, err = db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS "+table+" LIKE "+template+"."+table); err != nil {
			t.Fatal(err)
		}
	}
	store := bf.NewMySQLStore(db)
	batch, err := store.CreateBatch(ctx, "alice", bf.CreateBatchInput{Title: "style-upgrade", Books: []bf.CreateBookInput{{Title: "book", Videos: []bf.CreateVideoInput{{Label: "one"}, {Label: "two"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	book := batch.Books[0]
	v := book.Videos[0].ID
	legacyPageID, legacyImageID := batch.ID+"-old-page", batch.ID+"-old-image"
	if _, err = db.ExecContext(ctx, `INSERT INTO batch_factory_v12_comic_pages(id,owner_username,batch_id,book_id,video_id,comic_prompt,image_prompt,aspect_ratio,status,revision,created_at,updated_at) VALUES(?,'alice',?,?,?,'old story','old image','3:4','image_ready',1,NOW(6),NOW(6))`, legacyPageID, batch.ID, book.ID, v); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO batch_factory_v12_comic_page_images(id,owner_username,comic_page_id,storage_ref,media_type,source,is_primary,revision,created_at) VALUES(?,'alice',?,'legacy.png','image/png','provider',TRUE,1,NOW(6))`, legacyImageID, legacyPageID); err != nil {
		t.Fatal(err)
	}
	if err = storage.RunMigrations(ctx, db, storage.ComicStyleOutputMigrations()); err != nil {
		t.Fatal(err)
	}
	if err = store.AdoptLegacyComicStyle(ctx, "alice", batch.ID, book.ID, bf.ComicStyleOutput{PresetSnapshot: bf.PresetSnapshot{ID: "original-style", Name: "original"}}); err != nil {
		t.Fatal(err)
	}
	asset, err := store.CreateBookAsset(ctx, "alice", batch.ID, book.ID, bf.CreateBookAssetInput{Kind: "character", Name: "person", Prompt: "shared appearance", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	for _, styleID := range []string{"A", "B"} {
		scoped, err := bf.ResolveComicStyleContext(ctx, store, "alice", batch.ID, book.ID, styleID)
		if err != nil {
			t.Fatal(err)
		}
		image, err := store.CreateBookAssetImage(scoped, "alice", batch.ID, book.ID, asset.ID, bf.CreateBookAssetImageInput{URL: "https://example.test/" + styleID + ".png"})
		if err != nil || !image.IsPrimary || image.StyleID != styleID {
			t.Fatalf("asset scope: %+v %v", image, err)
		}
		images, err := store.ListBookAssetImages(scoped, "alice", batch.ID, book.ID, asset.ID)
		if err != nil || len(images) != 1 || images[0].ID != image.ID {
			t.Fatalf("asset list scope: %+v %v", images, err)
		}
	}
	a, b := store.ForComicStyle("A"), store.ForComicStyle("B")
	for _, output := range []bf.ComicPageStore{a, b} {
		for _, video := range book.Videos {
			page, err := output.UpsertComicPage(ctx, "alice", batch.ID, book.ID, video.ID, bf.UpsertComicPageInput{ComicPrompt: "shared story", ImagePrompt: "scoped image"})
			if err != nil {
				t.Fatal(err)
			}
			image, err := output.CreateComicPageImage(ctx, "alice", batch.ID, book.ID, page.ID, bf.CreateComicPageImageInput{StorageRef: page.ID + ".png", Source: "provider"})
			if err != nil {
				t.Fatal(err)
			}
			again, err := output.CreateComicPageImage(ctx, "alice", batch.ID, book.ID, page.ID, bf.CreateComicPageImageInput{StorageRef: page.ID + ".png", Source: "provider"})
			if err != nil || again.ID != image.ID {
				t.Fatalf("image save not idempotent: %v", err)
			}
		}
	}
	pa, err := a.ListComicPages(ctx, "alice", batch.ID, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	pb, err := b.ListComicPages(ctx, "alice", batch.ID, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pa) != 2 || len(pb) != 2 || pa[0].ID == pb[0].ID || pa[0].StyleID != "A" || pb[0].StyleID != "B" {
		t.Fatal("mixed style pages")
	}
	results := make(chan error, 2)
	for _, output := range []bf.ComicPageStore{a, b} {
		go func(output bf.ComicPageStore) {
			for i := 0; i < 4; i++ {
				if _, err := output.UpsertComicPage(ctx, "alice", batch.ID, book.ID, v, bf.UpsertComicPageInput{ComicPrompt: "shared story", ImagePrompt: fmt.Sprintf("updated image %d", i), Status: "image_ready"}); err != nil {
					results <- err
					return
				}
			}
			results <- nil
		}(output)
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent style saves: %v", err)
		}
	}
	if _, err := a.SetPrimaryComicPageImage(ctx, "alice", batch.ID, book.ID, pb[0].ID, pb[0].Images[0].ID); err == nil {
		t.Fatal("changed foreign style main image")
	}
	for _, output := range []bf.ComicPageStore{a, b} {
		if _, _, err := output.CreateComicCollection(ctx, "alice", batch.ID, book.ID); err != nil {
			t.Fatal(err)
		}
	}
	ca, err := a.ListComicCollections(ctx, "alice", batch.ID, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := b.ListComicCollections(ctx, "alice", batch.ID, book.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ca) != 1 || len(cb) != 1 || ca[0].StyleID != "A" || cb[0].StyleID != "B" || ca[0].Items[0].ComicPageID != pa[0].ID || cb[0].Items[0].ComicPageID != pb[0].ID {
		t.Fatal("mixed collections")
	}
	if err = storage.RunMigrations(ctx, db, storage.ComicStyleOutputMigrations()); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.ListComicPages(ctx, "alice", batch.ID, book.ID)
	if err != nil || len(legacy) != 1 || legacy[0].ID != legacyPageID || legacy[0].Images[0].ID != legacyImageID || !legacy[0].Images[0].IsPrimary {
		t.Fatalf("changed legacy image: %+v %v", legacy, err)
	}
	bound, err := store.ForComicStyle("original-style").ListComicPages(ctx, "alice", batch.ID, book.ID)
	if err != nil || len(bound) != 1 || bound[0].ID != legacyPageID || bound[0].StyleID != "original-style" {
		t.Fatalf("legacy scope not bound: %v %v", bound, err)
	}
}
