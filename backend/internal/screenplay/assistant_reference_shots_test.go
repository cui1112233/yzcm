package screenplay

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func referenceLargeShotView() AssistantReferenceView {
	b := Storyboard{ID: "board", EpisodeNo: 2, Engine: "seedance", Revision: 7}
	for i := 0; i < 231; i++ {
		b.Shots = append(b.Shots, StoryboardShot{ID: fmt.Sprintf("shot-%03d", i), Seq: i + 1, VideoDesc: strings.Repeat("字", 300), Line: "line", ImagePrompt: "IMAGE_MUST_NOT_LEAK", ProtocolFields: json.RawMessage(`{"secret":"PROTOCOL_MUST_NOT_LEAK"}`)})
	}
	return AssistantReferenceView{Boards: []Storyboard{b}}
}
func TestAssistantReferenceShotPageLargeBoardAndRange(t *testing.T) {
	v := referenceLargeShotView()
	whole, e := v.Describe(AssistantReferenceSelector{Kind: "storyboard", ID: "board"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = v.Resolve([]AssistantReferenceSelector{whole.Selector()}); !errors.Is(e, ErrInvalidArgument) {
		t.Fatal("fixture whole board must exceed item budget", e)
	}
	input := AssistantReferenceShotQuery{BoardID: whole.ID, ExpectedRevision: whole.Revision, ExpectedDigest: whole.Digest, Offset: 100, Limit: 100}
	page, e := v.ReferenceShotPage(input)
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Shots) != 100 || page.Total != 231 || page.NextOffset != 200 || page.Shots[0].ID != "shot-100" || page.Shots[0].Position != 100 || page.Board.Digest != whole.Digest {
		t.Fatalf("wrong page %+v", page)
	}
	for _, s := range page.Shots {
		if len(s.Excerpt) > 256 || !utf8.ValidString(s.Excerpt) || !s.ExcerptTruncated {
			t.Fatal("unbounded/split excerpt", s)
		}
	}
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), "IMAGE_MUST_NOT_LEAK") || strings.Contains(string(raw), "PROTOCOL_MUST_NOT_LEAK") {
		t.Fatal("page leaked non-whitelist fields")
	}
	sel := whole.Selector()
	sel.ShotIDs = []string{"shot-100", "shot-230"}
	refs, e := v.Resolve([]AssistantReferenceSelector{sel})
	if e != nil || len(refs) != 1 {
		t.Fatal(refs, e)
	}
	input.ExpectedDigest = refs[0].Digest
	if _, e = v.ReferenceShotPage(input); !errors.Is(e, ErrRevisionConflict) {
		t.Fatal("range digest accepted as full evidence", e)
	}
}
func TestAssistantReferenceShotPageRejectsStaleAndInvalidPaging(t *testing.T) {
	v := referenceLargeShotView()
	whole, _ := v.Describe(AssistantReferenceSelector{Kind: "storyboard", ID: "board"})
	valid := AssistantReferenceShotQuery{BoardID: whole.ID, ExpectedRevision: whole.Revision, ExpectedDigest: whole.Digest}
	page, e := v.ReferenceShotPage(valid)
	if e != nil || len(page.Shots) != 100 {
		t.Fatal("default page limit", e)
	}
	for _, change := range []func(*AssistantReferenceShotQuery){func(q *AssistantReferenceShotQuery) { q.Offset = -1 }, func(q *AssistantReferenceShotQuery) { q.Offset = 232 }, func(q *AssistantReferenceShotQuery) { q.Limit = 101 }, func(q *AssistantReferenceShotQuery) { q.Limit = -1 }} {
		q := valid
		change(&q)
		if _, e = v.ReferenceShotPage(q); !errors.Is(e, ErrInvalidArgument) {
			t.Fatal("invalid paging accepted", q, e)
		}
	}
	q := valid
	q.ExpectedRevision++
	if _, e = v.ReferenceShotPage(q); !errors.Is(e, ErrRevisionConflict) {
		t.Fatal("stale revision", e)
	}
	q = valid
	q.BoardID = "foreign"
	if _, e = v.ReferenceShotPage(q); !errors.Is(e, ErrNotFound) {
		t.Fatal("foreign board", e)
	}
	v.Boards[0].Shots[0].VideoDesc = "changed without revision"
	if _, e = v.ReferenceShotPage(valid); !errors.Is(e, ErrRevisionConflict) {
		t.Fatal("stale full digest accepted", e)
	}
}
