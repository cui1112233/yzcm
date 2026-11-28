package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
	"strings"
	"testing"
	"time"
)

func TestSafetyLengthFinishRequiresManualRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"finish_reason":"length","message":{"content":"","reasoning_content":"fixture thinking"}}]}`))
	}))
	defer server.Close()
	provider := &integrationTextProvider{Label: "fixture", Endpoint: server.URL, Model: "fixture", APIKey: "fake-key", Client: server.Client()}
	_, err := provider.Complete(context.Background(), batchfactoryv11.TextCompletionRequest{UserPrompt: "fixture", MaxTokens: 1200, DisableJSONResponse: true})
	if err == nil || !strings.Contains(err.Error(), "length") {
		t.Fatal("observation changed", err)
	}
	state := scheduleNovelFetchTaskRetry(map[string]any{}, novelFetchRetryStageRewrite, err, 0, time.Now())
	if !novelFetchRetryStateIsManual(state) || firstText(state["next_retry_at"]) != "" {
		t.Fatal("retry observation changed", state)
	}
	t.Log("length response preserved; automatic retry blocked")
}

func TestSafetyCustomTextChannelPreservesParams(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = nil
		json.NewDecoder(r.Body).Decode(&received)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"fixture"}}]}`))
	}))
	defer server.Close()
	config := newIntegrationTestUserConfig(t, map[string]any{"customChannels": []any{map[string]any{"id": "fixture", "name": "fixture", "group": "text", "enabled": true, "apiKey": "fake-key", "baseUrl": server.URL + "/v1", "model": "fixture", "params": map[string]any{"top_p": 0.1, "thinking": map[string]any{"type": "disabled"}}}}})
	p, matched, err := ResolveIntegrationTextProvider(context.Background(), config, "tester", "custom:fixture")
	if err != nil || !matched {
		t.Fatal(err)
	}
	p.(*integrationTextProvider).Client = server.Client()
	if _, err = p.Complete(context.Background(), batchfactoryv11.TextCompletionRequest{UserPrompt: "fixture", MaxTokens: 32, DisableJSONResponse: true, ExtraBody: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	if received["thinking"] == nil || received["top_p"] != 0.1 {
		t.Fatal("observation changed")
	}

	if _, err = p.Complete(context.Background(), batchfactoryv11.TextCompletionRequest{Temperature: 0, MaxTokens: 31, DisableJSONResponse: true, ExtraBody: map[string]any{"top_p": 0, "thinking": map[string]any{"type": "enabled"}}}); err != nil {
		t.Fatal(err)
	}
	if received["top_p"] != float64(0) || received["temperature"] != float64(0) || received["max_tokens"] != float64(31) || received["thinking"].(map[string]any)["type"] != "enabled" || received["response_format"] != nil {
		t.Fatalf("business parameters lost: %#v", received)
	}
	t.Log("saved channel thinking/top_p reached local HTTP payload")
}

func TestSafetyAIConcurrencyCapsFetchConcurrency(t *testing.T) {
	ctx := context.Background()
	s := novelfetchworkshop.NewMemoryStore()
	if err := s.PutConfig(ctx, "tester", map[string]any{"appConfig": map[string]any{"fetch": map[string]any{"concurrency": 8}, "ai": map[string]any{"max_concurrency": 2}}}); err != nil {
		t.Fatal(err)
	}
	if got := novelFetchConfiguredConcurrency(ctx, s, "tester", 12); got != 2 {
		t.Fatalf("AI max_concurrency ignored: got %d", got)
	}
}
func TestSafetyAIStageRetryConfiguration(t *testing.T) {
	for retries := 0; retries <= 2; retries++ {
		ctx := context.Background()
		s := novelfetchworkshop.NewMemoryStore()
		s.PutConfig(ctx, "tester", map[string]any{"appConfig": map[string]any{"ai": map[string]any{"retry_times": retries}}})
		s.PutDocument(ctx, "tester", novelfetchworkshop.Document{BookID: "book"})
		for attempt := 1; attempt <= retries+1; attempt++ {
			if err := saveWorkshopStageFailure(ctx, s, "tester", "book", workshopStageSaveFailure{Stage: novelFetchRetryStageRewrite, Message: "HTTP 429 rate limit", FailureID: fmt.Sprint(attempt)}); err != nil {
				t.Fatal(err)
			}
			doc, _ := s.GetDocument(ctx, "tester", "book")
			state := novelFetchRetrySchedule(doc.Meta)[novelFetchRetryStageRewrite]
			wantManual := attempt == retries+1
			if novelFetchRetryStateIsManual(state) != wantManual {
				t.Fatalf("retry_times=%d failure=%d state=%v", retries, attempt, state)
			}
		}
	}
	for _, failure := range []error{batchfactoryv11.ErrTextRefused, batchfactoryv11.ErrTextResultUnknown, context.DeadlineExceeded, errors.New("文本模型输出被长度截断")} {
		state := scheduleNovelFetchTaskRetry(map[string]any{}, novelFetchRetryStageRewrite, failure, 0, time.Now())
		if !novelFetchRetryStateIsManual(state) {
			t.Fatalf("unsafe automatic retry for %v: %v", failure, state)
		}
	}
}

func TestSafetyAIRetryBudgetDoesNotChangeUploadOrOriginalFetch(t *testing.T) {
	for _, stage := range []string{novelFetchRetryStageUpload, novelFetchRetryStageOriginalFetch} {
		state := scheduleNovelFetchTaskRetry(map[string]any{"ai_retry_max_failures": 1}, stage, errors.New("HTTP 429 rate limit"), 0, time.Now())
		if novelFetchRetryStateIsManual(state) || firstText(state["next_retry_at"]) == "" {
			t.Fatalf("AI retry setting changed %s policy: %v", stage, state)
		}
	}
}
