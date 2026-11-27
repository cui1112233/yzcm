package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
)

type novelFetchRuleVersionResult struct {
	BookID    string `json:"book_id"`
	VersionID string `json:"version_id"`
	Status    string `json:"status"`
	Error     string `json:"error"`
}

// Only absence permits the legacy inline fallback. A failed storage read must
// never replace the stored body with a stale inline value or serialized metadata.
func novelFetchRuleBody(ctx context.Context, store novelfetchworkshop.Store, owner, bookID, versionID string, raw any) (string, error) {
	body, err := store.GetBody(ctx, owner, bookID, versionID)
	if err == nil {
		return body.Content, nil
	}
	if !errors.Is(err, novelfetchworkshop.ErrNotFound) {
		return "", err
	}
	switch value := raw.(type) {
	case string:
		return value, nil
	case map[string]any:
		text, _ := value["text"].(string)
		return text, nil
	}
	return "", nil
}

func applyNovelFetchSafeRules(w http.ResponseWriter, req *http.Request, store novelfetchworkshop.Store, restore bool, resolveProvider func(*http.Request) (novelfetchworkshop.RewriteProvider, error)) {
	identity, _ := BridgeIdentityFromContext(req.Context())
	var in struct {
		IDs                []string `json:"ids"`
		BookIDs            []string `json:"bookIds"`
		Mode               string   `json:"mode"`
		Text               string   `json:"text"`
		SensitiveAIEnabled bool     `json:"sensitive_ai_enabled"`
		Scope              string   `json:"scope"`
	}
	if err := decodeNovelFetchJSON(w, req, &in); err != nil {
		return
	}
	ids := in.IDs
	if len(ids) == 0 {
		ids = in.BookIDs
	}
	ids, err := resolveWorkshopTaskIDs(req.Context(), store, identity.Username, in.Mode, ids, workshopTaskFailed)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "读取任务失败"})
		return
	}
	settings, settingsErr := novelfetchworkshop.ReadWorkflowConfig(req.Context(), store, identity.Username)
	var provider novelfetchworkshop.RewriteProvider
	if in.SensitiveAIEnabled && resolveProvider != nil {
		provider, _ = resolveProvider(req)
	}
	ids = uniqueNovelFetchTaskIDs(ids)
	scope := strings.ToLower(strings.TrimSpace(in.Scope))
	if scope != "original" && scope != "ai" && scope != "both" {
		scope = "both"
	}
	updated, failed, skipped, replacements := 0, 0, 0, 0
	updatedIDs := make([]string, 0, len(ids))
	results := make([]novelFetchRuleVersionResult, 0)
	for _, id := range ids {
		start := len(results)
		appendResult := func(version, status string, err error) {
			message := ""
			if err != nil {
				message = err.Error()
			}
			results = append(results, novelFetchRuleVersionResult{BookID: id, VersionID: version, Status: status, Error: message})
		}
		d, readErr := store.GetDocument(req.Context(), identity.Username, id)
		if readErr != nil {
			appendResult("", "failed", fmt.Errorf("读取任务失败: %w", readErr))
			failed++
			continue
		}
		if d.Meta == nil {
			d.Meta = map[string]any{}
		}
		if d.BodyRefs == nil {
			d.BodyRefs = map[string]novelfetchworkshop.BodyRef{}
		}
		if d.Versions == nil {
			d.Versions = map[string]any{}
		}
		versions := []string{}
		if scope == "original" || scope == "both" {
			versions = append(versions, "original")
		}
		if !restore && (scope == "ai" || scope == "both") {
			refs, refErr := store.ListBodyRefs(req.Context(), identity.Username, id)
			if refErr != nil {
				appendResult("", "failed", fmt.Errorf("读取正文版本失败: %w", refErr))
			}
			candidates := map[string]bool{}
			for v := range d.Versions {
				candidates[v] = true
			}
			for v := range d.BodyRefs {
				candidates[v] = true
			}
			for _, ref := range refs {
				candidates[ref.VersionID] = true
			}
			ai := []string{}
			for v := range candidates {
				if strings.HasPrefix(strings.ToLower(v), "ai") {
					ai = append(ai, v)
				}
			}
			sort.Strings(ai)
			versions = append(versions, ai...)
		}
		successful, bookReplacements := 0, 0
		for _, version := range versions {
			if settingsErr != nil {
				appendResult(version, "failed", fmt.Errorf("读取规则配置失败: %w", settingsErr))
				continue
			}
			raw := d.Versions[version]
			if version == "original" {
				raw = d.ProcessedText
				if strings.TrimSpace(d.ProcessedText) == "" {
					raw = d.Original
				}
			}
			content, bodyErr := novelFetchRuleBody(req.Context(), store, identity.Username, id, version, raw)
			if bodyErr == nil && restore && version == "original" {
				var rawContent string
				rawContent, bodyErr = novelFetchRuleBody(req.Context(), store, identity.Username, id, "original_raw", d.OriginalRaw)
				if bodyErr == nil && strings.TrimSpace(rawContent) != "" {
					content = novelfetchworkshop.ProcessRawText(rawContent, d.MaxTxt).ProcessedText
				}
			}
			if bodyErr != nil {
				appendResult(version, "failed", fmt.Errorf("读取正文失败: %w", bodyErr))
				continue
			}
			if strings.TrimSpace(content) == "" {
				appendResult(version, "skipped", nil)
				continue
			}
			var processed string
			var trace []map[string]any
			var items []map[string]any
			var processErr error
			textScope := "ai"
			if version == "original" {
				textScope = "original"
			}
			if restore {
				processed, _, items = novelfetchworkshop.ApplySensitiveRulesForScopeWithDetails(content, settings, "original")
				processed, items = applySensitiveAIRepair(req.Context(), provider, settings, id, processed, items, map[string]any{"book_name": firstText(d.Meta["bookName"], d.Meta["book_name"], d.Meta["title"]), "style": firstText(d.Meta["style"]), "gender": d.Gender})
			} else {
				processed, trace, items, processErr = processWorkshopConfiguredText(req.Context(), store, identity.Username, id, content, textScope, in.SensitiveAIEnabled, provider)
			}
			if processErr != nil {
				appendResult(version, "failed", fmt.Errorf("处理正文失败: %w", processErr))
				continue
			}
			ref, saveErr := store.PutBody(req.Context(), identity.Username, novelfetchworkshop.BodyRecord{BookID: id, BodyRef: novelfetchworkshop.BodyRef{VersionID: version, State: "ready"}, Content: processed})
			if saveErr != nil {
				appendResult(version, "failed", fmt.Errorf("保存正文失败: %w", saveErr))
				continue
			}
			// No reference, metadata, or inline content changes before successful save.
			d.BodyRefs[version] = ref
			if version == "original" {
				d.Original = processed
				d.ProcessedText = processed
				d.ProcessedChars = len([]rune(processed))
				d.Meta["sensitiveReplacementCount"] = len(items)
				d.Meta["sensitiveReplaced"] = len(items)
				setSensitiveDetails(d.Meta, items, len(items))
				if !restore {
					d.Meta["rulesTrace"] = trace
				}
			} else {
				if metadata, ok := d.Versions[version].(map[string]any); ok {
					copy := map[string]any{}
					for k, v := range metadata {
						copy[k] = v
					}
					copy["text"] = processed
					d.Versions[version] = copy
				} else {
					d.Versions[version] = processed
				}
				traces := mapOrEmpty(d.Meta["rulesTraceByVersion"])
				traces[version] = trace
				d.Meta["rulesTraceByVersion"] = traces
			}
			successful++
			bookReplacements += len(items)
			appendResult(version, "success", nil)
		}
		bookFailed := false
		for _, result := range results[start:] {
			if result.Status == "failed" {
				bookFailed = true
			}
		}
		if successful > 0 {
			d.Meta["sensitiveProcessed"] = !bookFailed
			if bookFailed {
				d.Meta["sensitiveStatus"] = "partial"
			} else {
				d.Meta["sensitiveStatus"] = "done"
			}
			d.Logs = append(d.Logs, map[string]any{"type": "rules_processed", "at": time.Now().UTC().Format(time.RFC3339Nano), "scope": scope, "partial": bookFailed})
			if saveErr := store.PutDocument(req.Context(), identity.Username, d); saveErr != nil {
				bookFailed = true
				for i := start; i < len(results); i++ {
					if results[i].Status == "success" {
						results[i].Status = "failed"
						results[i].Error = fmt.Sprintf("正文已保存，但任务信息保存失败: %v", saveErr)
					}
				}
			} else {
				updated++
				updatedIDs = append(updatedIDs, id)
				replacements += bookReplacements
			}
		}
		if bookFailed {
			failed++
		} else if successful == 0 {
			skipped++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "requested": len(ids), "updated": updated, "failed": failed, "skipped": skipped, "task_ids": updatedIDs, "processed": updated, "applied": updated, "restored": restore, "replacements": replacements, "version_results": results})
}
