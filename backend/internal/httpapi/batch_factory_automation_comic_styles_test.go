package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"qiantie/backend/internal/batchfactoryv11"
	"testing"
	"time"
)

func TestComicStyleAutomationResumesOnlyFailedStyleAndPublishesOthers(t *testing.T) {
	store := batchfactoryv11.NewMemoryStore()
	settings := batchfactoryv11.SettingsPatch{"productionMode": json.RawMessage(`"comic"`), "aiPromptConfig": json.RawMessage(`{"comic":{"style":{"presetId":"A"},"styles":[{"presetId":"A","body":"ink"},{"presetId":"B","body":"photo"},{"presetId":"C","body":"watercolor"}]}}`)}
	batch, err := store.CreateBatch(context.Background(), "alice", batchfactoryv11.CreateBatchInput{Title: "styles", InitialBatchSettings: settings, Books: []batchfactoryv11.CreateBookInput{{Title: "book"}}})
	if err != nil {
		t.Fatal(err)
	}
	calls, published := map[string]int{}, map[string]int{}
	c := &BatchAutomationCoordinator{store: store, now: func() time.Time { return time.Now() }}
	c.comicGenerate = func(ctx context.Context, o, b, k string) (batchfactoryv11.ComicCollection, error) {
		id := batchfactoryv11.ComicStyleOutputID(ctx)
		calls[id]++
		if id == "B" && calls[id] == 1 {
			return batchfactoryv11.ComicCollection{}, errors.New("B page 2 temporary failure")
		}
		return batchfactoryv11.ComicCollection{ID: "collection-" + id, StyleID: id}, nil
	}
	c.comicPublish = func(ctx context.Context, o, b, k string, col batchfactoryv11.ComicCollection, s batchfactoryv11.SettingsPatch) (N8ComicPublishRecord, error) {
		id := batchfactoryv11.ComicStyleOutputID(ctx)
		if col.ID != "collection-"+id {
			t.Fatal("wrong collection scope")
		}
		published[id]++
		return N8ComicPublishRecord{Status: "published", CarouselID: 100}, nil
	}
	entry := map[string]any{}
	writes := 0
	_, err = c.runComicStyleOutputs(context.Background(), "alice", batch.ID, batch.Books[0], settings, entry, func() { writes++ }, true, false)
	if err == nil || published["A"] != 1 || published["C"] != 1 || published["B"] != 0 {
		t.Fatalf("first: calls=%v uploads=%v err=%v", calls, published, err)
	}
	// Simulate restart: durable task state is decoded afresh; no in-memory run survives.
	raw, _ := json.Marshal(entry)
	entry = map[string]any{}
	_ = json.Unmarshal(raw, &entry)
	result, err := c.runComicStyleOutputs(context.Background(), "alice", batch.ID, batch.Books[0], settings, entry, func() { writes++ }, true, false)
	if err != nil || result.Completed != 3 {
		t.Fatalf("retry: %+v %v", result, err)
	}
	for _, id := range []string{"A", "B", "C"} {
		if published[id] != 1 {
			t.Fatalf("duplicate/missing uploads %v", published)
		}
	}
	if calls["A"] != 1 || calls["B"] != 2 || calls["C"] != 1 || writes < 6 {
		t.Fatalf("regenerated successful output: %v writes=%d", calls, writes)
	}
}
