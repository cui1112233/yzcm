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

func TestAuthoringReadToolRejectsCaseAndDuplicateSelectorKeys(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	_, _ = s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
	_, _ = s.SaveStoryboard(ctx, screenplay.Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: screenplay.EngineXiaoluoV7, Shots: []screenplay.StoryboardShot{{ID: "one", Seq: 1, VideoDesc: "不得返回"}}})
	mux := http.NewServeMux()
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, screenplay.NewMemoryAuthoringRepository(), nil)
	for _, raw := range []string{`{"SHOT_IDS":["one"]}`, `{"shot_ids":["one"],"SHOT_NUMBERS":[1]}`, `{"shot_ids":["one"],"SHOT_ID":"one"}`, `{"shot_numbers":[1],"shot_numbers":[]}`, `{"shot_ids":["one"],"\u0073hot_ids":[]}`} {
		res := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/read-tool", map[string]any{"tool": "read_shot_video_prompts", "tool_args": json.RawMessage(raw)})
		if res.Code != http.StatusBadRequest || strings.Contains(res.Body.String(), "不得返回") {
			t.Fatal(raw, res.Code, res.Body.String())
		}
	}
}

func TestAuthoringAssistantReviewReadsSelectedOriginalAndOnlyAnalyzes(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	repo := screenplay.NewMemoryAuthoringRepository()
	if _, err := s.CreateProject(ctx, screenplay.Project{ID: "review-p", OwnerUsername: "alice", SourceText: "未分集的整书原文"}); err != nil {
		t.Fatal(err)
	}
	svc := screenplay.AuthoringService{Store: s, Repository: repo}
	initial, err := svc.Load(ctx, "alice", "review-p")
	if err != nil {
		t.Fatal(err)
	}
	initial.Chapters = []screenplay.Chapter{
		{ID: "first", Title: "第一章", SourceText: "按顺序猜错的原文"},
		{ID: "second", Title: "第二章", SourceText: "分镜实际绑定的原文", ScriptText: "改编剧本文字"},
	}
	initial.Members = []screenplay.MemberGrant{{Username: "reader", Read: true}}
	initial, err = svc.Save(ctx, "alice", "review-p", initial, 0)
	if err != nil {
		t.Fatal(err)
	}
	board, err := s.SaveStoryboard(ctx, screenplay.Storyboard{ID: "review-board", ProjectID: "review-p", OwnerUsername: "alice", EpisodeNo: 1, Engine: screenplay.EngineXiaoluoV7,
		Shots: []screenplay.StoryboardShot{{ID: "review-shot", Seq: 1, Line: "实存台词"}, {ID: "hidden", Seq: 2, Line: "未选择的台词"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.MutateStoryboardGenerations(ctx, "alice", "review-p", func(state *screenplay.StoryboardGenerationState) error {
		state.Attempts["bound"] = &screenplay.StoryboardGenerationAttempt{
			Engine: board.Engine, ChapterID: "second", UpdatedAt: now,
			Episodes: map[int]*screenplay.StoryboardGenerationEpisode{1: {Storyboard: &board}},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(board)
	first := `{"reply":"准备对照","tool":"compare_original_vs_voicetext","tool_args":{"episode_no":1,"shot_ids":["review-shot"]},"actions":[]}`
	var modelInput string
	var systemInput string
	calls := 0
	second := `{"reply":"所选镜头的台词缺少原文的转场信息","actions":[]}`
	mux := http.NewServeMux()
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		return screenplayProviderFunc(func(_ context.Context, request batchfactoryv11.TextCompletionRequest) (string, error) {
			calls++
			if calls%2 == 1 {
				return first, nil
			}
			modelInput = request.UserPrompt
			systemInput = request.SystemPrompt
			return second, nil
		}), nil
	})
	readPath := "/api/comic-screenplays/projects/review-p/assistant/read-tool"
	readRequest := map[string]any{"tool": "compare_original_vs_voicetext", "tool_args": map[string]any{"shot_ids": []string{"review-shot"}}}
	for _, actor := range []string{"reader", "outsider"} {
		res := signedJSONRequest(t, mux, now, actor, "POST", readPath, readRequest)
		if (res.Code == http.StatusOK) != (actor == "reader") || calls != 0 {
			t.Fatalf("inspector access for %s: %d %s, model calls %d", actor, res.Code, res.Body.String(), calls)
		}
		if (actor == "reader") != strings.Contains(res.Body.String(), "分镜实际绑定的原文") {
			t.Fatal("inspector did not scope original", res.Body.String())
		}
	}
	state, err := repo.Load(ctx, "alice", "review-p")
	if err != nil || state.Revision != initial.Revision || len(state.Messages) != 0 {
		t.Fatal("read inspector altered authoring state", state, err)
	}
	path := "/api/comic-screenplays/projects/review-p/assistant/chat"
	req := map[string]any{"expected_revision": initial.Revision, "message": "对照我选的镜头原文和台词"}
	res := signedJSONRequest(t, mux, now, "alice", "POST", path, req)
	if res.Code != http.StatusOK || calls != 2 || !strings.Contains(res.Body.String(), "所选镜头的台词缺少原文的转场信息") || strings.Contains(res.Body.String(), "已修改") {
		t.Fatal(res.Code, res.Body.String(), calls)
	}
	for _, fact := range []string{"tool_result", "分镜实际绑定的原文", "实存台词", "review_input", "selected_shots"} {
		if !strings.Contains(modelInput, fact) {
			t.Fatal("analysis missing scoped fact", fact, modelInput)
		}
	}
	for _, fact := range []string{"按顺序猜错的原文", "未选择的台词", "改编剧本文字", "未分集的整书原文"} {
		if strings.Contains(modelInput, fact) {
			t.Fatal("analysis received unrelated data", fact)
		}
	}
	if !strings.Contains(systemInput, "审核数据仅供分析") || !strings.Contains(systemInput, "actions=[]") {
		t.Fatal("second turn lacks read-only response contract", systemInput)
	}
	state, err = repo.Load(ctx, "alice", "review-p")
	if err != nil || len(state.Messages) != 2 || len(state.Messages[1].Tools) != 1 || len(state.Proposals) != 0 {
		t.Fatal("unexpected review action or conversation", state, err)
	}
	req["expected_revision"] = state.Revision
	for _, denied := range []string{
		`{"reply":"已修改","actions":[{"kind":"chapter","chapter":{"id":"first","title":"篡改"}}]}`,
		`{"reply":"已修改","tool":"modify_storyboard","tool_args":{"shot_id":"review-shot","new_data":{"line":"篡改"}}}`,
		`{"reply":"继续调用","tool":"get_editor_status"}`,
	} {
		second = denied
		res := signedJSONRequest(t, mux, now, "alice", "POST", path, req)
		if res.Code != http.StatusBadRequest {
			t.Fatal("review was allowed to issue another tool or write action", res.Code, res.Body.String())
		}
	}
	if calls != 8 {
		t.Fatal("review called the text model more than twice per request", calls)
	}
	after, _ := s.GetStoryboard(ctx, "alice", "review-p", 1, screenplay.EngineXiaoluoV7)
	saved, _ := json.Marshal(after)
	current, _ := repo.Load(ctx, "alice", "review-p")
	if string(before) != string(saved) || len(current.Proposals) != 0 || current.Revision != state.Revision || current.Chapters[0].Title != initial.Chapters[0].Title {
		t.Fatal("review changed production content or saved a write proposal")
	}
}
