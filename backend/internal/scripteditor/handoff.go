package scripteditor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"qiantie/backend/internal/screenplay"
)

type HandoffInput struct {
	RequestKey string `json:"requestKey"`
	Episodes   []int  `json:"episodes"`
	Title      string `json:"title"`
}

var leadingEpisodeHeading = regexp.MustCompile(`(?i)^\s*(?:#{1,6}\s*)?(?:第\s*[0-9〇零一二三四五六七八九十百千万两]+\s*集|Episode\s+[0-9]+)(?:[ \t：:·—-][^\r\n]*)?(?:\r?\n|$)`)

func handoffEpisodeBody(content string) string {
	content = strings.TrimSpace(content)
	return strings.TrimSpace(leadingEpisodeHeading.ReplaceAllString(content, ""))
}

func (s *Service) CreateComicHandoff(ctx context.Context, owner, workID string, input HandoffInput) (Handoff, error) {
	if s == nil || s.Store == nil || s.ComicStore == nil || s.ComicAuthoring == nil {
		return Handoff{}, ErrValidation
	}
	input.RequestKey = strings.TrimSpace(input.RequestKey)
	input.Title = strings.TrimSpace(input.Title)
	if input.RequestKey == "" || input.Title == "" || len(input.Episodes) == 0 {
		return Handoff{}, ErrValidation
	}
	if existing, err := s.Store.GetHandoff(ctx, owner, workID, input.RequestKey); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Handoff{}, err
	}
	work, err := s.Store.GetWork(ctx, owner, workID)
	if err != nil {
		return Handoff{}, err
	}
	requested := map[int]bool{}
	for _, episode := range input.Episodes {
		if episode <= 0 || requested[episode] {
			return Handoff{}, ErrValidation
		}
		requested[episode] = true
	}
	preferred := map[int]Script{}
	for _, row := range work.Scripts {
		if requested[row.Episode] && row.Preferred {
			preferred[row.Episode] = row
		}
	}
	if len(preferred) != len(requested) {
		return Handoff{}, ErrValidation
	}
	episodes := append([]int(nil), input.Episodes...)
	sort.Ints(episodes)
	parts := make([]string, 0, len(episodes))
	for _, episode := range episodes {
		parts = append(parts, fmt.Sprintf("# 第%d集\n\n%s", episode, handoffEpisodeBody(preferred[episode].Content)))
	}
	sourceParts := make([]string, 0, len(work.Chapters))
	for _, chapter := range work.Chapters {
		sourceParts = append(sourceParts, chapter.Title+"\n"+chapter.Content)
	}
	projectID := randomID("comic")
	project, err := s.ComicStore.CreateProject(ctx, screenplay.Project{ID: projectID, OwnerUsername: owner, Title: input.Title, ScriptMode: "b", SourceText: strings.Join(sourceParts, "\n\n")})
	if err != nil {
		return Handoff{}, err
	}
	content := strings.Join(parts, "\n\n")
	parsed := screenplay.ParseScreenplayMarkdown(content)
	documentID := randomID("doc")
	shots := make([]screenplay.Shot, 0, len(episodes))
	for index, episode := range episodes {
		shots = append(shots, screenplay.Shot{ID: randomID("shot"), DocumentID: documentID, ProjectID: project.ID, OwnerUsername: owner, EpisodeNo: episode, SortOrder: index + 1, Heading: fmt.Sprintf("第%d集", episode), ScriptText: handoffEpisodeBody(preferred[episode].Content)})
	}
	for index := range parsed.Characters {
		parsed.Characters[index].ID = randomID("character")
		parsed.Characters[index].DocumentID, parsed.Characters[index].ProjectID, parsed.Characters[index].OwnerUsername = documentID, project.ID, owner
	}
	for index := range parsed.Scenes {
		parsed.Scenes[index].ID = randomID("scene")
		parsed.Scenes[index].DocumentID, parsed.Scenes[index].ProjectID, parsed.Scenes[index].OwnerUsername = documentID, project.ID, owner
	}
	_, err = s.ComicStore.SaveScreenplay(ctx, screenplay.Document{ID: documentID, ProjectID: project.ID, OwnerUsername: owner, Source: "script-editor", Content: content, EpisodeCount: len(episodes), SegmentCount: len(shots)}, shots, parsed.Characters, parsed.Scenes)
	if err != nil {
		_ = s.ComicStore.DeleteProject(ctx, owner, project.ID)
		return Handoff{}, err
	}
	chapterByID := make(map[string]Chapter, len(work.Chapters))
	for _, chapter := range work.Chapters {
		chapterByID[chapter.ID] = chapter
	}
	authoringChapters := make([]screenplay.Chapter, 0, len(episodes))
	for index, episode := range episodes {
		script := preferred[episode]
		sources := make([]string, 0, len(script.SourceChapterIDs))
		for _, chapterID := range script.SourceChapterIDs {
			if chapter, ok := chapterByID[chapterID]; ok {
				sources = append(sources, chapter.Title+"\n"+chapter.Content)
			}
		}
		if len(sources) == 0 && index == 0 {
			sources = append(sources, sourceParts...)
		}
		authoringChapters = append(authoringChapters, screenplay.Chapter{ID: randomID("chapter"), Title: script.Title, SourceText: strings.Join(sources, "\n\n"), ScriptText: handoffEpisodeBody(script.Content)})
	}
	if _, err = s.ComicAuthoring.Save(ctx, screenplay.AuthoringState{ProjectID: project.ID, Owner: owner, Chapters: authoringChapters, Display: map[string]any{"editor_version": "studio", "active_tab": "script", "font_size": 14}}, 0); err != nil {
		_ = s.ComicStore.DeleteProject(ctx, owner, project.ID)
		return Handoff{}, err
	}
	rawEpisodes, _ := json.Marshal(episodes)
	row, err := s.Store.CreateHandoff(ctx, Handoff{OwnerUsername: owner, WorkID: workID, RequestKey: input.RequestKey, ScreenplayProjectID: project.ID, Episodes: rawEpisodes})
	if errors.Is(err, ErrConflict) {
		_ = s.ComicStore.DeleteProject(ctx, owner, project.ID)
		return s.Store.GetHandoff(ctx, owner, workID, input.RequestKey)
	}
	if err != nil {
		_ = s.ComicStore.DeleteProject(ctx, owner, project.ID)
		return Handoff{}, err
	}
	return row, nil
}
