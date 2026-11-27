package httpapi

import (
	"context"
	"testing"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
)

type automationSummaryFake struct {
	*novelfetchworkshop.MemoryStore
	listCalls, summaryCalls int
}

type automationFullListFake struct {
	*novelfetchworkshop.MemoryStore
	listCalls int
}

func (s *automationFullListFake) ListDocuments(ctx context.Context, owner string) ([]novelfetchworkshop.Document, error) {
	s.listCalls++
	return s.MemoryStore.ListDocuments(ctx, owner)
}
func TestNovelFetchAutomationFallbackListsDocumentsOnlyOnce(t *testing.T) {
	s := &automationFullListFake{MemoryStore: novelfetchworkshop.NewMemoryStore()}
	_ = s.PutDocument(context.Background(), "alice", novelfetchworkshop.Document{BookID: "123", Meta: map[string]any{"status": "input_ready"}})
	job, err := dispatchNovelFetchAutomationOwner(context.Background(), s, novelfetchworkshop.NewMemoryQueue(), "alice", time.Now())
	if err != nil || job != nil || s.listCalls != 1 {
		t.Fatalf("full listings=%d job=%v err=%v", s.listCalls, job, err)
	}
}

func (s *automationSummaryFake) ListDocuments(context.Context, string) ([]novelfetchworkshop.Document, error) {
	s.listCalls++
	return nil, nil
}
func (s *automationSummaryFake) ListAutomationCandidates(_ context.Context, owner, batch, after string, limit int) ([]novelfetchworkshop.Document, error) {
	s.summaryCalls++
	return []novelfetchworkshop.Document{{BookID: "123", Meta: map[string]any{"status": "failed"}}}, nil
}
func TestNovelFetchAutomationUsesSummaryAndHonorsCancellation(t *testing.T) {
	s := &automationSummaryFake{MemoryStore: novelfetchworkshop.NewMemoryStore()}
	docs, err := listNovelFetchAutomationCandidates(context.Background(), s, "alice", "batch")
	if err != nil || len(docs) != 1 || s.summaryCalls != 1 || s.listCalls != 0 {
		t.Fatalf("summary=%d full=%d err=%v", s.summaryCalls, s.listCalls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := listNovelFetchAutomationCandidates(ctx, s, "alice", "batch"); err == nil || s.summaryCalls != 1 {
		t.Fatal("canceled scan made another query")
	}
}

func TestNovelFetchAutomationCandidatePresenceNeverRequiresFakeBody(t *testing.T) {
	doc := novelfetchworkshop.Document{BookID: "123", OriginalRawAvailable: true, Meta: map[string]any{"status": "waiting_sensitive_ai", "originalStatus": "waiting_sensitive_ai"}}
	if !novelFetchAutomationEligible(doc, time.Now().UTC()) {
		t.Fatal("typed original-body presence lost scheduling eligibility")
	}
	doc.OriginalRawAvailable = false
	if novelFetchAutomationEligible(doc, time.Now().UTC()) {
		t.Fatal("absent body became eligible without evidence")
	}
	if doc.OriginalRaw != "" || doc.Original != "" || doc.ProcessedText != "" {
		t.Fatal("candidate scheduling manufactured body text")
	}
}
