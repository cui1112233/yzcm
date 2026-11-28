package batchfactoryv11

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSafeProviderErrorPreservesTypedCauseAndIdentity(t *testing.T) {
	const key = "exact-unlabelled-fixture-key"
	typed := &DirectorHTTPError{StatusCode: 429, Model: "fixture-model", Body: "upstream echoed " + key}
	err := SafeProviderError(fmt.Errorf("provider: %w", errors.Join(typed, context.DeadlineExceeded)), key)
	var found *DirectorHTTPError
	if strings.Contains(err.Error(), key) || !errors.Is(err, context.DeadlineExceeded) || !errors.As(err, &found) || found.StatusCode != 429 || !strings.Contains(err.Error(), "429") {
		t.Fatalf("redaction lost classification or leaked: %v", err)
	}
	if SafeProviderError(nil) != nil {
		t.Fatal("nil error changed")
	}
}
func TestSafeProviderErrorTextCoversLabelsAndTruncatesAfterRedaction(t *testing.T) {
	for _, text := range []string{`{"api_key":"fixture-secret"}`, "Authorization: Bearer fixture-secret", "Cookie: session=fixture-secret; other=fixture-secret", "https://example.test/?token=fixture-secret&ok=yes", "credential=fixture-secret"} {
		safe := SafeProviderErrorText(text)
		if strings.Contains(safe, "fixture-secret") {
			t.Fatalf("labeled credential leaked: %s", safe)
		}
	}
	const key = "long-unlabelled-fixture-key"
	text := strings.Repeat("x", 990) + key + strings.Repeat("z", 100)
	safe := SafeProviderErrorText(text, key)
	if len([]rune(safe)) > 1000 || strings.Contains(safe, "long-unlabelled") {
		t.Fatal("truncation leaked a known key prefix")
	}
}

func TestSafeProviderErrorTextRedactsSensitiveQueryKeys(t *testing.T) {
	for _, key := range []string{"key", "api_key", "signature", "X-Amz-Credential"} {
		text := "HTTP 403 https://example.test/v1?" + key + "=fixture-query-secret&model=fixture"
		safe := SafeProviderErrorText(text)
		if strings.Contains(safe, "fixture-query-secret") || !strings.Contains(safe, "model=fixture") {
			t.Fatalf("sensitive query not safely filtered: %s", safe)
		}
	}
}
