package batchfactoryv11

import (
	"context"
	"encoding/json"
	"testing"
)

func TestComicStyleSettingsTransitionFreezesOldBindingBeforeChangingSelection(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	batch, err := s.CreateBatch(ctx, "alice", CreateBatchInput{Title: "old", InitialBatchSettings: SettingsPatch{"aiPromptConfig": json.RawMessage(`{"comic":{"style":{"presetId":"A"}}}`)}, Books: []CreateBookInput{{Title: "book", Videos: []CreateVideoInput{{Label: "one"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	book := batch.Books[0]
	old, err := s.UpsertComicPage(ctx, "alice", batch.ID, book.ID, book.Videos[0].ID, UpsertComicPageInput{ComicPrompt: "story", ImagePrompt: "old A"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveSettings(ctx, "alice", ScopeRef{Kind: ScopeBatch, BatchID: batch.ID}, SettingsUpdate{ExpectedRevision: batch.Revision, Patch: SettingsPatch{"aiPromptConfig": json.RawMessage(`{"comic":{"style":{"presetId":"B"},"styles":[{"presetId":"B"},{"presetId":"C"}]}}`)}})
	if err != nil {
		t.Fatal(err)
	}
	physical, _ := s.ComicStyleStorageID(ctx, "alice", batch.ID, book.ID, "A")
	if physical != "" {
		t.Fatal("old binding lost")
	}
	b, _ := s.ForComicStyle("B").ListComicPages(ctx, "alice", batch.ID, book.ID)
	if len(b) != 0 {
		t.Fatal("A pages relabeled B")
	}
	a, _ := s.ForComicStyle("A").ListComicPages(ctx, "alice", batch.ID, book.ID)
	if len(a) != 1 || a[0].ID != old.ID {
		t.Fatal("original A changed")
	}
}
