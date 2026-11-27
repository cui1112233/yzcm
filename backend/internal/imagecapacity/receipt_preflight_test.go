package imagecapacity

import (
	"context"
	"errors"
	"net/http"
	"qiantie/backend/internal/yzcmusage"
	"strings"
	"testing"
)

func TestReceiptPreflightRejectionReleasesImagePermitWithoutClaimingSend(t *testing.T) {
	providerCalls := 0
	tracker := &yzcmusage.Tracker{}
	tracked := tracker.Wrap(httpTestTransport(func(*http.Request) (*http.Response, error) {
		providerCalls++
		t.Fatal("failed receipt preflight reached provider")
		return nil, nil
	}))
	permit := &httpTestPermit{}
	transport := WrapHTTP(tracked, func(context.Context, Request) (HTTPPermit, error) {
		return permit, nil
	})
	ctx := WithIdentity(context.Background(), "receipt-preflight-test", "book-test")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://newapi.yzcm.site/v1/images/generations", strings.NewReader(`{"model":"test-model"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer test-not-a-real-key")
	req.Header.Set("Content-Type", "application/json")
	response, err := transport.RoundTrip(req)
	if err == nil || response != nil || providerCalls != 0 {
		t.Fatalf("expected local refusal, response=%v error=%v calls=%d", response, err, providerCalls)
	}
	if errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("local refusal incorrectly reports a sent request: %v", err)
	}
	if len(permit.outcomes) != 1 || permit.outcomes[0].Unknown || permit.outcomes[0].Success {
		t.Fatalf("unsent request must release exactly once without success: %+v", permit.outcomes)
	}
}

func TestImageTransportFailureAfterEntryStillHasUnknownOutcome(t *testing.T) {
	permit := &httpTestPermit{}
	transport := WrapHTTP(httpTestTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport interrupted after entry")
	}), func(context.Context, Request) (HTTPPermit, error) { return permit, nil })
	req, _ := http.NewRequest(http.MethodPost, "https://provider.test/v1/images/generations", strings.NewReader(`{"model":"test-model"}`))
	req.Header.Set("Content-Type", "application/json")
	_, err := transport.RoundTrip(req)
	if !errors.Is(err, ErrOutcomeUnknown) || len(permit.outcomes) != 1 || !permit.outcomes[0].Unknown {
		t.Fatalf("unconfirmed transport outcome must stay unknown: error=%v outcomes=%+v", err, permit.outcomes)
	}
}
