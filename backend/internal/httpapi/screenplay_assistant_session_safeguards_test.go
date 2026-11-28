package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"qiantie/backend/internal/screenplay"
	"strings"
	"testing"
	"unicode/utf8"
)

type ordinarySessionGenerator struct{ calls int }

func (g *ordinarySessionGenerator) Generate(context.Context, screenplay.TextRequest) (screenplay.TextResponse, error) {
	g.calls++
	return screenplay.TextResponse{}, nil
}

func TestSessionPaidChatGuardPrecedesReservationAndDispatch(t *testing.T) {
	g := &ordinarySessionGenerator{}
	for _, message := range []string{"hello", `{"allow_paid":true,"receipt_enabled":true}`} {
		out, err := runSessionChat(context.Background(), nil, screenplay.AuthoringService{}, g, "owner", "p", "actor", "s", 1, message)
		if !errors.Is(err, errSessionReceiptUnavailable) || g.calls != 0 || out.Revision != 0 {
			t.Fatal(out, err, g.calls)
		}
	}
}

func TestSessionContextBoundsUTF8AndKeepsServerEvidence(t *testing.T) {
	text := strings.Repeat("中🙂<&", 16000)
	targets := sessionEditView{boards: []screenplay.Storyboard{{ID: "board", Shots: make([]screenplay.StoryboardShot, 401)}}}
	for i := range targets.boards[0].Shots {
		targets.boards[0].Shots[i].VideoDesc = text
	}
	targets.boards[0].Shots[400].ID = "initial-target"
	req := screenplay.AssistantSessionRequest{Mode: "confirm", Chapters: []screenplay.Chapter{{ID: "c", ScriptText: text}}}
	prompt, err := sessionChatPrompt(req, screenplay.Project{SourceText: text}, screenplay.Document{Content: text}, targets, "读一下")
	if err != nil {
		t.Fatal(err)
	}
	data, _, ok := strings.Cut(prompt.UserPrompt, "\n用户要求：")
	var directory contextDirectory
	if !ok || json.Unmarshal([]byte(data), &directory) != nil || !directory.Truncated || directory.Status != "partial" || directory.Total["shot"] != 401 || directory.Included["shot"] > 200 || len(prompt.UserPrompt)+len(prompt.SystemPrompt) > sessionPromptBytes || !utf8.ValidString(data) || strings.Contains(data, "�") {
		t.Fatal("invalid bounded catalog")
	}
	if targets.boards[0].Shots[400].ID != "initial-target" || targets.boards[0].Shots[400].VideoDesc != text || req.Chapters[0].ScriptText != text {
		t.Fatal("model projection damaged server snapshot")
	}
}

func TestSessionApplyInvalidatesOnceAfterCommit(t *testing.T) {
	for action, kind := range map[string]string{"asset": "asset", "document": "document", "chapter": "chapter", "creative_shot_structure": "storyboard", "creative_shot": ""} {
		for _, fail := range []bool{false, true} {
			t.Run(action, func(t *testing.T) {
				writes, calls := 0, 0
				commit := func(context.Context, string, string, string, string, string, int64) (screenplay.AssistantSessionHistory, error) {
					if writes > 0 {
						return screenplay.AssistantSessionHistory{}, screenplay.ErrRevisionConflict
					}
					writes++
					return screenplay.AssistantSessionHistory{Proposals: []screenplay.AssistantProposal{{ID: "q", Status: "applied", Actions: []screenplay.AuthoringAction{{Kind: action}}}}}, nil
				}
				invalidate := func(_ context.Context, o, p, k string) error {
					calls++
					if writes != 1 || o != "owner" || p != "p" || k != kind {
						t.Fatalf("invalid call %s %s %s", o, p, k)
					}
					if fail {
						return errors.New("unavailable")
					}
					return nil
				}
				out, err := applySessionAndInvalidate(context.Background(), commit, invalidate, "owner", "p", "a", "s", "q", 3)
				if err != nil || out.Revision != 4 || out.Proposals[0].Status != "applied" || (out.Warning != "") != (fail && kind != "") {
					t.Fatal(out, err)
				}
				if _, err = applySessionAndInvalidate(context.Background(), commit, invalidate, "owner", "p", "a", "s", "q", 3); !errors.Is(err, screenplay.ErrRevisionConflict) {
					t.Fatal(err)
				}
				if writes != 1 || kind != "" && calls != 1 || kind == "" && calls != 0 {
					t.Fatal(writes, calls)
				}
			})
		}
	}
}

func TestSessionHTTPClientCannotEnableUntrackedGenerator(t *testing.T) {
	g := &ordinarySessionGenerator{}
	decode := func(w http.ResponseWriter, r *http.Request, dst any) bool {
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(dst) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return false
		}
		return true
	}
	handler := sessionChatHandler(nil, screenplay.AuthoringService{}, decode, func(*http.Request, string) screenplay.TextGenerator { return g })
	for body, status := range map[string]int{`{"message":"hello","expected_revision":1}`: 503, `{"message":"hello","receipt_enabled":true}`: 400} {
		rec := httptest.NewRecorder()
		handler(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), "owner", "actor")
		if rec.Code != status || g.calls != 0 {
			t.Fatal(rec.Code, g.calls)
		}
	}
}
