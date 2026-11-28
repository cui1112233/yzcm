package batchfactoryv11

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSafetyTextParametersReachBothProviderContractAndHTTP(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"choices":[{"message":{"content":"正文"}}]}`))
	}))
	defer server.Close()
	provider := &OpenAICompatibleProvider{Endpoint: server.URL, APIKey: "fake", Model: "chosen", Client: server.Client()}
	input := TextCompletionRequest{Temperature: 0, MaxTokens: 42, ExtraBody: map[string]any{"top_p": 0, "presence_penalty": -1, "frequency_penalty": 0, "thinking": map[string]any{"type": "disabled"}, "stop": []string{"END"}}}
	if _, err := provider.Complete(WithSingleAttempt(context.Background()), input); err != nil {
		t.Fatal(err)
	}
	if got["model"] != "chosen" || got["temperature"] != float64(0) || got["max_tokens"] != float64(42) || got["top_p"] != float64(0) || got["presence_penalty"] != float64(-1) || got["frequency_penalty"] != float64(0) || got["stream"] != nil {
		t.Fatalf("effective payload=%#v", got)
	}
	if !reflect.DeepEqual(got["thinking"], map[string]any{"type": "disabled"}) || got["response_format"] == nil {
		t.Fatalf("thinking/JSON contract lost: %#v", got)
	}
	input.DisableJSONResponse = true
	if _, err := provider.Complete(WithSingleAttempt(context.Background()), input); err != nil {
		t.Fatal(err)
	}
	if got["response_format"] != nil {
		t.Fatalf("plain text template forced JSON: %#v", got)
	}
}
func TestSafetyTextParametersRejectProtocolAndInvalidValuesBeforeHTTP(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	provider := &OpenAICompatibleProvider{Endpoint: server.URL, APIKey: "fake", Model: "chosen", Client: server.Client()}
	for _, extra := range []map[string]any{{"model": "other"}, {"messages": []any{}}, {"apiKey": "fake"}, {"authorization": "fake"}, {"response_format": map[string]any{}}, {"stream": true}, {"temperature": 1}, {"max_tokens": 12}, {"top_p": 1.1}, {"presence_penalty": -3}, {"thinking": "enabled"}} {
		_, err := provider.Complete(WithSingleAttempt(context.Background()), TextCompletionRequest{MaxTokens: 42, ExtraBody: extra})
		if !errors.Is(err, ErrConfigRequired) {
			t.Fatalf("invalid extra accepted: %#v err=%v", extra, err)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid settings caused %d paid requests", calls)
	}
}
func TestSafetyTextParametersBusinessWinsChannelDefaults(t *testing.T) {
	defaults := map[string]any{"temperature": 1.0, "max_tokens": 99, "top_p": 0.9, "enable_thinking": true}
	input := TextCompletionRequest{Temperature: 0, MaxTokens: 42, ExtraBody: map[string]any{"top_p": 0, "thinking": map[string]any{"type": "disabled"}}}
	payload := map[string]any{"temperature": input.Temperature, "max_tokens": input.MaxTokens}
	if err := ApplyTextRequestParameters(payload, input, defaults); err != nil {
		t.Fatal(err)
	}
	if payload["temperature"] != 0.0 || payload["max_tokens"] != 42 || payload["top_p"] != 0 || payload["enable_thinking"] != nil {
		t.Fatalf("defaults overrode business settings: %#v", payload)
	}
	if defaults["enable_thinking"] != true {
		t.Fatal("saved defaults mutated")
	}
}
func TestSafetyTextResponseErrorsStayDistinct(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want error
	}{
		{`{"choices":[{"finish_reason":"length","message":{"content":"半截"}}]}`, ErrTextOutputTruncated},
		{`{"choices":[{"finish_reason":"content_filter","message":{"content":""}}]}`, ErrTextRefused},
		{`{"choices":[{"message":{"content":"partial","refusal":"no"}}]}`, ErrTextRefused},
		{`{"choices":[{"message":{"content":""}}]}`, ErrTextEmpty},
		{`{"choices":[]}`, ErrTextResultUnknown},
	} {
		text, err := DecodeTextCompletionResponse([]byte(tc.raw))
		if text != "" || !errors.Is(err, tc.want) {
			t.Fatalf("raw=%s text=%q err=%v", tc.raw, text, err)
		}
	}
}

func TestSafetyLegacyChannelProtocolDefaultsDoNotBreakExistingCalls(t *testing.T) {
	defaults := map[string]any{"response_format": map[string]any{"type": "text"}, "stream": true, "model": "other"}
	payload := map[string]any{"model": "chosen", "response_format": map[string]any{"type": "json_object"}}
	if err := ApplyTextRequestParameters(payload, TextCompletionRequest{}, defaults); err != nil {
		t.Fatal(err)
	}
	if payload["model"] != "chosen" || payload["stream"] != nil || payload["response_format"].(map[string]any)["type"] != "json_object" {
		t.Fatalf("legacy overrides applied: %#v", payload)
	}
	if err := ApplyTextRequestParameters(payload, TextCompletionRequest{ExtraBody: map[string]any{}}, defaults); !errors.Is(err, ErrConfigRequired) {
		t.Fatalf("strict novel-fetch path accepted overrides: %v", err)
	}
}

type safetyDeadlineTransport struct{ remaining time.Duration }

func (p *safetyDeadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	deadline, _ := r.Context().Deadline()
	p.remaining = time.Until(deadline)
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"正文"}}]}`))}, nil
}
func TestSafetyExplicitTimeoutReachesHTTPWithoutChangingLegacyClient(t *testing.T) {
	transport := &safetyDeadlineTransport{}
	client := &http.Client{Timeout: 300 * time.Second, Transport: transport}
	provider := &OpenAICompatibleProvider{Endpoint: "http://127.0.0.1/fake", APIKey: "fake", Model: "chosen", Client: client}
	input := TextCompletionRequest{Timeout: 600 * time.Second}
	if _, err := provider.Complete(WithSingleAttempt(context.Background()), input); err != nil {
		t.Fatal(err)
	}
	if transport.remaining < 599*time.Second || transport.remaining > 601*time.Second || client.Timeout != 300*time.Second {
		t.Fatalf("timeout=%v savedclient=%v", transport.remaining, client.Timeout)
	}
	input.Timeout = 0
	if _, err := provider.Complete(WithSingleAttempt(context.Background()), input); err != nil {
		t.Fatal(err)
	}
	if transport.remaining < 299*time.Second || transport.remaining > 301*time.Second {
		t.Fatalf("legacy timeout changed: %v", transport.remaining)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	input.Timeout = 600 * time.Second
	if _, err := provider.Complete(WithSingleAttempt(ctx), input); err != nil {
		t.Fatal(err)
	}
	if transport.remaining > 20*time.Second {
		t.Fatalf("parent deadline ignored: %v", transport.remaining)
	}
}
