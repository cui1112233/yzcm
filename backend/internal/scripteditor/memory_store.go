package scripteditor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type MemoryStore struct {
	mu           sync.Mutex
	works        map[string]Work
	chapters     map[string]Chapter
	scripts      map[string]Script
	handoffs     map[string]Handoff
	jobs         map[string]Job
	reviews      map[string]Review
	workBindings map[string]WorkBinding
	sourceLinks  map[string]SourceLink
	assetLinks   map[string]AssetLink
	jobLinks     map[string]JobLink
	storyboards  map[string]StoryboardDocument
	settings     map[string]WorkSettings
	now          func() time.Time
	newID        func(string) string
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		works: map[string]Work{}, chapters: map[string]Chapter{}, scripts: map[string]Script{}, handoffs: map[string]Handoff{}, jobs: map[string]Job{}, reviews: map[string]Review{},
		workBindings: map[string]WorkBinding{}, sourceLinks: map[string]SourceLink{}, assetLinks: map[string]AssetLink{}, jobLinks: map[string]JobLink{}, storyboards: map[string]StoryboardDocument{}, settings: map[string]WorkSettings{},
		now: time.Now, newID: randomID,
	}
}

func (s *MemoryStore) GetWorkSettings(_ context.Context, owner, workID string) (WorkSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.settings[owner+"\x00"+workID]
	if !ok {
		return WorkSettings{}, ErrNotFound
	}
	return row, nil
}
func (s *MemoryStore) SaveWorkSettings(_ context.Context, owner, workID string, row WorkSettings, expected int64) (WorkSettings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeWork(owner, workID); err != nil {
		return WorkSettings{}, err
	}
	key := owner + "\x00" + workID
	current, ok := s.settings[key]
	if ok && current.Revision != expected {
		return WorkSettings{}, ErrConflict
	}
	if !ok && expected != 0 {
		return WorkSettings{}, ErrConflict
	}
	if ok {
		row.Revision = current.Revision + 1
	} else {
		row.Revision = 0
	}
	row.UpdatedAt = s.now().UTC()
	s.settings[key] = row
	return row, nil
}

func (s *MemoryStore) GetStoryboard(_ context.Context, owner, workID string, episode int) (StoryboardDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.storyboards[owner+"\x00"+workID+"\x00"+fmt.Sprint(episode)]
	if !ok {
		return StoryboardDocument{}, ErrNotFound
	}
	row.Shots = append([]StoryboardShot(nil), row.Shots...)
	return row, nil
}

func (s *MemoryStore) SaveStoryboard(_ context.Context, row StoryboardDocument, expected int64) (StoryboardDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeWork(row.OwnerUsername, row.WorkID); err != nil {
		return StoryboardDocument{}, err
	}
	key := row.OwnerUsername + "\x00" + row.WorkID + "\x00" + fmt.Sprint(row.Episode)
	now := s.now().UTC()
	current, exists := s.storyboards[key]
	if exists {
		if current.Revision != expected {
			return StoryboardDocument{}, ErrConflict
		}
		row.ID, row.CreatedAt, row.Revision = current.ID, current.CreatedAt, current.Revision+1
	} else {
		if expected != 0 {
			return StoryboardDocument{}, ErrConflict
		}
		row.ID, row.CreatedAt, row.Revision = s.newID("storyboard"), now, 0
	}
	row.UpdatedAt = now
	row.Shots = append([]StoryboardShot(nil), row.Shots...)
	s.storyboards[key] = row
	return row, nil
}

func (s *MemoryStore) UpsertWorkBinding(_ context.Context, row WorkBinding) (WorkBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeWork(row.OwnerUsername, row.WorkID); err != nil {
		return WorkBinding{}, err
	}
	if err := row.Validate(); err != nil {
		return WorkBinding{}, err
	}
	key := row.OwnerUsername + "\x00" + row.WorkID + "\x00" + string(row.TargetType)
	now := s.now().UTC()
	if current, ok := s.workBindings[key]; ok {
		row.ID, row.CreatedAt = current.ID, current.CreatedAt
	} else {
		row.ID, row.CreatedAt = s.newID("binding"), now
	}
	row.UpdatedAt = now
	s.workBindings[key] = row
	return row, nil
}

func (s *MemoryStore) WorkBinding(_ context.Context, owner, workID string, targetType BindingTargetType) (WorkBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.workBindings[owner+"\x00"+workID+"\x00"+string(targetType)]
	if !ok {
		return WorkBinding{}, ErrNotFound
	}
	return row, nil
}

func (s *MemoryStore) UpsertSourceLink(_ context.Context, row SourceLink) (SourceLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeWork(row.OwnerUsername, row.WorkID); err != nil {
		return SourceLink{}, err
	}
	if strings.TrimSpace(row.SourceType) == "" || strings.TrimSpace(row.SourceID) == "" || strings.TrimSpace(row.TargetType) == "" || strings.TrimSpace(row.TargetID) == "" {
		return SourceLink{}, ErrValidation
	}
	key := row.OwnerUsername + "\x00" + row.WorkID + "\x00" + row.SourceType + "\x00" + row.SourceID + "\x00" + row.TargetType + "\x00" + row.TargetID
	now := s.now().UTC()
	if current, ok := s.sourceLinks[key]; ok {
		row.ID, row.CreatedAt = current.ID, current.CreatedAt
	} else {
		row.ID, row.CreatedAt = s.newID("source_link"), now
	}
	row.UpdatedAt = now
	s.sourceLinks[key] = row
	return row, nil
}

func (s *MemoryStore) ListSourceLinks(_ context.Context, owner, workID, targetType string) ([]SourceLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []SourceLink{}
	for _, row := range s.sourceLinks {
		if row.OwnerUsername == owner && row.WorkID == workID && (targetType == "" || row.TargetType == targetType) {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *MemoryStore) UpsertAssetLink(_ context.Context, row AssetLink) (AssetLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeWork(row.OwnerUsername, row.WorkID); err != nil {
		return AssetLink{}, err
	}
	if strings.TrimSpace(row.AssetID) == "" || strings.TrimSpace(row.TargetType) == "" || strings.TrimSpace(row.TargetID) == "" {
		return AssetLink{}, ErrValidation
	}
	key := row.OwnerUsername + "\x00" + row.WorkID + "\x00" + row.AssetID + "\x00" + row.TargetType + "\x00" + row.TargetID
	now := s.now().UTC()
	if current, ok := s.assetLinks[key]; ok {
		row.ID, row.CreatedAt = current.ID, current.CreatedAt
	} else {
		row.ID, row.CreatedAt = s.newID("asset_link"), now
	}
	row.UpdatedAt = now
	s.assetLinks[key] = row
	return row, nil
}

func (s *MemoryStore) ListAssetLinks(_ context.Context, owner, workID, targetType string) ([]AssetLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []AssetLink{}
	for _, row := range s.assetLinks {
		if row.OwnerUsername == owner && row.WorkID == workID && (targetType == "" || row.TargetType == targetType) {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *MemoryStore) UpsertJobLink(_ context.Context, row JobLink) (JobLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeWork(row.OwnerUsername, row.WorkID); err != nil {
		return JobLink{}, err
	}
	if strings.TrimSpace(row.JobID) == "" || strings.TrimSpace(row.TargetType) == "" || strings.TrimSpace(row.TargetID) == "" {
		return JobLink{}, ErrValidation
	}
	key := row.OwnerUsername + "\x00" + row.WorkID + "\x00" + row.JobID + "\x00" + row.TargetType + "\x00" + row.TargetID
	now := s.now().UTC()
	if current, ok := s.jobLinks[key]; ok {
		row.ID, row.CreatedAt = current.ID, current.CreatedAt
	} else {
		row.ID, row.CreatedAt = s.newID("job_link"), now
	}
	row.UpdatedAt = now
	s.jobLinks[key] = row
	return row, nil
}

func (s *MemoryStore) ListJobLinks(_ context.Context, owner, workID, targetType string) ([]JobLink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []JobLink{}
	for _, row := range s.jobLinks {
		if row.OwnerUsername == owner && row.WorkID == workID && (targetType == "" || row.TargetType == targetType) {
			out = append(out, row)
		}
	}
	return out, nil
}

func (s *MemoryStore) GetHandoff(_ context.Context, owner, workID, requestKey string) (Handoff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.handoffs[owner+"\x00"+workID+"\x00"+requestKey]
	if !ok {
		return Handoff{}, ErrNotFound
	}
	return row, nil
}

func (s *MemoryStore) CreateHandoff(_ context.Context, row Handoff) (Handoff, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := row.OwnerUsername + "\x00" + row.WorkID + "\x00" + row.RequestKey
	if _, ok := s.handoffs[key]; ok {
		return Handoff{}, ErrConflict
	}
	if row.ID == "" {
		row.ID = s.newID("handoff")
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = s.now().UTC()
	}
	s.handoffs[key] = row
	return row, nil
}

func (s *MemoryStore) activeWork(owner, id string) (Work, error) {
	work, ok := s.works[id]
	if !ok || work.OwnerUsername != owner || work.DeletedAt != nil {
		return Work{}, ErrNotFound
	}
	return work, nil
}

func (s *MemoryStore) CreateWork(_ context.Context, owner string, input CreateWorkInput) (Work, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	owner, err := normalizedOwner(owner)
	if err != nil || !input.Type.Valid() {
		return Work{}, ErrValidation
	}
	title, err := NormalizeTitle(input.Title)
	if err != nil {
		return Work{}, err
	}
	now := s.now().UTC()
	work := Work{ID: s.newID("work"), OwnerUsername: owner, Title: title, Type: input.Type, SourceFilename: strings.TrimSpace(input.SourceFilename), Mark: MarkNone, CreatedAt: now, UpdatedAt: now}
	s.works[work.ID] = work
	return work, nil
}

func (s *MemoryStore) ListWorks(_ context.Context, owner string, filter WorkFilter) ([]WorkSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	rows := make([]WorkSummary, 0)
	for _, work := range s.works {
		if work.OwnerUsername != owner || work.DeletedAt != nil || filter.Type != "" && work.Type != filter.Type || filter.Mark != "" && work.Mark != filter.Mark || query != "" && !strings.Contains(strings.ToLower(work.Title), query) {
			continue
		}
		row := WorkSummary{Work: work}
		for _, chapter := range s.chapters {
			if chapter.OwnerUsername == owner && chapter.WorkID == work.ID {
				row.ChapterCount++
				row.WordCount += chapter.WordCount
			}
		}
		for _, script := range s.scripts {
			if script.OwnerUsername == owner && script.WorkID == work.ID {
				row.ScriptCount++
			}
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		switch filter.Sort {
		case "name-asc":
			return rows[i].Title < rows[j].Title
		case "name-desc":
			return rows[i].Title > rows[j].Title
		case "updated-asc":
			return rows[i].UpdatedAt.Before(rows[j].UpdatedAt)
		default:
			return rows[i].UpdatedAt.After(rows[j].UpdatedAt)
		}
	})
	return rows, nil
}

func (s *MemoryStore) GetWork(_ context.Context, owner, id string) (WorkDetail, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	work, err := s.activeWork(owner, id)
	if err != nil {
		return WorkDetail{}, err
	}
	detail := WorkDetail{Work: work, Chapters: []Chapter{}, Scripts: []Script{}}
	for _, chapter := range s.chapters {
		if chapter.OwnerUsername == owner && chapter.WorkID == id {
			detail.Chapters = append(detail.Chapters, chapter)
		}
	}
	for _, script := range s.scripts {
		if script.OwnerUsername == owner && script.WorkID == id {
			detail.Scripts = append(detail.Scripts, cloneScript(script))
			for _, chapterID := range script.SourceChapterIDs {
				for index := range detail.Chapters {
					if detail.Chapters[index].ID == chapterID {
						detail.Chapters[index].ReferencedByScriptCount++
					}
				}
			}
		}
	}
	sort.Slice(detail.Chapters, func(i, j int) bool { return detail.Chapters[i].Order < detail.Chapters[j].Order })
	sortScripts(detail.Scripts)
	return detail, nil
}

func (s *MemoryStore) UpdateWork(_ context.Context, owner, id string, input UpdateWorkInput, expected int64) (Work, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	work, err := s.activeWork(owner, id)
	if err != nil {
		return Work{}, err
	}
	if work.Revision != expected {
		return Work{}, ErrConflict
	}
	if input.Title != nil {
		work.Title, err = NormalizeTitle(*input.Title)
		if err != nil {
			return Work{}, err
		}
	}
	if input.Mark != nil {
		if !input.Mark.Valid() {
			return Work{}, ErrValidation
		}
		work.Mark = *input.Mark
	}
	work.Revision++
	work.UpdatedAt = s.now().UTC()
	s.works[id] = work
	return work, nil
}

func (s *MemoryStore) DeleteWork(_ context.Context, owner, id string, expected int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	work, err := s.activeWork(owner, id)
	if err != nil {
		return err
	}
	if work.Revision != expected {
		return ErrConflict
	}
	now := s.now().UTC()
	work.DeletedAt, work.UpdatedAt, work.Revision = &now, now, work.Revision+1
	s.works[id] = work
	return nil
}

func (s *MemoryStore) CreateChapter(_ context.Context, owner, workID string, input CreateChapterInput) (Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeWork(owner, workID); err != nil {
		return Chapter{}, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "新章节"
	}
	order := input.Order
	if order <= 0 {
		for _, chapter := range s.chapters {
			if chapter.OwnerUsername == owner && chapter.WorkID == workID && chapter.Order >= order {
				order = chapter.Order + 1
			}
		}
	}
	now := s.now().UTC()
	chapter := Chapter{ID: s.newID("chapter"), WorkID: workID, OwnerUsername: owner, Title: title, Order: order, Content: input.Content, WordCount: countWords(input.Content), CreatedAt: now, UpdatedAt: now}
	s.chapters[chapter.ID] = chapter
	s.touchWork(workID, now)
	return chapter, nil
}

func (s *MemoryStore) UpdateChapter(_ context.Context, owner, workID, id string, input UpdateChapterInput, expected int64) (Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeWork(owner, workID); err != nil {
		return Chapter{}, err
	}
	chapter, ok := s.chapters[id]
	if !ok || chapter.OwnerUsername != owner || chapter.WorkID != workID {
		return Chapter{}, ErrNotFound
	}
	if chapter.Revision != expected {
		return Chapter{}, ErrConflict
	}
	if input.Title != nil {
		title := strings.TrimSpace(*input.Title)
		if title == "" {
			return Chapter{}, ErrValidation
		}
		chapter.Title = title
	}
	if input.Content != nil {
		chapter.Content, chapter.WordCount = *input.Content, countWords(*input.Content)
	}
	chapter.Revision++
	chapter.UpdatedAt = s.now().UTC()
	s.chapters[id] = chapter
	s.touchWork(workID, chapter.UpdatedAt)
	return chapter, nil
}

func (s *MemoryStore) DeleteChapter(_ context.Context, owner, workID, id string, expected int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	chapter, ok := s.chapters[id]
	if !ok || chapter.OwnerUsername != owner || chapter.WorkID != workID {
		return ErrNotFound
	}
	if chapter.Revision != expected {
		return ErrConflict
	}
	delete(s.chapters, id)
	s.touchWork(workID, s.now().UTC())
	return nil
}

func (s *MemoryStore) ReorderChapters(_ context.Context, owner, workID string, ids []string) ([]Chapter, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeWork(owner, workID); err != nil {
		return nil, err
	}
	current := map[string]Chapter{}
	for id, chapter := range s.chapters {
		if chapter.OwnerUsername == owner && chapter.WorkID == workID {
			current[id] = chapter
		}
	}
	if len(current) != len(ids) {
		return nil, ErrValidation
	}
	seen := map[string]bool{}
	now := s.now().UTC()
	rows := make([]Chapter, 0, len(ids))
	for index, id := range ids {
		chapter, ok := current[id]
		if !ok || seen[id] {
			return nil, ErrValidation
		}
		seen[id] = true
		chapter.Order, chapter.Revision, chapter.UpdatedAt = index+1, chapter.Revision+1, now
		s.chapters[id] = chapter
		rows = append(rows, chapter)
	}
	s.touchWork(workID, now)
	return rows, nil
}

func (s *MemoryStore) CreateScript(_ context.Context, owner, workID string, input CreateScriptInput) (Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createScriptLocked(owner, workID, input)
}

func (s *MemoryStore) createScriptLocked(owner, workID string, input CreateScriptInput) (Script, error) {
	if _, err := s.activeWork(owner, workID); err != nil {
		return Script{}, err
	}
	if input.Episode <= 0 {
		return Script{}, ErrValidation
	}
	version := input.Version
	for _, row := range s.scripts {
		if row.OwnerUsername == owner && row.WorkID == workID && row.Episode == input.Episode {
			if version == row.Version {
				return Script{}, ErrConflict
			}
			if version <= 0 && row.Version >= version {
				version = row.Version + 1
			}
		}
	}
	if version <= 0 {
		version = 1
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = fmt.Sprintf("第%d集", input.Episode)
	}
	source := input.GenerationSource
	if source == "" {
		source = SourceManual
	}
	now := s.now().UTC()
	row := Script{ID: s.newID("script"), WorkID: workID, OwnerUsername: owner, Episode: input.Episode, Version: version, Title: title, Content: input.Content, SourceChapterIDs: append([]string(nil), input.SourceChapterIDs...), GenerationSource: source, ModelID: input.ModelID, PromptID: input.PromptID, PromptRevision: input.PromptRevision, CreatedAt: now, UpdatedAt: now}
	s.scripts[row.ID] = row
	s.touchWork(workID, now)
	return cloneScript(row), nil
}

func (s *MemoryStore) ListScripts(_ context.Context, owner, workID string) ([]Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeWork(owner, workID); err != nil {
		return nil, err
	}
	rows := []Script{}
	for _, row := range s.scripts {
		if row.OwnerUsername == owner && row.WorkID == workID {
			rows = append(rows, cloneScript(row))
		}
	}
	sortScripts(rows)
	return rows, nil
}

func (s *MemoryStore) UpdateScript(_ context.Context, owner, workID, id string, input UpdateScriptInput, expected int64) (Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.scripts[id]
	if !ok || row.OwnerUsername != owner || row.WorkID != workID {
		return Script{}, ErrNotFound
	}
	if row.Revision != expected {
		return Script{}, ErrConflict
	}
	if input.Title != nil {
		if strings.TrimSpace(*input.Title) == "" {
			return Script{}, ErrValidation
		}
		row.Title = strings.TrimSpace(*input.Title)
	}
	if input.Content != nil {
		row.Content = *input.Content
	}
	row.Revision++
	row.UpdatedAt = s.now().UTC()
	s.scripts[id] = row
	s.touchWork(workID, row.UpdatedAt)
	return cloneScript(row), nil
}

func (s *MemoryStore) DeleteScript(_ context.Context, owner, workID, id string, expected int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.scripts[id]
	if !ok || row.OwnerUsername != owner || row.WorkID != workID {
		return ErrNotFound
	}
	if row.Revision != expected {
		return ErrConflict
	}
	delete(s.scripts, id)
	s.touchWork(workID, s.now().UTC())
	return nil
}

func (s *MemoryStore) CopyScript(_ context.Context, owner, workID, id string) (Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.scripts[id]
	if !ok || row.OwnerUsername != owner || row.WorkID != workID {
		return Script{}, ErrNotFound
	}
	return s.createScriptLocked(owner, workID, CreateScriptInput{Episode: row.Episode, Title: row.Title, Content: row.Content, SourceChapterIDs: row.SourceChapterIDs, GenerationSource: SourceCopy, ModelID: row.ModelID, PromptID: row.PromptID, PromptRevision: row.PromptRevision})
}

func (s *MemoryStore) PreferScript(_ context.Context, owner, workID, id string, expected int64) (Script, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.scripts[id]
	if !ok || row.OwnerUsername != owner || row.WorkID != workID {
		return Script{}, ErrNotFound
	}
	if row.Revision != expected {
		return Script{}, ErrConflict
	}
	now := s.now().UTC()
	for otherID, other := range s.scripts {
		if other.OwnerUsername == owner && other.WorkID == workID && other.Episode == row.Episode && other.Preferred && otherID != id {
			other.Preferred, other.Revision, other.UpdatedAt = false, other.Revision+1, now
			s.scripts[otherID] = other
		}
	}
	row.Preferred, row.Revision, row.UpdatedAt = true, row.Revision+1, now
	s.scripts[id] = row
	s.touchWork(workID, now)
	return cloneScript(row), nil
}

func (s *MemoryStore) touchWork(id string, now time.Time) {
	work, ok := s.works[id]
	if !ok {
		return
	}
	work.UpdatedAt = now
	s.works[id] = work
}

func cloneScript(row Script) Script {
	row.SourceChapterIDs = append([]string(nil), row.SourceChapterIDs...)
	return row
}

func sortScripts(rows []Script) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Episode != rows[j].Episode {
			return rows[i].Episode < rows[j].Episode
		}
		if rows[i].Version != rows[j].Version {
			return rows[i].Version < rows[j].Version
		}
		return rows[i].CreatedAt.Before(rows[j].CreatedAt)
	})
}

func (s *MemoryStore) CreateJob(_ context.Context, row Job) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.activeWork(row.OwnerUsername, row.WorkID); err != nil {
		return Job{}, err
	}
	for _, existing := range s.jobs {
		if existing.OwnerUsername == row.OwnerUsername && existing.RequestKey == row.RequestKey {
			return cloneJob(existing), nil
		}
	}
	now := s.now().UTC()
	if row.ID == "" {
		row.ID = s.newID("job")
	}
	row.CreatedAt, row.UpdatedAt = now, now
	s.jobs[row.ID] = cloneJob(row)
	return cloneJob(row), nil
}

func (s *MemoryStore) GetJob(_ context.Context, owner, workID, id string) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.jobs[id]
	if !ok || row.OwnerUsername != owner || row.WorkID != workID {
		return Job{}, ErrNotFound
	}
	return cloneJob(row), nil
}

func (s *MemoryStore) ListJobs(_ context.Context, owner, workID string) ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := []Job{}
	for _, row := range s.jobs {
		if row.OwnerUsername == owner && row.WorkID == workID {
			rows = append(rows, cloneJob(row))
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.After(rows[j].CreatedAt) })
	return rows, nil
}

func (s *MemoryStore) ListRunnableJobs(_ context.Context, limit int) ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	rows := []Job{}
	for _, row := range s.jobs {
		if row.Status == JobQueued || row.Status == JobRunning && !row.LeaseUntil.After(now) {
			rows = append(rows, cloneJob(row))
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].UpdatedAt.Before(rows[j].UpdatedAt) })
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func (s *MemoryStore) ClaimJob(_ context.Context, owner, id, token string, until time.Time) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.jobs[id]
	now := s.now().UTC()
	if !ok || row.OwnerUsername != owner {
		return Job{}, ErrNotFound
	}
	if row.Status != JobQueued && !(row.Status == JobRunning && !row.LeaseUntil.After(now)) {
		return Job{}, ErrConflict
	}
	row.Status, row.LeaseToken, row.LeaseUntil, row.UpdatedAt = JobRunning, token, until, now
	row.Revision++
	s.jobs[id] = cloneJob(row)
	return cloneJob(row), nil
}

func (s *MemoryStore) UpdateJob(_ context.Context, row Job, expected int64) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.jobs[row.ID]
	if !ok || current.OwnerUsername != row.OwnerUsername || current.WorkID != row.WorkID {
		return Job{}, ErrNotFound
	}
	if current.Revision != expected {
		return Job{}, ErrConflict
	}
	row.Revision, row.UpdatedAt = expected+1, s.now().UTC()
	s.jobs[row.ID] = cloneJob(row)
	return cloneJob(row), nil
}

func (s *MemoryStore) CreateReview(_ context.Context, row Review) (Review, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.scripts[row.ScriptID]; !ok {
		return Review{}, ErrNotFound
	}
	if row.ID == "" {
		row.ID = s.newID("review")
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = s.now().UTC()
	}
	row.Result = append(json.RawMessage(nil), row.Result...)
	s.reviews[row.ID] = row
	return row, nil
}

func cloneJob(row Job) Job {
	row.Input = append(json.RawMessage(nil), row.Input...)
	row.Result = append(json.RawMessage(nil), row.Result...)
	return row
}
