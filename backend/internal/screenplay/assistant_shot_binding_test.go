package screenplay

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"qiantie/backend/internal/prompt"
)

func TestBindingLegacyDeleteProtection(t *testing.T) {
	for _, tc := range []struct {
		kind AssetType
		raw  string
	}{{AssetCharacter, `{"character_ids":["target"]}`}, {AssetScene, `{"scene_id":"target"}`}, {AssetProp, `{"prop_ids":["target"]}`}} {
		if !assistantAssetReference(Asset{ID: "target", Name: "物件", Type: tc.kind}, "", "", "", json.RawMessage(tc.raw)) {
			t.Errorf("legacy %s binding escaped deletion guard", tc.kind)
		}
	}
	if assistantAssetReference(Asset{ID: "old", Name: "旧", Type: AssetCharacter}, "", "", "", json.RawMessage(`{"asset_bindings":{"character_ids":[]},"character_ids":["old"]}`)) {
		t.Fatal("cleared character blocked deletion")
	}
}
func TestBindingPrepareUsesScopedRealAssetsAndPreservesSource(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	s.CreateAsset(ctx, Asset{ID: "street", ProjectID: "p", OwnerUsername: "alice", Type: AssetScene, Name: "街道"})
	_, e := s.SaveStoryboard(ctx, Storyboard{ID: "b", ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{ID: "s", Seq: 1, Duration: 4, SceneID: 7, ProtocolFields: json.RawMessage(`{"scene_id":7,"future":"keep"}`)}}})
	if e != nil {
		t.Fatal(e)
	}
	a, _, e := PrepareAssistantShotEdit(ctx, s, "alice", "p", "bind_shot_assets", json.RawMessage(`{"shot_id":"s","scene_id":"street","scene_name":"街道"}`))
	if e != nil {
		t.Fatal(e)
	}
	sh := a.CreativeShot.After
	var fields map[string]json.RawMessage
	if json.Unmarshal(sh.ProtocolFields, &fields) != nil || string(fields["scene_id"]) != "7" || string(fields["future"]) != `"keep"` || string(fields["asset_bindings"]) != `{"scene_id":"street"}` || sh.SceneID != 7 || sh.Scene != "街道" {
		t.Fatal(sh, string(sh.ProtocolFields))
	}
}

func bindingFixture(t *testing.T) (*MemoryStore, AuthoringService) {
	t.Helper()
	ctx := context.Background()
	s := NewMemoryStore()
	for _, p := range []string{"p", "other"} {
		if _, e := s.CreateProject(ctx, Project{ID: p, OwnerUsername: "alice"}); e != nil {
			t.Fatal(e)
		}
	}
	for _, a := range []Asset{{ID: "a", Name: "阿青", Type: AssetCharacter}, {ID: "b", Name: "阿蓝", Type: AssetCharacter}, {ID: "place", Name: "旧街", Type: AssetScene}, {ID: "room", Name: "室内", Type: AssetScene}, {ID: "book", Name: "书", Type: AssetProp}, {ID: "foreign", Name: "外人", Type: AssetCharacter, ProjectID: "other"}} {
		a.OwnerUsername = "alice"
		if a.ProjectID == "" {
			a.ProjectID = "p"
		}
		if _, e := s.CreateAsset(ctx, a); e != nil {
			t.Fatal(e)
		}
	}
	_, e := s.SaveStoryboard(ctx, Storyboard{ID: "board", OwnerUsername: "alice", ProjectID: "p", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{ID: "s", Seq: 1, Duration: 4, Line: "保留台词", Characters: "阿青", Scene: "旧街", Props: "书", SceneID: 4, SourceElements: json.RawMessage(`[4]`), ProtocolFields: json.RawMessage(`{"character_ids":["a"],"scene_id":4,"prop_ids":["book"],"extension":{"data":1}}`)}}})
	if e != nil {
		t.Fatal(e)
	}
	return s, AuthoringService{Store: s, Repository: NewMemoryAuthoringRepository()}
}
func proposeBinding(t *testing.T, s Store, svc AuthoringService, raw string) (AuthoringAction, AuthoringState) {
	t.Helper()
	ctx := context.Background()
	a, _, e := PrepareAssistantShotEdit(ctx, s, "alice", "p", "bind_shot_assets", json.RawMessage(raw))
	if e != nil {
		t.Fatal(e)
	}
	st, _ := svc.Load(ctx, "alice", "p")
	st.Proposals = append(st.Proposals, AssistantProposal{ID: "q", Revision: st.Revision + 1, Status: "pending", Actions: []AuthoringAction{a}})
	st, e = svc.Save(ctx, "alice", "p", st, st.Revision)
	if e != nil {
		t.Fatal(e)
	}
	return a, st
}
func TestBindingMergeClearAndStaleAssets(t *testing.T) {
	ctx := context.Background()
	for _, action := range []string{"merge", "clear", "rename", "delete", "revision"} {
		t.Run(action, func(t *testing.T) {
			s, svc := bindingFixture(t)
			raw := `{"shot_number":1,"character_ids":["b","a","b"],"prop_ids":[]}`
			if action == "clear" {
				raw = `{"shot_id":"s","mode":"replace","character_ids":[],"scene_name":"","prop_names":[]}`
			}
			a, st := proposeBinding(t, s, svc, raw)
			if action == "rename" || action == "revision" || action == "delete" {
				rows, _ := s.ListAssets(ctx, "alice", "p", AssetCharacter)
				for _, asset := range rows {
					if asset.ID != "b" {
						continue
					}
					if action == "delete" {
						s.DeleteAsset(ctx, "alice", "p", "b")
					} else {
						if action == "rename" {
							asset.Name = "新名"
						}
						asset.Description = "其他编辑"
						if _, e := s.UpdateAsset(ctx, "alice", "p", "b", asset, asset.Revision); e != nil {
							t.Fatal(e)
						}
					}
				}
			}
			_, e := svc.Apply(ctx, "alice", "p", "q", st.Revision, true)
			b, _ := s.GetStoryboard(ctx, "alice", "p", 1, EngineXiaoluoV7)
			sh := b.Shots[0]
			switch action {
			case "rename", "revision", "delete":
				saved, _ := svc.Load(ctx, "alice", "p")
				if e == nil || sh.Characters != "阿青" || b.Revision != a.ExpectedRevision || saved.Proposals[0].Status != "pending" {
					t.Fatal("stale asset wrote", e, b, saved)
				}
			case "merge":
				if e != nil || sh.Characters != "阿青、阿蓝" || sh.Scene != "旧街" || sh.Props != "书" {
					t.Fatal(e, sh)
				}
				var f map[string]json.RawMessage
				json.Unmarshal(sh.ProtocolFields, &f)
				if string(f["asset_bindings"]) != `{"character_ids":["a","b"]}` || string(f["prop_ids"]) != `["book"]` || f["character_ids"] != nil {
					t.Fatal(string(sh.ProtocolFields))
				}
			case "clear":
				if e != nil || sh.Characters != "" || sh.Scene != "" || sh.Props != "" || sh.SceneID != 4 || string(sh.SourceElements) != `[4]` {
					t.Fatal(e, sh)
				}
				var f map[string]json.RawMessage
				json.Unmarshal(sh.ProtocolFields, &f)
				if string(f["asset_bindings"]) != `{"character_ids":[],"prop_ids":[],"scene_id":""}` || string(f["scene_id"]) != "4" || string(f["extension"]) != `{"data":1}` {
					t.Fatal(string(sh.ProtocolFields))
				}
				for _, asset := range []Asset{{ID: "a", Name: "阿青", Type: AssetCharacter}, {ID: "place", Name: "旧街", Type: AssetScene}, {ID: "book", Name: "书", Type: AssetProp}} {
					if e := checkAssistantAssetReferences(ctx, s, "alice", "p", asset); e != nil {
						t.Fatal(e)
					}
				}
			}
		})
	}
}
func TestBindingRejectsInvalidSelections(t *testing.T) {
	s, _ := bindingFixture(t)
	ctx := context.Background()
	for _, raw := range []string{
		`{"shot_id":"s","scene_id":"room"}`, `{"shot_id":"s","character_names":["阿蓝"],"character_ids":["a"]}`,
		`{"shot_id":"s","character_ids":["foreign"]}`, `{"shot_id":"s","character_ids":["book"]}`, `{"shot_id":"s","character_ids":["missing"]}`,
		`{"shot_id":"s","character_ids":[1]}`, `{"shot_id":"s","scene_id":4}`, `{"shot_id":"s","character_names":null}`,
		`{"shot_id":"s","character_ids":[]}`, `{"shot_id":"s","character_ids":["a"],"CHARACTER_IDS":["b"]}`, `{"shot_id":"s","character_ids":["a"],"character_ids":["b"]}`,
		`{"shot_id":"s","confirmed":true,"prop_names":[]}`, `{"shot_id":"s","owner":"bob","prop_names":[]}`, `{"shot_number":2,"prop_ids":["book"]}`, `{"shot_id":"s","episode_no":2,"character_ids":["b"]}`,
	} {
		if _, _, e := PrepareAssistantShotEdit(ctx, s, "alice", "p", "bind_shot_assets", json.RawMessage(raw)); e == nil {
			t.Error("accepted", raw)
		}
	}
	s.mu.Lock()
	key := projectKey("alice", "p")
	other := s.assets[key][0]
	other.ID = "duplicate"
	s.assets[key] = append(s.assets[key], other)
	s.mu.Unlock()
	if _, _, e := PrepareAssistantShotEdit(ctx, s, "alice", "p", "bind_shot_assets", json.RawMessage(`{"shot_id":"s","mode":"replace","character_names":["阿青"]}`)); e == nil {
		t.Fatal("ambiguous")
	}
}

func TestBindingRepairsDeadSelectionOnlyWithReplace(t *testing.T) {
	ctx := context.Background()
	for _, raw := range []string{`{"shot_id":"s","mode":"replace","character_ids":[]}`, `{"shot_id":"s","mode":"replace","character_names":["阿蓝"]}`} {
		s, svc := bindingFixture(t)
		b, _ := s.GetStoryboard(ctx, "alice", "p", 1, EngineXiaoluoV7)
		b.Shots[0].ProtocolFields = json.RawMessage(`{"asset_bindings":{"character_ids":["dead"]},"scene_id":4}`)
		s.SaveStoryboardRevision(ctx, b, b.Revision)
		if _, _, e := PrepareAssistantShotEdit(ctx, s, "alice", "p", "bind_shot_assets", json.RawMessage(`{"shot_id":"s","character_ids":["b"]}`)); e == nil {
			t.Fatal("merged a dead selection")
		}
		_, st := proposeBinding(t, s, svc, raw)
		if _, e := svc.Apply(ctx, "alice", "p", "q", st.Revision, true); e != nil {
			t.Fatal(e)
		}
		current, _ := s.GetStoryboard(ctx, "alice", "p", 1, EngineXiaoluoV7)
		sh := current.Shots[0]
		if sh.Scene != "旧街" || sh.Props != "书" || sh.Characters == "阿青" {
			t.Fatal(sh)
		}
	}
}

func TestBindingNineGridSourceIsNotALegacyAssetReference(t *testing.T) {
	contract, e := prompt.ResolveCreativePrompt("luoshui_storyboard_175", "storyboard")
	if e != nil {
		t.Fatal(e)
	}
	marker := map[string]string{"prompt_id": contract.ID, "prompt_digest": contract.Digest, "parser_version": contract.ParserVersion, "output_protocol": contract.OutputProtocol}
	fields := map[string]any{"scene_id": "source-label", "_cm_contract": marker}
	raw, _ := json.Marshal(fields)
	old := Asset{ID: "source-label", Type: AssetScene, Name: "另一场景"}
	if assistantAssetReference(old, "", "街道", "", raw) {
		t.Fatal("source label blocked unrelated asset deletion")
	}
	actual := Asset{ID: "scene-asset", Type: AssetScene, Name: "街道"}
	if !assistantAssetReference(actual, "", "街道", "", raw) {
		t.Fatal("lost the real name-based scene reference")
	}
	marker["prompt_digest"] = "incorrect"
	raw, _ = json.Marshal(fields)
	if !assistantAssetReference(old, "", "街道", "", raw) {
		t.Fatal("unverified source marker suppressed a legacy asset ID")
	}
}

func TestBindingSQLNameColumnLimits(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		kind   AssetType
		total  int
		byName bool
		ok     bool
	}{
		{AssetCharacter, 512, true, true}, {AssetCharacter, 513, false, false}, {AssetProp, 512, false, true}, {AssetProp, 513, true, false}, {AssetScene, 191, true, true}, {AssetScene, 192, false, false},
	} {
		t.Run(string(tc.kind)+strconv.Itoa(tc.total), func(t *testing.T) {
			s := NewMemoryStore()
			s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
			names := []string{strings.Repeat("楼", tc.total)}
			if tc.kind != AssetScene {
				names = []string{strings.Repeat("人", 191), strings.Repeat("🌂", 191), strings.Repeat("伞", tc.total-384)}
			}
			ids := make([]string, len(names))
			snapshots := make([]bindingAsset, len(names))
			for i, name := range names {
				id := "a" + strconv.Itoa(i)
				ids[i] = id
				asset, e := s.CreateAsset(ctx, Asset{ID: id, ProjectID: "p", OwnerUsername: "alice", Type: tc.kind, Name: name})
				if e != nil {
					t.Fatal(e)
				}
				snapshots[i] = bindingAsset{id, tc.kind, name, asset.Revision}
			}
			sh := StoryboardShot{ID: "s", Seq: 1, Duration: 4, Line: "保留"}
			board, e := s.SaveStoryboard(ctx, Storyboard{ID: "board", ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{sh}})
			if e != nil {
				t.Fatal(e)
			}
			key := bindingIDKey(tc.kind)
			values := ids
			if tc.byName {
				values = names
				key = map[AssetType]string{AssetScene: "scene_name", AssetProp: "prop_names", AssetCharacter: "character_names"}[tc.kind]
			}
			args := map[string]any{"shot_id": "s", "mode": "merge", key: values}
			if tc.kind == AssetScene {
				args[key] = values[0]
			}
			raw, _ := json.Marshal(args)
			a, _, e := PrepareAssistantShotEdit(ctx, s, "alice", "p", "bind_shot_assets", raw)
			want := strings.Join(names, "、")
			if utf8.RuneCountInString(want) != tc.total {
				t.Fatal("invalid fixture")
			}
			if tc.ok {
				if e != nil {
					t.Fatal(e)
				}
				if got := *bindingShotField(&a.CreativeShot.After, tc.kind); got != want {
					t.Fatal("name truncated")
				}
				if e := validateCreativeShotAction(a); e != nil {
					t.Fatal(e)
				}
			} else {
				limit := "512"
				if tc.kind == AssetScene {
					limit = "191"
				}
				if e == nil || !strings.Contains(e.Error(), limit) {
					t.Errorf("prepare accepted oversized %s or missed limit %s: %v", tc.kind, limit, e)
				}
				// Rebuild the previously admissible persisted action without asking the
				// new preparation function, and exercise the actual confirmation validator.
				before := board.Shots[0]
				after := before
				*bindingShotField(&after, tc.kind) = want
				var storedIDs any = ids
				if tc.kind == AssetScene {
					storedIDs = ids[0]
				}
				after.ProtocolFields, _ = json.Marshal(map[string]any{"asset_bindings": map[string]any{bindingIDKey(tc.kind): storedIDs}})
				edit := AssistantCreativeShotEdit{Before: before, After: after, BoardID: board.ID, Engine: board.Engine, Episode: 1, Binding: &AssistantShotBinding{Selected: map[AssetType][]string{tc.kind: ids}, Assets: snapshots}, Changes: []AssistantShotChange{{bindingIDKey(tc.kind), "", bindingDisplay(want, ids)}}}
				stored := AuthoringAction{TargetID: "s", Kind: "creative_shot", ExpectedRevision: board.Revision, CreativeShot: &edit}
				st := AuthoringState{Revision: 2, Proposals: []AssistantProposal{{ID: "pending", Revision: 2, Status: "pending", Actions: []AuthoringAction{stored}}}}
				if _, _, e := pendingShotAction(st, "pending", 2); e == nil {
					t.Error("old oversized proposal passed confirmation")
				}
			}
		})
	}
}
