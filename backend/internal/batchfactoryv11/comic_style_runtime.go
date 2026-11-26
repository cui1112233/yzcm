package batchfactoryv11

import (
	"context"
	"encoding/json"
	"time"
)

// Minimal polling snapshot. It never loads source text, prompt bodies or image bytes.
type ComicStyleRuntimeOutput struct {
	Style       ComicStyleOutput
	Pages       []ComicPage
	Collections []ComicCollection
	Published   map[string]bool
	StorageID   string
}

func comicRuntimeSelections(patches map[string]SettingsPatch, batch SettingsPatch, books []string) map[string][]ComicStyleRuntimeOutput {
	out := map[string][]ComicStyleRuntimeOutput{}
	for _, book := range books {
		settings := ResolveSettings(batch, patches[book])
		var config AIReasoningPromptConfig
		_ = json.Unmarshal(settings["aiPromptConfig"], &config)
		styles, err := NormalizeComicStyleOutputs(config.Comic)
		if err != nil || config.Comic.Styles == nil || len(styles) < 1 {
			continue
		}
		for _, style := range styles {
			out[book] = append(out[book], ComicStyleRuntimeOutput{Style: style, StorageID: style.ID, Published: map[string]bool{}})
		}
	}
	return out
}
func (s *MemoryStore) ComicStyleRuntimeBooks(_ context.Context, o, b string, books []string) (map[string][]ComicStyleRuntimeOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	owned, ok := s.batches[b]
	if !ok || owned.Owner != o {
		return nil, ErrNotFound
	}
	patches := map[string]SettingsPatch{}
	for _, book := range books {
		patches[book] = s.patches[scopeKey(ScopeRef{Kind: ScopeBook, BatchID: b, BookID: book})]
	}
	out := comicRuntimeSelections(patches, s.patches[scopeKey(ScopeRef{Kind: ScopeBatch, BatchID: b})], books)
	for book, outputs := range out {
		if legacy, ok := s.comicLegacyStyles[[3]string{o, b, book}]; ok {
			for i := range outputs {
				if outputs[i].Style.ID == legacy.ID {
					outputs[i].StorageID = ""
				}
			}
		}
		for _, owned := range s.comicPages {
			p := owned.Value
			if owned.Owner != o || p.BatchID != b || p.BookID != book {
				continue
			}
			for i := range outputs {
				if outputs[i].StorageID == p.StyleID {
					p.Images = nil
					for _, image := range s.comicPageImages {
						if image.Owner == o && image.Value.ComicPageID == p.ID && image.Value.IsPrimary {
							p.Images = append(p.Images, image.Value)
						}
					}
					outputs[i].Pages = append(outputs[i].Pages, p)
				}
			}
		}
		for _, owned := range s.comicCollections {
			c := owned.Value
			if owned.Owner != o || c.BatchID != b || c.BookID != book {
				continue
			}
			for i := range outputs {
				if outputs[i].StorageID == c.StyleID {
					outputs[i].Collections = append(outputs[i].Collections, c)
				}
			}
		}
		for i := range outputs {
			for _, c := range outputs[i].Collections {
				for key, draft := range s.drafts {
					if key[0] != o || draft.Key != c.ID || draft.Kind != "n8-comic-publish" || draft.Scope != b {
						continue
					}
					var receipt struct {
						Status     string `json:"status"`
						CarouselID int64  `json:"carouselId"`
					}
					if json.Unmarshal([]byte(draft.Content), &receipt) == nil && receipt.Status == "published" && receipt.CarouselID > 0 {
						outputs[i].Published[c.ID] = true
					}
				}
			}
		}
		out[book] = outputs
	}
	return out, nil
}
func (s *MySQLStore) ComicStyleRuntimeBooks(ctx context.Context, o, b string, books []string) (map[string][]ComicStyleRuntimeOutput, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT scope_type,scope_id,patch_json FROM batch_factory_v12_settings_patches WHERE owner_username=? AND batch_id=? AND scope_type IN ('batch','book')`, o, b)
	if err != nil {
		return nil, err
	}
	patches := map[string]SettingsPatch{}
	var batch SettingsPatch
	for rows.Next() {
		var scope, id string
		var raw []byte
		if err = rows.Scan(&scope, &id, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var patch SettingsPatch
		if err = json.Unmarshal(raw, &patch); err != nil {
			rows.Close()
			return nil, err
		}
		if scope == "batch" {
			batch = patch
		} else {
			patches[id] = patch
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := comicRuntimeSelections(patches, batch, books)
	if len(out) == 0 {
		return out, nil
	}
	rows, err = s.db.QueryContext(ctx, `SELECT book_id,style_id FROM batch_factory_v12_comic_style_outputs WHERE owner_username=? AND batch_id=? AND is_legacy=TRUE`, o, b)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var book, id string
		if err = rows.Scan(&book, &id); err != nil {
			rows.Close()
			return nil, err
		}
		for i := range out[book] {
			if out[book][i].Style.ID == id {
				out[book][i].StorageID = ""
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT p.id,p.book_id,p.style_id,p.video_id,p.status,COALESCE(p.last_error,''),p.updated_at,(p.comic_prompt<>'' AND p.image_prompt<>''),EXISTS(SELECT 1 FROM batch_factory_v12_comic_page_images i WHERE i.comic_page_id=p.id AND i.owner_username=p.owner_username AND i.is_primary=TRUE) FROM batch_factory_v12_comic_pages p JOIN batch_factory_v12_videos v ON v.id=p.video_id AND v.owner_username=p.owner_username WHERE p.owner_username=? AND p.batch_id=? AND v.compatibility_state='active'`, o, b)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p ComicPage
		var ready, primary bool
		if err = rows.Scan(&p.ID, &p.BookID, &p.StyleID, &p.VideoID, &p.Status, &p.GenerationError, &p.UpdatedAt, &ready, &primary); err != nil {
			rows.Close()
			return nil, err
		}
		if ready {
			p.ComicPrompt, p.ImagePrompt = "ready", "ready"
		}
		if primary {
			p.Images = []ComicPageImage{{ID: "primary", IsPrimary: true}}
		}
		for i := range out[p.BookID] {
			if out[p.BookID][i].StorageID == p.StyleID {
				out[p.BookID][i].Pages = append(out[p.BookID][i].Pages, p)
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT c.id,c.book_id,c.style_id,COUNT(i.collection_id),COALESCE(JSON_UNQUOTE(JSON_EXTRACT(d.content,'$.status')),''),COALESCE(JSON_EXTRACT(d.content,'$.carouselId'),0) FROM batch_factory_v12_comic_collections c LEFT JOIN batch_factory_v12_comic_collection_items i ON i.collection_id=c.id LEFT JOIN batch_factory_v12_drafts d ON d.owner_username=c.owner_username AND d.draft_key=c.id AND d.kind='n8-comic-publish' AND d.scope=c.batch_id WHERE c.owner_username=? AND c.batch_id=? GROUP BY c.id,c.book_id,c.style_id,d.content`, o, b)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, book, style, status string
		var count int
		var carousel int64
		if err = rows.Scan(&id, &book, &style, &count, &status, &carousel); err != nil {
			rows.Close()
			return nil, err
		}
		for i := range out[book] {
			if out[book][i].StorageID == style {
				out[book][i].Collections = append(out[book][i].Collections, ComicCollection{ID: id, Items: make([]ComicCollectionItem, count), CreatedAt: time.Time{}})
				out[book][i].Published[id] = status == "published" && carousel > 0
			}
		}
	}
	err = rows.Err()
	rows.Close()
	return out, err
}
