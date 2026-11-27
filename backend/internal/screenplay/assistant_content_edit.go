package screenplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type AssistantDocumentEvidence struct {
	ID       string `json:"id"`
	Version  int    `json:"version"`
	Revision int64  `json:"revision"`
	SHA256   string `json:"sha256"`
}

type AssistantChapterPatch struct {
	ID         string  `json:"id"`
	Title      *string `json:"title,omitempty"`
	SourceText *string `json:"source_text,omitempty"`
	ScriptText *string `json:"script_text,omitempty"`
}

type AssistantContentEdit struct {
	AuthorRevision   int64                      `json:"author_revision"`
	SourceSHA256     string                     `json:"source_sha256"`
	Document         *AssistantDocumentEvidence `json:"document,omitempty"`
	Publish          bool                       `json:"publish"`
	Chapter          *AssistantChapterPatch     `json:"chapter,omitempty"`
	ChapterTitle     string                     `json:"chapter_title,omitempty"`
	ChapterSHA256    string                     `json:"chapter_sha256,omitempty"`
	CollectionSHA256 string                     `json:"collection_sha256,omitempty"`
}

const assistantContentLimit = 8 << 20

// ReadAssistantActiveDocument distinguishes a project with no active document
// from a storage failure, while keeping the actual document used in the prompt.
func ReadAssistantActiveDocument(ctx context.Context, store Store, owner, project string) (Document, error) {
	doc, err := store.ActiveDocument(ctx, owner, project)
	if errors.Is(err, ErrNoActiveScript) {
		return Document{}, nil
	}
	return doc, err
}

func assistantDocumentEvidence(doc Document) *AssistantDocumentEvidence {
	if doc.ID == "" {
		return nil
	}
	return &AssistantDocumentEvidence{ID: doc.ID, Version: doc.Version, Revision: doc.Revision, SHA256: contentDigest(doc.Content)}
}

func sameAssistantDocument(before, current *AssistantDocumentEvidence) bool {
	if before == nil || current == nil {
		return before == current
	}
	return *before == *current
}

// CheckAssistantContentContext runs after the model returns. The proposal must
// still refer to the same source and active document that the model saw.
func CheckAssistantContentContext(ctx context.Context, store Store, project Project, active Document) error {
	currentProject, err := store.GetProject(ctx, project.OwnerUsername, project.ID)
	if err != nil {
		return err
	}
	if currentProject.Revision != project.Revision || currentProject.SourceText != project.SourceText {
		return ErrRevisionConflict
	}
	current, err := ReadAssistantActiveDocument(ctx, store, project.OwnerUsername, project.ID)
	if err != nil {
		return err
	}
	if !sameAssistantDocument(assistantDocumentEvidence(active), assistantDocumentEvidence(current)) {
		return ErrRevisionConflict
	}
	return nil
}

// PrepareAssistantContentEdit accepts the same replacement fields from a
// human request and a model response. Only the human request supplies the
// authoring CAS version. A legacy model expected_revision is discarded.
func PrepareAssistantContentEdit(project Project, st AuthoringState, active Document, raw json.RawMessage, fromModel bool) (AuthoringAction, string, error) {
	invalid := func() (AuthoringAction, string, error) { return AuthoringAction{}, "", ErrInvalidArgument }
	if st.ProjectID != project.ID || st.Owner != project.OwnerUsername || st.Archived {
		return AuthoringAction{}, "", ErrInvalidState
	}
	fields, err := assistantStrictEditObject(raw, assistantContentLimit, "正文修改", "kind", "content", "chapter", "expected_revision")
	if err != nil {
		return AuthoringAction{}, "", err
	}
	var expected int64
	if fields["expected_revision"] != nil && (json.Unmarshal(fields["expected_revision"], &expected) != nil || expected < 0) {
		return invalid()
	}
	if !fromModel {
		if fields["expected_revision"] == nil {
			return invalid()
		}
		if expected != st.Revision {
			return AuthoringAction{}, "", ErrRevisionConflict
		}
	}
	action := AuthoringAction{ContentEdit: &AssistantContentEdit{AuthorRevision: st.Revision, SourceSHA256: contentDigest(project.SourceText)}}
	evidence := action.ContentEdit
	if json.Unmarshal(fields["kind"], &action.Kind) != nil {
		return invalid()
	}
	switch action.Kind {
	case "document":
		if fields["chapter"] != nil || json.Unmarshal(fields["content"], &action.Content) != nil {
			return invalid()
		}
		if active.ID == "" {
			return AuthoringAction{}, "", ErrNoActiveScript
		}
		if strings.TrimSpace(action.Content) == "" {
			return AuthoringAction{}, "", ErrEmptyContent
		}
		if err = rejectPartialScriptBatch(action.Content); err != nil {
			return AuthoringAction{}, "", err
		}
		editable, err := editableScreenplayOutput(action.Content)
		if err != nil {
			return AuthoringAction{}, "", err
		}
		if _, err = parsePublishableScreenplay(editable); err != nil {
			return AuthoringAction{}, "", err
		}
		action.TargetID = active.ID
		action.Content = editable
		action.ExpectedRevision = active.Revision
		evidence.Publish = true
		evidence.Document = assistantDocumentEvidence(active)
		return action, "更新剧本正文", nil
	case "chapter":
		if fields["content"] != nil {
			return invalid()
		}
		if _, err = assistantStrictEditObject(fields["chapter"], assistantContentLimit, "章节修改", "id", "title", "source_text", "script_text"); err != nil {
			return AuthoringAction{}, "", err
		}
		var patch AssistantChapterPatch
		if json.Unmarshal(fields["chapter"], &patch) != nil || patch.ID == "" || strings.TrimSpace(patch.ID) != patch.ID || patch.Title == nil && patch.SourceText == nil && patch.ScriptText == nil {
			return invalid()
		}
		index, ok := findChapter(st.Chapters, patch.ID)
		if !ok {
			return AuthoringAction{}, "", ErrNotFound
		}
		previous := st.Chapters[index]
		var next Chapter
		next, err = patchAssistantChapter(previous, patch)
		if err != nil {
			return AuthoringAction{}, "", err
		}
		evidence.Chapter = &patch
		evidence.ChapterTitle = previous.Title
		if evidence.ChapterSHA256, err = assistantChapterDigest(previous); err != nil {
			return AuthoringAction{}, "", err
		}
		action.TargetID = previous.ID
		chapters := append([]Chapter(nil), st.Chapters...)
		chapters[index] = next
		content, err := assistantChapterPublication(chapters)
		if err != nil {
			return AuthoringAction{}, "", err
		}
		if content != "" {
			editable, err := editableScreenplayOutput(content)
			if err != nil {
				return AuthoringAction{}, "", err
			}
			if _, err = parsePublishableScreenplay(editable); err != nil {
				return AuthoringAction{}, "", err
			}
			evidence.Publish = true
			evidence.Document = assistantDocumentEvidence(active)
			if evidence.CollectionSHA256, err = assistantChapterDigest(st.Chapters); err != nil {
				return AuthoringAction{}, "", err
			}
		}
		return action, "修改章节 · " + previous.Title, nil
	default:
		return invalid()
	}
}

func patchAssistantChapter(previous Chapter, patch AssistantChapterPatch) (Chapter, error) {
	next := previous
	if patch.Title != nil {
		next.Title = *patch.Title
		if strings.TrimSpace(next.Title) == "" || strings.ContainsAny(next.Title, "\r\n") {
			return Chapter{}, fmt.Errorf("%w: 章节标题必须为非空单行", ErrInvalidArgument)
		}
	}
	if patch.SourceText != nil {
		next.SourceText = *patch.SourceText
	}
	if patch.ScriptText != nil {
		next.ScriptText = *patch.ScriptText
		if next.ScriptText != previous.ScriptText && strings.TrimSpace(next.ScriptText) != "" {
			if err := ValidateChapterCollection([]Chapter{next}); err != nil {
				return Chapter{}, err
			}
		}
	}
	if next.Title == previous.Title && next.SourceText == previous.SourceText && next.ScriptText == previous.ScriptText {
		return Chapter{}, fmt.Errorf("%w: 章节内容没有变化", ErrInvalidArgument)
	}
	if next.SourceText != previous.SourceText || next.ScriptText != previous.ScriptText {
		next.RawResult = ""
	}
	rows := []Chapter{next}
	if err := PreserveScriptGeneration(rows, []Chapter{previous}); err != nil {
		return Chapter{}, err
	}
	return rows[0], nil
}

func assistantChapterPublication(chapters []Chapter) (string, error) {
	if !ScriptCollectionReady(chapters) {
		return "", nil
	}
	content, err := publishableChapterCollectionText(chapters)
	if errors.Is(err, ErrEmptyContent) {
		return "", nil
	}
	return content, err
}

func assistantChapterDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return contentDigest(string(data)), nil
}

func isAssistantContentAction(kind string) bool {
	return kind == "document" || kind == "chapter"
}

func validateAssistantContentAction(a AuthoringAction) error {
	e := a.ContentEdit
	if e == nil || e.AuthorRevision < 0 || len(e.SourceSHA256) != 64 {
		return ErrRevisionConflict
	}
	if e.Document != nil && (e.Document.ID == "" || e.Document.Version < 1 || e.Document.Revision < 1 || len(e.Document.SHA256) != 64) {
		return ErrRevisionConflict
	}
	if a.Kind == "document" {
		if !e.Publish || e.Document == nil || e.Document.ID != a.TargetID || e.Document.Revision != a.ExpectedRevision || e.Chapter != nil {
			return ErrRevisionConflict
		}
		if strings.TrimSpace(a.Content) == "" {
			return ErrEmptyContent
		}
		return nil
	}
	if e.Chapter == nil || e.Chapter.ID == "" || e.Chapter.ID != a.TargetID || len(e.ChapterSHA256) != 64 || e.Publish && len(e.CollectionSHA256) != 64 {
		return ErrRevisionConflict
	}
	return nil
}

// RefreshAssistantContentProposals only changes the read view. Confirmation
// rechecks the same dependencies with locks held.
func (s AuthoringService) RefreshAssistantContentProposals(ctx context.Context, st *AuthoringState) error {
	var project Project
	var active Document
	readActive := false
	for i := range st.Proposals {
		proposal := &st.Proposals[i]
		if proposal.Status != "pending" || len(proposal.Actions) != 1 {
			continue
		}
		action := proposal.Actions[0]
		if !isAssistantContentAction(action.Kind) {
			continue
		}
		if st.Archived || proposal.Revision != st.Revision || validateAssistantContentAction(action) != nil || action.ContentEdit.AuthorRevision != st.Revision-1 {
			proposal.Status = "stale"
			continue
		}
		if project.ID == "" {
			var err error
			project, err = s.Store.GetProject(ctx, st.Owner, st.ProjectID)
			if err != nil {
				return err
			}
		}
		if action.ContentEdit.Publish && !readActive {
			var err error
			active, err = ReadAssistantActiveDocument(ctx, s.Store, st.Owner, st.ProjectID)
			if err != nil {
				return err
			}
			readActive = true
		}
		if err := checkAssistantContentTarget(*st, action, project.SourceText, active); errors.Is(err, ErrRevisionConflict) {
			proposal.Status = "stale"
		} else if err != nil {
			return err
		}
	}
	return nil
}
