package httpapi

import (
	"encoding/json"
	"unicode/utf8"

	"qiantie/backend/internal/screenplay"
)

const sessionPromptBytes = 96 << 10
const sessionSourceBytes = 32 << 10

type contextDirectory struct {
	Mode      string            `json:"mode"`
	Total     map[string]int    `json:"total"`
	Included  map[string]int    `json:"included"`
	Truncated bool              `json:"truncated"`
	Status    string            `json:"status"`
	Rows      []json.RawMessage `json:"rows"`
	remaining int
}

func (d *contextDirectory) clip(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	d.Truncated = true
	return text[:limit]
}
func (d *contextDirectory) add(kind string, fields map[string]any) {
	if d.Included[kind] >= 200 || d.remaining <= 0 {
		d.Truncated = true
		return
	}
	fields["kind"] = kind
	raw, err := json.Marshal(fields)
	if err != nil || len(raw)+1 > d.remaining {
		d.Truncated = true
		return
	}
	d.Rows = append(d.Rows, raw)
	d.Included[kind]++
	d.remaining -= len(raw) + 1
}

func boundedSessionContext(req screenplay.AssistantSessionRequest, project screenplay.Project, active screenplay.Document, targets sessionEditView, message, system string) (string, error) {
	const separator = "\n用户要求："
	remaining := sessionPromptBytes - len(system) - len(message) - len(separator) - 2048
	if len(message) > sessionSourceBytes || remaining < 0 {
		return "", screenplay.ErrInvalidArgument
	}
	shotCount := 0
	for _, board := range targets.boards {
		shotCount += len(board.Shots)
	}
	d := contextDirectory{Mode: req.Mode, Total: map[string]int{"project": 1, "document": 1, "message": len(req.History.Messages), "proposal": len(req.History.Proposals), "chapter": len(req.Chapters), "asset": len(targets.assets), "board": len(targets.boards), "shot": shotCount}, Included: map[string]int{}, remaining: remaining}
	d.add("project", map[string]any{"title": d.clip(project.Title, 512), "synopsis": d.clip(project.Synopsis, 1024), "source_text": d.clip(project.SourceText, sessionSourceBytes)})
	d.add("document", map[string]any{"id": d.clip(active.ID, 128), "content": d.clip(active.Content, 4096)})
	start := len(req.History.Messages) - 32
	if start < 0 {
		start = 0
	}
	for _, m := range req.History.Messages[start:] {
		d.add("message", map[string]any{"role": d.clip(m.Role, 32), "content": d.clip(m.Content, 1024)})
	}
	for i, p := range req.History.Proposals {
		if i >= 200 {
			break
		}
		d.add("proposal", map[string]any{"id": d.clip(p.ID, 128), "status": d.clip(p.Status, 32), "summary": d.clip(p.Summary, 256)})
	}
	for i, c := range req.Chapters {
		if i >= 200 {
			break
		}
		d.add("chapter", map[string]any{"id": d.clip(c.ID, 128), "title": d.clip(c.Title, 256), "source_text": d.clip(c.SourceText, 256), "script_text": d.clip(c.ScriptText, 256)})
	}
	for i, a := range targets.assets {
		if i >= 200 {
			break
		}
		d.add("asset", map[string]any{"id": d.clip(a.ID, 128), "type": a.Type, "name": d.clip(a.Name, 128), "description": d.clip(a.Description, 256), "revision": a.Revision})
	}
	seen := 0
	for i, b := range targets.boards {
		if i >= 200 {
			break
		}
		d.add("board", map[string]any{"id": d.clip(b.ID, 128), "engine": d.clip(string(b.Engine), 128), "episode_no": b.EpisodeNo, "revision": b.Revision})
		for _, s := range b.Shots {
			if seen >= 200 {
				break
			}
			seen++
			d.add("shot", map[string]any{"board_id": d.clip(b.ID, 128), "id": d.clip(s.ID, 128), "seq": s.Seq, "video_desc": d.clip(s.VideoDesc, 256)})
		}
	}
	for kind, total := range d.Total {
		if d.Included[kind] < total {
			d.Truncated = true
		}
	}
	d.Status = "complete"
	if d.Truncated {
		d.Status = "partial"
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return "", err
	}
	output := string(raw) + separator + message
	if len(system)+len(output) > sessionPromptBytes {
		return "", screenplay.ErrInvalidArgument
	}
	return output, nil
}
