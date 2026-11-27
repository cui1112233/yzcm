package screenplay

import (
	"strings"
	"unicode/utf8"
)

type AssistantSession struct {
	requestActor  string
	ID            string `json:"id"`
	ParentID      string `json:"parent_id,omitempty"`
	Creator       string `json:"creator,omitempty"`
	Visibility    string `json:"visibility"`
	Mode          string `json:"mode"`
	Title         string `json:"title"`
	Revision      int64  `json:"revision"`
	Archived      bool   `json:"archived"`
	RequestStatus string `json:"request_status,omitempty"`
}

type AssistantSessionHistory struct {
	Messages  []AssistantMessage  `json:"messages"`
	Proposals []AssistantProposal `json:"proposals"`
}

type SessionCreate struct {
	Title      string `json:"title"`
	ParentID   string `json:"parent_id,omitempty"`
	Visibility string `json:"visibility,omitempty"`
	Mode       string `json:"mode,omitempty"`
}

func validSessionTitle(s string) bool {
	return strings.TrimSpace(s) != "" && utf8.RuneCountInString(s) <= 191
}

// Walk every ancestor even when the selected row is project-visible.
func assistantSessionVisible(rows map[string]*AssistantSession, id, actor string) bool {
	seen := map[string]bool{}
	for id != "" && id != "default" {
		row := rows[id]
		if row == nil || seen[id] || (row.Visibility != "project" && !(row.Visibility == "private" && row.Creator == actor)) {
			return false
		}
		seen[id] = true
		id = row.ParentID
	}
	return len(seen) > 0
}

func sessionActive(rows map[string]*AssistantSession, id string) bool {
	for id != "" && id != "default" {
		if rows[id].Archived {
			return false
		}
		id = rows[id].ParentID
	}
	return true
}

func sessionDescendant(rows map[string]*AssistantSession, id, ancestor string) bool {
	seen := map[string]bool{}
	for id != "" && rows[id] != nil && !seen[id] {
		if id == ancestor {
			return true
		}
		seen[id] = true
		id = rows[id].ParentID
	}
	return false
}

func sessionProjectAccess(st AuthoringState, owner, actor, right string) bool {
	if actor == "" {
		return false
	}
	if right != "read" && st.Archived {
		return false
	}
	if actor == owner {
		return true
	}
	for _, m := range st.Members {
		if m.Username == actor && m.Read {
			return right == "read" || right == "edit" && m.Edit || right == "generate" && m.Generate
		}
	}
	return false
}
