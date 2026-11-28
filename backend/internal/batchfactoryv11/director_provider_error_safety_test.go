package batchfactoryv11

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCatalogProviderHTTPErrorAndLogsNeverExposeKey(t *testing.T) {
	const key = "catalog-unlabelled-fixture-credential"
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = io.WriteString(w, "credentials rejected echo "+key)
	}))
	defer server.Close()
	p := &OpenAICompatibleProvider{Endpoint: server.URL, APIKey: key, Model: "fixture-model", Client: server.Client()}
	for _, single := range []bool{false, true} {
		ctx := context.Background()
		if single {
			ctx = WithSingleAttempt(ctx)
		}
		_, err := p.Complete(ctx, TextCompletionRequest{UserPrompt: "fixture", MaxTokens: 16})
		var typed *DirectorHTTPError
		if err == nil || !errors.As(err, &typed) || typed.StatusCode != 400 || !errors.Is(err, ErrConfigRequired) {
			t.Fatalf("lost typed HTTP classification: %v", err)
		}
		if strings.Contains(err.Error(), key) || strings.Contains(typed.Body, key) || strings.Contains(output.String(), key) {
			t.Fatal("catalog error/log contains credential")
		}
	}
}

type catalogSafetyTransport func(*http.Request) (*http.Response, error)

func (f catalogSafetyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type catalogSafetyBrokenBody struct{ failure error }

func (b catalogSafetyBrokenBody) Read([]byte) (int, error) { return 0, b.failure }
func (b catalogSafetyBrokenBody) Close() error             { return nil }
func TestCatalogProviderTransportAndReadErrorsKeepSafeCauses(t *testing.T) {
	for _, read := range []bool{false, true} {
		t.Run(map[bool]string{false: "transport", true: "read_checksum"}[read], func(t *testing.T) {
			const key = "catalog-network-fixture-credential"
			failure := errors.New("connection failed echo " + key)
			if read {
				failure = gzip.ErrChecksum
			}
			calls := 0
			p := &OpenAICompatibleProvider{Endpoint: "https://example.invalid/v1?key=" + key, APIKey: key, Model: "fixture", Client: &http.Client{Transport: catalogSafetyTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if read {
					return &http.Response{StatusCode: 200, Body: catalogSafetyBrokenBody{failure: failure}, Header: make(http.Header)}, nil
				}
				return nil, failure
			})}}
			ctx := WithSingleAttempt(context.Background())
			if read {
				ctx = context.Background()
			}
			_, err := p.Complete(ctx, TextCompletionRequest{UserPrompt: "fixture"})
			var transport *directorTransportError
			if err == nil || !errors.Is(err, failure) || !errors.As(err, &transport) || strings.Contains(err.Error(), key) || calls != 1 {
				t.Fatalf("lost safe cause or repeated request: calls=%d err=%v", calls, err)
			}
			if read && !errors.Is(err, ErrTextResultUnknown) {
				t.Fatalf("read checksum failure lost result-unknown class: %v", err)
			}
		})
	}
}
