package httpapi

import (
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"qiantie/backend/internal/batchfactoryv11"
	"testing"
	"time"
)

type safetyChecksumBody struct{}

func (safetyChecksumBody) Read([]byte) (int, error) { return 0, gzip.ErrChecksum }

type safetyChecksumTransport struct{}

func (safetyChecksumTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(safetyChecksumBody{})}, nil
}
func TestSafetyIntegrationReadFailureAlwaysRequiresManualReview(t *testing.T) {
	provider := &integrationTextProvider{Label: "fixture", Endpoint: "http://127.0.0.1/fake-only", Model: "fixture", APIKey: "fake-key", Client: &http.Client{Transport: safetyChecksumTransport{}}}
	_, err := provider.Complete(batchfactoryv11.WithSingleAttempt(context.Background()), batchfactoryv11.TextCompletionRequest{})
	if !errors.Is(err, batchfactoryv11.ErrTextResultUnknown) || !errors.Is(err, gzip.ErrChecksum) {
		t.Fatalf("read error lost unknown result or cause: %v", err)
	}
	state := scheduleNovelFetchTaskRetry(map[string]any{}, novelFetchRetryStageRewrite, err, 0, time.Now())
	if !novelFetchRetryStateIsManual(state) || firstText(state["next_retry_at"]) != "" {
		t.Fatalf("partial response scheduled another call: %v", state)
	}
}
