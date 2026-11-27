package screenplay

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAssistantShotStructureRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "a"})
	s.SaveStoryboard(ctx, Storyboard{ID: "b", ProjectID: "p", OwnerUsername: "a", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{ID: "s1", Seq: 1, Duration: 4, VideoDesc: "one"}, {ID: "s2", Seq: 2, Duration: 4, VideoDesc: "two", ProtocolFields: json.RawMessage(`{"media":"keep"}`)}}})
	for _, tc := range []struct {
		name, raw string
		count     int
	}{{"add_shot", `{"insert_after_shot_id":"s1","new_data":{"video_desc":"new","duration_sec":4}}`, 3}, {"delete_shots", `{"shot_ids":["s1"]}`, 2}} {
		a, _, e := PrepareAssistantShotEdit(ctx, s, "a", "p", tc.name, json.RawMessage(tc.raw))
		if e != nil {
			t.Fatal(e)
		}
		r := NewMemoryAuthoringRepository()
		svc := AuthoringService{Store: s, Repository: r}
		st, _ := svc.Load(ctx, "a", "p")
		st.Proposals = []AssistantProposal{{ID: "q", Revision: 1, Status: "pending", Actions: []AuthoringAction{a}}}
		st, e = svc.Save(ctx, "a", "p", st, 0)
		if e != nil {
			t.Fatal(e)
		}
		out, e := svc.Apply(ctx, "a", "p", "q", st.Revision, true)
		if e != nil {
			t.Fatal(e)
		}
		b, _ := s.GetStoryboard(ctx, "a", "p", 1, EngineXiaoluoV7)
		if len(b.Shots) != tc.count {
			t.Fatal(b)
		}
		for i, sh := range b.Shots {
			if sh.Seq != i+1 {
				t.Fatal(b)
			}
		}
		if _, e = svc.Apply(ctx, "a", "p", "q", out.Revision, true); e == nil {
			t.Fatal("repeat")
		}
	}
}
func TestAssistantShotStructureRejectsAndSnapshots(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "a"})
	s.SaveStoryboard(ctx, Storyboard{ID: "b", ProjectID: "p", OwnerUsername: "a", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{ID: "s1", Seq: 1, Duration: 4, VideoDesc: "one"}, {ID: "s2", Seq: 2, Duration: 4, VideoDesc: "two"}}})
	for _, tc := range []struct{ name, raw string }{
		{"add_shot", `{"insert_after_shot_id":"missing","new_data":{"line":"x"}}`}, {"add_shot", `{"insert_after_shot_number":1,"insert_after_shot_id":"s1","new_data":{"line":"x"}}`}, {"add_shot", `{"new_data":{"id":"fake","line":"x"}}`}, {"add_shot", `{"new_data":{"source_elements":[1],"line":"x"}}`}, {"add_shot", `{"new_data":{"line":"x"},"action":"delete"}`}, {"add_shot", `{"new_data":{"line":"x","line":"y"}}`}, {"add_shot", `{"new_data":{"LINE":"x"}}`}, {"add_shot", `{"new_data":null}`},
		{"delete_shots", `{"shot_ids":["s1","other"]}`}, {"delete_shots", `{"shot_ids":["s1"],"shot_numbers":[1]}`}, {"delete_shots", `{"shot_ids":["s1","s1"]}`}, {"delete_shots", `{"shot_numbers":[1,1]}`}, {"delete_shots", `{"shot_numbers":[0]}`}, {"delete_shots", `{"shot_ids":[]}`}, {"delete_shots", `{"shot_ids":["s1"],"shot_ids":["s2"]}`}, {"delete_shots", `{"shot_ids":["s1"],"creative_structure":{}}`}, {"delete_shots", `{"shot_ids":["s1"],"episode_no":2}`}, {"delete_shots", `{"shot_ids":["s1"],"engine":"prompt_138"}`},
	} {
		if _, _, e := PrepareAssistantShotEdit(ctx, s, "a", "p", tc.name, json.RawMessage(tc.raw)); e == nil {
			t.Fatal("accepted", tc)
		}
	}
	if _, _, e := PrepareAssistantShotEdit(ctx, s, "bob", "p", "delete_shots", json.RawMessage(`{"shot_ids":["s1"]}`)); e == nil {
		t.Fatal("cross owner")
	}
	a, _, e := PrepareAssistantShotEdit(ctx, s, "a", "p", "modify_storyboard", json.RawMessage(`{"action":"delete","shot_numbers":[1]}`))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := s.GetStoryboard(ctx, "a", "p", 1, EngineXiaoluoV7)
	b.Shots[1].VideoDesc = "changed same revision"
	if e = applyShotStructure(&b, a); e == nil {
		t.Fatal("same-version unselected content change")
	}
	b, _ = s.GetStoryboard(ctx, "a", "p", 1, EngineXiaoluoV7)
	b.Revision++
	if e = applyShotStructure(&b, a); e == nil {
		t.Fatal("stale board")
	}
	a.CreativeStructure.After[0].VideoDesc = "forged"
	if e = validateShotStructure(a); e == nil {
		t.Fatal("forged remaining field")
	}
	s.SaveStoryboard(ctx, Storyboard{ID: "other", ProjectID: "p", OwnerUsername: "a", EpisodeNo: 2, Engine: StoryboardEngine("prompt_138"), Shots: []StoryboardShot{{ID: "other", Seq: 1, Duration: 4, Line: "other"}}})
	if _, _, e = PrepareAssistantShotEdit(ctx, s, "a", "p", "delete_shots", json.RawMessage(`{"shot_numbers":[1]}`)); e == nil {
		t.Fatal("ambiguous board")
	}
}
func TestAssistantShotStructureEmptyBoardAndLateSave(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "a"})
	original, _ := s.SaveStoryboard(ctx, Storyboard{ID: "b", ProjectID: "p", OwnerUsername: "a", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{ID: "s", Seq: 1, Duration: 4, Line: "one"}}})
	r := NewMemoryAuthoringRepository()
	svc := AuthoringService{Store: s, Repository: r}
	for _, tc := range []struct{ name, raw string }{{"delete_shots", `{"shot_ids":["s"]}`}, {"modify_storyboard", `{"action":"add","new_data":{"line":"new"}}`}} {
		a, _, e := PrepareAssistantShotEdit(ctx, s, "a", "p", tc.name, json.RawMessage(tc.raw))
		if e != nil {
			t.Fatal(e)
		}
		st, _ := svc.Load(ctx, "a", "p")
		st.Proposals = []AssistantProposal{{ID: "q", Revision: st.Revision + 1, Status: "pending", Actions: []AuthoringAction{a}}}
		st, e = svc.Save(ctx, "a", "p", st, st.Revision)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = svc.Apply(ctx, "a", "p", "q", st.Revision, true); e != nil {
			t.Fatal(e)
		}
	}
	b, _ := s.GetStoryboard(ctx, "a", "p", 1, EngineXiaoluoV7)
	if len(b.Shots) != 1 || b.Shots[0].ID == "s" || b.Shots[0].Seq != 1 || len(b.Shots[0].SourceElements) > 0 {
		t.Fatal(b)
	}
	if _, e := s.SaveStoryboardRevision(ctx, original, original.Revision); e == nil {
		t.Fatal("late result resurrected original")
	}
}
func TestAssistantShotStructureAtomicConflict(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "a"})
	s.SaveStoryboard(ctx, Storyboard{ID: "b", ProjectID: "p", OwnerUsername: "a", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{ID: "s", Seq: 1, Duration: 4, Line: "original"}}})
	a, _, e := PrepareAssistantShotEdit(ctx, s, "a", "p", "delete_shots", json.RawMessage(`{"shot_ids":["s"]}`))
	if e != nil {
		t.Fatal(e)
	}
	r := NewMemoryAuthoringRepository()
	svc := AuthoringService{Store: s, Repository: r}
	st, _ := svc.Load(ctx, "a", "p")
	st.Proposals = []AssistantProposal{{ID: "q", Revision: 1, Status: "pending", Actions: []AuthoringAction{a}}}
	st, _ = svc.Save(ctx, "a", "p", st, 0)
	if _, e = svc.Apply(ctx, "a", "p", "q", st.Revision+1, true); e == nil {
		t.Fatal("stale state")
	}
	s.mu.Lock()
	s.storyboards[projectKey("a", "p")][0].Shots[0].Line = "concurrent same version"
	s.mu.Unlock()
	if _, e = svc.Apply(ctx, "a", "p", "q", st.Revision, true); e == nil {
		t.Fatal("changed shot accepted")
	}
	fresh, _ := svc.Load(ctx, "a", "p")
	b, _ := s.GetStoryboard(ctx, "a", "p", 1, EngineXiaoluoV7)
	if fresh.Revision != st.Revision || fresh.Proposals[0].Status != "pending" || len(b.Shots) != 1 || b.Shots[0].Line != "concurrent same version" {
		t.Fatal(fresh, b)
	}
}
