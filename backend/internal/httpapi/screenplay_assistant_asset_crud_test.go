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

func TestAuthoringAssistantAssetCRUDHTTPNoModelAndScopes(t *testing.T) {
	for _, kind := range []string{"character", "scene", "prop"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now()
			s := screenplay.NewMemoryStore()
			_, _ = s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
			_, _ = s.CreateProject(ctx, screenplay.Project{ID: "other", OwnerUsername: "alice"})
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
			path := "/api/comic-screenplays/projects/p/assistant/prepare-asset-edit"
			input := map[string]any{"expected_revision": st.Revision, "tool": "add_" + kind, "tool_args": map[string]any{"name": "新资产"}}
			for _, actor := range []string{"reader", "bob"} {
				if res := signedJSONRequest(t, mux, now, actor, "POST", path, input); res.Code == 200 {
					t.Fatal("unauthorized prepare", actor)
				}
			}
			res := signedJSONRequest(t, mux, now, "alice", "POST", path, input)
			if res.Code != 200 {
				t.Fatal(res.Code, res.Body.String())
			}
			var wire struct {
				State screenplay.AuthoringState `json:"state"`
			}
			if e := json.Unmarshal(res.Body.Bytes(), &wire); e != nil {
				t.Fatal(e)
			}
			proposal := wire.State.Proposals[0]
			if proposal.Actions[0].AssetOperation != "add" {
				t.Fatal(proposal)
			}
			assets, _ := s.ListAssets(ctx, "alice", "p", "")
			if len(assets) != 0 {
				t.Fatal("unconfirmed add wrote")
			}
			body := map[string]any{"expected_revision": wire.State.Revision, "proposal_id": proposal.ID, "confirmed": true}
			apply := "/api/comic-screenplays/projects/p/assistant/apply"
			for _, actor := range []string{"reader", "bob"} {
				if denied := signedJSONRequest(t, mux, now, actor, "POST", apply, body); denied.Code == 200 {
					t.Fatal("unauthorized confirm", actor)
				}
			}
			if denied := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/other/assistant/apply", body); denied.Code == 200 {
				t.Fatal("cross project confirm")
			}
			res = signedJSONRequest(t, mux, now, "alice", "POST", apply, body)
			if res.Code != 200 {
				t.Fatal(res.Code, res.Body.String())
			}
			_ = json.Unmarshal(res.Body.Bytes(), &wire)
			if res = signedJSONRequest(t, mux, now, "alice", "POST", apply, body); res.Code == 200 {
				t.Fatal("repeat add accepted")
			}
			assets, _ = s.ListAssets(ctx, "alice", "p", "")
			if len(assets) != 1 {
				t.Fatal(assets)
			}
			input = map[string]any{"expected_revision": wire.State.Revision, "tool": "modify_" + kind, "tool_args": map[string]any{"action": "delete", "asset_id": assets[0].ID}}
			res = signedJSONRequest(t, mux, now, "alice", "POST", path, input)
			if res.Code != 200 {
				t.Fatal(res.Code, res.Body.String())
			}
			_ = json.Unmarshal(res.Body.Bytes(), &wire)
			proposal = wire.State.Proposals[len(wire.State.Proposals)-1]
			if proposal.Actions[0].AssetOperation != "delete" {
				t.Fatal(proposal)
			}
			res = signedJSONRequest(t, mux, now, "alice", "POST", apply, map[string]any{"expected_revision": wire.State.Revision, "proposal_id": proposal.ID, "confirmed": true})
			if res.Code != 200 {
				t.Fatal(res.Code, res.Body.String())
			}
			assets, _ = s.ListAssets(ctx, "alice", "p", "")
			if len(assets) != 0 || calls != 0 {
				t.Fatal(assets, calls)
			}
		})
	}
}
