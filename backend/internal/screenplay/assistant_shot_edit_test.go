package screenplay

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAssistantShotEditPreparesThenAtomicallyConfirms(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	b, _ := s.SaveStoryboard(ctx, Storyboard{ID: "b", ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: EngineXiaoluoV7, SourceSHA256: "source", Shots: []StoryboardShot{{ID: "s", Seq: 1, Duration: 4, VideoDesc: "old video", ImagePrompt: "old image", Line: "old line", Characters: "甲", SourceElements: json.RawMessage(`[1]`), ProtocolFields: json.RawMessage(`{"media":"keep","binding":"a"}`)}}})
	a, _, e := PrepareAssistantShotEdit(ctx, s, "alice", "p", "modify_image_prompt", json.RawMessage(`{"shot_id":"s","new_prompt":"new image","mode":"replace"}`))
	if e != nil {
		t.Fatal(e)
	}
	if a.Kind != "creative_shot" || a.CreativeShot.Before.Line != "old line" || a.CreativeShot.After.ImagePrompt != "new image" {
		t.Fatal(a)
	}
	unchanged, _ := s.GetStoryboard(ctx, "alice", "p", 1, EngineXiaoluoV7)
	if unchanged.Shots[0].ImagePrompt != "old image" {
		t.Fatal("preparation wrote")
	}
	r := NewMemoryAuthoringRepository()
	svc := AuthoringService{Store: s, Repository: r}
	st, _ := svc.Load(ctx, "alice", "p")
	st.Proposals = []AssistantProposal{{ID: "q", Revision: 1, Status: "pending", Actions: []AuthoringAction{a}}}
	st, e = svc.Save(ctx, "alice", "p", st, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Apply(ctx, "alice", "p", "q", st.Revision, false); e == nil {
		t.Fatal("unconfirmed")
	}
	out, e := svc.Apply(ctx, "alice", "p", "q", st.Revision, true)
	if e != nil || out.Proposals[0].Status != "applied" {
		t.Fatal(out, e)
	}
	now, _ := s.GetStoryboard(ctx, "alice", "p", 1, EngineXiaoluoV7)
	if now.Revision != b.Revision+1 || now.Shots[0].ImagePrompt != "new image" || now.Shots[0].Line != "old line" || string(now.Shots[0].ProtocolFields) != string(b.Shots[0].ProtocolFields) || now.SourceSHA256 != "source" {
		t.Fatal(now)
	}
	if _, e = svc.Apply(ctx, "alice", "p", "q", out.Revision, true); e == nil {
		t.Fatal("repeat")
	}
}

func TestAssistantShotEditRejectsUnsafeScopeAndModes(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	for _, n := range []int{1, 2} {
		s.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: n, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{ID: "s", Seq: 1, Duration: 4, VideoDesc: "old"}}})
	}
	for _, raw := range []string{`{"shot_id":"s","new_prompt":"x"}`, `{"episode_no":1,"shot_id":"other","new_prompt":"x"}`, `{"episode_no":1,"shot_id":"s","mode":"rewrite","new_prompt":"x"}`, `{"episode_no":1,"shot_id":"s","new_prompt":"x","owner":"bob"}`, `{"episode_no":1,"shot_id":"s","new_prompt":"x","SHOT_ID":"other"}`, `{"episode_no":1,"shot_id":"s","shot_id":"other","new_prompt":"x"}`, `{"episode_no":1,"shot_id":"s","new_prompt":null}`} {
		if _, _, e := PrepareAssistantShotEdit(ctx, s, "alice", "p", "modify_image_prompt", json.RawMessage(raw)); e == nil {
			t.Fatal("accepted", raw)
		}
	}
	if _, _, e := PrepareAssistantShotEdit(ctx, s, "bob", "p", "modify_image_prompt", json.RawMessage(`{"episode_no":1,"shot_id":"s","new_prompt":"x"}`)); e == nil {
		t.Fatal("owner")
	}
}

func TestAssistantShotOriginalDirectContracts(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "a"})
	s.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "a", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{ID: "s", Seq: 1, Duration: 3, VideoDesc: "video", ImagePrompt: "image", Line: "dialogue"}}})
	for _, tc := range []struct{ name, raw, field, want string }{

		{"modify_video_prompt", `{"shot_id":"s","action":"append","new_prompt":"追加"}`, "video_desc", "video追加"},
		{"modify_image_prompt", `{"shot_id":"s","action":"replace","new_prompt":"新图词"}`, "image_prompt", "新图词"},
		{"modify_storyboard", `{"shot_id":"s","action":"update","new_data":{"line":"新台词","emotion":"高兴"}}`, "line", "新台词"},
	} {
		a, _, e := PrepareAssistantShotEdit(ctx, s, "a", "p", tc.name, json.RawMessage(tc.raw))
		if e != nil {
			t.Fatal(tc.name, e)
		}
		ptr := shotText(&a.CreativeShot.After, tc.field)
		if ptr == nil || *ptr != tc.want {
			t.Fatal(a)
		}
		if tc.name == "modify_script_text" && a.CreativeShot.After.Line != "dialogue" {
			t.Fatal("画面描述写错台词")
		}
	}
}

func TestAssistantShotJSONSnapshotFormattingDoesNotCauseFalseConflict(t *testing.T) {
	_, a := shotCommitFixture()
	current := a.CreativeShot.Before
	current.SourceElements = json.RawMessage(`[ 1 ]`)
	current.ProtocolFields = json.RawMessage(`{ "binding" : "asset", "media" : "keep" }`)
	b := Storyboard{ID: "b", EpisodeNo: 1, Engine: EngineXiaoluoV7, Revision: 2, Shots: []StoryboardShot{current}}
	if e := applyCreativeShot(&b, a); e != nil {
		t.Fatal("JSON snapshot formatting differs after persistence", e)
	}
}

func TestAssistantShotScopeSelectionAndUnsupportedBranches(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "a"})
	for _, row := range []struct {
		n  int
		e  StoryboardEngine
		id string
	}{{1, EngineXiaoluoV7, "one"}, {1, EngineXiaoyunqueV3, "alternate"}, {2, EngineXiaoluoV7, "two"}} {
		s.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "a", EpisodeNo: row.n, Engine: row.e, Shots: []StoryboardShot{{ID: row.id, Seq: 1, Duration: 4, VideoDesc: "old"}}})
	}
	for _, raw := range []string{`{"episode_no":1,"shot_id":"one","new_prompt":"new"}`, `{"episode_no":1,"engine":"xiaoluo_v7_15s","shot_id":"two","new_prompt":"new"}`, `{"episode_no":1,"engine":"xiaoluo_v7_15s","shot_id":"one","shot_number":2,"new_prompt":"new"}`, `{"episode_no":1,"engine":"xiaoluo_v7_15s","shot_index":0,"new_prompt":"new"}`, `{"episode_no":1,"engine":"xiaoluo_v7_15s","shot_id":1,"new_prompt":"new"}`, `{"episode_no":1,"engine":"xiaoluo_v7_15s","shot_id":"one","action":"optimize","new_prompt":"new"}`} {
		if _, _, e := PrepareAssistantShotEdit(ctx, s, "a", "p", "modify_video_prompt", json.RawMessage(raw)); e == nil {
			t.Fatal("unsafe selector accepted", raw)
		}
	}
	a, _, e := PrepareAssistantShotEdit(ctx, s, "a", "p", "modify_video_prompt", json.RawMessage(`{"episode_no":2,"engine":"xiaoluo_v7_15s","shot_number":1,"action":"append","new_prompt":"new"}`))
	if e != nil || a.TargetID != "two" || a.CreativeShot.After.VideoDesc != "oldnew" {
		t.Fatal(a, e)
	}
	for _, tc := range []struct{ name, raw string }{{"modify_script_text", `{"shot_number":1,"new_text":"画面描述"}`}, {"modify_storyboard", `{"shot_id":"one","action":"add","new_data":{"line":"x"}}`}, {"modify_storyboard", `{"shot_id":"one","action":"delete"}`}, {"modify_storyboard", `{"shot_id":"one","action":"update","new_data":{"duration_sec":10}}`}, {"modify_storyboard", `{"shot_id":"one","action":"update","new_data":{"LINE":"x"}}`}, {"modify_storyboard", `{"shot_id":"one","action":"update","new_data":{"line":"x","line":"y"}}`}} {
		if _, _, e := PrepareAssistantShotEdit(ctx, s, "a", "p", tc.name, json.RawMessage(tc.raw)); e == nil {
			t.Fatal("unsupported branch accepted", tc)
		}
	}
}
