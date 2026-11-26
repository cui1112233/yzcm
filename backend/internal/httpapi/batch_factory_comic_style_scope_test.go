package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/localartifact"
	"testing"
	"time"
)

func TestComicStyleScopeRoutesKeepTwoVersionsOfOnePage(t *testing.T) {
	now := time.Unix(1700000000, 0)
	store := batchfactoryv11.NewMemoryStore()
	settings := batchfactoryv11.SettingsPatch{"productionMode": json.RawMessage(`"comic"`), "aiPromptConfig": json.RawMessage(`{"comic":{"enabled":true,"style":{"presetId":"A"},"styles":[{"presetId":"A","body":"ink"},{"presetId":"B","body":"photo"}]}}`)}
	batch, err := store.CreateBatch(context.Background(), "alice", batchfactoryv11.CreateBatchInput{Title: "styles", InitialBatchSettings: settings, Books: []batchfactoryv11.CreateBookInput{{Title: "book", Videos: []batchfactoryv11.CreateVideoInput{{Label: "one"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	book := batch.Books[0]
	video := book.Videos[0]
	api := NewRouter(RouterOptions{BridgeSecret: "secret", Now: func() time.Time { return now }, Slice: 1, Store: store, LocalArtifacts: localartifact.NewStore(t.TempDir(), 1024)})
	path := "/api/batch-factory/v12/batches/" + batch.ID + "/books/" + book.ID + "/comic-pages"
	ids := map[string]string{}
	for _, style := range []string{"A", "B"} {
		response := signedJSONRequest(t, api, now, "alice", http.MethodPut, path+"/"+video.ID, map[string]any{"styleId": style, "comicPrompt": "shared story", "imagePrompt": style + " image"})
		if response.Code != http.StatusOK {
			t.Fatalf("put %s: %d %s", style, response.Code, response.Body.String())
		}
		page := decodeBody[struct {
			Page batchfactoryv11.ComicPage `json:"page"`
		}](t, response).Page
		ids[style] = page.ID
	}
	// Original image URLs contain immutable IDs, so switching or removing a
	// selector must never make an already generated image disappear.
	ctxB, err := batchfactoryv11.ResolveComicStyleContext(context.Background(), store, "alice", batch.ID, book.ID, "B")
	if err != nil {
		t.Fatal(err)
	}
	image, err := store.CreateComicPageImage(ctxB, "alice", batch.ID, book.ID, ids["B"], batchfactoryv11.CreateComicPageImageInput{URL: "https://example.test/b.png", MediaType: "image/png", Source: "provider"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = findComicPageImage(context.Background(), store, "alice", batch.ID, book.ID, ids["B"], image.ID); err != nil {
		t.Fatal("immutable B image missing:", err)
	}
	if _, err = findComicPageImage(context.Background(), store, "bob", batch.ID, book.ID, ids["B"], image.ID); err == nil {
		t.Fatal("foreign image readable")
	}
	wrong := signedJSONRequest(t, api, now, "alice", http.MethodPut, path+"/"+ids["B"]+"/images/"+image.ID+"/primary", map[string]any{"styleId": "A"})
	if wrong.Code != http.StatusNotFound {
		t.Fatalf("cross-style main selection: %d %s", wrong.Code, wrong.Body.String())
	}
	if ids["A"] == ids["B"] {
		t.Fatal("same physical page overwritten by different style")
	}
	for _, style := range []string{"A", "B"} {
		response := signedJSONRequest(t, api, now, "alice", http.MethodGet, path+"?styleId="+style, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("get %s: %d %s", style, response.Code, response.Body.String())
		}
		result := decodeBody[struct {
			Pages []batchfactoryv11.ComicPage `json:"pages"`
		}](t, response)
		if len(result.Pages) != 1 || result.Pages[0].ID != ids[style] || result.Pages[0].StyleID != style {
			t.Fatalf("wrong style view: %+v", result.Pages)
		}
	}
	conflict := signedJSONRequest(t, api, now, "alice", http.MethodPut, path+"/"+video.ID+"?styleId=A", map[string]any{"styleId": "B", "imagePrompt": "must not save"})
	if conflict.Code != http.StatusBadRequest {
		t.Fatalf("accepted conflicting style IDs: %d", conflict.Code)
	}
	unknown := signedJSONRequest(t, api, now, "alice", http.MethodGet, path+"?styleId=other", nil)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("accepted unselected style: %d", unknown.Code)
	}
	foreign := signedJSONRequest(t, api, now, "bob", http.MethodGet, path+"?styleId=A", nil)
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign book status: %d", foreign.Code)
	}
}

func TestComicStyleScopePersistsAcrossDetachedContext(t *testing.T) {
	parent, cancel := context.WithCancel(batchfactoryv11.ComicStyleContext(context.Background(), "A"))
	defer cancel()
	child, stop := comicPageGenerationContext(parent)
	defer stop()
	cancel()
	if child.Err() != nil || batchfactoryv11.ComicStyleOutputID(child) != "A" {
		t.Fatal("lost style scope or canceled result persistence")
	}
}
