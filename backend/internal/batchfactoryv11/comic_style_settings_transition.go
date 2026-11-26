package batchfactoryv11

import (
	"context"
	"encoding/json"
)

// Capture the old selection BEFORE applying a preset/configuration change.
// This also protects old in-flight jobs which have not yet saved an image.
func freezeLegacyComicStylesBeforeSettings(ctx context.Context, store Store, o string, ref ScopeRef, update SettingsUpdate) error {
	if ref.Kind != ScopeBatch && ref.Kind != ScopeBook {
		return nil
	}
	_, changed := update.Patch["aiPromptConfig"]
	for _, key := range update.RestoreKeys {
		if key == "aiPromptConfig" {
			changed = true
		}
	}
	if !changed {
		return nil
	}
	batch, err := store.GetBatch(ctx, o, ref.BatchID)
	if err != nil {
		return err
	}
	legacy, ok := store.(ComicLegacyStyleStore)
	if !ok {
		return nil
	}
	for _, book := range batch.Books {
		if ref.Kind == ScopeBook && book.ID != ref.BookID {
			continue
		}
		settings := ResolveSettings(batch.SettingsState.Patch, book.SettingsState.Patch)
		var config AIReasoningPromptConfig
		if json.Unmarshal(settings["aiPromptConfig"], &config) != nil || config.Comic.Style.ID == "" {
			continue
		}
		// Explicit output IDs already have their own namespaces. Only original
		// single-style settings are eligible to claim historical empty storage.
		if config.Comic.Styles != nil {
			continue
		}
		if err = legacy.AdoptLegacyComicStyle(ctx, o, batch.ID, book.ID, ComicStyleOutput{PresetSnapshot: config.Comic.Style, GlobalReferenceImage: config.Comic.GlobalReferenceImage}); err != nil {
			return err
		}
	}
	return nil
}
