package httpapi

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/screenplay"
)

func TestSessionChatPromptWhitelistsSharedText(t *testing.T) {
	req := screenplay.AssistantSessionRequest{Token: "secret-request", Actor: "secret-actor", Chapters: []screenplay.Chapter{{Title: "chapter title", RawResult: "secret-raw"}}, History: screenplay.AssistantSessionHistory{Messages: []screenplay.AssistantMessage{{Role: "user", Content: "selected private message"}}, Proposals: []screenplay.AssistantProposal{{Summary: "selected proposal", Actions: []screenplay.AuthoringAction{{Content: "secret-proposal-action"}}}}}}
	prompt, err := sessionChatPrompt(req, screenplay.Project{Title: "project title", PromptID: "secret-config"}, screenplay.Document{Content: "shared document", RawResult: "secret-provider"}, sessionEditView{}, "new message")
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"chapter title", "selected private message", "selected proposal", "shared document", "new message", "project title"} {
		if !strings.Contains(prompt.UserPrompt, needle) {
			t.Fatalf("missing %q", needle)
		}
	}
	if strings.Contains(prompt.UserPrompt, "secret-") || !prompt.SingleAttempt {
		t.Fatal("unexpected private field or repeated provider attempts")
	}
}

type delayedChatGenerator struct {
	started chan struct{}
	release chan struct{}
}

func (g delayedChatGenerator) Generate(ctx context.Context, _ screenplay.TextRequest) (screenplay.TextResponse, error) {
	close(g.started)
	select {
	case <-g.release:
		return screenplay.TextResponse{Content: `{"reply":"late answer"}`}, nil
	case <-ctx.Done():
		return screenplay.TextResponse{}, ctx.Err()
	}
}

type lateSessionRequests struct{ beginCalls, finishCalls int }

func (r *lateSessionRequests) BeginAssistantRequest(_ context.Context, o, p, a, id string, v int64) (screenplay.AssistantSessionRequest, error) {
	r.beginCalls++
	return screenplay.AssistantSessionRequest{Owner: o, Project: p, Actor: a, SessionID: id, Token: "initial token", Mode: "confirm", Revision: v + 1}, nil
}
func (r *lateSessionRequests) FinishAssistantRequest(_ context.Context, req screenplay.AssistantSessionRequest, h *screenplay.AssistantSessionHistory) error {
	r.finishCalls++
	if req.Token != "initial token" || req.SessionID != "first" || h == nil || len(h.Messages) != 2 {
		return errors.New("wrong session or changes")
	}
	return screenplay.ErrRevisionConflict
}

func TestSessionChatLateAnswerDoesNotReturnNewHistory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	store := screenplay.NewMemoryStore()
	project, err := store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "owner", Title: "chat", ScriptMode: "b"})
	if err != nil {
		t.Fatal(err)
	}
	svc := screenplay.AuthoringService{Store: store, Repository: screenplay.NewMemoryAuthoringRepository()}
	r := &lateSessionRequests{}
	g := delayedChatGenerator{make(chan struct{}), make(chan struct{})}
	type answer struct {
		result sessionChatResult
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		out, err := runSessionChat(ctx, r, svc, g, "owner", project.ID, "owner", "first", 4, "hello")
		done <- answer{out, err}
	}()
	select {
	case <-g.started:
	case a := <-done:
		t.Fatalf("model never called: %v", a.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	close(g.release)
	select {
	case a := <-done:
		if !errors.Is(a.err, screenplay.ErrRevisionConflict) || len(a.result.Messages) != 0 || r.beginCalls != 1 || r.finishCalls != 1 {
			t.Fatalf("late answer: %+v, %v, calls %d/%d", a.result, a.err, r.beginCalls, r.finishCalls)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

type gatedEditGenerator struct {
	started chan struct{}
	release chan struct{}
	reply   string
}

func (g gatedEditGenerator) Generate(ctx context.Context, req screenplay.TextRequest) (screenplay.TextResponse, error) {
	close(g.started)
	select {
	case <-g.release:
		return screenplay.TextResponse{Content: g.reply}, nil
	case <-ctx.Done():
		return screenplay.TextResponse{}, ctx.Err()
	}
}

type acceptSessionRequest struct{ lateSessionRequests }

func (r *acceptSessionRequest) FinishAssistantRequest(context.Context, screenplay.AssistantSessionRequest, *screenplay.AssistantSessionHistory) error {
	return nil
}

func TestSessionChatOnlySelectedEditTargetMustStayCurrent(t *testing.T) {
	for _, test := range []struct {
		name                  string
		board, changeSelected bool
	}{
		{"selected asset", false, true}, {"other asset", false, false}, {"selected board", true, true}, {"other board", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s := screenplay.NewMemoryStore()
			_, err := s.CreateProject(ctx, screenplay.Project{ID: "project", OwnerUsername: "owner"})
			if err != nil {
				t.Fatal(err)
			}
			assets := make([]screenplay.Asset, 2)
			boards := make([]screenplay.Storyboard, 2)
			for i, id := range []string{"one", "two"} {
				assets[i], err = s.CreateAsset(ctx, screenplay.Asset{ID: id, Type: screenplay.AssetCharacter, OwnerUsername: "owner", ProjectID: "project", Name: id})
				if err != nil {
					t.Fatal(err)
				}
				boards[i], err = s.SaveStoryboard(ctx, screenplay.Storyboard{ID: id, OwnerUsername: "owner", ProjectID: "project", EpisodeNo: i + 1, Engine: "seedance", Shots: []screenplay.StoryboardShot{{ID: id, Seq: 1, Duration: 5, VideoDesc: "old", ProjectID: "project", OwnerUsername: "owner"}}})
				if err != nil {
					t.Fatal(err)
				}
			}
			g := gatedEditGenerator{make(chan struct{}), make(chan struct{}), `{"reply":"已经修改成功","question":"已经保存","tool":"modify_character","tool_args":{"character_name":"one","description":"patched"}}`}
			if test.board {
				g.reply = `{"reply":"已经修改成功","question":"已经保存","tool":"modify_video_prompt","tool_args":{"episode_no":1,"engine":"seedance","shot_id":"one","new_prompt":"new"}}`
			}
			done := make(chan error, 1)
			svc := screenplay.AuthoringService{Store: s, Repository: screenplay.NewMemoryAuthoringRepository()}
			go func() {
				out, err := runSessionChat(ctx, &acceptSessionRequest{}, svc, g, "owner", "project", "owner", "first", 1, "edit")
				if err == nil && (len(out.Proposals) != 1 || out.Proposals[0].Status != "pending" || len(out.Messages) != 2 || !strings.Contains(out.Messages[1].Content, "请核对后确认") || strings.Contains(out.Messages[1].Content, "修改成功")) {
					err = errors.New("pending action displayed a successful write")
				}
				done <- err
			}()
			select {
			case <-g.started:
			case err := <-done:
				t.Fatalf("model never called: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			i := 1
			if test.changeSelected {
				i = 0
			}
			if test.board {
				b := boards[i]
				b.Shots = append([]screenplay.StoryboardShot(nil), b.Shots...)
				b.Shots[0].VideoDesc = "changed"
				_, err = s.SaveStoryboard(ctx, b)
			} else {
				a := assets[i]
				a.Description = "changed"
				_, err = s.UpdateAsset(ctx, "owner", "project", a.ID, a, a.Revision)
			}
			close(g.release)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if test.changeSelected && !errors.Is(err, screenplay.ErrRevisionConflict) || !test.changeSelected && err != nil {
					t.Fatalf("selected %v: %v", test.changeSelected, err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		})
	}
}

func (r *lateSessionRequests) CheckAssistantRequest(context.Context, screenplay.AssistantSessionRequest) error {
	return nil
}

func (g delayedChatGenerator) GenerateSessionStage(ctx context.Context, _ screenplay.AssistantSessionRequest, _ string, req screenplay.TextRequest) (screenplay.TextResponse, error) {
	return g.Generate(ctx, req)
}
func (g gatedEditGenerator) GenerateSessionStage(ctx context.Context, _ screenplay.AssistantSessionRequest, _ string, req screenplay.TextRequest) (screenplay.TextResponse, error) {
	return g.Generate(ctx, req)
}
