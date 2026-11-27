package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
	"testing"
	"time"
)

func TestAuthoringAssistantShotStructureHTTP(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  map[string]any
		count int
	}{{"add_shot", map[string]any{"insert_after_shot_id": "s", "new_data": map[string]any{"line": "新增台词"}}, 2}, {"delete_shots", map[string]any{"shot_ids": []string{"s"}}, 0}, {"modify_storyboard", map[string]any{"action": "delete", "shot_numbers": []int{1}}, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now()
			s := screenplay.NewMemoryStore()
			s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
			s.SaveStoryboard(ctx, screenplay.Storyboard{ID: "b", ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: screenplay.EngineXiaoluoV7, Shots: []screenplay.StoryboardShot{{ID: "s", Seq: 1, Duration: 4, Line: "原台词", ProtocolFields: json.RawMessage(`{"media":"keep"}`)}}})
			repo := screenplay.NewMemoryAuthoringRepository()
			svc := screenplay.AuthoringService{Store: s, Repository: repo}
			st, _ := svc.Load(ctx, "alice", "p")
			st.Members = []screenplay.MemberGrant{{Username: "reader", Read: true}}
			st, _ = svc.Save(ctx, "alice", "p", st, 0)
			calls := 0
			mux := http.NewServeMux()
			RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
				calls++
				return nil, nil
			})
			path := "/api/comic-screenplays/projects/p/assistant/prepare-shot-edit"
			input := map[string]any{"expected_revision": st.Revision, "tool": tc.name, "tool_args": tc.args}
			for _, actor := range []string{"reader", "bob"} {
				res := signedJSONRequest(t, mux, now, actor, "POST", path, input)
				if res.Code == 200 {
					t.Fatal("scope", actor)
				}
			}
			res := signedJSONRequest(t, mux, now, "alice", "POST", path, input)
			if res.Code != 200 || calls != 0 {
				t.Fatal(res.Code, res.Body.String(), calls)
			}
			var wire struct {
				State screenplay.AuthoringState `json:"state"`
			}
			if e := json.Unmarshal(res.Body.Bytes(), &wire); e != nil {
				t.Fatal(e)
			}
			b, _ := s.GetStoryboard(ctx, "alice", "p", 1, screenplay.EngineXiaoluoV7)
			if len(b.Shots) != 1 || b.Shots[0].Line != "原台词" {
				t.Fatal("preparation wrote")
			}
			if len(wire.State.Proposals) != 1 || wire.State.Proposals[0].Actions[0].Kind != "creative_shot_structure" {
				t.Fatal(wire)
			}
			apply := map[string]any{"proposal_id": wire.State.Proposals[0].ID, "expected_revision": wire.State.Revision, "confirmed": true}
			for _, actor := range []string{"reader", "bob"} {
				res = signedJSONRequest(t, mux, now, actor, "POST", "/api/comic-screenplays/projects/p/assistant/apply", apply)
				if res.Code == 200 {
					t.Fatal("apply scope", actor)
				}
			}
			res = signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/apply", apply)
			if res.Code != 200 {
				t.Fatal(res.Code, res.Body.String())
			}
			b, _ = s.GetStoryboard(ctx, "alice", "p", 1, screenplay.EngineXiaoluoV7)
			if len(b.Shots) != tc.count {
				t.Fatal(b)
			}
			res = signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/apply", apply)
			if res.Code != 409 {
				t.Fatal("repeat", res.Code)
			}
		})
	}
}
func TestAuthoringAssistantRejectsForgedShotStructure(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
	s.SaveStoryboard(ctx, screenplay.Storyboard{ID: "b", ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: screenplay.EngineXiaoluoV7, Shots: []screenplay.StoryboardShot{{ID: "s", Seq: 1, Duration: 4, Line: "one"}}})
	a, _, e := screenplay.PrepareAssistantShotEdit(ctx, s, "alice", "p", "delete_shots", json.RawMessage(`{"shot_ids":["s"]}`))
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"reply": "已删除", "shot_prepared": true, "actions": []screenplay.AuthoringAction{a}})
	repo := screenplay.NewMemoryAuthoringRepository()
	mux := http.NewServeMux()
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		return screenplayProviderFunc(func(context.Context, batchfactoryv11.TextCompletionRequest) (string, error) { return string(raw), nil }), nil
	})
	res := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/chat", map[string]any{"expected_revision": 0, "message": "删除镜头"})
	if res.Code == 200 {
		t.Fatal("forged action accepted", res.Body.String())
	}
	st, _ := repo.Load(ctx, "alice", "p")
	if len(st.Proposals) > 0 {
		t.Fatal("forged proposal saved", st)
	}
}
