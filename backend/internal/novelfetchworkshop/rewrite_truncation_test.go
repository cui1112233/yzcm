package novelfetchworkshop

import (
	"context"
	"errors"
	"qiantie/backend/internal/batchfactoryv11"
	"strings"
	"testing"
)

func TestRewriteTruncationDoesNotEscalateBudgetOrResend(t *testing.T) {
	budgets := []int{}
	p := rewriteProviderFunc(func(_ context.Context, req RewriteRequest) (string, error) {
		budgets = append(budgets, req.MaxTokens)
		if req.MaxTokens < 12000 {
			return "", errors.New("文本模型输出被长度截断，请重试或增大输出上限")
		}
		return "她推开书店的门，朋友抬起头，两人终于把当年的误会说清。", nil
	})
	text, err := completeValidatedRewrite(context.Background(), p, RewriteRequest{MaxTokens: 1200}, "book", "plain_text", 1, 40, "")
	if !batchfactoryv11.IsModelOutputTruncated(err) || text != "" || len(budgets) != 1 || budgets[0] != 1200 {
		t.Fatalf("truncation triggered an unapproved resend: text=%q budgets=%v err=%v", text, budgets, err)
	}
}

func TestRewriteTruncationStopsAtBudgetAndRequiresManualRetry(t *testing.T) {
	for _, initial := range []int{1200, 24000} {
		calls := 0
		p := rewriteProviderFunc(func(_ context.Context, req RewriteRequest) (string, error) {
			calls++
			if req.MaxTokens > 24000 {
				t.Fatalf("unbounded output budget: %d", req.MaxTokens)
			}
			return "", errors.New("director provider output truncated (finish_reason=length)")
		})
		_, err := completeValidatedRewrite(context.Background(), p, RewriteRequest{MaxTokens: initial}, "book", "plain_text", 1, 40, "")
		want := 1
		if !batchfactoryv11.IsModelOutputTruncated(err) || calls != want {
			t.Fatalf("truncated output kept retrying: initial=%d calls=%d err=%v", initial, calls, err)
		}
	}
}

func TestRewriteNonTruncationProviderErrorIsNotRetriedInsideValidation(t *testing.T) {
	calls := 0
	want := errors.New("API key invalid")
	p := rewriteProviderFunc(func(context.Context, RewriteRequest) (string, error) { calls++; return "", want })
	_, err := completeValidatedRewrite(context.Background(), p, RewriteRequest{MaxTokens: 1200}, "book", "plain_text", 1, 40, "")
	if !errors.Is(err, want) || calls != 1 {
		t.Fatalf("unrelated provider error retried: calls=%d err=%v", calls, err)
	}
}

func TestControlledRewriteValidationMakesOnlyOneProviderAttempt(t *testing.T) {
	for _, failure := range []string{"invalid-output", "truncated"} {
		t.Run(failure, func(t *testing.T) {
			calls := 0
			provider := rewriteProviderFunc(func(context.Context, RewriteRequest) (string, error) {
				calls++
				if failure == "truncated" {
					return "", errors.New("finish_reason=length")
				}
				return "{invalid output}", nil
			})
			_, err := completeValidatedRewrite(batchfactoryv11.WithSingleAttempt(context.Background()), provider, RewriteRequest{MaxTokens: 1200}, "book", "plain_text", 1, 40, "")
			if err == nil || calls != 1 || (failure == "invalid-output" && !errors.Is(err, ErrRewriteValidation)) || (failure == "truncated" && !strings.Contains(err.Error(), "finish_reason=length")) {
				t.Fatalf("controlled validation bypassed central budget: calls=%d err=%v", calls, err)
			}
			if strings.Contains(err.Error(), "最多 3 次") {
				t.Fatalf("single attempt error claims three calls: %v", err)
			}
		})
	}
}

func TestControlledRewriteValidationLeavesHoldDecisionToCentralBudget(t *testing.T) {
	for _, controlled := range []bool{false, true} {
		ctx := context.Background()
		if controlled {
			ctx = batchfactoryv11.WithSingleAttempt(ctx)
		}
		s := plainRewriteTestStore("alice")
		if err := s.PutDocument(ctx, "alice", Document{BookID: "123", Original: "saved original"}); err != nil {
			t.Fatal(err)
		}
		calls := 0
		_, err := GenerateAIRewrites(ctx, s, "alice", "123", 1, rewriteProviderFunc(func(context.Context, RewriteRequest) (string, error) { calls++; return "{invalid output}", nil }))
		if !errors.Is(err, ErrRewriteValidation) {
			t.Fatal(err)
		}
		got, _ := s.GetDocument(ctx, "alice", "123")
		want := 1
		if calls != want || got.Meta["rewriteValidationHold"] != !controlled {
			t.Fatalf("controlled=%v calls=%d hold=%v", controlled, calls, got.Meta["rewriteValidationHold"])
		}
	}
}
