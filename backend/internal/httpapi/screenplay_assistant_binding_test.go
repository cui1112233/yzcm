package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"qiantie/backend/internal/agentworkspace"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/prompt"
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
	configs := NewUserConfigStore(t.TempDir())
	r.configs = configs
	if e := configs.write("alice", map[string]any{"scriptDefaults": map[string]any{"videoModelKey": "fixture"}}); e != nil {
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
func TestBindingNineGridSourceSceneInSubjectAndSoraRequests(t *testing.T) {
	ctx := context.Background()
	library, first, _, _, _ := assetLibraryServiceFixture(t)
	m := library.Media
	p, e := m.Projects.GetProject(ctx, "alice", first.ProjectID)
	if e != nil {
		t.Fatal(e)
	}
	scene, e := m.Projects.CreateAsset(ctx, screenplay.Asset{ID: "old-scene-uuid", OwnerUsername: "alice", ProjectID: p.ID, Type: screenplay.AssetScene, Name: "街道", Extra: map[string]string{"sora2_info": "asset-street"}})
	if e != nil {
		t.Fatal(e)
	}
	runner := screenplayVideoRunner{References: m.References, ReferenceIDs: m.ReferenceIDs, Subjects: m.VideoSubjects}
	selected, e := prompt.ResolveCreativePrompt("luoshui_storyboard_175", "storyboard")
	if e != nil {
		t.Fatal(e)
	}
	contract := map[string]any{"prompt_id": selected.ID, "prompt_digest": selected.Digest, "parser_version": selected.ParserVersion, "output_protocol": selected.OutputProtocol}
	for _, tc := range []struct {
		name, id, label      string
		source, explicit, ok bool
	}{
		{"nine-grid source", "S1", "街道", true, false, true}, {"unicode source", "场景一", "街道", true, false, true}, {"arbitrary source", "雨后/片头", "街道", true, false, true},
		{"legacy asset", scene.ID, "街道", false, false, true}, {"unproven label", "S1", "街道", false, false, false}, {"expired explicit ID", "S1", "街道", true, true, false},
	} {
		for _, subject := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/subject=%t", tc.name, subject), func(t *testing.T) {
				fields := map[string]any{"scene_id": tc.id}
				if tc.source {
					fields["_cm_contract"] = contract
				}
				if tc.explicit {
					fields["asset_bindings"] = map[string]any{"scene_id": "deleted-asset"}
				}
				raw, _ := json.Marshal(fields)
				sh := screenplay.StoryboardShot{ID: "shot", Scene: tc.label, Seq: 1, Duration: 4, VideoDesc: "@街道 夜景", ProtocolFields: raw}
				p.CreativeConfig = &screenplay.CreativeConfig{UseSubjectMode: subject, AutoReplaceSora2Info: !subject}
				if !subject {
					p.CreativeConfig.VideoGenerationMode = "text_to_video"
				}
				in, _, e := runner.prepareInput(ctx, p, screenplay.Storyboard{ID: "b"}, sh, "fixture", "720p")
				if !tc.ok {
					if e == nil {
						t.Fatalf("ambiguous/dead binding accepted: %+v", in)
					}
					return
				}
				if e != nil {
					t.Fatal(e)
				}
				if subject {
					if !slices.Equal(in.SubjectReferenceURIs, []string{"asset://asset-street"}) {
						t.Fatal(in)
					}
				} else if in.Prompt != "@asset-street 夜景" || len(in.ImageURLs) != 0 {
					t.Fatal(in)
				}
			})
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
