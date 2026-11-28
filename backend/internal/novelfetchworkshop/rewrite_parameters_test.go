package novelfetchworkshop

import (
	"context"
	"errors"
	"qiantie/backend/internal/batchfactoryv11"
	"reflect"
	"testing"
	"time"
)

func TestSafetyRewriteSettingsReachProvider(t *testing.T) {
	ctx := context.Background()
	store := plainRewriteTestStore("tester")
	settings, _ := store.GetConfig(ctx, "tester")
	app := mapOrEmpty(settings["appConfig"])
	app["ai"] = map[string]any{"timeout_seconds": 23, "max_tokens": 777, "top_p": 0, "presence_penalty": -1, "frequency_penalty": 0, "temperature": 1.9, "disable_thinking": true, "stream": true, "json_mode": true, "extra_body_json": `{"stop":["END"]}`}
	app["rewrite"] = map[string]any{"temperature": 0}
	settings["appConfig"] = app
	store.PutConfig(ctx, "tester", settings)
	store.PutDocument(ctx, "tester", Document{BookID: "book", Original: "原文"})
	calls := 0
	_, err := GenerateAIRewrites(ctx, store, "tester", "book", 1, rewriteProviderFunc(func(_ context.Context, req RewriteRequest) (string, error) {
		calls++
		if req.Timeout != 23*time.Second || req.MaxTokens != 777 || req.Temperature != 0 || req.ExtraBody["top_p"] != float64(0) || req.JSONResponse {
			t.Fatalf("wrong effective request: %#v", req)
		}
		if !reflect.DeepEqual(req.ExtraBody["thinking"], map[string]any{"type": "disabled"}) || req.ExtraBody["stream"] != nil {
			t.Fatalf("thinking/legacy stream wrong: %#v", req.ExtraBody)
		}
		return "改写后的小说正文", nil
	}))
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}
func TestSafetyRewriteSettingsRejectInvalidConfigurationWithoutCalling(t *testing.T) {
	for _, ai := range []map[string]any{{"enable_thinking": true, "disable_thinking": true}, {"extra_body_json": "[]"}, {"extra_body_json": "null"}, {"extra_body_json": `{"stream":true}`}, {"extra_body_json": `{"model":"other"}`}, {"top_p": -0.1}, {"enable_thinking": true, "extra_body_json": `{"thinking":{"type":"disabled"}}`}} {
		ctx := context.Background()
		s := plainRewriteTestStore("tester")
		s.PutConfig(ctx, "tester", map[string]any{"appConfig": map[string]any{"ai": ai}})
		s.PutDocument(ctx, "tester", Document{BookID: "book", Original: "原文"})
		calls := 0
		_, err := GenerateAIRewrites(ctx, s, "tester", "book", 1, rewriteProviderFunc(func(context.Context, RewriteRequest) (string, error) { calls++; return "正文", nil }))
		if !errors.Is(err, batchfactoryv11.ErrConfigRequired) || calls != 0 {
			t.Fatalf("bad ai=%#v calls=%d err=%v", ai, calls, err)
		}
	}
}
func TestSafetyRewriteRefusalAndTruncationNeverAutomaticallyResend(t *testing.T) {
	for _, failure := range []error{batchfactoryv11.ErrTextRefused, batchfactoryv11.ErrTextOutputTruncated, batchfactoryv11.ErrTextResultUnknown} {
		calls := 0
		_, err := completeValidatedRewrite(context.Background(), rewriteProviderFunc(func(context.Context, RewriteRequest) (string, error) { calls++; return "", failure }), RewriteRequest{MaxTokens: 1200}, "book", "plain_text", 1, 40, "")
		if !errors.Is(err, failure) || calls != 1 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	}
	calls := 0
	_, err := completeValidatedRewrite(batchfactoryv11.WithSingleAttempt(context.Background()), rewriteProviderFunc(func(context.Context, RewriteRequest) (string, error) {
		calls++
		return "抱歉，我无法协助这个请求", nil
	}), RewriteRequest{MaxTokens: 1200}, "book", "plain_text", 1, 40, "")
	if calls != 1 || !errors.Is(err, batchfactoryv11.ErrTextRefused) {
		t.Fatalf("prose refusal: calls=%d err=%v", calls, err)
	}
}

func TestSafetyAIParameterTimeoutBoundsAndTemperatureCompatibility(t *testing.T) {
	for _, value := range []any{0, -1, 901, 1.5, "bad"} {
		if _, _, err := AIRequestParameters(map[string]any{"ai": map[string]any{"timeout_seconds": value}}); !errors.Is(err, batchfactoryv11.ErrConfigRequired) {
			t.Fatalf("invalid timeout %v accepted: %v", value, err)
		}
	}
	timeout, _, err := AIRequestParameters(map[string]any{})
	if err != nil || timeout != 180*time.Second {
		t.Fatalf("default timeout=%v err=%v", timeout, err)
	}
	settings := map[string]any{"ai": map[string]any{"temperature": 0.7}}
	if got := rewritePlan(settings, "ai1", 1).Temperature; got != 0.7 {
		t.Fatalf("legacy ai.temperature lost: %v", got)
	}
	settings["rewrite"] = map[string]any{"temperature": 0}
	if got := rewritePlan(settings, "ai1", 1).Temperature; got != 0 {
		t.Fatalf("rewrite.temperature did not win: %v", got)
	}
}
