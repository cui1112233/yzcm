package scripteditor

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

type SourceRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}
type CoverageReport struct {
	Covered   []SourceRange `json:"covered"`
	Missing   []SourceRange `json:"missing"`
	Duplicate []SourceRange `json:"duplicate"`
}

type StoryboardShot struct {
	ID          string      `json:"id"`
	Scene       string      `json:"scene"`
	Picture     string      `json:"picture"`
	ShotSize    string      `json:"shotSize"`
	Camera      string      `json:"camera"`
	Movement    string      `json:"movement"`
	Bindings    []string    `json:"bindings"`
	Dialogue    string      `json:"dialogue"`
	Narration   string      `json:"narration"`
	Duration    float64     `json:"duration"`
	ImagePrompt string      `json:"imagePrompt"`
	VideoPrompt string      `json:"videoPrompt"`
	AudioPrompt string      `json:"audioPrompt"`
	SourceRange SourceRange `json:"sourceRange"`
}

type StoryboardDocument struct {
	ID            string           `json:"id"`
	OwnerUsername string           `json:"-"`
	WorkID        string           `json:"workId"`
	ScriptID      string           `json:"scriptId"`
	Episode       int              `json:"episode"`
	Shots         []StoryboardShot `json:"shots"`
	Revision      int64            `json:"revision"`
	CreatedAt     time.Time        `json:"createdAt"`
	UpdatedAt     time.Time        `json:"updatedAt"`
}

type StoryboardStore interface {
	GetStoryboard(context.Context, string, string, int) (StoryboardDocument, error)
	SaveStoryboard(context.Context, StoryboardDocument, int64) (StoryboardDocument, error)
}

func ComputeCoverage(length int, ranges []SourceRange) CoverageReport {
	if length < 0 {
		length = 0
	}
	points := make([]int, length+1)
	for _, row := range ranges {
		start, end := row.Start, row.End
		if start < 0 {
			start = 0
		}
		if end > length {
			end = length
		}
		if end <= start {
			continue
		}
		for index := start; index < end; index++ {
			points[index]++
		}
	}
	collect := func(match func(int) bool) []SourceRange {
		rows := []SourceRange{}
		start := -1
		for index := 0; index <= length; index++ {
			yes := index < length && match(points[index])
			if yes && start < 0 {
				start = index
			}
			if !yes && start >= 0 {
				rows = append(rows, SourceRange{Start: start, End: index})
				start = -1
			}
		}
		return rows
	}
	return CoverageReport{Covered: collect(func(v int) bool { return v > 0 }), Missing: collect(func(v int) bool { return v == 0 }), Duplicate: collect(func(v int) bool { return v > 1 })}
}

func (s *Service) Storyboard(ctx context.Context, owner, workID string, episode int) (StoryboardDocument, error) {
	store, ok := s.Store.(StoryboardStore)
	if !ok || episode <= 0 {
		return StoryboardDocument{}, ErrValidation
	}
	if _, err := s.Store.GetWork(ctx, owner, workID); err != nil {
		return StoryboardDocument{}, err
	}
	row, err := store.GetStoryboard(ctx, owner, workID, episode)
	if err == ErrNotFound {
		return StoryboardDocument{WorkID: workID, Episode: episode, Shots: []StoryboardShot{}}, nil
	}
	return row, err
}

func (s *Service) SaveStoryboard(ctx context.Context, owner, workID string, input StoryboardDocument, expected int64) (StoryboardDocument, error) {
	store, ok := s.Store.(StoryboardStore)
	if !ok || input.Episode <= 0 {
		return StoryboardDocument{}, ErrValidation
	}
	detail, err := s.Store.GetWork(ctx, owner, workID)
	if err != nil {
		return StoryboardDocument{}, err
	}
	if strings.TrimSpace(input.ScriptID) != "" {
		found := false
		for _, row := range detail.Scripts {
			if row.ID == input.ScriptID && row.Episode == input.Episode {
				found = true
				break
			}
		}
		if !found {
			return StoryboardDocument{}, ErrForbidden
		}
	}
	ids := map[string]bool{}
	for index := range input.Shots {
		if strings.TrimSpace(input.Shots[index].ID) == "" {
			input.Shots[index].ID = randomID("shot")
		}
		if ids[input.Shots[index].ID] || input.Shots[index].SourceRange.End < input.Shots[index].SourceRange.Start {
			return StoryboardDocument{}, ErrValidation
		}
		ids[input.Shots[index].ID] = true
	}
	input.OwnerUsername, input.WorkID = owner, workID
	return store.SaveStoryboard(ctx, input, expected)
}

func storyboardJSON(shots []StoryboardShot) ([]byte, error) { return json.Marshal(shots) }
func decodeStoryboardJSON(raw []byte) ([]StoryboardShot, error) {
	var shots []StoryboardShot
	err := json.Unmarshal(raw, &shots)
	if shots == nil {
		shots = []StoryboardShot{}
	}
	return shots, err
}
func sortStoryboards(rows []StoryboardDocument) {
	sort.Slice(rows, func(i, j int) bool { return rows[i].Episode < rows[j].Episode })
}
