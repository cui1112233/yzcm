package scripteditor

import (
	"context"
	"strings"

	"qiantie/backend/internal/screenplay"
)

type Service struct {
	Store          Store
	ComicStore     screenplay.Store
	ComicAuthoring screenplay.AuthoringRepository
}

func NewService(store Store) *Service { return &Service{Store: store} }

type ImportCommitInput struct {
	Title              string
	Type               WorkType
	Filename           string
	Sections           []ImportSection
	SelectedSectionIDs []string
}

func (s *Service) CommitImport(ctx context.Context, owner string, input ImportCommitInput) (WorkDetail, error) {
	if s == nil || s.Store == nil || !input.Type.Valid() || len(input.SelectedSectionIDs) == 0 {
		return WorkDetail{}, ErrValidation
	}
	selected := map[string]bool{}
	for _, id := range input.SelectedSectionIDs {
		id = strings.TrimSpace(id)
		if id == "" || selected[id] {
			return WorkDetail{}, ErrValidation
		}
		selected[id] = true
	}
	sections := make([]ImportSection, 0, len(selected))
	for _, section := range input.Sections {
		if selected[section.ID] {
			sections = append(sections, section)
			delete(selected, section.ID)
		}
	}
	if len(selected) != 0 || len(sections) == 0 {
		return WorkDetail{}, ErrValidation
	}
	work, err := s.Store.CreateWork(ctx, owner, CreateWorkInput{Title: input.Title, Type: input.Type, SourceFilename: input.Filename})
	if err != nil {
		return WorkDetail{}, err
	}
	cleanup := func() { _ = s.Store.DeleteWork(context.Background(), owner, work.ID, work.Revision) }
	for index, section := range sections {
		if input.Type == WorkScript {
			episode := section.Number
			if episode <= 0 {
				episode = index + 1
			}
			if _, err := s.Store.CreateScript(ctx, owner, work.ID, CreateScriptInput{Episode: episode, Title: section.Title, Content: section.Content, GenerationSource: SourceImport}); err != nil {
				cleanup()
				return WorkDetail{}, err
			}
			continue
		}
		if _, err := s.Store.CreateChapter(ctx, owner, work.ID, CreateChapterInput{Title: section.Title, Content: section.Content, Order: index + 1}); err != nil {
			cleanup()
			return WorkDetail{}, err
		}
	}
	return s.Store.GetWork(ctx, owner, work.ID)
}
