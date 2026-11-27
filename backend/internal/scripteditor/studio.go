package scripteditor

import (
	"context"
	"strings"
)

const workCopyLinkType = "work_copy"

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
