package screenplay

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// SourceCoverageReport describes identifier coverage of the selected saved chapter,
// not semantic completeness of the novel. Reusing an element in different shots is valid.
type SourceCoverageReport struct {
	Status        string   `json:"status"`
	Reason        string   `json:"reason,omitempty"`
	Basis         string   `json:"basis"`
	Scope         string   `json:"scope"`
	ChapterID     string   `json:"chapter_id,omitempty"`
	EpisodeNo     int      `json:"episode_no,omitempty"`
	SourceSHA256  string   `json:"source_sha256,omitempty"`
	ExpectedTotal int      `json:"expected_total"`
	CoveredTotal  int      `json:"covered_total"`
	Missing       []string `json:"missing"`
	Extra         []string `json:"extra"`
	Duplicates    []string `json:"duplicates"`
	Reused        []string `json:"reused"`
	UnmappedShots []int    `json:"unmapped_shots"`
}

const coverageLimit = 10000
const coverageBytesLimit = 1024 * 1024

var numberedSourceLine = regexp.MustCompile(`(?m)^\s*([0-9]+)[.、．]\s*\S`)

// CheckStoryboardSourceCoverage never derives source identifiers from shot seq or
// SourceSceneIndex. The only supported mappings are saved source_elements values.
func CheckStoryboardSourceCoverage(chapter Chapter, original bool, shots []StoryboardShot) SourceCoverageReport {
	r := SourceCoverageReport{Status: "unverified", ChapterID: chapter.ID, Basis: "unavailable", Scope: "所选集当前保存的剧本元素编号；不代表小说情节完整", Missing: []string{}, Extra: []string{}, Duplicates: []string{}, Reused: []string{}, UnmappedShots: []int{}}
	source := chapter.ScriptText
	if original || strings.TrimSpace(source) == "" {
		source = chapter.SourceText
		r.Scope = "所选集当前保存的章节原文显式编号；不代表整部小说情节完整"
	}
	if len(source) > coverageBytesLimit || len(chapter.RawResult) > coverageBytesLimit {
		r.Reason = "来源内容超出核验上限"
		return r
	}
	if strings.TrimSpace(source) == "" {
		r.Reason = "所选集没有已保存的来源正文"
		return r
	}
	r.SourceSHA256 = contentDigest(source)
	ids := []string{}
	if !original && strings.TrimSpace(chapter.ScriptText) != "" && strings.TrimSpace(chapter.RawResult) != "" {
		rendered, e := professionalScriptMarkdown(chapter.RawResult)
		if e != nil || strings.TrimSpace(rendered) != strings.TrimSpace(source) {
			r.Reason = "保存的元素编号与当前剧本不一致"
			return r
		}
		payload, err := decodeProfessionalScriptObject(chapter.RawResult)
		if err == nil {
			var doc struct {
				Scenes []struct {
					Content []struct {
						ID string `json:"element_id"`
					} `json:"script_content"`
				} `json:"scenes"`
			}
			if json.Unmarshal(payload, &doc) != nil {
				r.Reason = "来源元素无法读取"
				return r
			}
			for _, scene := range doc.Scenes {
				for _, element := range scene.Content {
					if strings.TrimSpace(element.ID) == "" {
						r.Reason = "剧本元素缺少稳定编号"
						return r
					}
					ids = append(ids, element.ID)
				}
			}
			r.Basis = "saved_script_element_ids"
		}
	}
	if len(ids) == 0 {
		for _, m := range numberedSourceLine.FindAllStringSubmatch(source, coverageLimit+1) {
			ids = append(ids, m[1])
		}
		r.Basis = "saved_chapter_numbered_lines"
	}
	if len(ids) == 0 || len(ids) > coverageLimit {
		r.Reason = "来源没有可核验的稳定编号或超出核验上限"
		return r
	}
	expected := map[string]bool{}
	for _, id := range ids {
		if expected[id] {
			r.Reason = "来源编号重复，无法唯一核验"
			return r
		}
		expected[id] = true
	}
	r.ExpectedTotal = len(ids)
	seen := map[string]int{}
	extra := map[string]bool{}
	duplicate := map[string]bool{}
	for _, shot := range shots {
		members := 0
		references, ok := coverageShotReferences(shot, 0, &members)
		if !ok || len(references) == 0 {
			r.UnmappedShots = append(r.UnmappedShots, shot.Seq)
			continue
		}
		local := map[string]bool{}
		for _, id := range references {
			if local[id] {
				duplicate[id] = true
				continue
			}
			local[id] = true
			seen[id]++
			if !expected[id] {
				extra[id] = true
			}
		}
	}
	for _, id := range ids {
		if seen[id] == 0 {
			r.Missing = append(r.Missing, id)
		} else {
			r.CoveredTotal++
			if seen[id] > 1 {
				r.Reused = append(r.Reused, id)
			}
		}
	}
	for id := range extra {
		r.Extra = append(r.Extra, id)
	}
	sort.Strings(r.Extra)
	for id := range duplicate {
		r.Duplicates = append(r.Duplicates, id)
	}
	sort.Strings(r.Duplicates)
	if len(r.UnmappedShots) > 0 || len(shots) == 0 {
		r.Missing = []string{}
		r.Reason = "部分镜头没有可核验的来源映射"
		return r
	}
	r.Status = "passed"
	if len(r.Missing)+len(r.Extra)+len(r.Duplicates) > 0 {
		r.Status = "incomplete"
	}
	return r
}

// A real merge keeps the first member's protocol alongside an archive of every
// member. Validate that inherited field against the first member, and the current
// aggregate against all members; neither archive nor aggregate may hide a gap.
func coverageShotReferences(shot StoryboardShot, depth int, visited *int) ([]string, bool) {
	*visited++
	if depth > 8 || *visited > coverageLimit || len(shot.ProtocolFields) > coverageBytesLimit || len(shot.SourceElements) > coverageBytesLimit {
		return nil, false
	}
	var fields map[string]json.RawMessage
	if len(shot.ProtocolFields) > 0 && json.Unmarshal(shot.ProtocolFields, &fields) != nil {
		return nil, false
	}
	if archive, merged := fields["merged_shots"]; merged {
		var members []StoryboardShot
		if json.Unmarshal(archive, &members) != nil || len(members) < 2 || len(members) > coverageLimit {
			return nil, false
		}
		if count, exists := fields["merged_count"]; exists {
			var n int
			if json.Unmarshal(count, &n) != nil || n != len(members) {
				return nil, false
			}
		}
		groups := make([][]string, 0, len(members))
		for _, member := range members {
			refs, ok := coverageShotReferences(member, depth+1, visited)
			if !ok || len(refs) == 0 {
				return nil, false
			}
			groups = append(groups, refs)
		}
		raw, _ := json.Marshal(groups)
		refs, ok := coverageReferences(raw)
		if !ok {
			return nil, false
		}
		if len(shot.SourceElements) > 0 && string(shot.SourceElements) != "null" {
			current, valid := coverageReferences(shot.SourceElements)
			if !valid || !sameCoverageReferences(current, refs) {
				return nil, false
			}
		}
		if inherited, exists := fields["source_elements"]; exists {
			var firstFields map[string]json.RawMessage
			if json.Unmarshal(members[0].ProtocolFields, &firstFields) != nil {
				return nil, false
			}
			first, valid := coverageReferences(firstFields["source_elements"])
			current, currentValid := coverageReferences(inherited)
			if !valid || !currentValid || !sameCoverageReferences(first, current) {
				return nil, false
			}
		}
		return refs, true
	}
	raw := shot.SourceElements
	if len(raw) == 0 || string(raw) == "null" {
		raw = fields["source_elements"]
	}
	refs, ok := coverageReferences(raw)
	if !ok || len(refs) == 0 {
		return nil, false
	}
	if alternate := fields["source_elements"]; len(alternate) > 0 && len(shot.SourceElements) > 0 && string(shot.SourceElements) != "null" {
		other, valid := coverageReferences(alternate)
		if !valid || !sameCoverageReferences(refs, other) {
			return nil, false
		}
	}
	return refs, true
}

// Merging existing shots saves nested arrays; flatten only arrays of explicit
// string IDs, within fixed size/depth limits. Objects and inferred ranges are unknown.
func coverageReferences(raw json.RawMessage) ([]string, bool) {
	if len(raw) == 0 || len(raw) > coverageBytesLimit {
		return nil, false
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return nil, false
	}
	visited := 0
	var walk func(any, int) ([]string, bool)
	walk = func(value any, depth int) ([]string, bool) {
		if depth > 8 || visited > coverageLimit {
			return nil, false
		}
		switch x := value.(type) {
		case string:
			visited++
			if strings.TrimSpace(x) == "" {
				return nil, false
			}
			return []string{x}, visited <= coverageLimit
		case []any:
			if len(x) == 0 {
				return nil, false
			}
			out := []string{}
			direct, maximum := map[string]int{}, map[string]int{}
			for _, item := range x {
				refs, ok := walk(item, depth+1)
				if !ok {
					return nil, false
				}
				counts := map[string]int{}
				for _, id := range refs {
					counts[id]++
					count := counts[id]
					if _, scalar := item.(string); scalar {
						direct[id]++
						count = direct[id]
					}
					// Nested arrays are saved merge groups. Reuse across groups is
					// legitimate; repeated IDs within an original group remain visible.
					if count > maximum[id] {
						maximum[id] = count
						out = append(out, id)
					}
				}
			}
			return out, true
		default:
			return nil, false
		}
	}
	return walk(v, 0)
}
func sameCoverageReferences(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string{}, a...)
	y := append([]string{}, b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// CheckStoryboard adds independent source evidence without changing legacy seq
// results or the seq-only fill path. Reads use the same authoring repository as generation.
func (s *StoryboardService) CheckStoryboard(ctx context.Context, owner, project string, episode int, engine StoryboardEngine) (StoryboardCheckReport, error) {
	board, err := s.Store.GetStoryboard(ctx, owner, project, episode, engine)
	if err != nil {
		return StoryboardCheckReport{}, err
	}
	report, err := CheckStoryboardShots(board.Shots)
	if err != nil {
		return report, err
	}
	p, err := s.Store.GetProject(ctx, owner, project)
	if err != nil {
		return report, err
	}
	chapter, _, original, err := s.storyboardSourceChapter(ctx, p, board)
	if err != nil {
		return report, err
	}
	report.SourceCoverage = CheckStoryboardSourceCoverage(chapter, original, board.Shots)
	report.SourceCoverage.EpisodeNo = episode
	return report, nil
}

// storyboardSourceChapter shares the saved episode/chapter selector across
// source coverage and review inputs. A missing bound chapter is not guessed.
func (s *StoryboardService) storyboardSourceChapter(ctx context.Context, p Project, board Storyboard) (Chapter, string, bool, error) {
	state, err := s.storyboardAuthoringState(ctx, p)
	if err != nil {
		return Chapter{}, "", false, err
	}
	boundID := ""
	original := false
	if history, ok := s.Store.(StoryboardGenerationStore); ok {
		generations, e := history.LoadStoryboardGenerations(ctx, p.OwnerUsername, p.ID)
		if e != nil {
			return Chapter{}, "", false, e
		}
		var latest *StoryboardGenerationAttempt
		var latestRevision int64
		for _, attempt := range generations.Attempts {
			if attempt == nil || attempt.Engine != board.Engine {
				continue
			}
			ep := attempt.Episodes[board.EpisodeNo]
			if ep == nil || ep.Storyboard == nil || ep.Storyboard.ID != board.ID {
				continue
			}
			generated := ep.Storyboard
			if generated.Revision > board.Revision || generated.SourceSHA256 != board.SourceSHA256 {
				continue
			}
			if latest == nil || generated.Revision > latestRevision || generated.Revision == latestRevision && attempt.UpdatedAt.After(latest.UpdatedAt) {
				latest, latestRevision = attempt, generated.Revision
			}
		}
		if latest != nil {
			boundID = latest.ChapterID
			original = latest.UseOriginal
		}
	}
	if state != nil {
		for i, c := range state.Chapters {
			if (boundID != "" && c.ID == boundID) || (boundID == "" && i+1 == board.EpisodeNo) {
				return c, boundID, original, nil
			}
		}
	}
	return Chapter{}, boundID, original, nil
}
