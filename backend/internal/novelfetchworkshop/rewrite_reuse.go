package novelfetchworkshop

import (
	"context"
	"strings"
)

func AIRewriteVersionReady(ctx context.Context, store BodyStore, owner string, document Document, version string) (bool, error) {
	if ConfirmedAIRewriteVersion(document, version) {
		return true, nil
	}
	text, err := ReadBodyText(ctx, store, owner, document, version)
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(text) != "" && ValidateRewriteProse(text) == nil, nil
}

// Per-version done lists are populated only by confirmed submission handling.
// A task-level submitted label alone cannot protect another version.
func ConfirmedAIRewriteVersion(document Document, version string) bool {
	version = strings.ToLower(strings.TrimSpace(version))
	for _, key := range []string{"siteSubmitDoneVersions", "site_submit_done_versions"} {
		for _, done := range metaStringList(document.Meta[key]) {
			if strings.EqualFold(strings.TrimSpace(done), version) {
				return true
			}
		}
	}
	entries := append([]any{}, document.Logs...)
	if history, ok := document.Meta["websiteSubmitHistory"].([]any); ok {
		entries = append(entries, history...)
	}
	for _, raw := range entries {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if rewriteString(item["version"]) != version || rewriteString(item["status"]) != "submitted" {
			continue
		}
		receipt := mapOrEmpty(item["remote_receipt"])
		if receipt["verified"] == true && rewriteString(receipt["submission_id"]) != "" {
			return true
		}
	}
	return false
}
