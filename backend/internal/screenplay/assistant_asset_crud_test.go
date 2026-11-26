package screenplay

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAssistantAssetCRUDPrepareAndConfirm(t *testing.T) {
	for _, kind := range []string{"character", "scene", "prop"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			store := NewMemoryStore()
			_, _ = store.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
			action, summary, err := PrepareAssistantAssetEdit(ctx, store, "alice", "p", "add_"+kind, json.RawMessage(`{"name":"新资产","description":"说明"}`))
			if err != nil {
				t.Fatal(err)
			}
			if action.TargetID == "" || action.Asset.ID != action.TargetID || summary == "" {
				t.Fatal(action, summary)
			}
			rows, _ := store.ListAssets(ctx, "alice", "p", "")
			if len(rows) != 0 {
				t.Fatal("prepared add wrote asset")
			}
			svc := AuthoringService{Store: store, Repository: NewMemoryAuthoringRepository()}
			st, _ := svc.Load(ctx, "alice", "p")
			st.Proposals = []AssistantProposal{{ID: "add", Status: "pending", Revision: 1, Actions: []AuthoringAction{action}}}
			st, err = svc.Save(ctx, "alice", "p", st, 0)
			if err != nil {
				t.Fatal(err)
			}
			st, err = svc.Apply(ctx, "alice", "p", "add", st.Revision, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = svc.Apply(ctx, "alice", "p", "add", st.Revision, true); err == nil {
				t.Fatal("repeat add accepted")
			}
			rows, _ = store.ListAssets(ctx, "alice", "p", "")
			if len(rows) != 1 || rows[0].ID != action.TargetID {
				t.Fatal(rows)
			}
			del, _, err := PrepareAssistantAssetEdit(ctx, store, "alice", "p", "delete_"+kind, json.RawMessage(`{"asset_id":"`+action.TargetID+`"}`))
			if err != nil {
				t.Fatal(err)
			}
			st.Proposals = append(st.Proposals, AssistantProposal{ID: "delete", Status: "pending", Revision: st.Revision + 1, Actions: []AuthoringAction{del}})
			st, err = svc.Save(ctx, "alice", "p", st, st.Revision)
			if err != nil {
				t.Fatal(err)
			}
			_, err = svc.Apply(ctx, "alice", "p", "delete", st.Revision, true)
			if err != nil {
				t.Fatal(err)
			}
			rows, _ = store.ListAssets(ctx, "alice", "p", "")
			if len(rows) != 0 {
				t.Fatal(rows)
			}
		})
	}
}

func TestAssistantAssetCRUDRejectsReferencedDeleteAndUnsafeAdd(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	_, _ = s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	_, _ = s.CreateAsset(ctx, Asset{ID: "a", ProjectID: "p", OwnerUsername: "alice", Type: AssetCharacter, Name: "甲"})
	_, _ = s.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 2, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{Seq: 1, Characters: "甲"}}})
	if _, _, e := PrepareAssistantAssetEdit(ctx, s, "alice", "p", "modify_character", json.RawMessage(`{"action":"delete","asset_id":"a"}`)); e == nil {
		t.Fatal("referenced delete prepared")
	}
	for _, raw := range []string{`{"name":"甲"}`, `{"name":"乙","asset_id":"model-id"}`, `{"name":"乙","asset_prepared":true}`, `{"name":"乙","owner":"bob"}`} {
		if _, _, e := PrepareAssistantAssetEdit(ctx, s, "alice", "p", "add_character", json.RawMessage(raw)); e == nil {
			t.Fatal("unsafe add", raw)
		}
	}
	if _, _, e := PrepareAssistantAssetEdit(ctx, s, "alice", "p", "add_character", json.RawMessage(`{"action":"delete","name":"乙"}`)); e == nil {
		t.Fatal("conflicting alias")
	}
}

func TestAssistantAssetCRUDConfirmationRechecksReferenceRevisionAndName(t *testing.T) {
	for _, change := range []string{"reference", "revision", "duplicate", "project"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			s := NewMemoryStore()
			_, _ = s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
			old, _ := s.CreateAsset(ctx, Asset{ID: "a", ProjectID: "p", OwnerUsername: "alice", Type: AssetProp, Name: "信封", Extra: map[string]string{"image_path": "保留文件"}})
			raw := `{"action":"delete","asset_id":"a"}`
			if change == "duplicate" {
				raw = `{"action":"add","new_data":{"name":"新信封"}}`
			}
			action, _, e := PrepareAssistantAssetEdit(ctx, s, "alice", "p", "modify_prop", json.RawMessage(raw))
			if e != nil {
				t.Fatal(e)
			}
			svc := AuthoringService{Store: s, Repository: NewMemoryAuthoringRepository()}
			st, _ := svc.Load(ctx, "alice", "p")
			st.Proposals = []AssistantProposal{{ID: "proposal", Status: "pending", Revision: 1, Actions: []AuthoringAction{action}}}
			st, e = svc.Save(ctx, "alice", "p", st, 0)
			if e != nil {
				t.Fatal(e)
			}
			switch change {
			case "reference":
				_, _ = s.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{Seq: 1, Props: "信封"}}})
			case "revision":
				old.Description = "别人改过"
				_, _ = s.UpdateAsset(ctx, "alice", "p", "a", old, old.Revision)
			case "duplicate":
				_, _ = s.CreateAsset(ctx, Asset{ProjectID: "p", OwnerUsername: "alice", Type: AssetProp, Name: "新信封"})
			case "project":
				_, _ = s.CreateProject(ctx, Project{ID: "other", OwnerUsername: "alice"})
			}
			project := "p"
			if change == "project" {
				project = "other"
			}
			if _, e = svc.Apply(ctx, "alice", project, "proposal", st.Revision, true); e == nil {
				t.Fatal("unsafe confirmation accepted", change)
			}
			current, _ := svc.Repository.Load(ctx, "alice", "p")
			if current.Proposals[0].Status != "pending" || current.Revision != st.Revision {
				t.Fatal(current)
			}
			assets, _ := s.ListAssets(ctx, "alice", "p", "")
			if len(assets) == 0 || assets[0].Extra["image_path"] != "保留文件" {
				t.Fatal(assets)
			}
		})
	}
}

func TestAssistantAssetCRUDChecksAllDocumentVersionsAndExactNames(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	_, _ = s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	_, _ = s.CreateAsset(ctx, Asset{ID: "a", ProjectID: "p", OwnerUsername: "alice", Type: AssetScene, Name: "院中"})
	_, e := s.SaveScreenplay(ctx, Document{ID: "doc", ProjectID: "p", OwnerUsername: "alice", Content: "旧版"}, []Shot{{ID: "shot", SceneName: "院中"}}, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.SaveScreenplay(ctx, Document{ID: "doc2", ProjectID: "p", OwnerUsername: "alice", Content: "新版"}, nil, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = PrepareAssistantAssetEdit(ctx, s, "alice", "p", "delete_scene", json.RawMessage(`{"scene_name":"院中"}`)); e == nil {
		t.Fatal("historical shot ignored")
	}
	if assistantAssetReference(Asset{ID: "a", Name: "甲", Type: AssetCharacter}, "甲乙", "", "", nil) {
		t.Fatal("partial name reference")
	}
}

func TestAssistantAssetCRUDNestedBindingReference(t *testing.T) {
	asset := Asset{ID: "a", Name: "甲", Type: AssetCharacter}
	if !assistantAssetReference(asset, "", "", "", json.RawMessage(`{"data":{"reference_assets":[{"id":"a"}]}}`)) {
		t.Fatal("nested shot binding ignored")
	}
	if assistantAssetReference(asset, "", "", "", json.RawMessage(`{"description":"a"}`)) {
		t.Fatal("prose treated as binding")
	}
}

func TestAssistantAssetCRUDUsesActualReferenceNameSeparators(t *testing.T) {
	for _, field := range []string{"Alice Smith, Bob", "@Alice Smith", "[\"Alice Smith\",\"Bob\"]"} {
		if !assistantAssetReference(Asset{ID: "a", Name: "Alice Smith", Type: AssetCharacter}, field, "", "", nil) {
			t.Fatal("saved name reference missed", field)
		}
	}
}
