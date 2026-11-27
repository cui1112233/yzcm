package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

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
	var providerErr error
	if in.SensitiveAIEnabled && resolveProvider != nil {
		provider, providerErr = resolveProvider(req)
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
				message = safeSensitiveFailureText(err.Error())
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
		changes := []novelFetchSensitiveVersionChange{}
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
			if in.SensitiveAIEnabled && providerErr != nil {
				appendResult(version, "failed", fmt.Errorf("读取敏感词 AI 模型失败: %s", safeSensitiveModelFailure(providerErr)))
				continue
			}
			if restore {
				processed, _, items = novelfetchworkshop.ApplySensitiveRulesForScopeWithDetails(content, settings, "original")
				if in.SensitiveAIEnabled {
					repairSettings := cloneAnyMap(settings)
					ai := cloneAnyMap(mapOrEmpty(settings["sensitive_ai"]))
					if len(ai) == 0 {
						ai = cloneAnyMap(mapOrEmpty(mapOrEmpty(settings["appConfig"])["sensitive_ai"]))
					}
					ai["enabled"] = true
					repairSettings["sensitive_ai"] = ai
					processed, items = applySensitiveAIRepair(req.Context(), provider, repairSettings, id, processed, items, map[string]any{"book_name": firstText(d.Meta["bookName"], d.Meta["book_name"], d.Meta["title"]), "style": firstText(d.Meta["style"]), "gender": d.Gender})
					for _, item := range items {
						if item["status"] == "failed" || item["ai_status"] == "failed" {
							processErr = fmt.Errorf("敏感词 AI 修复失败: %s", firstText(item["error"]))
							break
						}
					}
				}
			} else {
				processed, trace, items, processErr = processWorkshopConfiguredText(req.Context(), store, identity.Username, id, content, textScope, in.SensitiveAIEnabled, provider)
			}
			if in.SensitiveAIEnabled && processErr == nil {
				for _, item := range items {
					if item["status"] == "failed" || item["ai_status"] == "failed" {
						processErr = fmt.Errorf("敏感词 AI 修复失败: %s", safeSensitiveFailureText(firstText(item["error"])))
						break
					}
				}
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
			changes = append(changes, novelFetchSensitiveVersionChange{VersionID: version, Ref: ref, Trace: trace, Items: items})
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
		failureReasons := map[string]string{}
		for _, result := range results[start:] {
			if result.Status == "failed" {
				failureReasons[result.VersionID] = result.Error
			}
		}
		coveredAll := !restore && scope == "both" && !bookFailed && len(changes) == len(versions) && len(versions) > 0
		if len(changes) > 0 || (in.SensitiveAIEnabled && len(failureReasons) > 0) {
			metadataErr := saveNovelFetchSensitiveMetadata(req.Context(), store, identity.Username, id, novelFetchSensitiveMetadataUpdate{Changes: changes, Failures: failureReasons, ExplicitAI: in.SensitiveAIEnabled, CoveredAll: coveredAll, Restore: restore, Scope: scope})
			if metadataErr != nil {
				bookFailed = true
				for index := start; index < len(results); index++ {
					if results[index].Status == "success" {
						results[index].Status = "failed"
						results[index].Error = safeSensitiveFailureText("正文已保存，但任务信息保存失败: " + metadataErr.Error())
					} else if results[index].Status == "failed" {
						results[index].Error = safeSensitiveFailureText(results[index].Error + "; 保存敏感词失败状态失败: " + metadataErr.Error())
					}
				}
			} else if len(changes) > 0 {
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": failed == 0, "requested": len(ids), "updated": updated, "failed": failed, "skipped": skipped, "task_ids": updatedIDs, "processed": updated, "applied": updated, "restored": restore, "replacements": replacements, "version_results": results})
}
