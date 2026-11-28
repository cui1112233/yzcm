package app

import (
	"context"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
	"testing"
	"time"
)

type safetyDeadlineProvider struct {
	remaining time.Duration
	request   batchfactoryv11.TextCompletionRequest
	calls     int
	content   string
}

func (p *safetyDeadlineProvider) Complete(ctx context.Context, request batchfactoryv11.TextCompletionRequest) (string, error) {
	p.request = request
	p.calls++
	deadline, ok := ctx.Deadline()
	if ok {
		p.remaining = time.Until(deadline)
	}
	if p.content != "" {
		return p.content, nil
	}
	return "fixture", nil
}

func TestSafetyRewriteUsesIndependentDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	provider := &safetyDeadlineProvider{}
	_, err := (novelFetchRewriteAdapter{provider: provider}).Complete(ctx, novelfetchworkshop.RewriteRequest{UserPrompt: "fixture", MaxTokens: 8192})
	if err != nil || provider.remaining < 179*time.Second || provider.remaining > 181*time.Second {
		t.Fatal("observation changed", provider.remaining, err)
	}
	t.Logf("OBSERVED parent deadline=180s; actual rewrite provider deadline=%.0fs", provider.remaining.Seconds())
}

func TestSafetyClassificationAndExplicitRewriteDeadlines(t *testing.T) {
	provider := &safetyDeadlineProvider{}
	judge := textGenderJudge(provider)
	if _, err := judge(context.Background(), novelfetchworkshop.BookInfo{}); err != nil {
		t.Fatal(err)
	}
	if provider.remaining < 59*time.Second || provider.remaining > 61*time.Second {
		t.Fatalf("classification remaining=%v", provider.remaining)
	}
	_, err := (novelFetchRewriteAdapter{provider: provider}).Complete(context.Background(), novelfetchworkshop.RewriteRequest{Timeout: 240 * time.Second})
	if err != nil || provider.remaining < 239*time.Second || provider.remaining > 241*time.Second {
		t.Fatalf("configured rewrite remaining=%v err=%v", provider.remaining, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err = (novelFetchRewriteAdapter{provider: provider}).Complete(ctx, novelfetchworkshop.RewriteRequest{Timeout: 240 * time.Second})
	if err != nil || provider.remaining > 20*time.Second {
		t.Fatalf("parent deadline ignored: %v err=%v", provider.remaining, err)
	}
}

func TestSafetyClassifiersReceiveAccountExtraParameters(t *testing.T) {
	_, extra, err := novelfetchworkshop.AIRequestParameters(map[string]any{"ai": map[string]any{"top_p": 0, "disable_thinking": true, "presence_penalty": -1}})
	if err != nil {
		t.Fatal(err)
	}
	provider := &safetyDeadlineProvider{content: `{"gender":"男频"}`}
	if _, err = textGenderJudge(provider, extra)(context.Background(), novelfetchworkshop.BookInfo{Text: "分类测试"}); err != nil {
		t.Fatal(err)
	}
	if provider.calls != 1 || provider.request.ExtraBody["top_p"] != 0 || provider.request.Temperature != 0 || provider.request.MaxTokens != 256 || provider.request.DisableJSONResponse {
		t.Fatalf("gender request=%#v calls=%d", provider.request, provider.calls)
	}
	provider.calls = 0
	provider.content = `{"style":"现代甜文"}`
	decision := novelfetchworkshop.ResolveStyle(context.Background(), novelfetchworkshop.BookInfo{Text: "分类测试"}, novelFetchRewriteAdapter{provider: provider, extraBody: extra})
	if decision.Style != "现代甜文" || provider.calls != 1 || provider.request.ExtraBody["top_p"] != 0 || provider.request.MaxTokens != 80 || provider.request.DisableJSONResponse || provider.remaining < 59*time.Second || provider.remaining > 61*time.Second {
		t.Fatalf("style request=%#v calls=%d deadline=%v result=%v", provider.request, provider.calls, provider.remaining, decision)
	}
	if provider.request.ExtraBody["thinking"].(map[string]any)["type"] != "disabled" {
		t.Fatal("classification thinking dropped")
	}
}
