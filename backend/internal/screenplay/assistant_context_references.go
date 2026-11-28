package screenplay

import (
	"context"
	"encoding/json"
	"fmt"
)

const AssistantReferenceItemBytes = 16 << 10
const AssistantReferenceTotalBytes = 32 << 10

// Selectors never accept caller-supplied content, locations or ownership.
type AssistantReferenceSelector struct {
	Kind             string   `json:"kind"`
	ID               string   `json:"id"`
	ExpectedRevision int64    `json:"expected_revision,omitempty"`
	ExpectedDigest   string   `json:"expected_digest"`
	ShotIDs          []string `json:"shot_ids,omitempty"`
}
type AssistantReferenceSnapshot struct {
	Kind       string                           `json:"kind"`
	ID         string                           `json:"id"`
	Name       string                           `json:"name"`
	Revision   int64                            `json:"revision,omitempty"`
	Version    int                              `json:"version,omitempty"`
	Digest     string                           `json:"digest"`
	ShotIDs    []string                         `json:"shot_ids,omitempty"`
	Text       string                           `json:"text"`
	Bytes      int                              `json:"bytes"`
	Attachment *AssistantTextAttachmentEvidence `json:"attachment,omitempty"`
}

func (s AssistantReferenceSnapshot) Selector() AssistantReferenceSelector {
	return AssistantReferenceSelector{s.Kind, s.ID, s.Revision, s.Digest, append([]string(nil), s.ShotIDs...)}
}

type AssistantReferenceView struct {
	Project   Project
	Chapters  []Chapter
	Documents []Document
	Assets    []Asset
	Boards    []Storyboard
}

func LoadAssistantReferenceView(ctx context.Context, store Store, repo AuthoringRepository, owner, project string) (v AssistantReferenceView, err error) {
	if v.Project, err = store.GetProject(ctx, owner, project); err != nil {
		return
	}
	var st AuthoringState
	st, err = (AuthoringService{Store: store, Repository: repo}).Load(ctx, owner, project)
	if err != nil {
		return
	}
	v.Chapters = st.Chapters
	if v.Documents, err = store.ListDocuments(ctx, owner, project); err != nil {
		return
	}
	// Document lists may contain metadata only. Reference evidence must come
	// from the actual active version, using the same authenticated scope.
	for i, document := range v.Documents {
		if document.Status != "active" {
			continue
		}
		if v.Documents[i], err = store.GetDocument(ctx, owner, project, document.ID); err != nil {
			return
		}
	}
	if v.Assets, err = store.ListAssets(ctx, owner, project, ""); err != nil {
		return
	}
	v.Boards, err = store.ListStoryboards(ctx, owner, project, "")
	return
}
func (v AssistantReferenceView) Describe(sel AssistantReferenceSelector) (out AssistantReferenceSnapshot, err error) {
	out.Kind = sel.Kind
	out.ID = sel.ID
	out.ShotIDs = append([]string(nil), sel.ShotIDs...)
	if sel.ID == "" || len(sel.ID) > 128 || len(sel.ShotIDs) > 200 {
		return out, ErrInvalidArgument
	}
	if sel.Kind != "storyboard" && len(sel.ShotIDs) > 0 {
		return out, ErrInvalidArgument
	}
	found := false
	switch sel.Kind {
	case "project_source":
		if sel.ID == v.Project.ID {
			out.Name = "作品原文"
			out.Text = v.Project.SourceText
			found = true
		}
	case "document":
		for _, d := range v.Documents {
			if d.ID == sel.ID && d.Status == "active" {
				out.Name = "已保存正文"
				out.Text = d.Content
				out.Revision = d.Revision
				out.Version = d.Version
				found = true
				break
			}
		}
	case "chapter_source", "chapter_script":
		for _, c := range v.Chapters {
			if c.ID == sel.ID {
				out.Name = c.Title
				out.Text = c.SourceText
				if sel.Kind == "chapter_script" {
					out.Text = c.ScriptText
				}
				found = true
				break
			}
		}
	case "asset_text":
		for _, a := range v.Assets {
			if a.ID == sel.ID {
				out.Name = a.Name
				out.Revision = a.Revision
				raw, e := json.Marshal(struct {
					Name        string    `json:"name"`
					Type        AssetType `json:"type"`
					Identity    string    `json:"identity"`
					Description string    `json:"description"`
					Prompt      string    `json:"prompt"`
				}{a.Name, a.Type, a.Identity, a.Description, a.Prompt})
				if e != nil {
					return out, e
				}
				out.Text = string(raw)
				found = true
				break
			}
		}
	case "storyboard":
		for _, b := range v.Boards {
			if b.ID != sel.ID {
				continue
			}
			found = true
			out.Name = fmt.Sprintf("第%d集 · %s", b.EpisodeNo, b.Engine)
			out.Revision = b.Revision
			selected := map[string]bool{}
			for _, id := range sel.ShotIDs {
				if id == "" || selected[id] {
					return out, ErrInvalidArgument
				}
				selected[id] = true
			}
			type shot struct {
				ID          string `json:"id"`
				Seq         int    `json:"seq"`
				Characters  string `json:"characters"`
				Scene       string `json:"scene"`
				Props       string `json:"props"`
				Description string `json:"description"`
				Line        string `json:"line"`
				ImagePrompt string `json:"image_prompt"`
			}
			rows := []shot{}
			seen := 0
			for _, s := range b.Shots {
				if len(selected) > 0 && !selected[s.ID] {
					continue
				}
				seen++
				rows = append(rows, shot{s.ID, s.Seq, s.Characters, s.Scene, s.Props, s.VideoDesc, s.Line, s.ImagePrompt})
			}
			if len(selected) > 0 && seen != len(selected) {
				return out, ErrNotFound
			}
			raw, e := json.Marshal(rows)
			if e != nil {
				return out, e
			}
			out.Text = string(raw)
			break
		}
	default:
		return out, ErrInvalidArgument
	}
	if !found {
		return out, ErrNotFound
	}
	out.Bytes = len(out.Text)
	// Canonical digest covers the exact whitelist, identity, version and range.
	raw, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	out.Digest = contentDigest(string(raw))
	return out, nil
}
func (v AssistantReferenceView) Resolve(selectors []AssistantReferenceSelector) ([]AssistantReferenceSnapshot, error) {
	return resolveAssistantReferences(selectors, v.Describe)
}

func resolveAssistantReferences(selectors []AssistantReferenceSelector, describe func(AssistantReferenceSelector) (AssistantReferenceSnapshot, error)) ([]AssistantReferenceSnapshot, error) {
	if len(selectors) > 8 {
		return nil, ErrInvalidArgument
	}
	out := make([]AssistantReferenceSnapshot, 0, len(selectors))
	seen := map[string]bool{}
	for _, sel := range selectors {
		item, err := describe(sel)
		if err != nil {
			return nil, err
		}
		digestMatches := sel.ExpectedDigest == item.Digest
		// A directory entry signs the whole board. A selected shot range may use
		// that version once; previews return the more precise range digest.
		if !digestMatches && sel.Kind == "storyboard" && len(sel.ShotIDs) > 0 {
			whole := sel
			whole.ShotIDs = nil
			all, e := describe(whole)
			digestMatches = e == nil && all.Digest == sel.ExpectedDigest
		}
		if len(sel.ExpectedDigest) != 64 || !digestMatches || sel.ExpectedRevision != item.Revision {
			return nil, ErrRevisionConflict
		}
		if seen[item.Digest] {
			continue
		}
		seen[item.Digest] = true
		raw, err := json.Marshal(item)
		if err != nil {
			return nil, err
		}
		if len(raw) > AssistantReferenceItemBytes {
			return nil, fmt.Errorf("%w: 单项引用超过16KiB，请缩小范围", ErrInvalidArgument)
		}
		out = append(out, item)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(raw) > AssistantReferenceTotalBytes {
		return nil, fmt.Errorf("%w: 引用合计超过32KiB，请减少选择", ErrInvalidArgument)
	}
	return out, nil
}
func (v AssistantReferenceView) Directory() []AssistantReferenceSnapshot {
	selectors := []AssistantReferenceSelector{{Kind: "project_source", ID: v.Project.ID}}
	for _, d := range v.Documents {
		if d.Status == "active" {
			selectors = append(selectors, AssistantReferenceSelector{Kind: "document", ID: d.ID})
		}
	}
	for _, c := range v.Chapters {
		selectors = append(selectors, AssistantReferenceSelector{Kind: "chapter_source", ID: c.ID}, AssistantReferenceSelector{Kind: "chapter_script", ID: c.ID})
	}
	for _, b := range v.Boards {
		selectors = append(selectors, AssistantReferenceSelector{Kind: "storyboard", ID: b.ID})
	}
	for _, a := range v.Assets {
		selectors = append(selectors, AssistantReferenceSelector{Kind: "asset_text", ID: a.ID})
	}
	out := []AssistantReferenceSnapshot{}
	for _, s := range selectors {
		item, err := v.Describe(s)
		if err == nil {
			item.Text = ""
			out = append(out, item)
		}
	}
	return out
}
