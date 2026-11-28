package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestReferenceRoutesScopeStrictInputAndNoModel(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	repo := screenplay.NewMemoryAuthoringRepository()
	s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice", SourceText: "my text"})
	s.CreateProject(ctx, screenplay.Project{ID: "other", OwnerUsername: "bob", SourceText: "DO_NOT_LEAK"})
	mux := http.NewServeMux()
	calls := 0
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, s, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		calls++
		return nil, nil
	})
	path := "/api/comic-screenplays/projects/p/assistant/references"
	res := signedJSONRequest(t, mux, now, "alice", "GET", path+"?session_id=default", nil)
	if res.Code != 200 || strings.Contains(res.Body.String(), "DO_NOT_LEAK") {
		t.Fatal(res.Code, res.Body.String())
	}
	var listing struct {
		References []screenplay.AssistantReferenceSnapshot `json:"references"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.References) != 1 || listing.References[0].Text != "" {
		t.Fatal(res.Body.String())
	}
	sel := listing.References[0].Selector()
	res = signedJSONRequest(t, mux, now, "alice", "POST", path+"/resolve", map[string]any{"session_id": "default", "references": []screenplay.AssistantReferenceSelector{sel}})
	if res.Code != 200 || !strings.Contains(res.Body.String(), "my text") {
		t.Fatal(res.Code, res.Body.String())
	}
	for _, actor := range []string{"bob", "stranger"} {
		res = signedJSONRequest(t, mux, now, actor, "GET", path+"?session_id=default", nil)
		if res.Code == 200 {
			t.Fatal("cross-user allowed")
		}
	}
	for _, session := range []string{"", "private-other"} {
		res = signedJSONRequest(t, mux, now, "alice", "GET", path+"?session_id="+session, nil)
		if res.Code == 200 {
			t.Fatal("missing/private session allowed")
		}
	}
	res = signedJSONRequest(t, mux, now, "alice", "POST", path+"/resolve", map[string]any{"session_id": "default", "references": []any{map[string]any{"kind": "project_source", "id": "p", "expected_digest": sel.ExpectedDigest, "text": "forged", "url": "https://example.invalid"}}})
	if res.Code == 200 {
		t.Fatal("forged content accepted")
	}
	sel.ExpectedDigest = strings.Repeat("0", 64)
	res = signedJSONRequest(t, mux, now, "alice", "POST", path+"/resolve", map[string]any{"session_id": "default", "references": []screenplay.AssistantReferenceSelector{sel}})
	if res.Code != 409 {
		t.Fatal(res.Code, res.Body.String())
	}
	if calls != 0 {
		t.Fatal("read-only reference routes called model")
	}
}

func TestReferencePromptTransmitsSnapshotAndNotHistoryAttachments(t *testing.T) {
	refs := []screenplay.AssistantReferenceSnapshot{{Kind: "chapter_script", ID: "c", Text: "SELECTED正文完整"}}
	req := screenplay.AssistantSessionRequest{History: screenplay.AssistantSessionHistory{Messages: []screenplay.AssistantMessage{{Role: "user", Content: "old question", References: []screenplay.AssistantReferenceSnapshot{{Text: "OLD_ATTACHMENT_DO_NOT_EXPAND"}}}}}}
	request, err := sessionChatPromptWithReferences(req, screenplay.Project{}, screenplay.Document{}, sessionEditView{}, "hello", refs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(request.UserPrompt, "SELECTED正文完整") || strings.Contains(request.UserPrompt, "OLD_ATTACHMENT_DO_NOT_EXPAND") {
		t.Fatal(request.UserPrompt)
	}
	raw, _ := json.Marshal([]screenplay.TextMessage{{Role: "system", Content: request.SystemPrompt}, {Role: "user", Content: request.UserPrompt}})
	if len(raw) > 96<<10 {
		t.Fatal("escaped budget exceeded")
	}
}
func TestReferencePromptRejectsEscapedTotalBeforeDispatch(t *testing.T) {
	_, err := sessionChatPromptWithReferences(screenplay.AssistantSessionRequest{}, screenplay.Project{}, screenplay.Document{}, sessionEditView{}, strings.Repeat("<", 32000), []screenplay.AssistantReferenceSnapshot{{Text: "a"}})
	if !errors.Is(err, screenplay.ErrInvalidArgument) {
		t.Fatalf("escaped request accepted: %v", err)
	}
}

func TestReferenceBudgetIncludesResolvedModelAndPayloadEnvelope(t *testing.T) {
	req := screenplay.TextRequest{Model: strings.Repeat("<", 17000), SystemPrompt: "s", UserPrompt: "q", JSONResponse: true, MaxOutputTokens: 16000, Temperature: 0.4}
	if err := checkReferenceRequestBudget(req); !errors.Is(err, screenplay.ErrInvalidArgument) {
		t.Fatal("model and envelope not counted", err)
	}
}

type captureReferenceGenerator struct {
	calls   int
	request screenplay.TextRequest
	after   func()
}

func (g *captureReferenceGenerator) Generate(context.Context, screenplay.TextRequest) (screenplay.TextResponse, error) {
	panic("generic generator bypass")
}
func (g *captureReferenceGenerator) GenerateSessionStage(_ context.Context, _ screenplay.AssistantSessionRequest, _ string, req screenplay.TextRequest) (screenplay.TextResponse, error) {
	g.calls++
	g.request = req
	if g.after != nil {
		g.after()
	}
	return screenplay.TextResponse{Content: `{"reply":"answer"}`}, nil
}

type referenceChatRepo struct {
	begins, finishes int
	history          *screenplay.AssistantSessionHistory
	afterBegin       func()
}

func (r *referenceChatRepo) CheckAssistantRequest(context.Context, screenplay.AssistantSessionRequest) error {
	return nil
}
func (r *referenceChatRepo) BeginAssistantRequest(_ context.Context, o, p, a, id string, v int64) (screenplay.AssistantSessionRequest, error) {
	r.begins++
	if r.afterBegin != nil {
		r.afterBegin()
	}
	return screenplay.AssistantSessionRequest{Owner: o, Project: p, Actor: a, SessionID: id, Revision: v + 1, Mode: "confirm", Token: "token"}, nil
}
func (r *referenceChatRepo) FinishAssistantRequest(_ context.Context, _ screenplay.AssistantSessionRequest, h *screenplay.AssistantSessionHistory) error {
	r.finishes++
	r.history = h
	return nil
}
func TestReferenceChatCapturesActualRequestRejectsBeforeDispatchAndDoesNotBypassChannel(t *testing.T) {
	for _, kind := range []string{"valid", "oversize", "stale-after-begin", "changed-after-model", "closed"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			s := screenplay.NewMemoryStore()
			text := "EXPLICIT_SELECTED"
			if kind == "oversize" {
				text = strings.Repeat("<", 4000)
			}
			p, _ := s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "o", SourceText: text})
			ar := screenplay.NewMemoryAuthoringRepository()
			svc := screenplay.AuthoringService{Store: s, Repository: ar}
			view, _ := screenplay.LoadAssistantReferenceView(ctx, s, ar, "o", p.ID)
			snap, _ := view.Describe(screenplay.AssistantReferenceSelector{Kind: "project_source", ID: p.ID})
			repo := &referenceChatRepo{}
			g := &captureReferenceGenerator{}
			change := func() { _, _ = s.SaveProjectSource(ctx, "o", p.ID, "changed") }
			if kind == "stale-after-begin" {
				repo.afterBegin = change
			}
			if kind == "changed-after-model" {
				g.after = change
			}
			var generator screenplay.TextGenerator = g
			if kind == "closed" {
				generator = nil
			}
			out, err := runSessionChat(ctx, repo, svc, generator, "o", p.ID, "o", "s", 1, "question", snap.Selector())
			if kind == "valid" {
				if err != nil || g.calls != 1 || !strings.Contains(g.request.UserPrompt, "EXPLICIT_SELECTED") || len(out.Messages[0].References) != 1 {
					t.Fatalf("%v calls=%d output=%+v", err, g.calls, out)
				}
			} else {
				if err == nil {
					t.Fatal("invalid request accepted")
				}
				if kind != "changed-after-model" && g.calls != 0 {
					t.Fatal("invalid preflight dispatched")
				}
			}
			if kind == "oversize" && repo.begins != 0 {
				t.Fatal("oversize reserved request")
			}
			if kind == "stale-after-begin" && (repo.finishes != 1 || repo.history == nil) {
				t.Fatal("unsent not safely released")
			}
		})
	}
}

func TestReferenceDefaultNoReferenceRoundsPreserveSnapshotsWhileCompactingModel(t *testing.T) {
	for _, trigger := range []string{"message-count-at-limit", "message-count", "character-count-at-limit", "character-count", "legacy-without-references"} {
		t.Run(trigger, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now()
			store := screenplay.NewMemoryStore()
			repo := screenplay.NewMemoryAuthoringRepository()
			_, err := store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice", SourceText: "ordinary source"})
			if err != nil {
				t.Fatal(err)
			}
			count := 41
			text := "ordinary message"
			if trigger == "message-count-at-limit" {
				count = 40
			}
			if trigger == "character-count-at-limit" {
				count = 20
				text = strings.Repeat("字", 1200)
			}
			if trigger == "character-count" {
				count = 20
				text = strings.Repeat("字", 1300)
			}
			messages := make([]screenplay.AssistantMessage, count)
			for i := range messages {
				messages[i] = screenplay.AssistantMessage{Role: "user", Content: text}
			}
			snapshot := screenplay.AssistantReferenceSnapshot{Kind: "project_source", ID: "p", Digest: "recorded-digest", Text: "SAVED_SNAPSHOT_NEVER_RESEND", Name: "source", Bytes: 27}
			if trigger != "legacy-without-references" {
				messages[0].References = []screenplay.AssistantReferenceSnapshot{snapshot}
			}
			if _, err = repo.Save(ctx, screenplay.AuthoringState{Owner: "alice", ProjectID: "p", Messages: messages}, 0); err != nil {
				t.Fatal(err)
			}
			calls := 0
			mux := http.NewServeMux()
			RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, store, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
				return screenplayProviderFunc(func(_ context.Context, request batchfactoryv11.TextCompletionRequest) (string, error) {
					calls++
					if strings.Contains(request.UserPrompt, "SAVED_SNAPSHOT_NEVER_RESEND") || strings.Contains(request.SystemPrompt, "SAVED_SNAPSHOT_NEVER_RESEND") {
						t.Fatal("old snapshot leaked to model")
					}
					raw := strings.Split(request.UserPrompt, "\n用户要求：")[0]
					var body struct {
						Conversation []screenplay.AssistantMessage `json:"conversation"`
					}
					if err := json.Unmarshal([]byte(raw), &body); err != nil {
						t.Fatal(err)
					}
					maxModelMessages := 14
					if trigger == "legacy-without-references" || calls == 1 && (trigger == "message-count-at-limit" || trigger == "character-count-at-limit") {
						maxModelMessages = 40
					}
					if len(body.Conversation) > maxModelMessages {
						t.Fatalf("model history unbounded: %d", len(body.Conversation))
					}
					for _, m := range body.Conversation {
						if len(m.References) > 0 {
							t.Fatal("history reference metadata sent to model")
						}
					}
					return `{"reply":"answer","actions":[]}`, nil
				}), nil
			})
			for round := 1; round <= 2; round++ {
				before, _ := repo.Load(ctx, "alice", "p")
				res := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/chat", map[string]any{"expected_revision": before.Revision, "message": "next round without references"})
				if res.Code != 200 {
					t.Fatal(res.Code, res.Body.String())
				}
				saved, err := repo.Load(ctx, "alice", "p")
				if err != nil {
					t.Fatal(err)
				}
				var response struct {
					State screenplay.AuthoringState `json:"state"`
				}
				if err = json.Unmarshal(res.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if trigger != "legacy-without-references" {
					for _, history := range [][]screenplay.AssistantMessage{saved.Messages, response.State.Messages} {
						if len(history) != count+round*2 || len(history[0].References) != 1 || !reflect.DeepEqual(history[0].References[0], snapshot) {
							t.Fatalf("snapshot/original history lost after round %d: messages=%d first=%+v", round, len(history), history[0])
						}
					}
				} else if round == 1 && (len(saved.Messages) != 15 || saved.Messages[0].Kind != screenplay.AssistantMessageKindCompacted) {
					t.Fatal("old no-reference persistence contract changed", len(saved.Messages))
				}
			}
			if calls != 2 {
				t.Fatal("unexpected model rounds", calls)
			}
		})
	}
}
