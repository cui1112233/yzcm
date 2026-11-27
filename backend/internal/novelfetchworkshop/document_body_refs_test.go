package novelfetchworkshop

import (
	"context"
	"strings"
	"testing"
)

func TestMemoryStoreDocumentPreservesBodyRefs(t *testing.T) {
	store := NewMemoryStore()
	var saved BodyRef
	for i := 0; i < 2; i++ {
		var err error
		saved, err = store.PutBody(context.Background(), "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "original"}, Content: strings.Repeat("a", 100)})
		if err != nil {
			t.Fatal(err)
		}
	}
	document := Document{
		BookID: "123",
		Meta:   map[string]any{"bookName": "test"},
		BodyRefs: map[string]BodyRef{
			"original": saved,
		},
	}
	if err := store.PutDocument(context.Background(), "alice", document); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetDocument(context.Background(), "alice", "123")
	if err != nil {
		t.Fatal(err)
	}
	ref, ok := got.BodyRefs["original"]
	if !ok || ref.VersionID != "original" || ref.Revision != 2 || ref.CharCount != 100 {
		t.Fatalf("bodyRefs=%+v", got.BodyRefs)
	}
}
