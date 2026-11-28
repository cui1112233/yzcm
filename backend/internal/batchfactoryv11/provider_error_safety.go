package batchfactoryv11

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

type safeProviderError struct {
	cause error
	text  string
}

func (e *safeProviderError) Error() string { return e.text }
func (e *safeProviderError) Unwrap() error { return e.cause }

// SafeProviderError retains the cause for errors.Is/As while exposing only the
// bounded, redacted message. Known credentials must be supplied at the provider.
func SafeProviderError(err error, secrets ...string) error {
	if err == nil {
		return nil
	}
	return &safeProviderError{cause: err, text: SafeProviderErrorText(err.Error(), secrets...)}
}

var providerJSONSecret = regexp.MustCompile(`(?i)("(?:authorization|cookie|api[_-]?key|access[_-]?token|refresh[_-]?token|password|secret|credential|token)"\s*:\s*")(?:\\.|[^"\\])*`)
var providerBearerSecret = regexp.MustCompile(`(?i)(\b(?:bearer|basic)\s+)[^\s,"';]+`)
var providerCookieSecret = regexp.MustCompile(`(?i)(\bcookie\s*:\s*)[^\r\n]+`)
var providerNamedSecret = regexp.MustCompile(`(?i)((?:api[_-]?key|access[_-]?token|refresh[_-]?token|password|secret|credential|token)\s*[:=]\s*["']?)[^\s,"';&}]+`)
var providerURLUserSecret = regexp.MustCompile(`(?i)(https?://)[^/\s:@]+:[^/\s@]+@`)
var providerQuerySecret = regexp.MustCompile(`(?i)([?&](?:key|api[_-]?key|token|access[_-]?token|refresh[_-]?token|secret|password|credential|sig|signature|x-amz-signature|x-amz-credential|x-amz-security-token)=)[^&#\s"']+`)
var providerKeyToken = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`)

// Redact before truncation so a long upstream echo cannot leak a key prefix.
// Historical errors without a known key use the established labeled patterns.
func SafeProviderErrorText(text string, secrets ...string) string {
	values := []string{}
	for _, secret := range secrets {
		if secret != "" {
			values = append(values, secret, url.QueryEscape(secret), url.PathEscape(secret))
			encoded, _ := json.Marshal(secret)
			if len(encoded) > 2 {
				values = append(values, string(encoded[1:len(encoded)-1]))
			}
		}
	}
	sort.Slice(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	for _, value := range values {
		text = strings.ReplaceAll(text, value, "[已隐藏]")
	}
	for _, pattern := range []*regexp.Regexp{providerJSONSecret, providerBearerSecret, providerCookieSecret, providerNamedSecret, providerQuerySecret} {
		text = pattern.ReplaceAllString(text, `${1}[已隐藏]`)
	}
	text = providerURLUserSecret.ReplaceAllString(text, `${1}[已隐藏]@`)
	text = providerKeyToken.ReplaceAllString(text, "[已隐藏]")
	runes := []rune(text)
	if len(runes) > 1000 {
		text = string(runes[:999]) + "…"
	}
	return text
}
