package httpapi

import (
	"context"

	"qiantie/backend/internal/batchfactoryv11"
)

// comicPageLiteStore is optional so older/in-memory stores keep working. The
// MySQL runtime implements it and avoids reading prompt-sized columns for list
// and status requests.
type comicPageLiteStore interface {
	ListComicPagesLite(ctx context.Context, owner, batchID, bookID string) ([]batchfactoryv11.ComicPage, error)
}

func listComicPagesForView(ctx context.Context, store batchfactoryv11.ComicPageStore, owner, batchID, bookID string, lite bool) ([]batchfactoryv11.ComicPage, error) {
	var pages []batchfactoryv11.ComicPage
	var err error
	if lite {
		if liteStore, ok := store.(comicPageLiteStore); ok {
			pages, err = liteStore.ListComicPagesLite(ctx, owner, batchID, bookID)
		} else {
			pages, err = store.ListComicPages(ctx, owner, batchID, bookID)
		}
	} else {
		pages, err = store.ListComicPages(ctx, owner, batchID, bookID)
	}
	if err == nil {
		for i := range pages {
			pages[i] = withComicStyleResponse(pages[i], ctx)
		}
	}
	return pages, err
}
