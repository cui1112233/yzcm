package screenplay

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type metadataOnlyReferenceStore struct {
	Store
	t        *testing.T
	full     Document
	getCalls int
	getErr   error
}

func (s *metadataOnlyReferenceStore) ListDocuments(_ context.Context, owner, project string) ([]Document, error) {
	if owner != "alice" || project != "p" {
		s.t.Fatal("document list scope changed", owner, project)
	}
	meta := s.full
	meta.Content = ""
	meta.RawResult = ""
	return []Document{{ID: "inactive", OwnerUsername: owner, ProjectID: project, Status: "superseded", Version: 1}, meta}, nil
}
func (s *metadataOnlyReferenceStore) GetDocument(_ context.Context, owner, project, id string) (Document, error) {
	s.getCalls++
	if owner != "alice" || project != "p" || id != "active" {
		s.t.Fatal("full document read changed scope or loaded inactive version", owner, project, id)
	}
	return s.full, s.getErr
}
func TestAssistantReferencesLoadActiveFullDocumentWhenListIsMetadataOnly(t *testing.T) {
	ctx := context.Background()
	base := NewMemoryStore()
	if _, err := base.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"}); err != nil {
		t.Fatal(err)
	}
	full := Document{ID: "active", OwnerUsername: "alice", ProjectID: "p", Status: "active", Version: 2, Revision: 3, Content: "真实已保存正文\n第二行", RawResult: "RAW_NOT_A_REFERENCE"}
	store := &metadataOnlyReferenceStore{Store: base, t: t, full: full}
	view, err := LoadAssistantReferenceView(ctx, store, NewMemoryAuthoringRepository(), "alice", "p")
	if err != nil {
		t.Fatal(err)
	}
	if store.getCalls != 1 {
		t.Fatalf("active full document was not read exactly once: %d", store.getCalls)
	}
	selector := AssistantReferenceSelector{Kind: "document", ID: "active"}
	actual, err := view.Describe(selector)
	if err != nil {
		t.Fatal(err)
	}
	want, err := (AssistantReferenceView{Documents: []Document{full}}).Describe(selector)
	if err != nil {
		t.Fatal(err)
	}
	if actual.Text != full.Content || actual.Bytes != len(full.Content) || actual.Digest != want.Digest {
		t.Fatalf("metadata substituted for actual document: %+v want %+v", actual, want)
	}
	refs, err := view.Resolve([]AssistantReferenceSelector{actual.Selector()})
	if err != nil || len(refs) != 1 || refs[0].Text != full.Content {
		t.Fatal("full reference roundtrip", refs, err)
	}
	for _, row := range view.Directory() {
		if row.Kind == "document" && (row.ID != "active" || row.Bytes != len(full.Content) || row.Digest != want.Digest || row.Text != "") {
			t.Fatal("directory must keep full-content evidence without including text", row)
		}
	}
	if _, err = view.Describe(AssistantReferenceSelector{Kind: "document", ID: "inactive"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("inactive document exposed", err)
	}
}

func TestAssistantReferencesActiveDocumentReadFailureHasNoEmptyFallback(t *testing.T) {
	ctx := context.Background()
	base := NewMemoryStore()
	if _, err := base.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"}); err != nil {
		t.Fatal(err)
	}
	store := &metadataOnlyReferenceStore{Store: base, t: t, full: Document{ID: "active", Status: "active"}, getErr: ErrNotFound}
	if _, err := LoadAssistantReferenceView(ctx, store, NewMemoryAuthoringRepository(), "alice", "p"); !errors.Is(err, ErrNotFound) {
		t.Fatal("failed full document read fell back to metadata", err)
	}
}

func TestAssistantReferencesResolveAndVersion(t *testing.T) {
	v := AssistantReferenceView{Project: Project{ID: "p", SourceText: "原文"}, Chapters: []Chapter{{ID: "c", ScriptText: "章节内容"}}, Documents: []Document{{ID: "d", Status: "active", Content: "正文", Revision: 2, Version: 3}}, Assets: []Asset{{ID: "a", Name: "人", Description: "说明", Revision: 1, Extra: map[string]string{"secret": "DO_NOT_SEND"}}}, Boards: []Storyboard{{ID: "b", Engine: "138", EpisodeNo: 1, Revision: 2, Shots: []StoryboardShot{{ID: "s", Seq: 1, VideoDesc: "镜头内容"}}}}}
	for _, sel := range []AssistantReferenceSelector{{Kind: "project_source", ID: "p"}, {Kind: "document", ID: "d"}, {Kind: "chapter_source", ID: "c"}, {Kind: "chapter_script", ID: "c"}, {Kind: "asset_text", ID: "a"}, {Kind: "storyboard", ID: "b", ShotIDs: []string{"s"}}} {
		item, err := v.Describe(sel)
		if err != nil {
			t.Fatal(err)
		}
		sel.ExpectedDigest = item.Digest
		sel.ExpectedRevision = item.Revision
		refs, err := v.Resolve([]AssistantReferenceSelector{sel})
		if err != nil || len(refs) != 1 {
			t.Fatalf("resolve %s: %v", sel.Kind, err)
		}
		raw, _ := json.Marshal(refs)
		if strings.Contains(string(raw), "DO_NOT_SEND") {
			t.Fatal("extra leaked")
		}
		sel.ExpectedDigest = strings.Repeat("0", 64)
		if _, err = v.Resolve([]AssistantReferenceSelector{sel}); !errors.Is(err, ErrRevisionConflict) {
			t.Fatalf("stale accepted: %v", err)
		}
	}
	if _, err := v.Describe(AssistantReferenceSelector{Kind: "storyboard", ID: "b", ShotIDs: []string{"foreign"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign shot: %v", err)
	}
}

func TestAssistantReferencesAggregateLimitAndBoardRange(t *testing.T) {
	v := AssistantReferenceView{Project: Project{ID: "p"}, Chapters: []Chapter{{ID: "one", SourceText: strings.Repeat("a", 9000)}, {ID: "two", SourceText: strings.Repeat("b", 9000)}, {ID: "three", SourceText: strings.Repeat("c", 9000)}, {ID: "four", SourceText: strings.Repeat("d", 9000)}}, Boards: []Storyboard{{ID: "b", Revision: 2, Shots: []StoryboardShot{{ID: "s1", Seq: 1, VideoDesc: "first"}, {ID: "s2", Seq: 2, VideoDesc: "second"}}}}}
	selectors := []AssistantReferenceSelector{}
	for _, c := range v.Chapters {
		r, _ := v.Describe(AssistantReferenceSelector{Kind: "chapter_source", ID: c.ID})
		selectors = append(selectors, r.Selector())
	}
	if _, err := v.Resolve(selectors[:3]); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Resolve(selectors); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("total limit missing", err)
	}
	whole, _ := v.Describe(AssistantReferenceSelector{Kind: "storyboard", ID: "b"})
	sel := whole.Selector()
	sel.ShotIDs = []string{"s2"}
	refs, err := v.Resolve([]AssistantReferenceSelector{sel})
	if err != nil || len(refs) != 1 || strings.Contains(refs[0].Text, "first") || !strings.Contains(refs[0].Text, "second") {
		t.Fatal(refs, err)
	}
	if _, err = v.Resolve([]AssistantReferenceSelector{refs[0].Selector()}); err != nil {
		t.Fatal("preview range digest not reusable", err)
	}
}

func TestAssistantReferencesBudgetAndUnknownSelectors(t *testing.T) {
	v := AssistantReferenceView{Project: Project{ID: "p", SourceText: strings.Repeat("<", 4000)}}
	item, err := v.Describe(AssistantReferenceSelector{Kind: "project_source", ID: "p"})
	if err != nil {
		t.Fatal(err)
	}
	sel := AssistantReferenceSelector{Kind: "project_source", ID: "p", ExpectedDigest: item.Digest}
	if _, err = v.Resolve([]AssistantReferenceSelector{sel}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("escaped bytes not counted: %v", err)
	}
	if _, err = v.Resolve(make([]AssistantReferenceSelector, 9)); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("9 accepted")
	}
	if _, err = v.Describe(AssistantReferenceSelector{Kind: "url", ID: "p"}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatal("unknown kind")
	}
	if _, err = v.Describe(AssistantReferenceSelector{Kind: "project_source", ID: "other"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("other project accepted")
	}
}
