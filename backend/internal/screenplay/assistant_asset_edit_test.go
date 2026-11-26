package screenplay

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAssistantAssetEditPreparesFullScopedSnapshotWithoutWriting(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	_, _ = s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	old, e := s.CreateAsset(ctx, Asset{ID: "a", ProjectID: "p", OwnerUsername: "alice", Type: AssetCharacter, Name: "甲", Identity: "原身份", Description: "原描述", Prompt: "原提示", Voice: "原音色", Extra: map[string]string{"image_path": "已生成的图片"}})
	if e != nil {
		t.Fatal(e)
	}
	action, _, e := PrepareAssistantAssetEdit(ctx, s, "alice", "p", "modify_character", json.RawMessage(`{"character_id":"a","description":"新描述"}`))
	if e != nil || action.Kind != "asset" || action.TargetID != old.ID || action.ExpectedRevision != old.Revision {
		t.Fatal(action, e)
	}
	if action.Asset.Description != "新描述" || action.Asset.Prompt != old.Prompt || action.Asset.Extra["image_path"] != "已生成的图片" || action.Asset.Voice != old.Voice {
		t.Fatal("untouched fields lost", action.Asset)
	}
	rows, _ := s.ListAssets(ctx, "alice", "p", "")
	if rows[0].Description != "原描述" || rows[0].Revision != old.Revision {
		t.Fatal("preparation wrote asset")
	}
	_, _ = s.CreateAsset(ctx, Asset{ID: "b", ProjectID: "p", OwnerUsername: "alice", Type: AssetCharacter, Name: "乙"})
	if _, _, e := PrepareAssistantAssetEdit(ctx, s, "alice", "p", "modify_character", json.RawMessage(`{"character_id":"a","name":"乙"}`)); e == nil {
		t.Fatal("rename collision should fail before confirmation")
	}
	for _, args := range []string{`{"character_id":"a","owner":"bob","description":"x"}`, `{"character_id":"a","action":"delete"}`, `{"character_id":"a","action":"add","description":"x"}`, `{"character_id":"a","new_data":{"project_id":"other"}}`, `{"character_id":"a","character_name":"另一人","description":"x"}`, `{"character_id":"a","prompt":"一","image_prompt":"二"}`} {
		if _, _, e := PrepareAssistantAssetEdit(ctx, s, "alice", "p", "modify_character", json.RawMessage(args)); e == nil {
			t.Fatal("invalid update accepted", args)
		}
	}
	if _, _, e := PrepareAssistantAssetEdit(ctx, s, "bob", "p", "modify_character", json.RawMessage(`{"character_id":"a","description":"x"}`)); e == nil {
		t.Fatal("cross-owner target allowed")
	}
	if _, _, e := PrepareAssistantAssetEdit(ctx, s, "alice", "p", "modify_prop", json.RawMessage(`{"prop_id":"a","description":"x"}`)); e == nil {
		t.Fatal("wrong asset type allowed")
	}
}

func TestAssistantAssetEditUsesExactNameAndPreservesRevisionForConfirmation(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	_, _ = s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	_, _ = s.CreateAsset(ctx, Asset{ID: "scene", ProjectID: "p", OwnerUsername: "alice", Type: AssetScene, Name: "院中", Description: "白天", Prompt: "原图词"})
	action, _, e := PrepareAssistantAssetEdit(ctx, s, "alice", "p", "modify_scene", json.RawMessage(`{"action":"update","scene_name":"院中","new_data":{"description":"黄昏"}}`))
	if e != nil {
		t.Fatal(e)
	}
	repo := NewMemoryAuthoringRepository()
	svc := AuthoringService{Store: s, Repository: repo}
	st, _ := svc.Load(ctx, "alice", "p")
	st.Proposals = []AssistantProposal{{ID: "proposal", Status: "pending", Revision: 1, Actions: []AuthoringAction{action}}}
	st, e = svc.Save(ctx, "alice", "p", st, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Apply(ctx, "alice", "p", "proposal", st.Revision, false); e == nil {
		t.Fatal("unconfirmed applied")
	}
	st, e = svc.Apply(ctx, "alice", "p", "proposal", st.Revision, true)
	if e != nil {
		t.Fatal(e)
	}
	rows, _ := s.ListAssets(ctx, "alice", "p", "")
	if rows[0].Description != "黄昏" || rows[0].Prompt != "原图词" {
		t.Fatal(rows)
	}
	if _, e = svc.Apply(ctx, "alice", "p", "proposal", st.Revision, true); e == nil {
		t.Fatal("same proposal applied twice")
	}
	if _, _, e = PrepareAssistantAssetEdit(ctx, s, "alice", "p", "modify_scene", json.RawMessage(`{"scene_name":"院","description":"x"}`)); e == nil {
		t.Fatal("partial name guessed")
	}
}

func TestAssistantAssetEditConfirmationDoesNotOverwriteNewerAsset(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	_, _ = s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	old, _ := s.CreateAsset(ctx, Asset{ID: "a", ProjectID: "p", OwnerUsername: "alice", Type: AssetProp, Name: "信封", Description: "旧"})
	action, _, e := PrepareAssistantAssetEdit(ctx, s, "alice", "p", "modify_prop", json.RawMessage(`{"prop_id":"a","description":"助手建议"}`))
	if e != nil {
		t.Fatal(e)
	}
	repo := NewMemoryAuthoringRepository()
	svc := AuthoringService{Store: s, Repository: repo}
	state, _ := svc.Load(ctx, "alice", "p")
	state.Proposals = []AssistantProposal{{ID: "proposal", Status: "pending", Revision: 1, Actions: []AuthoringAction{action}}}
	state, e = svc.Save(ctx, "alice", "p", state, 0)
	if e != nil {
		t.Fatal(e)
	}
	current := old
	current.Description = "用户已在别处修改"
	if _, e = s.UpdateAsset(ctx, "alice", "p", old.ID, current, old.Revision); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Apply(ctx, "alice", "p", "proposal", state.Revision, true); e == nil {
		t.Fatal("stale proposal overwrote current asset")
	}
	rows, _ := s.ListAssets(ctx, "alice", "p", "")
	if rows[0].Description != "用户已在别处修改" {
		t.Fatal(rows)
	}
}

func TestAssistantLegacyIncompleteAssetProposalCannotClearExistingFields(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	_, _ = s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	old, _ := s.CreateAsset(ctx, Asset{ID: "a", ProjectID: "p", OwnerUsername: "alice", Type: AssetCharacter, Name: "甲", Prompt: "保留图词", Voice: "保留音色", Extra: map[string]string{"image_path": "保留图"}})
	repo := NewMemoryAuthoringRepository()
	svc := AuthoringService{Store: s, Repository: repo}
	state, _ := svc.Load(ctx, "alice", "p")
	state.Proposals = []AssistantProposal{{ID: "old", Status: "pending", Revision: 1, Actions: []AuthoringAction{{Kind: "asset", TargetID: "a", ExpectedRevision: old.Revision, Asset: &Asset{Type: AssetCharacter, Name: "甲", Description: "缺少其他字段"}}}}}
	state, e := svc.Save(ctx, "alice", "p", state, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Apply(ctx, "alice", "p", "old", state.Revision, true); e == nil {
		t.Fatal("legacy incomplete proposal applied")
	}
	rows, _ := s.ListAssets(ctx, "alice", "p", "")
	before, _ := json.Marshal(old)
	after, _ := json.Marshal(rows[0])
	if string(before) != string(after) {
		t.Fatal("legacy action cleared data", rows)
	}
}
