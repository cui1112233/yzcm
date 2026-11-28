package screenplay

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestAssistantAttachmentDefaultNonPublishingChapterProposal(t *testing.T) {
	st := AuthoringState{Owner: "owner", ProjectID: "project", Revision: 7, Chapters: []Chapter{{ID: "chapter", Title: "old title", SourceText: "source"}}}
	project := Project{ID: "project", OwnerUsername: "owner", SourceText: "project source"}
	action, _, e := PrepareAssistantContentEdit(project, st, Document{}, json.RawMessage(`{"kind":"chapter","chapter":{"id":"chapter","title":"new title"}}`), true)
	if e != nil || action.ContentEdit.Publish || action.ContentEdit.CollectionSHA256 != "" {
		t.Fatal("existing default proposal fixture invalid", e)
	}
	proposal := AssistantProposal{ID: "proposal", Status: "pending", Actions: []AuthoringAction{action}}
	if e = validateDefaultAssistantAttachmentProposal(st, proposal); e != nil {
		t.Fatal("valid default-only title proposal rejected", e)
	}
	st.Chapters[0].Title = "changed concurrently"
	if e = validateDefaultAssistantAttachmentProposal(st, proposal); !errors.Is(e, ErrRevisionConflict) {
		t.Fatal("changed chapter accepted", e)
	}
}
