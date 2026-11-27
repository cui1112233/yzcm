package screenplay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Full ordered snapshots protect even same-version writes to unselected shots.
type AssistantShotStructure struct {
	BoardID   string           `json:"board_id"`
	Episode   int              `json:"episode_no"`
	Engine    StoryboardEngine `json:"engine"`
	Operation string           `json:"operation"`
	Before    []StoryboardShot `json:"before"`
	After     []StoryboardShot `json:"after"`
	Targets   []StoryboardShot `json:"targets"`
}

func isShotStructure(name string, raw json.RawMessage) bool {
	if name == "add_shot" || name == "delete_shots" {
		return true
	}
	if name != "modify_storyboard" {
		return false
	}
	var p struct {
		Action string `json:"action"`
	}
	json.Unmarshal(raw, &p)
	return p.Action == "add" || p.Action == "delete"
}
func PrepareAssistantShotStructure(ctx context.Context, s Store, o, p, name string, raw json.RawMessage) (AuthoringAction, string, error) {
	fail := func() (AuthoringAction, string, error) {
		return AuthoringAction{}, "", fmt.Errorf("%w: 新增/删除镜头参数无效、目标不存在或有歧义", ErrInvalidArgument)
	}
	f, e := assistantShotObject(raw, "episode_no", "engine", "action", "insert_after_shot_id", "insert_after_shot_number", "new_data", "shot_ids", "shot_numbers")
	if e != nil {
		return AuthoringAction{}, "", e
	}
	op := "add"
	if name == "delete_shots" {
		op = "delete"
	}
	var action string
	if f["action"] != nil {
		if json.Unmarshal(f["action"], &action) != nil {
			return fail()
		}
		if name == "modify_storyboard" {
			op = action
		} else if action != op {
			return fail()
		}
	}
	if op != "add" && op != "delete" {
		return fail()
	}
	var ep int
	var engine StoryboardEngine
	if f["episode_no"] != nil && (json.Unmarshal(f["episode_no"], &ep) != nil || ep <= 0) {
		return fail()
	}
	if f["engine"] != nil && (json.Unmarshal(f["engine"], &engine) != nil || engine == "" || len(engine) > 128) {
		return fail()
	}
	if _, e = s.GetProject(ctx, o, p); e != nil {
		return AuthoringAction{}, "", e
	}
	bs, e := s.ListStoryboards(ctx, o, p, engine)
	if e != nil {
		return AuthoringAction{}, "", e
	}
	var matches []Storyboard
	for _, b := range bs {
		if ep == 0 || ep == b.EpisodeNo {
			matches = append(matches, b)
		}
	}
	if len(matches) != 1 {
		return fail()
	}
	b := matches[0]
	sort.Slice(b.Shots, func(i, j int) bool { return b.Shots[i].Seq < b.Shots[j].Seq })
	if len(b.Shots) > 10000 || (op == "add" && len(b.Shots) == 10000) {
		return fail()
	}
	d := AssistantShotStructure{BoardID: b.ID, Episode: b.EpisodeNo, Engine: b.Engine, Operation: op, Before: append([]StoryboardShot{}, b.Shots...), After: []StoryboardShot{}, Targets: []StoryboardShot{}}
	if op == "add" {
		if f["shot_ids"] != nil || f["shot_numbers"] != nil || f["insert_after_shot_id"] != nil && f["insert_after_shot_number"] != nil {
			return fail()
		}
		var id string
		var n int
		if f["insert_after_shot_id"] != nil && (json.Unmarshal(f["insert_after_shot_id"], &id) != nil || id == "" || len(id) > 256 || strings.TrimSpace(id) != id) {
			return fail()
		}
		if f["insert_after_shot_number"] != nil && (json.Unmarshal(f["insert_after_shot_number"], &n) != nil || n <= 0) {
			return fail()
		}
		pos := len(b.Shots)
		if id != "" || n != 0 {
			pos = -1
			for i, sh := range b.Shots {
				if id != "" && sh.ID == id || n != 0 && sh.Seq == n {
					if pos != -1 {
						return fail()
					}
					pos = i + 1
				}
			}
			if pos < 0 {
				return fail()
			}
		}
		fields, e := assistantShotObject(f["new_data"], "duration_sec", "video_desc", "image_prompt", "line", "voice_actor", "emotion")
		if e != nil {
			return AuthoringAction{}, "", e
		}
		sh := StoryboardShot{ID: newScreenplayID("sb_shot"), StoryboardID: b.ID, ProjectID: p, OwnerUsername: o, Duration: 4}
		for k, v := range fields {
			if k == "duration_sec" {
				if json.Unmarshal(v, &sh.Duration) != nil {
					return fail()
				}
			} else {
				dst := shotText(&sh, k)
				if json.Unmarshal(v, dst) != nil || len(*dst) > 16384 {
					return fail()
				}
			}
		}
		if len(fields) == 0 {
			return fail()
		}
		sh.Seq = pos + 1
		if e = validateStoryboardShotInput(sh); e != nil {
			return AuthoringAction{}, "", e
		}
		d.After = append(d.After, b.Shots[:pos]...)
		d.After = append(d.After, sh)
		d.After = append(d.After, b.Shots[pos:]...)
		d.Targets = append(d.Targets, sh)
	} else {
		if f["new_data"] != nil || f["insert_after_shot_id"] != nil || f["insert_after_shot_number"] != nil || (f["shot_ids"] == nil) == (f["shot_numbers"] == nil) {
			return fail()
		}
		selected := map[string]bool{}
		if f["shot_ids"] != nil {
			var ids []string
			if json.Unmarshal(f["shot_ids"], &ids) != nil || len(ids) == 0 || len(ids) > 200 {
				return fail()
			}
			for _, id := range ids {
				if id == "" || len(id) > 256 || strings.TrimSpace(id) != id || selected[id] {
					return fail()
				}
				selected[id] = true
			}
		} else {
			var nums []int
			if json.Unmarshal(f["shot_numbers"], &nums) != nil || len(nums) == 0 || len(nums) > 200 {
				return fail()
			}
			seen := map[int]bool{}
			for _, n := range nums {
				if n <= 0 || seen[n] {
					return fail()
				}
				seen[n] = true
				found := ""
				for _, sh := range b.Shots {
					if sh.Seq == n {
						if found != "" {
							return fail()
						}
						found = sh.ID
					}
				}
				if found == "" {
					return fail()
				}
				selected[found] = true
			}
		}
		for _, sh := range b.Shots {
			if selected[sh.ID] {
				d.Targets = append(d.Targets, sh)
				delete(selected, sh.ID)
			} else {
				d.After = append(d.After, sh)
			}
		}
		if len(selected) > 0 {
			return fail()
		}
	}
	for i := range d.After {
		d.After[i].Seq = i + 1
	}
	a := AuthoringAction{Kind: "creative_shot_structure", TargetID: b.ID, ExpectedRevision: b.Revision, CreativeStructure: &d}
	if e = validateShotStructure(a); e != nil {
		return AuthoringAction{}, "", e
	}
	verb := "新增"
	if op == "delete" {
		verb = "删除"
	}
	return a, fmt.Sprintf("%s第%d集%d个镜头", verb, b.EpisodeNo, len(d.Targets)), nil
}
func sameShotSnapshot(a, b any) bool {
	aa, e := json.Marshal(a)
	bb, f := json.Marshal(b)
	return e == nil && f == nil && bytes.Equal(aa, bb)
}
func validateShotStructure(a AuthoringAction) error {
	d := a.CreativeStructure
	if d == nil || d.BoardID == "" || a.TargetID != d.BoardID || a.ExpectedRevision <= 0 || d.Episode <= 0 || d.Engine == "" || len(d.Targets) == 0 || len(d.Before) > 10000 || len(d.After) > 10000 {
		return ErrInvalidArgument
	}
	ids := map[string]bool{}
	seqs := map[int]bool{}
	for _, sh := range d.Before {
		if sh.ID == "" || ids[sh.ID] || sh.Seq <= 0 || seqs[sh.Seq] {
			return ErrInvalidArgument
		}
		ids[sh.ID] = true
		seqs[sh.Seq] = true
	}
	targets := map[string]StoryboardShot{}
	for _, sh := range d.Targets {
		if sh.ID == "" {
			return ErrInvalidArgument
		}
		if _, ok := targets[sh.ID]; ok {
			return ErrInvalidArgument
		}
		targets[sh.ID] = sh
	}
	expected := []StoryboardShot{}
	switch d.Operation {
	case "add":
		if len(d.Targets) != 1 || ids[d.Targets[0].ID] || len(d.After) != len(d.Before)+1 {
			return ErrInvalidArgument
		}
		added := d.Targets[0]
		if added.ProjectID == "" || added.OwnerUsername == "" || added.StoryboardID != d.BoardID || added.SceneID != 0 || added.Characters != "" || added.Scene != "" || added.Props != "" || len(added.SourceElements) > 0 || len(added.ProtocolFields) > 0 || added.SourceSceneIndex != 0 || added.SourceSceneLocalOrder != 0 || validateStoryboardShotInput(added) != nil {
			return ErrInvalidArgument
		}
		pos := added.Seq - 1
		if pos < 0 || pos > len(d.Before) {
			return ErrInvalidArgument
		}
		expected = append(expected, d.Before[:pos]...)
		expected = append(expected, added)
		expected = append(expected, d.Before[pos:]...)
	case "delete":
		if len(d.Targets) > 200 {
			return ErrInvalidArgument
		}
		for _, sh := range d.Before {
			if target, ok := targets[sh.ID]; ok {
				if !sameShotSnapshot(sh, target) {
					return ErrInvalidArgument
				}
				delete(targets, sh.ID)
			} else {
				expected = append(expected, sh)
			}
		}
		if len(targets) > 0 {
			return ErrInvalidArgument
		}
	default:
		return ErrInvalidArgument
	}
	for i := range expected {
		expected[i].Seq = i + 1
	}
	if !sameShotSnapshot(expected, d.After) {
		return ErrInvalidArgument
	}
	return nil
}
func applyShotStructure(b *Storyboard, a AuthoringAction) error {
	if e := validateShotStructure(a); e != nil {
		return e
	}
	d := a.CreativeStructure
	if b.ID != d.BoardID || b.Revision != a.ExpectedRevision || b.EpisodeNo != d.Episode || b.Engine != d.Engine {
		return ErrRevisionConflict
	}
	if b.Shots == nil {
		b.Shots = []StoryboardShot{}
	}
	sort.Slice(b.Shots, func(i, j int) bool { return b.Shots[i].Seq < b.Shots[j].Seq })
	if !sameShotSnapshot(b.Shots, d.Before) {
		return ErrRevisionConflict
	}
	b.Shots = append([]StoryboardShot{}, d.After...)
	b.ShotCount = len(b.Shots)
	return nil
}
