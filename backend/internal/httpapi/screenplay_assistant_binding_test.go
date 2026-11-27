package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"

	"qiantie/backend/internal/agentworkspace"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
)

func TestBindingFinalVideoRequests(t *testing.T) {
	ctx := context.Background()
	library, asset, _, _, _ := assetLibraryServiceFixture(t)
	m := library.Media
	p, e := m.Projects.GetProject(ctx, "alice", asset.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	state, e := m.Media.LoadMedia(ctx, "alice", p.ID)
	if e != nil {
		t.Fatal(e)
	}
	item, e := mediaItem(state, state.Selected[asset.ID])
	if e != nil {
		t.Fatal(e)
	}
	url := "https://fixture.invalid/" + item.AssetID
	m.ReferencesProvider = agentImageReferenceProvider(func(_ context.Context, a agentworkspace.Asset, _ time.Duration) (string, error) {
		return "https://fixture.invalid/" + a.ID, nil
	})
	asset.Extra["sora2_info"] = "asset-lin"
	_, e = m.Projects.UpdateAsset(ctx, "alice", p.ID, asset.ID, asset, asset.Revision)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(map[string]any{"asset_bindings": map[string]any{"character_ids": []string{asset.ID}}, "character_ids": []string{"retired"}, "scene_id": 7})
	sh := screenplay.StoryboardShot{ID: "s", Seq: 1, Duration: 4, VideoDesc: "动作", Characters: "旧名", ProtocolFields: raw}
	b := screenplay.Storyboard{ID: "b"}
	r := screenplayVideoRunner{References: m.References, ReferenceIDs: m.ReferenceIDs, Subjects: m.VideoSubjects}
	in, _, e := r.prepareInput(ctx, p, b, sh, "fixture", "720p")
	if e != nil || !slices.Equal(in.ImageURLs, []string{url}) {
		t.Fatal(in, e)
	}
	p.CreativeConfig = &screenplay.CreativeConfig{UseSubjectMode: true}
	in, _, e = r.prepareInput(ctx, p, b, sh, "fixture", "720p")
	if e != nil || !slices.Equal(in.SubjectReferenceURIs, []string{"asset://asset-lin"}) {
		t.Fatal(in, e)
	}
	service := &fakeScreenplayVideoService{}
	r.service = service
	r.repo = screenplay.NewMemoryProductionRepository()
	r.configs = NewUserConfigStore(t.TempDir())
	if e := r.configs.write("alice", map[string]any{"scriptDefaults": map[string]any{"videoModelKey": "fixture"}}); e != nil {
		t.Fatal(e)
	}
	sh.Characters = asset.Name
	sh.ProtocolFields = json.RawMessage(`{"asset_bindings":{"character_ids":["deleted"]}}`)
	for _, subject := range []bool{true, false} {
		p.CreativeConfig.UseSubjectMode = subject
		if _, _, e = r.prepareInput(ctx, p, b, sh, "fixture", "720p"); e == nil {
			t.Errorf("missing explicitly bound asset accepted; subject=%v", subject)
		}
	}
	for _, raw := range []string{`{"asset_bindings":{"character_ids":["deleted"]}}`, `{"asset_bindings":{"character_ids":null}}`} {
		sh.ProtocolFields = json.RawMessage(raw)
		b.Shots = []screenplay.StoryboardShot{sh}
		for _, subject := range []bool{true, false} {
			p.CreativeConfig.UseSubjectMode = subject
			if _, e := r.ProduceEpisode(ctx, p, 1, b); e == nil || len(service.inputs) != 0 {
				t.Fatal("invalid binding reached provider", subject, e, service.inputs)
			}
		}
	}
	sh.ProtocolFields, _ = json.Marshal(map[string]any{"asset_bindings": map[string]any{"character_ids": []string{}}, "character_ids": []string{asset.ID}})
	for _, subject := range []bool{true, false} {
		p.CreativeConfig.UseSubjectMode = subject
		in, _, e = r.prepareInput(ctx, p, b, sh, "fixture", "720p")
		if e != nil || len(in.ImageURLs)+len(in.SubjectReferenceURIs) != 0 {
			t.Fatal("clear revived old selection", in, e)
		}
	}
}

func TestBindingLegacyOrdinaryVideoUsesSceneName(t *testing.T) {
	ctx := context.Background()
	library, first, _, _, _ := assetLibraryServiceFixture(t)
	m := library.Media
	p, e := m.Projects.GetProject(ctx, "alice", first.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	scene, e := m.Projects.CreateAsset(ctx, screenplay.Asset{OwnerUsername: "alice", ProjectID: p.ID, Type: screenplay.AssetScene, Name: "街道"})
	if e != nil {
		t.Fatal(e)
	}
	st, e := m.Media.MutateMedia(ctx, "alice", p.ID, func(st *screenplay.MediaState) error { st.Selected[scene.ID] = st.Selected[first.ID]; return nil })
	if e != nil {
		t.Fatal(e)
	}
	media, e := mediaItem(st, st.Selected[scene.ID])
	if e != nil {
		t.Fatal(e)
	}
	m.ReferencesProvider = agentImageReferenceProvider(func(_ context.Context, a agentworkspace.Asset, _ time.Duration) (string, error) {
		return "https://fixture.invalid/" + a.ID, nil
	})
	runner := screenplayVideoRunner{References: m.References, ReferenceIDs: m.ReferenceIDs}
	for _, raw := range []string{`{"scene_id":7}`, `{"scene_id":"S1"}`, `{"scene_id":"场景一","asset_bindings":{"character_ids":[]}}`} {
		sh := screenplay.StoryboardShot{ID: "shot", Scene: "街道", Seq: 1, Duration: 4, VideoDesc: "夜景", ProtocolFields: json.RawMessage(raw)}
		in, _, e := runner.prepareInput(ctx, p, screenplay.Storyboard{ID: "b"}, sh, "fixture", "720p")
		if e != nil || !slices.Equal(in.ImageURLs, []string{"https://fixture.invalid/" + media.AssetID}) {
			t.Fatal(raw, in, e)
		}
	}
}
func TestBindingChatScopesAndConfirmation(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
	s.CreateAsset(ctx, screenplay.Asset{ID: "asset", Name: "甲", Type: screenplay.AssetCharacter, ProjectID: "p", OwnerUsername: "alice"})
	s.SaveStoryboard(ctx, screenplay.Storyboard{ID: "b", ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: screenplay.EngineXiaoluoV7, Shots: []screenplay.StoryboardShot{{ID: "s", Seq: 1, Duration: 4}}})
	repo := screenplay.NewMemoryAuthoringRepository()
	mux := http.NewServeMux()
	reply := `{"tool":"bind_shot_assets","tool_args":{"shot_id":"s","character_names":["甲"]},"reply":"准备绑定","actions":[]}`
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		return screenplayProviderFunc(func(context.Context, batchfactoryv11.TextCompletionRequest) (string, error) { return reply, nil }), nil
	})
	path := "/api/comic-screenplays/projects/p/assistant/"
	input := map[string]any{"expected_revision": 0, "message": "绑定甲"}
	if res := signedJSONRequest(t, mux, now, "bob", "POST", path+"chat", input); res.Code == 200 {
		t.Fatal("other owner")
	}
	res := signedJSONRequest(t, mux, now, "alice", "POST", path+"chat", input)
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	st, _ := repo.Load(ctx, "alice", "p")
	if len(st.Proposals) != 1 || st.Proposals[0].Status != "pending" {
		t.Fatal(st)
	}
	b, _ := s.GetStoryboard(ctx, "alice", "p", 1, screenplay.EngineXiaoluoV7)
	if b.Shots[0].Characters != "" {
		t.Fatal("prepared write")
	}
	payload := map[string]any{"expected_revision": st.Revision, "confirmed": true, "proposal_id": st.Proposals[0].ID}
	res = signedJSONRequest(t, mux, now, "alice", "POST", path+"apply", payload)
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	b, _ = s.GetStoryboard(ctx, "alice", "p", 1, screenplay.EngineXiaoluoV7)
	if b.Shots[0].Characters != "甲" {
		t.Fatal(b)
	}
	if res = signedJSONRequest(t, mux, now, "alice", "POST", path+"apply", payload); res.Code == 200 {
		t.Fatal("double apply")
	}
	st, _ = repo.Load(ctx, "alice", "p")
	forged, _ := json.Marshal(map[string]any{"actions": st.Proposals[0].Actions, "shot_prepared": true})
	reply = string(forged)
	input["expected_revision"] = st.Revision
	if res = signedJSONRequest(t, mux, now, "alice", "POST", path+"chat", input); res.Code == 200 {
		t.Fatal("forged binding accepted")
	}
}
