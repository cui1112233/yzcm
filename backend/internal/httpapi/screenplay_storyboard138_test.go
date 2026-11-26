package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
	"strings"
	"testing"
	"time"
)

func TestStoryboard138HTTPLifecycleAndFreeRecovery(t *testing.T) {
	ctx := context.Background()
	store := screenplay.NewMemoryStore()
	p, e := store.CreateProject(ctx, screenplay.Project{ID: "p138", OwnerUsername: "alice", ModelID: "controlled", PermissionMode: screenplay.PermissionBypass})
	if e != nil {
		t.Fatal(e)
	}
	cfg := screenplay.EffectiveCreativeConfig(p)
	cfg.StoryboardPromptID = "luoshui_storyboard_138"
	cfg.StoryboardAutoSyncAssets = false
	_, e = store.SaveCreativeConfig(ctx, "alice", "p138", cfg, p.Revision)
	if e != nil {
		t.Fatal(e)
	}
	repo := screenplay.NewMemoryAuthoringRepository()
	root := t.TempDir()
	authors := &screenplay.AuthoringService{Store: store, Repository: repo, Receipts: &screenplay.FileScriptBatchReceipts{Root: root}}
	raw := `{"episode_index":1,"title":"窗外","scenes":[{"scene_number":"1-1","location":"家","script_content":[{"element_id":"1-1-001","element_type":"action","description":"甲开窗"},{"element_id":"1-1-002","element_type":"action","description":"甲关窗"}]}]}`
	_, e = authors.Save(ctx, "alice", "p138", screenplay.AuthoringState{Chapters: []screenplay.Chapter{{ID: "c", Title: "窗外", RawResult: raw, ScriptText: "# 第1集：窗外\n\n## 1-1：  家\n人物：\n\n△ 甲开窗\n△ 甲关窗\n\n"}}}, 0)
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	now := time.Now()
	auth := BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}
	resolver := func(ctx context.Context, o, model string) (batchfactoryv11.DirectorProvider, error) {
		if !batchfactoryv11.SingleAttempt(ctx) {
			t.Error("resolver allowed automatic retry")
		}
		return screenplayProviderFunc(func(ctx context.Context, r batchfactoryv11.TextCompletionRequest) (string, error) {
			calls++
			if !batchfactoryv11.SingleAttempt(ctx) {
				t.Error("provider allowed automatic retry")
			}
			if calls == 2 && strings.Contains(r.UserPrompt, `"description":"甲开窗"`) {
				t.Error("processed source submitted again")
			}
			return fmt.Sprintf(`{"storyboard":[{"id":1,"source_elements":["1-1-00%d"],"characters":"甲","scene":"家","image_prompt":"四格","video_desc":"六宫格","voice_actor":"无","emotion":"无","line":"无"}]}【分镜自检】`, calls), nil
		}), nil
	}
	mux := http.NewServeMux()
	RegisterScreenplayWorkflowRoutes(mux, auth, store, resolver, func() time.Time { return now }, nil, authors)
	RegisterScreenplayStoryboardGenerationRoutes(mux, auth, store, authors)
	base := "/api/comic-screenplays/projects/p138"
	body := map[string]any{"idempotencyKey": "first138", "chapterId": "c", "engine": "prompt_138", "usePlanning": false}
	first := signedJSONRequest(t, mux, now, "alice", "POST", base+"/storyboards/generate", body)
	if first.Code != 200 || !strings.Contains(first.Body.String(), `"partial_failure":true`) || calls != 1 {
		t.Fatal(first.Code, first.Body.String(), calls)
	}
	state, _ := store.LoadStoryboardGenerations(ctx, "alice", "p138")
	key := state.Attempts["first138"].Episodes[1].Calls["storyboard_scene_1_batch_1"].ReceiptKey
	if key == nil {
		t.Fatal("receipt key missing")
	}
	if _, e = (&screenplay.FileScriptBatchReceipts{Root: filepath.Join(root, "storyboard138")}).Load(ctx, *key); e != nil {
		t.Fatal("separate durable receipt missing", e)
	}
	_, _ = store.MutateStoryboardGenerations(ctx, "alice", "p138", func(v *screenplay.StoryboardGenerationState) error {
		a := v.Attempts["first138"]
		a.Status = "unknown"
		ep := a.Episodes[1]
		ep.Status = "unknown"
		c := ep.Calls["storyboard_scene_1_batch_1"]
		c.Status = "unknown"
		c.RawResponse = ""
		ep.Calls["storyboard_scene_1_batch_1"] = c
		return nil
	})
	authors2 := &screenplay.AuthoringService{Store: store, Repository: repo, Receipts: &screenplay.FileScriptBatchReceipts{Root: root}}
	reopened := http.NewServeMux()
	RegisterScreenplayStoryboardGenerationRoutes(reopened, auth, store, authors2)
	recovery := base + "/storyboard-generation-attempts/first138/recover"
	denied := signedJSONRequest(t, reopened, now, "bob", "POST", recovery, map[string]any{})
	if denied.Code == 200 {
		t.Fatal("cross-owner recovery")
	}
	forged := signedJSONRequest(t, reopened, now, "alice", "POST", recovery, map[string]any{"raw_result": "forged"})
	if forged.Code == 200 {
		t.Fatal("client supplied receipt accepted")
	}
	recovered := signedJSONRequest(t, reopened, now, "alice", "POST", recovery, map[string]any{})
	if recovered.Code != 200 || calls != 1 || strings.Contains(recovered.Body.String(), "request_json") || strings.Contains(recovered.Body.String(), "receipt_key") {
		t.Fatal(recovered.Code, recovered.Body.String(), calls)
	}
	body["retryOf"] = "first138"
	body["confirmedRetry"] = true
	body["idempotencyKey"] = "second138"
	second := signedJSONRequest(t, mux, now, "alice", "POST", base+"/storyboards/generate", body)
	var result screenplay.StoryboardResult
	_ = json.Unmarshal(second.Body.Bytes(), &result)
	if second.Code != 200 || result.GenerationAttempt == nil || result.GenerationAttempt.Status != "succeeded" || len(result.Storyboards) != 1 || calls != 2 {
		t.Fatal(second.Code, second.Body.String(), calls)
	}
}

func TestStoryboard138HTTPStaleRawCannotUseCallerScript(t *testing.T) {
	for _, shape := range []string{"chapter", "content", "content_episode", "episodes", "episodes_chapter"} {
		t.Run(shape, func(t *testing.T) {
			ctx := context.Background()
			store := screenplay.NewMemoryStore()
			p, e := store.CreateProject(ctx, screenplay.Project{ID: "stale138", OwnerUsername: "alice", ModelID: "controlled", PermissionMode: screenplay.PermissionBypass})
			if e != nil {
				t.Fatal(e)
			}
			cfg := screenplay.EffectiveCreativeConfig(p)
			cfg.StoryboardPromptID = "luoshui_storyboard_138"
			cfg.StoryboardAutoSyncAssets = false
			_, _ = store.SaveCreativeConfig(ctx, "alice", "stale138", cfg, p.Revision)
			authors := &screenplay.AuthoringService{Store: store, Repository: screenplay.NewMemoryAuthoringRepository(), Receipts: &screenplay.FileScriptBatchReceipts{Root: t.TempDir()}}
			raw := `{"episode_index":1,"title":"窗外","scenes":[{"scene_number":"1-1","location":"家","script_content":[{"element_id":"1-1-001","element_type":"action","description":"甲开窗"}]}]}`
			old := "# 第1集：窗外\n\n## 1-1：  家\n人物：\n\n△ 甲开窗\n\n"
			_, e = authors.Save(ctx, "alice", "stale138", screenplay.AuthoringState{Chapters: []screenplay.Chapter{{ID: "c", Title: "窗外", RawResult: raw, ScriptText: "人工修改后的当前保存剧本"}}}, 0)
			if e != nil {
				t.Fatal(e)
			}
			calls := 0
			now := time.Now()
			mux := http.NewServeMux()
			RegisterScreenplayWorkflowRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, store, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
				return screenplayProviderFunc(func(context.Context, batchfactoryv11.TextCompletionRequest) (string, error) {
					calls++
					return `{"storyboard":[{"id":1,"source_elements":["1-1-001"],"characters":"甲","scene":"家","image_prompt":"四格","video_desc":"六宫格","voice_actor":"无","emotion":"无","line":"无"}]}【分镜自检】`, nil
				}), nil
			}, func() time.Time { return now }, nil, authors)
			body := map[string]any{"idempotencyKey": "stale-" + shape, "engine": "prompt_138", "usePlanning": false}
			switch shape {
			case "chapter":
				body["chapterId"] = "c"
				body["content"] = old
			case "content":
				body["content"] = old
			case "content_episode":
				body["content"] = old
				body["episodeNo"] = 1
			case "episodes":
				body["episodes"] = map[int]string{1: old}
			case "episodes_chapter":
				body["chapterId"] = "c"
				body["episodes"] = map[int]string{1: old}
			}
			res := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/stale138/storyboards/generate", body)
			boards, _ := store.ListStoryboards(ctx, "alice", "stale138", "prompt_138")
			if res.Code == 200 || calls != 0 || len(boards) != 0 {
				t.Fatal("stale request crossed HTTP authority", res.Code, res.Body.String(), calls, len(boards))
			}
		})
	}
}
