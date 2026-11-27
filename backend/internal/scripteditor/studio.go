package scripteditor

import (
	"context"
	"strings"
)

const workCopyLinkType = "work_copy"

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
