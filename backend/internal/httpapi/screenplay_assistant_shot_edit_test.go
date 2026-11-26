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

func TestAuthoringAssistantShotToolPreviewsThenRequiresConfirmation(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	_, _ = s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
	old, _ := s.SaveStoryboard(ctx, screenplay.Storyboard{ID: "b", ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: screenplay.EngineXiaoluoV7, Shots: []screenplay.StoryboardShot{{ID: "s", Seq: 1, Duration: 4, VideoDesc: "旧视频", ImagePrompt: "旧图词", Line: "旧台词", ProtocolFields: json.RawMessage(`{"media":"keep"}`)}}})
	repo := screenplay.NewMemoryAuthoringRepository()
	mux := http.NewServeMux()
	calls := 0
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		return screenplayProviderFunc(func(context.Context, batchfactoryv11.TextCompletionRequest) (string, error) {
			calls++
			return `{"reply":"已修改成功","tool":"modify_image_prompt","tool_args":{"shot_id":"s","action":"replace","new_prompt":"新图词"},"actions":[]}`, nil
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
	board, _ := s.GetStoryboard(ctx, "alice", "p", 1, screenplay.EngineXiaoluoV7)
	if board.Shots[0].ImagePrompt != old.Shots[0].ImagePrompt || board.Revision != old.Revision {
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
	board, _ = s.GetStoryboard(ctx, "alice", "p", 1, screenplay.EngineXiaoluoV7)
	if board.Shots[0].ImagePrompt != "新图词" || board.Shots[0].VideoDesc != "旧视频" || board.Shots[0].Line != "旧台词" || calls != 1 {
		t.Fatal("fields lost or extra model call", board, calls)
	}
}

func TestAuthoringAssistantRejectsUnpreparedShotActions(t *testing.T) {
	for _, kind := range []string{"shot", "creative_shot"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now()
			s := screenplay.NewMemoryStore()
			s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
			repo := screenplay.NewMemoryAuthoringRepository()
			mux := http.NewServeMux()
			raw := fmt.Sprintf(`{"reply":"已修改成功","actions":[{"kind":%q,"target_id":"s","shot":{"script_text":"新"},"creative_shot":{"board_id":"b"},"expected_revision":1}]}`, kind)
			if kind == "creative_shot" {
				s.SaveStoryboard(ctx, screenplay.Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: screenplay.EngineXiaoluoV7, Shots: []screenplay.StoryboardShot{{ID: "s", Seq: 1, Duration: 4, VideoDesc: "old"}}})
				a, _, e := screenplay.PrepareAssistantShotEdit(ctx, s, "alice", "p", "modify_video_prompt", json.RawMessage(`{"shot_id":"s","new_prompt":"new"}`))
				if e != nil {
					t.Fatal(e)
				}
				encoded, _ := json.Marshal(map[string]any{"reply": "已修改成功", "actions": []screenplay.AuthoringAction{a}, "shot_prepared": true})
				raw = string(encoded)
			}
			RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
				return screenplayProviderFunc(func(context.Context, batchfactoryv11.TextCompletionRequest) (string, error) { return raw, nil }), nil
			})
			res := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/chat", map[string]any{"expected_revision": 0, "message": "改分镜"})
			if res.Code == 200 {
				t.Fatal("unsafe raw action accepted", res.Body.String())
			}
			if _, e := repo.Load(ctx, "alice", "p"); e == nil {
				t.Fatal("unsafe proposal saved")
			}
		})
	}
}

func TestAuthoringPrepareShotRequiresEditAndPreservesScope(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
	s.CreateProject(ctx, screenplay.Project{ID: "other", OwnerUsername: "alice"})
	for _, p := range []string{"p", "other"} {
		s.SaveStoryboard(ctx, screenplay.Storyboard{ProjectID: p, OwnerUsername: "alice", EpisodeNo: 1, Engine: screenplay.EngineXiaoluoV7, Shots: []screenplay.StoryboardShot{{ID: p + "-s", Seq: 1, Duration: 4, VideoDesc: "原词"}}})
	}
	repo := screenplay.NewMemoryAuthoringRepository()
	svc := screenplay.AuthoringService{Store: s, Repository: repo}
	st, _ := svc.Load(ctx, "alice", "p")
	st.Members = []screenplay.MemberGrant{{Username: "reader", Read: true}}
	st, _ = svc.Save(ctx, "alice", "p", st, 0)
	mux := http.NewServeMux()
	calls := 0
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		calls++
		return nil, nil
	})
	path := "/api/comic-screenplays/projects/p/assistant/prepare-shot-edit"
	input := map[string]any{"expected_revision": st.Revision, "tool": "modify_video_prompt", "tool_args": map[string]any{"shot_id": "p-s", "action": "append", "new_prompt": "新词"}}
	for _, actor := range []string{"reader", "bob"} {
		res := signedJSONRequest(t, mux, now, actor, "POST", path, input)
		if res.Code == 200 {
			t.Fatal("scope allowed", actor)
		}
	}
	for _, args := range []map[string]any{{"shot_id": "other-s", "new_prompt": "新"}, {"shot_id": "p-s", "action": "rewrite", "new_prompt": "新"}, {"shot_id": "p-s", "action": "fix_violations"}, {"shot_id": "p-s", "action": "replace", "new_prompt": "新", "creative_shot": map[string]any{}}, {"shot_id": "p-s", "action": "replace", "new_prompt": "新", "protocol_fields": map[string]any{}}} {
		input["tool_args"] = args
		res := signedJSONRequest(t, mux, now, "alice", "POST", path, input)
		if res.Code == 200 {
			t.Fatal("unsafe request", args)
		}
	}
	input["tool_args"] = map[string]any{"shot_id": "p-s", "action": "append", "new_prompt": "新词"}
	res := signedJSONRequest(t, mux, now, "alice", "POST", path, input)
	if res.Code != 200 || calls != 0 {
		t.Fatal(res.Code, res.Body.String(), calls)
	}
	res = signedJSONRequest(t, mux, now, "alice", "POST", path, input)
	if res.Code != 409 {
		t.Fatal("stale prepare accepted", res.Code, res.Body.String())
	}
	b, _ := s.GetStoryboard(ctx, "alice", "p", 1, screenplay.EngineXiaoluoV7)
	if b.Shots[0].VideoDesc != "原词" {
		t.Fatal("prepare wrote", b)
	}
}
