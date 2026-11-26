package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
	"strings"
	"testing"
	"time"
)

func TestAuthoringAssistantAssetToolPreviewsThenRequiresConfirmation(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	_, _ = s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
	old, _ := s.CreateAsset(ctx, screenplay.Asset{ID: "a", ProjectID: "p", OwnerUsername: "alice", Type: screenplay.AssetCharacter, Name: "甲", Description: "旧描述", Prompt: "旧图词", Extra: map[string]string{"image_path": "保留的图"}})
	repo := screenplay.NewMemoryAuthoringRepository()
	mux := http.NewServeMux()
	calls := 0
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		return screenplayProviderFunc(func(context.Context, batchfactoryv11.TextCompletionRequest) (string, error) {
			calls++
			return `{"reply":"已修改成功","tool":"modify_character","tool_args":{"character_id":"a","description":"新描述"},"actions":[]}`, nil
		}), nil
	})
	res := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/chat", map[string]any{"expected_revision": 0, "message": "把甲的描述改成新描述"})
	if res.Code != 200 || calls != 1 {
		t.Fatal(res.Code, res.Body.String(), calls)
	}
	var wire struct {
		State screenplay.AuthoringState `json:"state"`
	}
	if json.Unmarshal(res.Body.Bytes(), &wire) != nil {
		t.Fatal(res.Body.String())
	}
	if len(wire.State.Proposals) != 1 || wire.State.Proposals[0].Status != "pending" || strings.Contains(wire.State.Messages[1].Content, "已修改成功") {
		t.Fatal("false write or missing proposal", res.Body.String())
	}
	rows, _ := s.ListAssets(ctx, "alice", "p", "")
	if rows[0].Description != old.Description || rows[0].Revision != old.Revision {
		t.Fatal("prepared tool wrote data")
	}
	proposal := wire.State.Proposals[0]
	base := "/api/comic-screenplays/projects/p/assistant/apply"
	denied := signedJSONRequest(t, mux, now, "alice", "POST", base, map[string]any{"expected_revision": wire.State.Revision, "proposal_id": proposal.ID, "confirmed": false})
	if denied.Code == 200 {
		t.Fatal("unconfirmed edit applied")
	}
	applied := signedJSONRequest(t, mux, now, "alice", "POST", base, map[string]any{"expected_revision": wire.State.Revision, "proposal_id": proposal.ID, "confirmed": true})
	if applied.Code != 200 {
		t.Fatal(applied.Code, applied.Body.String())
	}
	rows, _ = s.ListAssets(ctx, "alice", "p", "")
	if rows[0].Description != "新描述" || rows[0].Prompt != "旧图词" || rows[0].Extra["image_path"] != "保留的图" || calls != 1 {
		t.Fatal("fields lost or extra model call", rows, calls)
	}
}

func TestAuthoringAssistantReadThenAssetProposalUsesTwoModelRounds(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	_, _ = s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
	_, _ = s.CreateAsset(ctx, screenplay.Asset{ID: "prop-a", ProjectID: "p", OwnerUsername: "alice", Type: screenplay.AssetProp, Name: "信封", Description: "旧"})
	repo := screenplay.NewMemoryAuthoringRepository()
	mux := http.NewServeMux()
	calls := 0
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		return screenplayProviderFunc(func(context.Context, batchfactoryv11.TextCompletionRequest) (string, error) {
			calls++
			if calls == 1 {
				return `{"reply":"核对道具","tool":"read_assets","tool_args":{"asset_type":"prop"}}`, nil
			}
			return `{"reply":"完成","tool":"modify_prop","tool_args":{"prop_id":"prop-a","new_data":{"description":"新"}}}`, nil
		}), nil
	})
	res := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/chat", map[string]any{"expected_revision": 0, "message": "核对后把信封描述改成新"})
	if res.Code != 200 || calls != 2 {
		t.Fatal(res.Code, res.Body.String(), calls)
	}
	state, _ := repo.Load(ctx, "alice", "p")
	if len(state.Proposals) != 1 || len(state.Messages[1].Tools) != 1 || state.Messages[1].Tools[0].Name != "read_assets" {
		t.Fatal(state)
	}
	rows, _ := s.ListAssets(ctx, "alice", "p", "")
	if rows[0].Description != "旧" {
		t.Fatal("unconfirmed asset changed")
	}
}

func TestAuthoringPrepareAssetEditEndpointNeedsEditButNoModel(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	_, _ = s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
	_, _ = s.CreateAsset(ctx, screenplay.Asset{ID: "a", ProjectID: "p", OwnerUsername: "alice", Type: screenplay.AssetScene, Name: "院中", Description: "白天"})
	repo := screenplay.NewMemoryAuthoringRepository()
	svc := screenplay.AuthoringService{Store: s, Repository: repo}
	state, _ := svc.Load(ctx, "alice", "p")
	state.Members = []screenplay.MemberGrant{{Username: "reader", Read: true}}
	state, _ = svc.Save(ctx, "alice", "p", state, 0)
	mux := http.NewServeMux()
	calls := 0
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		calls++
		return nil, nil
	})
	path := "/api/comic-screenplays/projects/p/assistant/prepare-asset-edit"
	input := map[string]any{"expected_revision": state.Revision, "tool": "modify_scene", "tool_args": map[string]any{"scene_id": "a", "description": "黄昏"}}
	no := signedJSONRequest(t, mux, now, "reader", "POST", path, input)
	if no.Code == 200 {
		t.Fatal("read-only member prepared write")
	}
	res := signedJSONRequest(t, mux, now, "alice", "POST", path, input)
	if res.Code != 200 || calls != 0 {
		t.Fatal(res.Code, res.Body.String(), calls)
	}
	saved, _ := repo.Load(ctx, "alice", "p")
	if len(saved.Proposals) != 1 || saved.Proposals[0].Status != "pending" {
		t.Fatal(saved)
	}
	rows, _ := s.ListAssets(ctx, "alice", "p", "")
	if rows[0].Description != "白天" {
		t.Fatal("unconfirmed write")
	}
	again := signedJSONRequest(t, mux, now, "alice", "POST", path, input)
	if again.Code == 200 || calls != 0 {
		t.Fatal("duplicate preparation accepted", again.Code, calls)
	}
}

func TestAuthoringAssistantRejectsRawAssetActionEvenWithForgedPreparedFlag(t *testing.T) {
	for _, forged := range []bool{false, true} {
		t.Run(fmt.Sprint(forged), func(t *testing.T) {
			ctx := context.Background()
			now := time.Now()
			s := screenplay.NewMemoryStore()
			_, _ = s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
			old, _ := s.CreateAsset(ctx, screenplay.Asset{ID: "a", ProjectID: "p", OwnerUsername: "alice", Type: screenplay.AssetCharacter, Name: "甲", Description: "旧", Prompt: "必须保留图词", Voice: "必须保留音色", Extra: map[string]string{"image_path": "必须保留图"}})
			repo := screenplay.NewMemoryAuthoringRepository()
			mux := http.NewServeMux()
			raw, _ := json.Marshal(map[string]any{"reply": "已修改成功", "actions": []map[string]any{{"kind": "asset", "target_id": "a", "expected_revision": old.Revision, "asset_prepared": forged, "asset": map[string]any{"type": "character", "name": "甲", "description": "新"}}}})
			RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
				return screenplayProviderFunc(func(context.Context, batchfactoryv11.TextCompletionRequest) (string, error) { return string(raw), nil }), nil
			})
			result := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/chat", map[string]any{"expected_revision": 0, "message": "修改甲的描述"})
			if result.Code == 200 {
				t.Fatal("raw model asset action bypassed server preparation", result.Body.String())
			}
			if _, e := repo.Load(ctx, "alice", "p"); e == nil {
				t.Fatal("unsafe proposal persisted")
			}
			rows, _ := s.ListAssets(ctx, "alice", "p", "")
			before, _ := json.Marshal(old)
			after, _ := json.Marshal(rows[0])
			if string(before) != string(after) {
				t.Fatal("asset changed")
			}
		})
	}
}
