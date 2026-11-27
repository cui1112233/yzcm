package yzcmusage

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"qiantie/backend/internal/safehttp"
)

func TestOrdinaryReceiptPreflightRefusalIsNotSent(t *testing.T) {
	calls := 0
	tracker := &Tracker{}
	transport := tracker.Wrap(transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected provider dispatch")
	}))
	req, err := http.NewRequestWithContext(WithIdentity(context.Background(), "alice", "image"), http.MethodPost, Origin+"/v1/images/generations", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer fixture-only")
	response, err := transport.RoundTrip(req)
	if response != nil || !errors.Is(err, safehttp.ErrRequestNotSent) || !errors.Is(err, ErrReceiptPersistenceUnavailable) || calls != 0 {
		t.Fatalf("preflight refusal must be known unsent: response=%v error=%v provider_calls=%d", response, err, calls)
	}
}
