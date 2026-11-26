package screenplay

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestAssistantReadToolsScopedAndReadOnly(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	for _, owner := range []string{"alice", "bob"} {
		_, err := store.CreateProject(ctx, Project{ID: "p", OwnerUsername: owner, SourceText: owner + "原文"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.CreateAsset(ctx, Asset{ID: owner + "-asset", ProjectID: "p", OwnerUsername: owner, Type: AssetCharacter, Name: owner + "角色"})
		if err != nil {
			t.Fatal(err)
		}
		_, err = store.SaveStoryboard(ctx, Storyboard{ID: owner + "-board", ProjectID: "p", OwnerUsername: owner, Engine: EngineXiaoluoV7, EpisodeNo: 1, Shots: []StoryboardShot{{ID: owner + "-shot", Seq: 1, VideoDesc: owner + "视频提示", ImagePrompt: owner + "图片提示", Line: owner + "台词"}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	before, _ := store.GetStoryboard(ctx, "alice", "p", 1, EngineXiaoluoV7)
	beforeRaw, _ := json.Marshal(before)
	tools := []string{"get_current_screenplay", "analyze_screenplay", "get_editor_status", "get_shot_numbers", "get_shot_detail", "read_assets", "read_shot_video_prompts", "read_shot_image_prompts", "get_all_shots_voicetext"}
	for _, name := range tools {
		t.Run(name, func(t *testing.T) {
			args := json.RawMessage(`{}`)
			if name == "get_shot_detail" {
				args = json.RawMessage(`{"shot_id":"alice-shot"}`)
			}
			run, data, err := ReadAssistantTool(ctx, store, "alice", "p", name, args)
			if err != nil || !run.OK {
				t.Fatalf("%s: %v %#v", name, err, run)
			}
			raw, _ := json.Marshal(data)
			if strings.Contains(string(raw), "bob") {
				t.Fatal("another member leaked", string(raw))
			}
			if data == nil {
				t.Fatal("missing actual result")
			}
		})
	}
	after, _ := store.GetStoryboard(ctx, "alice", "p", 1, EngineXiaoluoV7)
	afterRaw, _ := json.Marshal(after)
	if string(beforeRaw) != string(afterRaw) {
		t.Fatal("read tool changed board")
	}
	for _, args := range []string{`{"owner":"bob"}`, `{"project_id":"other"}`, `{"shot_numbers":[0]}`, `{"episode_no":-1}`, `{} {}`} {
		if _, _, err := ReadAssistantTool(ctx, store, "alice", "p", "read_assets", json.RawMessage(args)); err == nil {
			t.Fatal("bad args accepted", args)
		}
	}
	if _, _, err := ReadAssistantTool(ctx, store, "alice", "p", "run_all_agents", json.RawMessage(`{}`)); err == nil {
		t.Fatal("write tool accepted")
	}
	if _, _, err := ReadAssistantTool(ctx, store, "charlie", "p", "read_assets", json.RawMessage(`{}`)); err == nil {
		t.Fatal("nonexistent ownership accepted")
	}
}

func TestAssistantReadToolsRequireUnambiguousBoard(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	_, _ = store.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	for _, n := range []int{1, 2} {
		_, err := store.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: n, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{Seq: 1}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := ReadAssistantTool(ctx, store, "alice", "p", "get_shot_numbers", json.RawMessage(`{}`)); err == nil {
		t.Fatal("ambiguous episode silently selected")
	}
	if run, data, err := ReadAssistantTool(ctx, store, "alice", "p", "get_shot_numbers", json.RawMessage(`{"episode_no":2}`)); err != nil || !run.OK || data["episode_no"] != 2 {
		t.Fatalf("wrong episode: %#v %v", data, err)
	}
}

func TestAssistantReadToolsBoundResultsAndRespectPromptFilter(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	_, _ = s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	_, _ = s.CreateAsset(ctx, Asset{ID: "a", ProjectID: "p", OwnerUsername: "alice", Type: AssetCharacter, Name: "甲", Description: strings.Repeat("长", 100000)})
	_, data, e := ReadAssistantTool(ctx, s, "alice", "p", "read_assets", json.RawMessage(`{}`))
	if e != nil || data["truncated"] != true {
		t.Fatal(data, e)
	}
	raw, _ := json.Marshal(data)
	if len(raw) > 768*1024 {
		t.Fatal("unbounded result", len(raw))
	}
	_, _ = s.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{ID: "s", Seq: 1, VideoDesc: "不得返回的完整视频提示", Characters: "甲"}}})
	_, data, e = ReadAssistantTool(ctx, s, "alice", "p", "read_shot_video_prompts", json.RawMessage(`{"include_prompt":false}`))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ = json.Marshal(data)
	if strings.Contains(string(raw), "不得返回") {
		t.Fatal("include_prompt false ignored")
	}
	if _, _, e = ReadAssistantTool(ctx, s, "alice", "p", "get_shot_numbers", json.RawMessage(`{"shot_numbers":[1,999]}`)); e == nil {
		t.Fatal("missing requested shot silently ignored")
	}
}
