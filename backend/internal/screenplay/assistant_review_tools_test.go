package screenplay

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"qiantie/backend/internal/prompt"
)

func TestAssistantReviewToolsReturnScopedFacts(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	source := " 旧钟响了。阿竹说：别回头。 "
	for _, owner := range []string{"alice", "bob"} {
		if _, err := store.CreateProject(ctx, Project{ID: "p", OwnerUsername: owner, SourceText: source}); err != nil {
			t.Fatal(err)
		}
	}
	board := Storyboard{ID: "saved", ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 2, Engine: EngineXiaoluoV7,
		SourceSHA256: contentDigest(strings.TrimSpace(source)), Shots: []StoryboardShot{
			{ID: "other", Seq: 1, Line: "未选台词"},
			{ID: "selected", Seq: 2, Line: "别回头。", VoiceActor: "阿竹", Emotion: "压低声音", ImagePrompt: "长廊旧钟", VideoDesc: "缓缓推近钟面"},
		}}
	if _, err := store.SaveStoryboard(ctx, board); err != nil {
		t.Fatal(err)
	}
	board.EpisodeNo = 1
	board.ID = "other-board"
	board.SourceSHA256 = ""
	board.Shots = []StoryboardShot{{ID: "other-episode", Seq: 1, Line: "他集台词"}}
	if _, err := store.SaveStoryboard(ctx, board); err != nil {
		t.Fatal(err)
	}
	board.OwnerUsername, board.ID = "bob", "bob-board"
	board.Shots = []StoryboardShot{{ID: "bob-shot", Seq: 1, Line: "他人台词"}}
	if _, err := store.SaveStoryboard(ctx, board); err != nil {
		t.Fatal(err)
	}
	before, _ := store.ListStoryboards(ctx, "alice", "p", "")
	for _, tc := range []struct {
		name     string
		evidence []string
	}{
		{"compare_original_vs_voicetext", []string{source, "别回头。", "project_source_digest_match"}},
		{"review_voice_emotions", []string{source, "别回头。", "阿竹", "压低声音"}},
		{"review_image_prompts", []string{"长廊旧钟", "缓缓推近钟面", "scene_description_available"}},
		{"review_video_prompts", []string{"缓缓推近钟面", "platform_moderation_verified"}},
		{"review_asset_bindings", []string{"别回头。", "缓缓推近钟面", "bindings"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run, data, err := ReadAssistantTool(ctx, store, "alice", "p", tc.name, json.RawMessage(`{"episode_no":2,"engine":"xiaoluo_v7_15s","shot_ids":["selected"]}`))
			if err != nil || !run.OK || data["result_kind"] != "review_input" || data["review_status"] != "unverified" || data["shot_scope"] != "selected_shots" || data["truncated"] != false {
				t.Fatalf("unexpected tool result: %v %+v %v", err, run, data)
			}
			raw, _ := json.Marshal(data)
			for _, value := range tc.evidence {
				if !strings.Contains(string(raw), value) {
					t.Fatalf("missing %q: %s", value, raw)
				}
			}
			for _, value := range []string{"未选台词", "他集台词", "他人台词"} {
				if strings.Contains(string(raw), value) {
					t.Fatalf("scope leak: %s", raw)
				}
			}
			for _, raw := range []string{
				`null`, `{"episode_no":null}`, `{"episode_no":0}`, `{"engine":null}`, `{"engine":""}`,
				`{"episode_no":2,"SHOT_IDS":["selected"]}`, `{"episode_no":2,"shot_ids":null}`, `{"episode_no":2,"shot_numbers":null}`,
				`{"episode_no":2,"shot_numbers":[]}`, `{"episode_no":2,"shot_ids":[]}`, `{"episode_no":2,"shot_id":"selected"}`,
				`{"episode_no":2,"shot_ids":["selected","selected"]}`, `{"episode_no":2,"shot_numbers":[2,2]}`,
				`{"episode_no":2,"shot_ids":["selected"],"shot_numbers":[2]}`, `{"episode_no":2,"shot_numbers":[2],"shot_numbers":[1]}`,
				`{"episode_no":2,"shot_ids":["selected"],"\u0073hot_ids":["other"]}`, `{"episode_no":2,"owner":"bob"}`,
				`{"episode_no":2,"include_prompt":false}`, `{"episode_no":2,"project_id":"other"}`,
			} {
				if _, data, err := ReadAssistantTool(ctx, store, "alice", "p", tc.name, json.RawMessage(raw)); !errors.Is(err, ErrInvalidArgument) || data != nil {
					t.Fatalf("invalid selector %s accepted: %v %v", raw, err, data)
				}
			}
			for _, raw := range []string{
				`{"episode_no":2,"shot_ids":["selected","other-episode"]}`, `{"episode_no":2,"shot_ids":["bob-shot"]}`,
				`{"episode_no":2,"shot_numbers":[2,3]}`, `{"episode_no":2,"engine":"nonexistent"}`,
			} {
				if _, data, err := ReadAssistantTool(ctx, store, "alice", "p", tc.name, json.RawMessage(raw)); !errors.Is(err, ErrNotFound) || data != nil {
					t.Fatalf("missing scope %s accepted: %v %v", raw, err, data)
				}
			}
			if _, _, err := ReadAssistantTool(ctx, store, "alice", "p", tc.name, nil); !errors.Is(err, ErrInvalidArgument) {
				t.Fatal("ambiguous board should fail", err)
			}
		})
	}
	_, data, err := ReadAssistantTool(ctx, store, "alice", "p", "compare_original_vs_voicetext", json.RawMessage(`{"episode_no":1,"shot_numbers":[1]}`))
	if err != nil || data["source_status"] != "unverified" || data["source_text"] != "" {
		t.Fatalf("unmatched project original was reported as episode source: %v %v", err, data)
	}
	_, data, err = ReadAssistantTool(ctx, store, "alice", "p", "get_editor_status", nil)
	status, _ := json.Marshal(data)
	if err != nil || !strings.Contains(string(status), `"storyboards":[`) || !strings.Contains(string(status), `"shot_count":2`) {
		t.Fatalf("real board choices were missing: %v %s", err, status)
	}
	_, data, err = ReadAssistantTool(ctx, store, "alice", "p", "review_image_prompts", json.RawMessage(`{"episode_no":2,"shot_numbers":[1],"focus":"style"}`))
	if err != nil || data["focus"] != "style" || !strings.Contains(string(mustAssistantReviewJSON(t, data)), `"missing_fields":["image_prompt"]`) {
		t.Fatalf("empty field concealed or focus ignored: %v %v", err, data)
	}
	after, _ := store.ListStoryboards(ctx, "alice", "p", "")
	if !reflect.DeepEqual(before, after) {
		t.Fatal("review changed saved boards")
	}
}

func mustAssistantReviewJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAssistantReviewReportsTruncatedSourceAndShots(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	source := strings.Repeat("长", 50000)
	_, _ = store.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice", SourceText: source})
	shots := make([]StoryboardShot, 201)
	for i := range shots {
		shots[i] = StoryboardShot{Seq: i + 1, Line: "确实存在的台词"}
	}
	_, _ = store.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: EngineXiaoluoV7,
		SourceSHA256: contentDigest(strings.TrimSpace(source)), Shots: shots})
	run, data, err := ReadAssistantTool(ctx, store, "alice", "p", "compare_original_vs_voicetext", nil)
	if err != nil || !run.OK || data["truncated"] != true || data["source_status"] != "truncated" || data["matched_total"] != 201 || data["shot_scope"] != "whole_board" {
		t.Fatalf("missing limit evidence: %v %v %v", err, run, data)
	}
	rows, _ := data["shots"].([]map[string]any)
	if len(rows) != 200 || !strings.Contains(run.Summary, "截取") || !strings.Contains(string(mustAssistantReviewJSON(t, rows[0])), "确实存在的台词") {
		t.Fatalf("unexpected bounded result: %v %d", run, len(rows))
	}
}

func TestAssistantReviewBindingsDistinguishesSourceAndLostAssets(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	for _, owner := range []string{"alice", "bob"} {
		_, _ = store.CreateProject(ctx, Project{ID: "p", OwnerUsername: owner})
	}
	for _, a := range []Asset{
		{ID: "source-label", OwnerUsername: "alice", Type: AssetScene, Name: "错误场景"},
		{ID: "scene", OwnerUsername: "alice", Type: AssetScene, Name: "真实街道"},
		{ID: "old-hero", OwnerUsername: "alice", Type: AssetCharacter, Name: "当前角色名"},
		{ID: "bob-only", OwnerUsername: "bob", Type: AssetProp, Name: "他人道具"},
	} {
		a.ProjectID = "p"
		if _, err := store.CreateAsset(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	contract, err := prompt.ResolveCreativePrompt("luoshui_storyboard_175", "storyboard")
	if err != nil {
		t.Fatal(err)
	}
	raw := mustAssistantReviewJSON(t, map[string]any{
		"_cm_contract": map[string]string{"prompt_id": contract.ID, "prompt_digest": contract.Digest, "parser_version": contract.ParserVersion, "output_protocol": contract.OutputProtocol},
		"scene_id":     "source-label", "character_ids": []string{"old-hero"}, "prop_ids": []string{"old-prop"},
		"asset_bindings": map[string]any{"prop_ids": []string{"bob-only", "scene"}},
	})
	_, _ = store.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: EngineXiaoluoV7,
		Shots: []StoryboardShot{
			{ID: "nine", Seq: 1, Characters: "旧角色名", Scene: "真实街道", ProtocolFields: raw},
			{ID: "legacy", Seq: 2, Scene: "真实街道", ProtocolFields: json.RawMessage(`{"scene_id":"source-label"}`)},
			{ID: "clear", Seq: 3, Scene: "真实街道", ProtocolFields: json.RawMessage(`{"scene_id":"source-label","asset_bindings":{"scene_id":""}}`)},
			{ID: "malformed", Seq: 4, ProtocolFields: json.RawMessage(`{"asset_bindings":{"scene_id":null}}`)},
			{ID: "numeric", Seq: 5, Scene: "真实街道", ProtocolFields: json.RawMessage(`{"scene_id":"37"}`)},
		}})
	_, data, err := ReadAssistantTool(ctx, store, "alice", "p", "review_asset_bindings", nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := data["shots"].([]map[string]any)
	for i, tc := range []struct{ want, forbidden []string }{
		{[]string{"explicit", "legacy", "missing", "wrong_type", "真实街道", "当前角色名", "旧角色名", "unresolved"}, []string{"错误场景", "old-prop", "source-label", "他人道具"}},
		{[]string{"legacy", "错误场景", "真实街道", "source-label"}, nil},
		{[]string{"explicit", "unbound", "真实街道"}, []string{"错误场景", "source-label"}},
		{[]string{"explicit", "invalid"}, nil},
		{[]string{"name_only", "真实街道"}, []string{`"37"`}},
	} {
		row := string(mustAssistantReviewJSON(t, rows[i]))
		for _, value := range tc.want {
			if !strings.Contains(row, value) {
				t.Errorf("row %d missing %q: %s", i, value, row)
			}
		}
		for _, value := range tc.forbidden {
			if strings.Contains(row, value) {
				t.Errorf("row %d misinterpreted %q: %s", i, value, row)
			}
		}
	}
}
