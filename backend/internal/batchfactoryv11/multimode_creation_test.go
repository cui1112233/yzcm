package batchfactoryv11

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestMultiModeCreationIdempotentAndIndependent(t *testing.T) {
	s := NewMemoryStore()
	ctx := context.Background()
	input := CreateMultiModeInput{RequestID: "test-request", Title: "same source", Books: []CreateBookInput{{BookID: "738579", Title: "book", SourceText: "full source", SourceMetadata: map[string]any{"gender": "女频", "style": "现代言情"}}}}
	for _, mode := range []CreationMode{"video", "comic", "illustrated_text", "text"} {
		input.Projects = append(input.Projects, ModeProjectInput{Mode: mode, Settings: SettingsPatch{"captureCharacters": json.RawMessage(`4000`)}})
	}
	first, err := s.CreateMultiModeBatches(ctx, "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.CreateMultiModeBatches(ctx, "alice", input)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Batches) != 4 || first.GroupID != second.GroupID || first.Batches[3].ID != second.Batches[3].ID {
		t.Fatal("duplicate project group")
	}
	input.Title = "different"
	if _, err = s.CreateMultiModeBatches(ctx, "alice", input); !errors.Is(err, ErrConflict) {
		t.Fatalf("request changed: %v", err)
	}
	if _, err = s.GetMultiModeCreation(ctx, "bob", first.GroupID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign group: %v", err)
	}
	a, b := first.Batches[0], first.Batches[1]
	if a.Books[0].ID == b.Books[0].ID {
		t.Fatal("shared mutable book")
	}
	original := b.Books[0].SourceMetadata["gender"]
	_, err = s.UpdateBookMetadata(ctx, "alice", a.ID, a.Books[0].ID, UpdateBookMetadataInput{ExpectedRevision: a.Books[0].Revision, Metadata: map[string]any{"gender": "男频"}})
	if err != nil {
		t.Fatal(err)
	}
	reread, err := s.GetBatch(ctx, "alice", b.ID)
	if err != nil || reread.Books[0].SourceMetadata["gender"] != original {
		t.Fatal("child metadata leaked")
	}
}
func TestMultiModeRejectsInvalidModes(t *testing.T) {
	for _, modes := range [][]CreationMode{{}, {"comic", "comic"}, {"bad"}, {"video", "comic", "illustrated_text", "text", "video"}} {
		input := CreateMultiModeInput{RequestID: "x", Title: "t", Books: []CreateBookInput{{Title: "b"}}}
		for _, m := range modes {
			input.Projects = append(input.Projects, ModeProjectInput{Mode: m})
		}
		if _, err := NewMemoryStore().CreateMultiModeBatches(context.Background(), "alice", input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %v: %v", modes, err)
		}
	}
}
