package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
	"qiantie/backend/internal/yzcmusage"
	"strings"
	"testing"
)

func TestReferenceResolvedProviderBudgetBeforeAttemptAndSameProviderSend(t *testing.T) {
	ctx := context.WithValue(context.Background(), bridgeIdentityKey{}, BridgeIdentity{Username: "member"})
	accounts := &applicationTestStore{accounts: map[string]auth.Account{"member": {Username: "member", Active: true}, "manager": {Username: "manager", Active: true}}}
	reservations := &failedSessionReservation{t: t}
	lookups := 0
	channel := NewScreenplaySessionChannel(accounts, newModelOwnerTestCenter(), func(context.Context, string, string) (*batchfactoryv11.OpenAICompatibleProvider, error) {
		lookups++
		return &batchfactoryv11.OpenAICompatibleProvider{Endpoint: yzcmusage.SessionChatEndpoint, APIKey: "fixture", Model: strings.Repeat("<", 17000)}, nil
	}, &yzcmusage.Tracker{})
	g := sessionChannelGenerator{channel: channel, repo: reservations, subject: "member", verify: func(context.Context) error { return nil }}
	_, err := g.GenerateSessionStage(ctx, screenplay.AssistantSessionRequest{Actor: "member"}, "initial", screenplay.TextRequest{Model: "selection", UserPrompt: "q", RequestByteLimit: sessionPromptBytes})
	if !errors.Is(err, screenplay.ErrInvalidArgument) || !errors.Is(err, yzcmusage.ErrSessionNotSent) || reservations.calls != 0 || lookups != 1 {
		t.Fatalf("budget reached attempt: %v %d/%d", err, reservations.calls, lookups)
	}
	sent := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "resolved-model" {
			t.Error(body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}]}`))
	}))
	defer server.Close()
	lookups = 0
	resolver := func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		lookups++
		return &batchfactoryv11.OpenAICompatibleProvider{Endpoint: server.URL, Model: "resolved-model", APIKey: "fixture", Client: server.Client()}, nil
	}
	_, err = generateReferenceText(context.Background(), "owner", resolver, screenplay.TextRequest{Model: "selection", UserPrompt: "q", JSONResponse: true, MaxOutputTokens: 16000, SingleAttempt: true, RequestByteLimit: sessionPromptBytes})
	if err != nil || lookups != 1 || sent != 1 {
		t.Fatal(err, lookups, sent)
	}
}
