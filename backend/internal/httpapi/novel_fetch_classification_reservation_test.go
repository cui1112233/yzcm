package httpapi

import (
	"context"
	"errors"
	"testing"

	"qiantie/backend/internal/novelfetchworkshop"
)

type classificationSaveFailureStore struct {
	novelfetchworkshop.Store
	fail bool
}

func (s *classificationSaveFailureStore) PutDocument(ctx context.Context, owner string, doc novelfetchworkshop.Document) error {
	if s.fail {
		return errors.New("classification database unavailable")
	}
	return s.Store.PutDocument(ctx, owner, doc)
}
func (s *classificationSaveFailureStore) CompareAndSwapDocument(ctx context.Context, owner string, doc novelfetchworkshop.Document) (string, error) {
	if s.fail {
		return "", errors.New("classification database unavailable")
	}
	return s.Store.CompareAndSwapDocument(ctx, owner, doc)
}

func TestClassificationSaveFailureDoesNotRepeatPaidCall(t *testing.T) {
	ctx := context.Background()
	memory := novelfetchworkshop.NewMemoryStore()
	if err := memory.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Original: "小说正文", Meta: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	store := &classificationSaveFailureStore{Store: memory}
	calls := 0
	provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
		calls++
		store.fail = true
		return `{"style":"现代甜文"}`, nil
	})
	if err := ensureWorkshopDocumentClassification(ctx, store, "alice", "123", nil, provider); err == nil {
		t.Fatal("expected failed result save")
	}
	store.fail = false
	_ = ensureWorkshopDocumentClassification(ctx, store, "alice", "123", nil, provider)
	if calls != 1 {
		t.Fatalf("paid classifier repeated after uncertain save: calls=%d", calls)
	}
}

func TestClassificationReservationFailureDoesNotCallProvider(t *testing.T) {
	ctx := context.Background()
	memory := novelfetchworkshop.NewMemoryStore()
	_ = memory.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Original: "正文", Meta: map[string]any{}})
	store := &classificationSaveFailureStore{Store: memory, fail: true}
	doc, _ := store.GetDocument(ctx, "alice", "123")
	calls := 0
	provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
		calls++
		return `{"style":"现代甜文"}`, nil
	})
	if err := runWorkshopClassification(ctx, store, "alice", &doc, novelfetchworkshop.BookInfo{Text: "正文"}, nil, provider); err == nil {
		t.Fatal("reservation failure was hidden")
	}
	if calls != 0 {
		t.Fatalf("spent before reservation: %d", calls)
	}
}

func TestClassificationDeterministicDoesNotReservePaidCall(t *testing.T) {
	ctx := context.Background()
	memory := novelfetchworkshop.NewMemoryStore()
	_ = memory.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Original: "正文", Meta: map[string]any{}})
	doc, _ := memory.GetDocument(ctx, "alice", "123")
	provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
		t.Fatal("upstream classification called provider")
		return "", nil
	})
	if err := runWorkshopClassification(ctx, memory, "alice", &doc, novelfetchworkshop.BookInfo{Text: "正文", Category: "女频", Genre: "现代甜文"}, nil, provider); err != nil {
		t.Fatal(err)
	}
	saved, _ := memory.GetDocument(ctx, "alice", "123")
	if saved.Gender != "女频" || saved.Meta["classificationInFlight"] != nil {
		t.Fatalf("unexpected deterministic classification: %+v", saved)
	}
}

func TestClassificationStaleSnapshotPreservesManualDecision(t *testing.T) {
	ctx := context.Background()
	store := novelfetchworkshop.NewMemoryStore()
	_ = store.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Original: "正文", Meta: map[string]any{}})
	stale, _ := store.GetDocument(ctx, "alice", "123")
	current, _ := store.GetDocument(ctx, "alice", "123")
	current.Gender = "男频"
	current.GenderSource = novelfetchworkshop.GenderManual
	recordNovelFetchStyleDecision(current.Meta, novelfetchworkshop.StyleDecision{Style: "玄幻", Source: "manual"}, nil)
	current.Meta["custom_latest"] = true
	_ = store.PutDocument(ctx, "alice", current)
	provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
		t.Fatal("stale snapshot called model after manual resolution")
		return "", nil
	})
	if err := runWorkshopClassification(ctx, store, "alice", &stale, novelfetchworkshop.BookInfo{Text: "正文"}, nil, provider); err != nil {
		t.Fatal(err)
	}
	saved, _ := store.GetDocument(ctx, "alice", "123")
	if saved.Gender != "男频" || saved.Meta["style"] != "玄幻" || saved.Meta["custom_latest"] != true {
		t.Fatalf("manual decision overwritten: %+v", saved)
	}
}

func TestClassificationOnlyCallsMissingDecision(t *testing.T) {
	for _, onlyStyle := range []bool{true, false} {
		t.Run(map[bool]string{true: "style", false: "gender"}[onlyStyle], func(t *testing.T) {
			ctx := context.Background()
			store := novelfetchworkshop.NewMemoryStore()
			doc := novelfetchworkshop.Document{BookID: "123", Meta: map[string]any{}}
			if onlyStyle {
				doc.Gender = "女频"
				doc.GenderSource = novelfetchworkshop.GenderManual
			} else {
				recordNovelFetchStyleDecision(doc.Meta, novelfetchworkshop.StyleDecision{Style: "玄幻", Source: "manual"}, nil)
			}
			_ = store.PutDocument(ctx, "alice", doc)
			doc, _ = store.GetDocument(ctx, "alice", "123")
			genderCalls, styleCalls := 0, 0
			judge := func(context.Context, novelfetchworkshop.BookInfo) (string, error) {
				genderCalls++
				return `{"gender":"男频"}`, nil
			}
			provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
				styleCalls++
				return `{"style":"现代甜文"}`, nil
			})
			if err := runWorkshopClassification(ctx, store, "alice", &doc, novelfetchworkshop.BookInfo{Text: "正文"}, judge, provider); err != nil {
				t.Fatal(err)
			}
			if onlyStyle && (genderCalls != 0 || styleCalls != 1) || !onlyStyle && (genderCalls != 1 || styleCalls != 0) {
				t.Fatalf("called resolved classifier: gender=%d style=%d", genderCalls, styleCalls)
			}
		})
	}
}
