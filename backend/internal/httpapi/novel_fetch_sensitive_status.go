package httpapi

import (
	"context"
	"errors"
	"fmt"
	"qiantie/backend/internal/novelfetchworkshop"
	"regexp"
	"sort"
	"strings"
	"time"
)

var sensitiveFailureURLPattern = regexp.MustCompile(`(?i)https?://[^\s<>"']+`)
var sensitiveFailureTokenPattern = regexp.MustCompile(`(?i)((?:token|credential)\s*[:=]\s*)[^,\s;]+`)

func safeSensitiveModelFailure(err error) string {
	message := novelfetchworkshop.SafeSensitiveModelError(err)
	if novelFetchRetryResultUnknown(err) && !novelFetchRetryResultUnknown(errors.New(message)) {
		message += "，结果未知"
	}
	return message
}

func safeSensitiveFailureText(message string) string {
	message = novelFetchBearerSecretPattern.ReplaceAllString(strings.TrimSpace(message), `${1}[已隐藏]`)
	message = novelFetchCookieSecretPattern.ReplaceAllString(message, `${1}[已隐藏]`)
	message = novelFetchNamedSecretPattern.ReplaceAllString(message, `${1}[已隐藏]`)
	message = sensitiveFailureTokenPattern.ReplaceAllString(message, `${1}[已隐藏]`)
	message = sensitiveFailureURLPattern.ReplaceAllString(message, `[地址已隐藏]`)
	if len([]rune(message)) > 500 {
		message = string([]rune(message)[:500]) + "…"
	}
	return message
}

type novelFetchSensitiveVersionChange struct {
	VersionID string
	Ref       novelfetchworkshop.BodyRef
	Trace     []map[string]any
	Items     []map[string]any
}
type novelFetchSensitiveMetadataUpdate struct {
	Changes    []novelFetchSensitiveVersionChange
	Failures   map[string]string
	ExplicitAI bool
	CoveredAll bool
	Restore    bool
	Scope      string
}

// PutBody already updates authoritative references and text counts. This CAS
// merges only this attempt's evidence; it never replays the earlier document.
func saveNovelFetchSensitiveMetadata(ctx context.Context, store novelfetchworkshop.Store, owner, bookID string, update novelFetchSensitiveMetadataUpdate) error {
	for attempt := 0; attempt < 3; attempt++ {
		latest, err := store.GetDocument(ctx, owner, bookID)
		if err != nil {
			return err
		}
		if latest.Meta == nil {
			latest.Meta = map[string]any{}
		}
		succeeded := make([]string, 0, len(update.Changes))
		for _, change := range update.Changes {
			ref, exists := latest.BodyRefs[change.VersionID]
			if !exists || ref.Revision != change.Ref.Revision || ref.ContentHash != change.Ref.ContentHash || ref.State != change.Ref.State {
				return fmt.Errorf("%s 正文版本已变化: %w", change.VersionID, novelfetchworkshop.ErrDocumentChanged)
			}
			succeeded = append(succeeded, change.VersionID)
			traces := cloneAnyMap(mapOrEmpty(latest.Meta["rulesTraceByVersion"]))
			traces[change.VersionID] = change.Trace
			latest.Meta["rulesTraceByVersion"] = traces
			details := cloneAnyMap(mapOrEmpty(latest.Meta["sensitiveByVersion"]))
			details[change.VersionID] = change.Items
			latest.Meta["sensitiveByVersion"] = details
			if change.VersionID == "original" {
				latest.Meta["sensitiveReplacementCount"] = len(change.Items)
				latest.Meta["sensitiveReplaced"] = len(change.Items)
				setSensitiveDetails(latest.Meta, change.Items, len(change.Items))
				if !update.Restore {
					latest.Meta["rulesTrace"] = change.Trace
				}
			}
		}
		if len(update.Changes) > 0 {
			if latest.Meta["sensitiveManualRetryRequired"] == true || (latest.Meta["sensitiveStatus"] == "failed" && !update.ExplicitAI) {
				latest.Meta["sensitiveStatus"] = "failed"
				latest.Meta["sensitiveProcessed"] = false
			} else if len(update.Failures) > 0 {
				latest.Meta["sensitiveStatus"] = "partial"
				latest.Meta["sensitiveProcessed"] = false
			} else {
				latest.Meta["sensitiveStatus"] = "done"
				latest.Meta["sensitiveProcessed"] = true
			}
			latest.Logs = append(latest.Logs, map[string]any{"type": "rules_processed", "at": time.Now().UTC().Format(time.RFC3339Nano), "scope": update.Scope, "partial": len(update.Failures) > 0})
		}
		if update.ExplicitAI {
			mergeManualSensitiveHolds(latest.Meta, update.Failures, succeeded, update.CoveredAll)
		}
		if _, err = store.CompareAndSwapDocument(ctx, owner, latest); !errors.Is(err, novelfetchworkshop.ErrDocumentChanged) {
			return err
		}
	}
	return novelfetchworkshop.ErrDocumentChanged
}

// The error map is the sole source of the manual hold. Successful versions
// clear only their own entry, leaving newer failures of other versions intact.
func mergeManualSensitiveHolds(meta map[string]any, failures map[string]string, succeeded []string, coveredAll bool) {
	held := map[string]any{}
	for version, reason := range mapOrEmpty(meta["sensitiveManualRetryErrors"]) {
		if firstText(reason) != "" {
			held[strings.ToLower(strings.TrimSpace(version))] = safeSensitiveFailureText(firstText(reason))
		}
	}
	hadHold := meta["sensitiveManualRetryRequired"] == true || len(held) > 0
	if hadHold && len(held) == 0 {
		held["original"] = firstText(meta["sensitiveError"], "敏感词 AI 修复需要人工确认")
	}
	for _, version := range succeeded {
		delete(held, strings.ToLower(strings.TrimSpace(version)))
	}
	if coveredAll {
		delete(held, "*")
	}
	for version, reason := range failures {
		version = strings.ToLower(strings.TrimSpace(version))
		if version == "" {
			version = "*"
		}
		held[version] = safeSensitiveFailureText(reason)
	}
	if len(held) == 0 {
		if hadHold {
			delete(meta, "sensitiveManualRetryErrors")
			delete(meta, "sensitiveManualRetryRequired")
			delete(meta, "sensitiveError")
			meta["sensitiveStatus"] = "done"
			meta["sensitiveProcessed"] = true
		}
		return
	}
	keys := make([]string, 0, len(held))
	for version := range held {
		keys = append(keys, version)
	}
	sort.Strings(keys)
	meta["sensitiveManualRetryErrors"] = held
	meta["sensitiveManualRetryRequired"] = true
	meta["sensitiveStatus"] = "failed"
	meta["sensitiveProcessed"] = false
	meta["sensitiveError"] = safeSensitiveFailureText(fmt.Sprintf("%s: %s", keys[0], held[keys[0]]))
}
