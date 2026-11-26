package httpapi

import (
	"context"
	"errors"
	"qiantie/backend/internal/batchfactoryv11"
	"testing"
)

func TestComicStylePublishCannotReuseAnotherStylesReceipt(t *testing.T) {
	ctx := context.Background()
	store := batchfactoryv11.NewMemoryStore()
	batch, err := store.CreateBatch(ctx, "alice", batchfactoryv11.CreateBatchInput{Title: "scope", Books: []batchfactoryv11.CreateBookInput{{Title: "one"}}})
	if err != nil {
		t.Fatal(err)
	}
	book := batch.Books[0]
	pub := &BatchFactoryN8ComicPublisher{Store: store}
	// A nonexistent collection must fail before a saved receipt can be consulted.
	err = pub.validateComicStyleCollection(batchfactoryv11.ComicStyleContext(ctx, "A"), "alice", batch.ID, book.ID, "from-B")
	if !errors.Is(err, batchfactoryv11.ErrNotFound) {
		t.Fatalf("cross style receipt: %v", err)
	}
}
