package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
)

func TestNovelFetchAdaptersDoNotHideTransportRetries(t *testing.T) {
	for _, operation := range []string{"rewrite", "classification"} {
		t.Run(operation, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":"temporary test rejection"}`))
			}))
			defer server.Close()
			provider := &batchfactoryv11.OpenAICompatibleProvider{Endpoint: server.URL + "/v1/chat/completions", APIKey: "test-only", Model: "test-only", Client: server.Client()}
			var err error
			if operation == "rewrite" {
				_, err = (novelFetchRewriteAdapter{provider: provider}).Complete(context.Background(), novelfetchworkshop.RewriteRequest{UserPrompt: "测试", MaxTokens: 100})
			} else {
				_, err = textGenderJudge(provider)(context.Background(), novelfetchworkshop.BookInfo{Text: "测试"})
			}
			if err == nil {
				t.Fatal("test rejection must propagate")
			}
			if calls.Load() != 1 {
				t.Fatalf("durable policy bypassed by hidden provider retries: HTTP calls=%d", calls.Load())
			}
		})
	}
}
