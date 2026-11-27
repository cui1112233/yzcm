package screenplay

import "testing"

func TestAssistantSessionAncestorAccess(t *testing.T) {
	sessions := map[string]*AssistantSession{
		"p": {ID: "p", Creator: "alice", Visibility: "private"},
		"c": {ID: "c", ParentID: "p", Creator: "bob", Visibility: "project"},
	}
	if assistantSessionVisible(sessions, "c", "bob") {
		t.Fatal("private ancestor exposed")
	}
	if !assistantSessionVisible(sessions, "c", "alice") {
		t.Fatal("creator cannot read child")
	}
	sessions["p"].ParentID = "c"
	if assistantSessionVisible(sessions, "c", "alice") {
		t.Fatal("cycle allowed")
	}
}
