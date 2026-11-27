package scripteditor

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type AgentReference struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type AgentContext struct {
	WorkID        string   `json:"workId"`
	ChapterIDs    []string `json:"chapterIds"`
	ScriptIDs     []string `json:"scriptIds"`
	AssetIDs      []string `json:"assetIds"`
	StoryboardIDs []string `json:"storyboardIds"`
}

type AgentDraft struct {
	ID       string `json:"id"`
	WorkID   string `json:"workId"`
	ScriptID string `json:"scriptId"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Mode     string `json:"mode"`
}

type AgentDraftResult struct {
	Script  Script `json:"script"`
	AuditID string `json:"auditId"`
}

type AgentBridge struct {
	Store Store
	Now   func() time.Time
}

func (b AgentBridge) BuildContext(ctx context.Context, owner, workID string, refs []AgentReference) (AgentContext, error) {
	if b.Store == nil {
		return AgentContext{}, ErrValidation
	}
	detail, err := b.Store.GetWork(ctx, owner, workID)
	if err != nil {
		return AgentContext{}, err
	}
	chapters := map[string]bool{}
	scripts := map[string]bool{}
	for _, row := range detail.Chapters {
		chapters[row.ID] = true
	}
	for _, row := range detail.Scripts {
		scripts[row.ID] = true
	}
	result := AgentContext{WorkID: workID, ChapterIDs: []string{}, ScriptIDs: []string{}, AssetIDs: []string{}, StoryboardIDs: []string{}}
	seen := map[string]bool{}
	for _, ref := range refs {
		kind, id := strings.TrimSpace(ref.Type), strings.TrimSpace(ref.ID)
		key := kind + ":" + id
		if id == "" || seen[key] {
			continue
		}
		seen[key] = true
		switch kind {
		case "chapter":
			if !chapters[id] {
				return AgentContext{}, ErrForbidden
			}
			result.ChapterIDs = append(result.ChapterIDs, id)
		case "script":
			if !scripts[id] {
				return AgentContext{}, ErrForbidden
			}
			result.ScriptIDs = append(result.ScriptIDs, id)
		case "asset", "storyboard":
			// Asset and storyboard ownership is resolved by their bridges. Unknown
			// IDs are intentionally rejected here instead of accepting free text.
			return AgentContext{}, ErrForbidden
		default:
			return AgentContext{}, ErrValidation
		}
	}
	return result, nil
}

func (b AgentBridge) ApplyDraft(ctx context.Context, owner string, draft AgentDraft, expectedRevision int64) (AgentDraftResult, error) {
	if b.Store == nil || strings.TrimSpace(draft.ID) == "" || strings.TrimSpace(draft.WorkID) == "" || strings.TrimSpace(draft.ScriptID) == "" || strings.TrimSpace(draft.Content) == "" {
		return AgentDraftResult{}, ErrValidation
	}
	if draft.Mode != "confirm" && draft.Mode != "direct" {
		return AgentDraftResult{}, ErrValidation
	}
	detail, err := b.Store.GetWork(ctx, owner, draft.WorkID)
	if err != nil {
		return AgentDraftResult{}, err
	}
	var source *Script
	for i := range detail.Scripts {
		if detail.Scripts[i].ID == draft.ScriptID {
			source = &detail.Scripts[i]
			break
		}
	}
	if source == nil {
		return AgentDraftResult{}, ErrForbidden
	}
	if source.Revision != expectedRevision {
		return AgentDraftResult{}, ErrConflict
	}
	title := strings.TrimSpace(draft.Title)
	if title == "" {
		title = source.Title + " Agent 稿"
	}
	created, err := b.Store.CreateScript(ctx, owner, draft.WorkID, CreateScriptInput{Episode: source.Episode, Title: title, Content: draft.Content, SourceChapterIDs: source.SourceChapterIDs, GenerationSource: SourceAI, PromptID: "agent:" + draft.ID})
	if err != nil {
		return AgentDraftResult{}, err
	}
	now := time.Now().UTC()
	if b.Now != nil {
		now = b.Now().UTC()
	}
	payload, _ := json.Marshal(map[string]any{"draftId": draft.ID, "mode": draft.Mode, "sourceScriptId": source.ID, "sourceRevision": expectedRevision, "createdScriptId": created.ID})
	audit, err := b.Store.CreateReview(ctx, Review{ID: randomID("audit"), WorkID: draft.WorkID, ScriptID: created.ID, OwnerUsername: owner, Type: "agent_draft_applied", Result: payload, PromptID: "agent:" + draft.ID, CreatedAt: now})
	if err != nil {
		return AgentDraftResult{}, err
	}
	return AgentDraftResult{Script: created, AuditID: audit.ID}, nil
}
