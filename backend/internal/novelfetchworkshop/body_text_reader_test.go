package novelfetchworkshop

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestStoredBodyOnlyGenerateUsesSavedOriginal(t *testing.T) {
	for _, entry := range []string{"count", "versions", "processed"} {
		t.Run(entry, func(t *testing.T) {
			ctx := context.Background()
			s := plainRewriteTestStore("alice")
			if err := s.PutDocument(ctx, "alice", Document{BookID: "stored-only"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PutBody(ctx, "alice", BodyRecord{BookID: "stored-only", BodyRef: BodyRef{VersionID: "original"}, Content: "已保存的原文"}); err != nil {
				t.Fatal(err)
			}
			calls := 0
			provider := rewriteProviderFunc(func(_ context.Context, r RewriteRequest) (string, error) {
				calls++
				if !strings.Contains(r.UserPrompt, "已保存的原文") {
					t.Error("saved original missing from model input")
				}
				return "模型改写正文", nil
			})
			var result Document
			var err error
			switch entry {
			case "count":
				result, err = GenerateAIRewrites(ctx, s, "alice", "stored-only", 1, provider)
			case "versions":
				result, err = GenerateAIRewritesForVersions(ctx, s, "alice", "stored-only", []string{"ai1"}, provider)
			case "processed":
				result, err = GenerateAIRewritesForVersionsProcessed(ctx, s, "alice", "stored-only", []string{"ai1"}, provider, nil)
			}
			if err != nil || calls != 1 || result.Versions["ai1"] != "模型改写正文" {
				t.Fatalf("stored body not used: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestBodyEditAtomicallyUpdatesTaskMetadata(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	doc := Document{BookID: "123", Original: "旧原文", OriginalRaw: "原始完整内容不应丢失", ProcessedText: "旧原文", OriginalRawChars: 11, ProcessedChars: 3, Meta: map[string]any{"keep": "unchanged", "autoSubmitReadyFingerprint": "stale"}, Versions: map[string]any{"ai1": map[string]any{"text": "old AI", "label": "keep", "state": "submitted"}}}
	if err := s.PutDocument(ctx, "alice", doc); err != nil {
		t.Fatal(err)
	}
	ref, err := s.PutBody(ctx, "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "original"}, Content: "现在正文已经修改为更长内容"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetDocument(ctx, "alice", "123")
	if got.ProcessedChars != int(ref.CharCount) || got.BodyRefs["original"].ContentHash != ref.ContentHash || got.BodyRefs["original"].Revision != ref.Revision {
		t.Fatalf("stale task summary: %+v", got)
	}
	if got.Original != "" || got.ProcessedText != "" {
		t.Fatal("duplicated original text in metadata")
	}
	if got.OriginalRaw != doc.OriginalRaw || got.OriginalRawChars != doc.OriginalRawChars || got.Meta["keep"] != "unchanged" {
		t.Fatal("raw or unrelated metadata changed")
	}
	if _, ok := got.Meta["autoSubmitReadyFingerprint"]; ok {
		t.Fatal("old submission fingerprint retained")
	}
	ai, err := s.PutBody(ctx, "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "ai1"}, Content: "新的AI正文"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetDocument(ctx, "alice", "123")
	version, _ := got.Versions["ai1"].(map[string]any)
	if got.BodyRefs["ai1"].Revision != ai.Revision || version["text"] != nil || version["label"] != "keep" || version["state"] != "submitted" {
		t.Fatalf("AI metadata stale: %+v", got)
	}
	body, _ := s.GetBody(ctx, "alice", "123", "original")
	if body.Content != "现在正文已经修改为更长内容" {
		t.Fatal("saved content lost")
	}
	if _, err := s.GetDocument(ctx, "bob", "123"); !errors.Is(err, ErrNotFound) {
		t.Fatal("metadata crossed account boundary")
	}
}

type bodyReaderFaultStore struct {
	*MemoryStore
	failure error
	calls   int
}

func (s *bodyReaderFaultStore) GetBody(context.Context, string, string, string) (BodyRecord, error) {
	s.calls++
	return BodyRecord{}, s.failure
}

func TestReadBodyTextHasExplicitFallbacksAndSurfacesStorageErrors(t *testing.T) {
	ctx := context.Background()
	doc := Document{BookID: "123", Original: "legacy original", ProcessedText: "processed", OriginalRaw: "full raw", Versions: map[string]any{"ai1": "legacy ai", "ai2": map[string]any{"text": "AI text", "state": "ready"}, "ai3": map[string]any{"state": "ready"}, "ai4": false}}
	for _, test := range []struct{ version, want string }{{"original", "legacy original"}, {"original_raw", "full raw"}, {"ai1", "legacy ai"}, {"ai2", "AI text"}, {"ai3", ""}, {"ai4", ""}} {
		s := &bodyReaderFaultStore{MemoryStore: NewMemoryStore(), failure: ErrNotFound}
		got, err := ReadBodyText(ctx, s, "alice", doc, test.version)
		if err != nil || got != test.want || s.calls != 1 {
			t.Fatalf("%s=%q err=%v", test.version, got, err)
		}
	}
	fault := errors.New("body storage unavailable")
	s := &bodyReaderFaultStore{MemoryStore: NewMemoryStore(), failure: fault}
	if got, err := ReadBodyText(ctx, s, "alice", doc, "original"); got != "" || !errors.Is(err, fault) {
		t.Fatalf("storage failure hidden by legacy fallback: %q %v", got, err)
	}
	base := NewMemoryStore()
	if _, err := base.PutBody(ctx, "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "original"}, Content: "saved authoritative body"}); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadBodyText(ctx, base, "alice", doc, "original"); err != nil || got != "saved authoritative body" {
		t.Fatalf("inline won over saved body: %q %v", got, err)
	}
}

func TestRewriteBodyReadFailureNeverCallsModel(t *testing.T) {
	ctx := context.Background()
	base := plainRewriteTestStore("alice")
	if err := base.PutDocument(ctx, "alice", Document{BookID: "123", Original: "stale inline body"}); err != nil {
		t.Fatal(err)
	}
	fault := errors.New("TOS read failed")
	s := &bodyReaderFaultStore{MemoryStore: base, failure: fault}
	calls := 0
	_, err := GenerateAIRewrites(ctx, s, "alice", "123", 1, rewriteProviderFunc(func(context.Context, RewriteRequest) (string, error) { calls++; return "bad", nil }))
	if !errors.Is(err, fault) || calls != 0 {
		t.Fatalf("read failure triggered model: calls=%d err=%v", calls, err)
	}
}

func TestStaleDocumentWriteCannotReintroduceOldBodySummary(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	old := Document{BookID: "stale", Original: "old", ProcessedChars: 3, Versions: map[string]any{"ai2": "untouched legacy"}}
	if err := s.PutDocument(ctx, "alice", old); err != nil {
		t.Fatal(err)
	}
	ref, err := s.PutBody(ctx, "alice", BodyRecord{BookID: "stale", BodyRef: BodyRef{VersionID: "original"}, Content: "new authoritative body"})
	if err != nil {
		t.Fatal(err)
	}
	old.Meta = map[string]any{"status": "done"}
	if err := s.PutDocument(ctx, "alice", old); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetDocument(ctx, "alice", "stale")
	if got.ProcessedChars != int(ref.CharCount) || got.BodyRefs["original"].Revision != ref.Revision || got.Original != "" {
		t.Fatalf("stale document overwrote saved body metadata: %+v", got)
	}
	if got.Versions["ai2"] != "untouched legacy" {
		t.Fatal("removed unrelated legacy version")
	}
}

func TestCanonicalDocumentWritePreservesCurrentContentFingerprint(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	ref, err := s.PutBody(ctx, "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "original"}, Content: "body"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PutDocument(ctx, "alice", Document{BookID: "123", BodyRefs: map[string]BodyRef{"original": ref}, Meta: map[string]any{"autoSubmitReadyFingerprint": "current"}}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetDocument(ctx, "alice", "123")
	if got.Meta["autoSubmitReadyFingerprint"] != "current" {
		t.Fatal("unchanged-body status write invalidated current submission state")
	}
}

type rewriteBodyWriteFailureStore struct {
	*MemoryStore
	failure error
}

func (s rewriteBodyWriteFailureStore) PutBody(context.Context, string, BodyRecord) (BodyRef, error) {
	return BodyRef{}, s.failure
}

func TestFailedFirstRewriteBodyWriteNeverPersistsGeneratedInlineText(t *testing.T) {
	ctx := context.Background()
	base := plainRewriteTestStore("alice")
	if err := base.PutDocument(ctx, "alice", Document{BookID: "123", Original: "existing original"}); err != nil {
		t.Fatal(err)
	}
	fault := errors.New("injected body write failure")
	store := rewriteBodyWriteFailureStore{MemoryStore: base, failure: fault}
	_, err := GenerateAIRewrites(ctx, store, "alice", "123", 1, rewriteProviderFunc(func(context.Context, RewriteRequest) (string, error) {
		return "model output must not become legacy inline", nil
	}))
	if !errors.Is(err, ErrRewritePersistence) || !errors.Is(err, fault) || !strings.Contains(err.Error(), "结果未知") {
		t.Fatalf("persistence failure is not safely classified: %v", err)
	}
	got, _ := base.GetDocument(ctx, "alice", "123")
	if _, ok := got.Versions["ai1"]; ok {
		t.Fatalf("failed save persisted a new inline version: %#v", got.Versions)
	}
	if _, ok := got.BodyRefs["ai1"]; ok {
		t.Fatal("failed save persisted a body reference")
	}
	text, readErr := ReadBodyText(ctx, base, "alice", got, "ai1")
	if readErr != nil || text != "" {
		t.Fatalf("failed save became readable: %q %v", text, readErr)
	}
	if _, err := base.GetBody(ctx, "alice", "123", "ai1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed save has body: %v", err)
	}
}

func TestStaleDocumentWriteCannotResurrectDeletedSavedBody(t *testing.T) {
	for _, version := range []string{"original", "original_raw", "ai1"} {
		t.Run(version, func(t *testing.T) {
			ctx := context.Background()
			s := NewMemoryStore()
			if err := s.PutDocument(ctx, "alice", Document{BookID: "123"}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.PutBody(ctx, "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: version}, Content: "saved body"}); err != nil {
				t.Fatal(err)
			}
			old, _ := s.GetDocument(ctx, "alice", "123")
			switch version {
			case "original":
				old.Original = "saved body"
				old.ProcessedText = "saved body"
			case "original_raw":
				old.OriginalRaw = "saved body"
			default:
				old.Versions[version] = "saved body"
			}
			if deleted, err := s.DeleteBody(ctx, "alice", "123", version); err != nil || !deleted {
				t.Fatal(deleted, err)
			}
			if err := s.PutDocument(ctx, "alice", old); err != nil {
				t.Fatal(err)
			}
			got, _ := s.GetDocument(ctx, "alice", "123")
			if _, ok := got.BodyRefs[version]; ok {
				t.Fatalf("deleted body reference resurrected: %+v", got)
			}
			text, err := ReadBodyText(ctx, s, "alice", got, version)
			if err != nil || text != "" {
				t.Fatalf("deleted body resurrected as inline: %q %v", text, err)
			}
			if _, err := s.GetBody(ctx, "alice", "123", version); !errors.Is(err, ErrNotFound) {
				t.Fatal("deleted body row resurrected")
			}
		})
	}
}

type rewriteDocumentWriteFailureStore struct {
	*MemoryStore
	failure error
}

func (s rewriteDocumentWriteFailureStore) PutDocument(ctx context.Context, owner string, document Document) error {
	if document.BodyRefs["ai1"].Revision > 0 {
		return s.failure
	}
	return s.MemoryStore.PutDocument(ctx, owner, document)
}

func TestPaidRewriteDocumentWriteFailureIsPersistenceUnknown(t *testing.T) {
	ctx := context.Background()
	base := plainRewriteTestStore("alice")
	if err := base.PutDocument(ctx, "alice", Document{BookID: "123", Original: "existing original"}); err != nil {
		t.Fatal(err)
	}
	fault := errors.New("metadata store failed")
	s := rewriteDocumentWriteFailureStore{MemoryStore: base, failure: fault}
	_, err := GenerateAIRewrites(ctx, s, "alice", "123", 1, rewriteProviderFunc(func(context.Context, RewriteRequest) (string, error) { return "saved paid model output", nil }))
	if !errors.Is(err, ErrRewritePersistence) || !errors.Is(err, fault) || !strings.Contains(err.Error(), "结果未知") {
		t.Fatalf("paid output metadata failure may replay model: %v", err)
	}
	body, bodyErr := base.GetBody(ctx, "alice", "123", "ai1")
	if bodyErr != nil || body.Content != "saved paid model output" {
		t.Fatalf("already saved body lost: %v", bodyErr)
	}
}

func TestBodyDeletionInvalidatesOldDocumentCAS(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	if err := s.PutDocument(ctx, "alice", Document{BookID: "123"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutBody(ctx, "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "original"}, Content: "saved"}); err != nil {
		t.Fatal(err)
	}
	old, _ := s.GetDocument(ctx, "alice", "123")
	if _, err := s.DeleteBody(ctx, "alice", "123", "original"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompareAndSwapDocument(ctx, "alice", old); !errors.Is(err, ErrDocumentChanged) {
		t.Fatalf("old CAS accepted after body deletion: %v", err)
	}
}

func TestPaidRewriteProcessorFailureRequiresManualConfirmation(t *testing.T) {
	ctx := context.Background()
	s := plainRewriteTestStore("alice")
	if err := s.PutDocument(ctx, "alice", Document{BookID: "123", Original: "saved original"}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fault := errors.New("local processor unavailable")
	_, err := GenerateAIRewritesForVersionsProcessed(ctx, s, "alice", "123", []string{"ai1"}, rewriteProviderFunc(func(context.Context, RewriteRequest) (string, error) { calls++; return "paid response", nil }), func(context.Context, string, string) (string, []map[string]any, []map[string]any, error) {
		return "", nil, nil, fault
	})
	if !errors.Is(err, ErrRewritePersistence) || !errors.Is(err, fault) || calls != 1 {
		t.Fatalf("paid result processor failure may replay: calls=%d err=%v", calls, err)
	}
	got, _ := s.GetDocument(ctx, "alice", "123")
	if got.Meta["rewritePersistenceHold"] != true {
		t.Fatal("paid processor failure did not persist manual hold")
	}
	if _, exists := got.Versions["ai1"]; exists {
		t.Fatal("failed processor created inline version")
	}
}
