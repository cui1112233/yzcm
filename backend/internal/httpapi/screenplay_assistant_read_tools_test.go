package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
	"strings"
	"testing"
	"time"
)

func TestAuthoringAssistantReadsActualAssetsThroughScopedTool(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	repo := screenplay.NewMemoryAuthoringRepository()
	for _, owner := range []string{"alice", "bob"} {
		_, _ = s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: owner})
		_, _ = s.CreateAsset(ctx, screenplay.Asset{ID: owner + "-a", ProjectID: "p", OwnerUsername: owner, Type: screenplay.AssetCharacter, Name: owner + "人物"})
	}
	before, _ := s.ListAssets(ctx, "alice", "p", "")
	beforeJSON, _ := json.Marshal(before)
	calls := 0
	var modelInput string
	mux := http.NewServeMux()
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		return screenplayProviderFunc(func(_ context.Context, r batchfactoryv11.TextCompletionRequest) (string, error) {
			calls++
			if calls == 1 {
				return `{"reply":"读取人物","tool":"read_assets","tool_args":{"asset_type":"character","name":"alice"},"actions":[]}`, nil
			}
			modelInput = r.UserPrompt
			return `{"reply":"已查到实际人物alice人物","actions":[]}`, nil
		}), nil
	})
	res := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/chat", map[string]any{"expected_revision": 0, "message": "查我实际的人物资产"})
	if res.Code != 200 || calls != 2 {
		t.Fatal(res.Code, res.Body.String(), calls)
	}
	if !strings.Contains(modelInput, "tool_result") || !strings.Contains(modelInput, "alice人物") || strings.Contains(modelInput, "bob人物") {
		t.Fatal("wrong scoped data", modelInput)
	}
	state, _ := repo.Load(ctx, "alice", "p")
	if len(state.Messages) != 2 || len(state.Messages[1].Tools) != 1 || state.Messages[1].Tools[0].Name != "read_assets" || !state.Messages[1].Tools[0].OK {
		t.Fatalf("missing actual tool record %#v", state)
	}
	assets, _ := s.ListAssets(ctx, "alice", "p", "")
	afterJSON, _ := json.Marshal(assets)
	if string(beforeJSON) != string(afterJSON) {
		t.Fatal("read changed assets", assets)
	}
}

func TestAuthoringAssistantReadToolRejectsScopeOverridesAndSecondCall(t *testing.T) {
	for _, tc := range []struct {
		name, first, second string
		count               int
	}{
		{"scope", `{"reply":"读取","tool":"read_assets","tool_args":{"owner":"bob"}}`, ``, 1},
		{"unknown", `{"reply":"执行","tool":"run_all_agents"}`, ``, 1},
		{"loop", `{"reply":"读取","tool":"read_assets"}`, `{"reply":"继续读取","tool":"get_current_screenplay"}`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now()
			s := screenplay.NewMemoryStore()
			_, _ = s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
			repo := screenplay.NewMemoryAuthoringRepository()
			calls := 0
			mux := http.NewServeMux()
			RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
				return screenplayProviderFunc(func(context.Context, batchfactoryv11.TextCompletionRequest) (string, error) {
					calls++
					if calls == 1 {
						return tc.first, nil
					}
					return tc.second, nil
				}), nil
			})
			res := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/chat", map[string]any{"expected_revision": 0, "message": "检查"})
			if res.Code == 200 || calls != tc.count {
				t.Fatal(res.Code, res.Body.String(), calls)
			}
			state, e := repo.Load(ctx, "alice", "p")
			if e == nil {
				raw, _ := json.Marshal(state)
				t.Fatal("invalid tool unexpectedly saved", string(raw))
			}
		})
	}
}

func TestAuthoringReadToolEndpointNeedsNoModelAndChecksMembership(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	_, _ = s.CreateProject(ctx, screenplay.Project{ID: "alice-only", OwnerUsername: "alice"})
	_, _ = s.CreateAsset(ctx, screenplay.Asset{ProjectID: "alice-only", OwnerUsername: "alice", Type: screenplay.AssetCharacter, Name: "实际人物"})
	repo := screenplay.NewMemoryAuthoringRepository()
	mux := http.NewServeMux()
	calls := 0
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		calls++
		return nil, nil
	})
	path := "/api/comic-screenplays/projects/alice-only/assistant/read-tool"
	res := signedJSONRequest(t, mux, now, "alice", "POST", path, map[string]any{"tool": "read_assets", "tool_args": map[string]any{"asset_type": "character"}})
	if res.Code != 200 || !strings.Contains(res.Body.String(), "实际人物") || calls != 0 {
		t.Fatal(res.Code, res.Body.String(), calls)
	}
	if _, e := repo.Load(ctx, "alice", "alice-only"); e == nil {
		t.Fatal("read endpoint persisted conversation")
	}
	res = signedJSONRequest(t, mux, now, "bob", "POST", path, map[string]any{"tool": "read_assets"})
	if res.Code == 200 || strings.Contains(res.Body.String(), "实际人物") || calls != 0 {
		t.Fatal("cross-member read", res.Code, res.Body.String())
	}
	res = signedJSONRequest(t, mux, now, "alice", "POST", path, map[string]any{"tool": "run_all_agents"})
	if res.Code == 200 || calls != 0 {
		t.Fatal("write tool invoked", res.Code, calls)
	}
	svc := screenplay.AuthoringService{Store: s, Repository: repo}
	state, _ := svc.Load(ctx, "alice", "alice-only")
	state.Members = []screenplay.MemberGrant{{Username: "reader", Read: true}}
	if _, e := svc.Save(ctx, "alice", "alice-only", state, 0); e != nil {
		t.Fatal(e)
	}
	res = signedJSONRequest(t, mux, now, "reader", "POST", path, map[string]any{"tool": "read_assets"})
	if res.Code != 200 || !strings.Contains(res.Body.String(), "实际人物") || calls != 0 {
		t.Fatal("read-only member could not inspect", res.Code, res.Body.String(), calls)
	}
}

func TestAuthoringReadToolShotIDsUseAuthenticatedScope(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	repo := screenplay.NewMemoryAuthoringRepository()
	for _, owner := range []string{"alice", "bob"} {
		_, _ = s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: owner})
		_, _ = s.SaveStoryboard(ctx, screenplay.Storyboard{ProjectID: "p", OwnerUsername: owner, EpisodeNo: 1, Engine: screenplay.EngineXiaoluoV7, Shots: []screenplay.StoryboardShot{{ID: owner + "-shot", Seq: 1, VideoDesc: owner + "视频"}, {ID: owner + "-other", Seq: 2, VideoDesc: owner + "未选"}}})
	}
	mux := http.NewServeMux()
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, nil)
	for _, tc := range []struct {
		id string
		ok bool
	}{{"alice-shot", true}, {"bob-shot", false}, {"missing", false}} {
		res := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/read-tool", map[string]any{"tool": "read_shot_video_prompts", "tool_args": map[string]any{"episode_no": 1, "engine": screenplay.EngineXiaoluoV7, "shot_ids": []string{tc.id}}})
		if (res.Code == 200) != tc.ok {
			t.Fatal(tc, res.Code, res.Body.String())
		}
		if strings.Contains(res.Body.String(), "bob视频") || strings.Contains(res.Body.String(), "alice未选") {
			t.Fatal("scope leak", res.Body.String())
		}
		if tc.ok && !strings.Contains(res.Body.String(), "alice视频") {
			t.Fatal("missing selected prompt", res.Body.String())
		}
	}
	if _, e := repo.Load(ctx, "alice", "p"); e == nil {
		t.Fatal("read persisted conversation")
	}
}
