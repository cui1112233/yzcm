package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"qiantie/backend/internal/screenplay"
	"strings"
	"testing"
	"time"
)

func TestAssistantReferenceShotRoutesScopeEvidenceAndPage(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := screenplay.NewMemoryStore()
	repo := screenplay.NewMemoryAuthoringRepository()
	_, e := store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
	if e != nil {
		t.Fatal(e)
	}
	b := screenplay.Storyboard{ID: "board", OwnerUsername: "alice", ProjectID: "p", EpisodeNo: 1, Engine: "seedance"}
	for i := 0; i < 231; i++ {
		b.Shots = append(b.Shots, screenplay.StoryboardShot{ID: fmt.Sprintf("s%03d", i), Seq: i + 1, Duration: 5, VideoDesc: strings.Repeat("字", 300), ImagePrompt: "IMAGE_PRIVATE", ProtocolFields: json.RawMessage(`{"secret":"PROTOCOL_PRIVATE"}`)})
	}
	b, e = store.SaveStoryboard(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	view := screenplay.AssistantReferenceView{Boards: []screenplay.Storyboard{b}}
	whole, e := view.Describe(screenplay.AssistantReferenceSelector{Kind: "storyboard", ID: b.ID})
	if e != nil {
		t.Fatal(e)
	}
	mux := http.NewServeMux()
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, store, repo, nil)
	path := "/api/comic-screenplays/projects/p/assistant/references/storyboard-shots"
	input := func() map[string]any {
		return map[string]any{"session_id": "default", "board_id": b.ID, "expected_revision": whole.Revision, "expected_digest": whole.Digest, "offset": 100, "limit": 100}
	}
	res := signedJSONRequest(t, mux, now, "alice", "POST", path, input())
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	var page screenplay.AssistantReferenceShotPage
	if e = json.Unmarshal(res.Body.Bytes(), &page); e != nil {
		t.Fatal(e)
	}
	if len(page.Shots) != 100 || page.Total != 231 || page.Shots[0].ID != "s100" || page.Board.Digest != whole.Digest {
		t.Fatal(res.Body.String())
	}
	if strings.Contains(res.Body.String(), "IMAGE_PRIVATE") || strings.Contains(res.Body.String(), "PROTOCOL_PRIVATE") {
		t.Fatal("private fields leaked")
	}
	for _, actor := range []string{"bob", "stranger"} {
		res = signedJSONRequest(t, mux, now, actor, "POST", path, input())
		if res.Code != 403 {
			t.Fatal("nonmember", actor, res.Code)
		}
	}
	cases := []struct {
		field string
		value any
		code  int
	}{{"board_id", "foreign", 404}, {"expected_revision", whole.Revision + 1, 409}, {"expected_digest", strings.Repeat("0", 64), 409}, {"offset", -1, 400}, {"offset", 232, 400}, {"limit", 101, 400}, {"session_id", "", 400}, {"session_id", "private-other", 404}, {"owner", "bob", 400}, {"shot_ids", []string{"s100"}, 400}, {"text", "forged", 400}, {"url", "https://invalid.example", 400}}
	for _, tc := range cases {
		body := input()
		body[tc.field] = tc.value
		res = signedJSONRequest(t, mux, now, "alice", "POST", path, body)
		if res.Code != tc.code {
			t.Fatalf("%s: %d %s", tc.field, res.Code, res.Body.String())
		}
	}
	sel := whole.Selector()
	sel.ShotIDs = []string{"s100"}
	refs, e := view.Resolve([]screenplay.AssistantReferenceSelector{sel})
	if e != nil {
		t.Fatal(e)
	}
	body := input()
	body["expected_digest"] = refs[0].Digest
	res = signedJSONRequest(t, mux, now, "alice", "POST", path, body)
	if res.Code != 409 {
		t.Fatal("range digest authorized full page", res.Code)
	}
	b.Shots[0].VideoDesc = "changed"
	if _, e = store.SaveStoryboard(ctx, b); e != nil {
		t.Fatal(e)
	}
	res = signedJSONRequest(t, mux, now, "alice", "POST", path, input())
	if res.Code != 409 {
		t.Fatal("source changed page accepted", res.Code)
	}
}
