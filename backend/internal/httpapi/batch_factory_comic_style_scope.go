package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/prompt"
	"strings"
)

func readComicStyleRequestID(w http.ResponseWriter, r *http.Request) (string, bool) {
	query := strings.TrimSpace(r.URL.Query().Get("styleId"))
	bodyID := ""
	if r.Method != http.MethodGet && r.Body != nil && !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		limit := int64(maxJSONBodyBytes)
		if strings.Contains(r.URL.Path, "/images") {
			limit = maxComicPageImageUploadBodyBytes
		}
		if !limitRequestBody(w, r, limit) {
			return "", false
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			if isBodyTooLarge(err) {
				writePayloadTooLarge(w, limit)
			} else {
				writeJSON(w, 400, map[string]string{"error": "读取画风请求失败"})
			}
			return "", false
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		if len(bytes.TrimSpace(raw)) > 0 {
			var input struct {
				StyleID string `json:"styleId"`
			}
			if json.Unmarshal(raw, &input) != nil {
				writeJSON(w, 400, map[string]string{"error": "画风请求格式不正确"})
				return "", false
			}
			bodyID = strings.TrimSpace(input.StyleID)
		}
	}
	if query != "" && bodyID != "" && query != bodyID {
		writeJSON(w, 400, map[string]string{"error": "请求中的画风选择不一致"})
		return "", false
	}
	if query == "" {
		query = bodyID
	}
	if len(query) > 191 {
		writeJSON(w, 400, map[string]string{"error": "画风选择无效"})
		return "", false
	}
	return query, true
}

func comicStyleSettings(ctx context.Context, store batchfactoryv11.Store, o, b, k string) (batchfactoryv11.SettingsPatch, error) {
	if reader, ok := store.(batchfactoryv11.ComicStyleSettingsReader); ok {
		return reader.ComicStyleSettings(ctx, o, b, k)
	}
	batch, err := store.GetBatch(ctx, o, b)
	if err != nil {
		return nil, err
	}
	for _, book := range batch.Books {
		if book.ID == k {
			return batchfactoryv11.ResolveSettings(batch.SettingsState.Patch, book.SettingsState.Patch), nil
		}
	}
	return nil, batchfactoryv11.ErrNotFound
}

// This wrapper runs inside BridgeAuth after ServeMux sets the owner/book path.
// Image content is addressed by immutable image IDs and keeps owner-only access.
func comicStyleHandler(store batchfactoryv11.Store, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := bridgeOwner(r)
		if !ok {
			next(w, r)
			return
		}
		if r.Method == http.MethodGet && (strings.HasSuffix(r.URL.Path, "/content") || strings.HasSuffix(r.URL.Path, "/thumbnail")) {
			next(w, r)
			return
		}
		styleID, ok := readComicStyleRequestID(w, r)
		if !ok {
			return
		}
		if styleID == "" && r.Method == http.MethodGet {
			next(w, r)
			return
		}
		batchID, bookID := r.PathValue("batchId"), r.PathValue("bookId")
		settings, err := comicStyleSettings(r.Context(), store, owner, batchID, bookID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		var config batchfactoryv11.AIReasoningPromptConfig
		if raw := settings["aiPromptConfig"]; len(raw) > 0 {
			if err = json.Unmarshal(raw, &config); err != nil {
				writeStoreError(w, batchfactoryv11.ErrInvalid)
				return
			}
		}
		outputs, err := batchfactoryv11.NormalizeComicStyleOutputs(config.Comic)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		if styleID == "" {
			if len(outputs) > 1 && !strings.HasSuffix(r.URL.Path, "/comic-collections/generate") {
				writeJSON(w, 400, map[string]string{"error": "请选择要操作的画风"})
				return
			}
			// The whole-book generation endpoint is orchestrated over all configured
			// outputs. Other legacy single-output requests retain the original scope.
			if len(outputs) == 1 && config.Comic.Styles != nil {
				styleID = outputs[0].ID
			} else {
				next(w, r)
				return
			}
		}
		selected := false
		for _, output := range outputs {
			if output.ID == styleID {
				selected = true
				break
			}
		}
		if !selected && r.Method == http.MethodGet {
			if reader, ok := store.(batchfactoryv11.ComicStyleSettingsReader); ok {
				selected, err = reader.ComicStyleOutputExists(r.Context(), owner, batchID, bookID, styleID)
				if err != nil {
					writeStoreError(w, err)
					return
				}
			}
		}
		if !selected {
			writeJSON(w, 400, map[string]string{"error": "当前书籍未选择该画风"})
			return
		}
		if legacy, ok := store.(batchfactoryv11.ComicLegacyStyleStore); ok && config.Comic.Style.ID != "" {
			if err = legacy.AdoptLegacyComicStyle(r.Context(), owner, batchID, bookID, batchfactoryv11.ComicStyleOutput{PresetSnapshot: config.Comic.Style, GlobalReferenceImage: config.Comic.GlobalReferenceImage}); err != nil {
				writeStoreError(w, err)
				return
			}
		}
		pages, available := store.(batchfactoryv11.ComicPageStore)
		if !available {
			writeStoreError(w, batchfactoryv11.ErrUnavailable)
			return
		}
		scoped, err := batchfactoryv11.ResolveComicStyleContext(r.Context(), pages, owner, batchID, bookID, styleID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		frozen, resolveErr := batchfactoryv11.ResolveComicStyleSettings(settings, styleID)
		if resolveErr == nil {
			scoped = context.WithValue(scoped, comicStyleFrozenSettingsKey{}, frozen)
		}
		next(w, r.WithContext(scoped))
	}
}

func resolveScopedPublishedComicStyleReference(ctx context.Context, settings batchfactoryv11.SettingsPatch, catalog *prompt.Catalog) (batchfactoryv11.SettingsPatch, error) {
	if frozen, ok := ctx.Value(comicStyleFrozenSettingsKey{}).(batchfactoryv11.SettingsPatch); ok {
		settings = frozen
	}
	if selected := batchfactoryv11.ComicStyleOutputID(ctx); selected != "" {
		var err error
		settings, err = batchfactoryv11.ResolveComicStyleSettings(settings, selected)
		if err != nil {
			return nil, err
		}
	}
	return resolvePublishedComicStyleReference(settings, catalog)
}

func comicStyleRepairKey(base string, styleID ...string) string {
	if len(styleID) > 0 && styleID[0] != "" {
		return base + "|style:" + styleID[0]
	}
	return base
}

func withComicStyleResponse(page batchfactoryv11.ComicPage, ctx context.Context) batchfactoryv11.ComicPage {
	if id := batchfactoryv11.ComicStyleOutputID(ctx); id != "" {
		page.StyleID = id
	}
	return page
}

func withComicCollectionStyleResponse(collection batchfactoryv11.ComicCollection, ctx context.Context) batchfactoryv11.ComicCollection {
	if id := batchfactoryv11.ComicStyleOutputID(ctx); id != "" {
		collection.StyleID = id
	}
	return collection
}
