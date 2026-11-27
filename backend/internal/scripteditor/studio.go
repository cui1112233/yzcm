package scripteditor

import (
	"context"
	"strings"
)

const workCopyLinkType = "work_copy"
const scriptImportLinkType = "script_import"

type ReplaceScope string

const (
	ReplaceCurrentVersion ReplaceScope = "current_version"
	ReplaceEpisode        ReplaceScope = "current_episode"
	ReplaceAllScripts     ReplaceScope = "all_scripts"
)

type ReplaceScriptsInput struct {
	Scope             ReplaceScope     `json:"scope"`
	ScriptID          string           `json:"scriptId"`
	Find              string           `json:"find"`
	Replace           string           `json:"replace"`
	ExpectedRevisions map[string]int64 `json:"expectedRevisions"`
}

func (s *Service) ImportScripts(ctx context.Context, owner, workID string, inputs []CreateScriptInput, requestKey string) ([]Script, error) {
	if len(inputs) == 0 || strings.TrimSpace(requestKey) == "" {
		return nil, ErrValidation
	}
	bindings, ok := s.Store.(BindingStore)
	if !ok {
		return nil, ErrValidation
	}
	links, err := bindings.ListJobLinks(ctx, owner, workID, scriptImportLinkType)
	if err != nil {
		return nil, err
	}
	existing := []Script{}
	for _, link := range links {
		if link.JobID == requestKey {
			detail, getErr := s.Store.GetWork(ctx, owner, workID)
			if getErr != nil {
				return nil, getErr
			}
			for _, row := range detail.Scripts {
				if row.ID == link.TargetID {
					existing = append(existing, row)
				}
			}
		}
	}
	if len(existing) > 0 {
		sortScripts(existing)
		return existing, nil
	}
	created := make([]Script, 0, len(inputs))
	for _, input := range inputs {
		input.Version = 0
		input.GenerationSource = SourceImport
		row, createErr := s.Store.CreateScript(ctx, owner, workID, input)
		if createErr != nil {
			return created, createErr
		}
		created = append(created, row)
		if _, createErr = bindings.UpsertJobLink(ctx, JobLink{OwnerUsername: owner, WorkID: workID, JobID: requestKey, TargetType: scriptImportLinkType, TargetID: row.ID, SyncState: SyncSynced, ConflictState: ConflictNone}); createErr != nil {
			return created, createErr
		}
	}
	return created, nil
}

func (s *Service) ReplaceScripts(ctx context.Context, owner, workID string, input ReplaceScriptsInput) ([]Script, error) {
	if strings.TrimSpace(input.Find) == "" {
		return nil, ErrValidation
	}
	detail, err := s.Store.GetWork(ctx, owner, workID)
	if err != nil {
		return nil, err
	}
	var active *Script
	for index := range detail.Scripts {
		if detail.Scripts[index].ID == input.ScriptID {
			active = &detail.Scripts[index]
			break
		}
	}
	if active == nil {
		return nil, ErrNotFound
	}
	selected := []Script{}
	for _, row := range detail.Scripts {
		include := input.Scope == ReplaceAllScripts || input.Scope == ReplaceEpisode && row.Episode == active.Episode || input.Scope == ReplaceCurrentVersion && row.ID == active.ID
		if include {
			expected, ok := input.ExpectedRevisions[row.ID]
			if !ok || expected != row.Revision {
				return nil, ErrConflict
			}
			selected = append(selected, row)
		}
	}
	if input.Scope != ReplaceAllScripts && input.Scope != ReplaceEpisode && input.Scope != ReplaceCurrentVersion {
		return nil, ErrValidation
	}
	updated := make([]Script, 0, len(selected))
	for _, row := range selected {
		next := strings.ReplaceAll(row.Content, input.Find, input.Replace)
		if next == row.Content {
			updated = append(updated, row)
			continue
		}
		saved, updateErr := s.Store.UpdateScript(ctx, owner, workID, row.ID, UpdateScriptInput{Content: &next}, row.Revision)
		if updateErr != nil {
			return updated, updateErr
		}
		updated = append(updated, saved)
	}
	return updated, nil
}

type SearchHit struct {
	ChapterID string `json:"chapterId"`
	Title     string `json:"title"`
	Field     string `json:"field"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Excerpt   string `json:"excerpt"`
}

type ImportMode string

const (
	ImportAppend         ImportMode = "append"
	ImportReplaceChapter ImportMode = "replace_chapter"
)

type ImportIntoWorkInput struct {
	Mode               ImportMode      `json:"mode"`
	ChapterID          string          `json:"chapterId,omitempty"`
	ExpectedRevision   int64           `json:"expectedRevision,omitempty"`
	Sections           []ImportSection `json:"sections"`
	SelectedSectionIDs []string        `json:"selectedSectionIds"`
}

func (s *Service) SearchWork(ctx context.Context, owner, workID, query string) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return []SearchHit{}, nil
	}
	work, err := s.Store.GetWork(ctx, owner, workID)
	if err != nil {
		return nil, err
	}
	hits := []SearchHit{}
	for _, chapter := range work.Chapters {
		for _, field := range []struct{ name, value string }{{"title", chapter.Title}, {"content", chapter.Content}} {
			offset := 0
			for {
				index := strings.Index(field.value[offset:], query)
				if index < 0 {
					break
				}
				start := offset + index
				end := start + len(query)
				from, to := start-36, end+64
				if from < 0 {
					from = 0
				}
				if to > len(field.value) {
					to = len(field.value)
				}
				hits = append(hits, SearchHit{ChapterID: chapter.ID, Title: chapter.Title, Field: field.name, Start: start, End: end, Excerpt: field.value[from:to]})
				offset = end
				if offset >= len(field.value) {
					break
				}
			}
		}
	}
	return hits, nil
}

func selectedImportSections(input ImportIntoWorkInput) ([]ImportSection, error) {
	selected := map[string]bool{}
	for _, id := range input.SelectedSectionIDs {
		if strings.TrimSpace(id) == "" || selected[id] {
			return nil, ErrValidation
		}
		selected[id] = true
	}
	rows := []ImportSection{}
	for _, section := range input.Sections {
		if selected[section.ID] {
			rows = append(rows, section)
			delete(selected, section.ID)
		}
	}
	if len(rows) == 0 || len(selected) != 0 {
		return nil, ErrValidation
	}
	return rows, nil
}

func (s *Service) ImportIntoWork(ctx context.Context, owner, workID string, input ImportIntoWorkInput) (WorkDetail, error) {
	work, err := s.Store.GetWork(ctx, owner, workID)
	if err != nil {
		return WorkDetail{}, err
	}
	sections, err := selectedImportSections(input)
	if err != nil {
		return WorkDetail{}, err
	}
	switch input.Mode {
	case ImportAppend:
		for index, section := range sections {
			if _, err = s.Store.CreateChapter(ctx, owner, workID, CreateChapterInput{Title: section.Title, Content: section.Content, Order: len(work.Chapters) + index + 1}); err != nil {
				return WorkDetail{}, err
			}
		}
	case ImportReplaceChapter:
		if len(sections) != 1 || strings.TrimSpace(input.ChapterID) == "" {
			return WorkDetail{}, ErrValidation
		}
		section := sections[0]
		if _, err = s.Store.UpdateChapter(ctx, owner, workID, input.ChapterID, UpdateChapterInput{Title: &section.Title, Content: &section.Content}, input.ExpectedRevision); err != nil {
			return WorkDetail{}, err
		}
	default:
		return WorkDetail{}, ErrValidation
	}
	return s.Store.GetWork(ctx, owner, workID)
}

func (s *Service) CopyWork(ctx context.Context, owner, workID, requestKey string) (WorkDetail, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(requestKey) == "" {
		return WorkDetail{}, ErrValidation
	}
	bindings, ok := s.Store.(BindingStore)
	if !ok {
		return WorkDetail{}, ErrValidation
	}
	links, err := bindings.ListJobLinks(ctx, owner, workID, workCopyLinkType)
	if err != nil {
		return WorkDetail{}, err
	}
	for _, link := range links {
		if link.JobID == requestKey {
			return s.Store.GetWork(ctx, owner, link.TargetID)
		}
	}
	source, err := s.Store.GetWork(ctx, owner, workID)
	if err != nil {
		return WorkDetail{}, err
	}
	copyWork, err := s.Store.CreateWork(ctx, owner, CreateWorkInput{Title: source.Title + " 副本", Type: source.Type, SourceFilename: source.SourceFilename})
	if err != nil {
		return WorkDetail{}, err
	}
	chapterIDs := map[string]string{}
	for _, chapter := range source.Chapters {
		created, createErr := s.Store.CreateChapter(ctx, owner, copyWork.ID, CreateChapterInput{Title: chapter.Title, Content: chapter.Content, Order: chapter.Order})
		if createErr != nil {
			return WorkDetail{}, createErr
		}
		chapterIDs[chapter.ID] = created.ID
	}
	for _, script := range source.Scripts {
		mapped := make([]string, 0, len(script.SourceChapterIDs))
		for _, chapterID := range script.SourceChapterIDs {
			if value := chapterIDs[chapterID]; value != "" {
				mapped = append(mapped, value)
			}
		}
		created, createErr := s.Store.CreateScript(ctx, owner, copyWork.ID, CreateScriptInput{Episode: script.Episode, Version: script.Version, Title: script.Title, Content: script.Content, SourceChapterIDs: mapped, GenerationSource: SourceCopy, ModelID: script.ModelID, PromptID: script.PromptID, PromptRevision: script.PromptRevision})
		if createErr != nil {
			return WorkDetail{}, createErr
		}
		if script.Preferred {
			if _, createErr = s.Store.PreferScript(ctx, owner, copyWork.ID, created.ID, created.Revision); createErr != nil {
				return WorkDetail{}, createErr
			}
		}
	}
	_, err = bindings.UpsertJobLink(ctx, JobLink{OwnerUsername: owner, WorkID: workID, JobID: requestKey, TargetType: workCopyLinkType, TargetID: copyWork.ID, SyncState: SyncSynced, ConflictState: ConflictNone})
	if err != nil {
		return WorkDetail{}, err
	}
	return s.Store.GetWork(ctx, owner, copyWork.ID)
}
