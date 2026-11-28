package httpapi

import (
	"context"
	"database/sql"
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

func TestAssistantAttachmentFrozenDefaultMaterials(t *testing.T) {
	sent := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent++
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		if payload["model"] != "actual-model" || r.Header.Get("Authorization") != "Bearer frozen-key" || r.URL.Path != "/v1/chat/completions" {
			t.Fatal("sent materials differ from budget snapshot")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"answer"}}]}`))
	}))
	defer server.Close()
	raw := &batchfactoryv11.OpenAICompatibleProvider{Endpoint: server.URL + "/v1/chat/completions", APIKey: "frozen-key", Model: "actual-model", Client: server.Client()}
	lookups := 0
	resolver := freezeAssistantAttachmentResolver(func(_ context.Context, owner, selection string) (batchfactoryv11.DirectorProvider, error) {
		lookups++
		if owner != "owner" || selection != "selection-key" {
			t.Fatal("selection key changed")
		}
		return raw, nil
	})
	request := screenplay.TextRequest{Model: "selection-key", UserPrompt: "q", SingleAttempt: true, RequestByteLimit: sessionPromptBytes}
	if _, e := generateReferenceText(context.Background(), "owner", resolver, request); e != nil || sent != 1 || lookups != 1 {
		t.Fatal(e, sent, lookups)
	}
	raw.Model = "changed"
	if _, e := generateReferenceText(context.Background(), "owner", resolver, request); !errors.Is(e, screenplay.ErrAccessDenied) || sent != 1 {
		t.Fatal("adopted changed catalog", e, sent)
	}
}

type attachmentMaterialReservation struct{ calls int }

func (r *attachmentMaterialReservation) ReserveSessionAttempt(context.Context, screenplay.AssistantSessionRequest, string, string, string, string) (string, error) {
	r.calls++
	return "", errors.New("controlled prewrite failure")
}
func (r *attachmentMaterialReservation) ClaimSessionAttempt(context.Context, screenplay.AssistantSessionRequest, string, string, string, string) error {
	return errors.New("unexpected claim")
}
func TestAssistantAttachmentFrozenSessionBudgetAndChannel(t *testing.T) {
	ctx := context.WithValue(context.Background(), bridgeIdentityKey{}, BridgeIdentity{Username: "member"})
	accounts := &applicationTestStore{accounts: map[string]auth.Account{"member": {Username: "member", Active: true}, "manager": {Username: "manager", Active: true}}}
	raw := &batchfactoryv11.OpenAICompatibleProvider{Endpoint: yzcmusage.SessionChatEndpoint, APIKey: "frozen-key", Model: strings.Repeat("<", 17000)}
	lookups := 0
	channel := NewScreenplaySessionChannel(accounts, newModelOwnerTestCenter(), func(_ context.Context, owner, selection string) (*batchfactoryv11.OpenAICompatibleProvider, error) {
		lookups++
		if owner != "manager" || selection != "selection-key" {
			t.Fatal("selection key or owner changed")
		}
		return raw, nil
	}, &yzcmusage.Tracker{})
	reservations := &attachmentMaterialReservation{}
	g, e := freezeAssistantAttachmentSessionGenerator(sessionChannelGenerator{channel: channel, repo: reservations, subject: "member", verify: func(context.Context) error { return nil }, commitGuard: func(context.Context, *sql.Tx) error { return nil }})
	if e != nil {
		t.Fatal(e)
	}
	request := screenplay.TextRequest{Model: "selection-key", UserPrompt: "q", RequestByteLimit: sessionPromptBytes}
	if e = checkAssistantAttachmentSessionBudget(ctx, g, request); !errors.Is(e, screenplay.ErrInvalidArgument) || request.Model != "selection-key" || reservations.calls != 0 {
		t.Fatal(e, request.Model, reservations.calls)
	}
	p1, e := g.provider(ctx, "selection-key")
	if e != nil {
		t.Fatal(e)
	}
	p2, e := g.provider(ctx, "selection-key")
	if e != nil || p1 == p2 || p1.Model != p2.Model || p1.APIKey != p2.APIKey || p1.Endpoint != p2.Endpoint {
		t.Fatal("material freeze or Go pointer claim", e)
	}
	raw.APIKey = "rotated"
	if _, e = g.provider(ctx, "selection-key"); e == nil {
		t.Fatal("configuration change adopted", e)
	}
	if reservations.calls != 0 || lookups < 3 {
		t.Fatal("unexpected paid activity")
	}
	if _, e = freezeAssistantAttachmentSessionGenerator(sessionChannelGenerator{}); !errors.Is(e, errSessionReceiptUnavailable) {
		t.Fatal("closed channel enabled", e)
	}
}
func TestAssistantAttachmentModelHistoryProjection(t *testing.T) {
	messages := []screenplay.AssistantMessage{{Role: "user", Content: "old", ReferenceOperationID: "SERVER_HISTORY_OP", References: []screenplay.AssistantReferenceSnapshot{{Text: "PRIVATE_OLD_SNAPSHOT"}}}}
	model := assistantReferenceModelHistory(messages)
	raw, _ := json.Marshal(model)
	if strings.Contains(string(raw), "SERVER_HISTORY_OP") || strings.Contains(string(raw), "PRIVATE_OLD_SNAPSHOT") || messages[0].ReferenceOperationID == "" || len(messages[0].References) == 0 {
		t.Fatal("projection changed saved history or leaked metadata")
	}
}
