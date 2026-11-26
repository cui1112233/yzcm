package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"qiantie/backend/internal/batchfactoryv11"
	"testing"
	"time"
)

func TestComicStyleAutomationSixPagesOnlyRetriesTheMissingPage(t *testing.T) {
	ctx := context.Background()
	store := batchfactoryv11.NewMemoryStore()
	settings := batchfactoryv11.SettingsPatch{"productionMode": json.RawMessage(`"comic"`), "aiPromptConfig": json.RawMessage(`{"comic":{"styles":[{"presetId":"A","body":"INK_ONLY"},{"presetId":"B","body":"PHOTO_ONLY"},{"presetId":"C","body":"WATERCOLOR_ONLY"}]}}`)}
	batch, err := store.CreateBatch(ctx, "alice", batchfactoryv11.CreateBatchInput{Title: "six", InitialBatchSettings: settings, Books: []batchfactoryv11.CreateBookInput{{Title: "book", Videos: []batchfactoryv11.CreateVideoInput{{Label: "one"}, {Label: "two"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	book := batch.Books[0]
	paidCalls := map[string]int{}
	uploaded := map[string]int{}
	imageIDs := map[string]string{}
	c := &BatchAutomationCoordinator{store: store, now: time.Now}
	c.comicGenerate = func(ctx context.Context, o, b, k string) (batchfactoryv11.ComicCollection, error) {
		style := batchfactoryv11.ComicStyleOutputID(ctx)
		existing, err := store.ListComicPages(ctx, o, b, k)
		if err != nil {
			return batchfactoryv11.ComicCollection{}, err
		}
		frozen := ctx.Value(comicStyleFrozenSettingsKey{}).(batchfactoryv11.SettingsPatch)
		for i, video := range book.Videos {
			complete := false
			for _, p := range existing {
				if p.VideoID == video.ID {
					for _, image := range p.Images {
						if image.IsPrimary {
							complete = true
						}
					}
				}
			}
			if complete {
				continue
			}
			contract, err := batchfactoryv11.BuildComicImageContract(frozen, batchfactoryv11.ComicImagePurposePage, "", "same shared story "+video.ID, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			var config batchfactoryv11.AIReasoningPromptConfig
			_ = json.Unmarshal(frozen["aiPromptConfig"], &config)
			if contract.StylePresetID != style || config.Comic.Style.ID != style {
				t.Fatal("wrong frozen style contract")
			}
			key := style + "/" + video.ID
			paidCalls[key]++
			if style == "B" && i == 1 && paidCalls[key] == 1 {
				return batchfactoryv11.ComicCollection{}, errors.New("simulated B page2 provider failure")
			}
			page, err := store.UpsertComicPage(ctx, o, b, k, video.ID, batchfactoryv11.UpsertComicPageInput{ComicPrompt: "same story", ImagePrompt: "shared frame", Status: "image_ready"})
			if err != nil {
				t.Fatal(err)
			}
			image, err := store.CreateComicPageImage(ctx, o, b, k, page.ID, batchfactoryv11.CreateComicPageImageInput{URL: "https://example.test/" + page.ID + ".png", Source: "provider"})
			if err != nil {
				t.Fatal(err)
			}
			imageIDs[key] = image.ID
		}
		collection, _, err := store.CreateComicCollection(ctx, o, b, k)
		return collection, err
	}
	c.comicPublish = func(ctx context.Context, o, b, k string, col batchfactoryv11.ComicCollection, _ batchfactoryv11.SettingsPatch) (N8ComicPublishRecord, error) {
		id := batchfactoryv11.ComicStyleOutputID(ctx)
		if len(col.Items) != 2 {
			t.Fatal("incomplete collection uploaded")
		}
		for _, item := range col.Items {
			if imageIDs[id+"/"+item.VideoID] != item.ImageID {
				t.Fatal("mixed style collection")
			}
		}
		uploaded[id]++
		return N8ComicPublishRecord{Status: "published", CarouselID: 123}, nil
	}
	entry := map[string]any{}
	_, err = c.runComicStyleOutputs(ctx, "alice", batch.ID, book, settings, entry, func() {}, true, false)
	if err == nil {
		t.Fatal("missing page considered complete")
	}
	saved := map[string]string{}
	for key, id := range imageIDs {
		saved[key] = id
	}
	raw, _ := json.Marshal(entry)
	entry = map[string]any{}
	_ = json.Unmarshal(raw, &entry)
	result, err := c.runComicStyleOutputs(ctx, "alice", batch.ID, book, settings, entry, func() {}, true, false)
	if err != nil || result.Published != 3 || result.Pages != 6 {
		t.Fatalf("result %+v %v", result, err)
	}
	for key, id := range saved {
		if imageIDs[key] != id {
			t.Fatal("successful image replaced")
		}
	}
	for key, calls := range paidCalls {
		want := 1
		if key == "B/"+book.Videos[1].ID {
			want = 2
		}
		if calls != want {
			t.Fatalf("repeated generation %s=%d", key, calls)
		}
	}
	for _, id := range []string{"A", "B", "C"} {
		if uploaded[id] != 1 {
			t.Fatal("duplicate upload", uploaded)
		}
	}
}
