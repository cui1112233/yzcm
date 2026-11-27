package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
	"strings"
	"testing"
)

func precisionSensitiveSettings(contextChars int, group bool, rules ...map[string]any) map[string]any {
	values := []any{}
	for _, r := range rules {
		values = append(values, r)
	}
	return map[string]any{"sensitive_ai": map[string]any{"enabled": true, "group_hits": group, "context_chars": contextChars, "retries": 0, "concurrency": 1}, "knowledgeLibraries": map[string]any{"sensitive": map[string]any{"groups": []any{map[string]any{"enabled": true, "apply_to_original": true, "apply_to_ai": true, "rules": values}}}}}
}
func TestSensitivePreciseRepairPreservesRanges(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want, reply string
		context                int
		group                  bool
		rules                  []map[string]any
	}{
		{name: "long_line", raw: strings.Repeat("前", 20) + "禁词" + strings.Repeat("后", 20), want: strings.Repeat("前", 8) + "安全短句" + strings.Repeat("后", 8), reply: "安全短句", context: 12},
		{name: "repeated_word_group", raw: "禁词和禁词", want: "甲和乙", reply: `["甲","乙"]`, group: true},
		{name: "overlapping_contexts", raw: "前禁词中禁词后", want: "完整安全句", reply: "完整安全句", context: 12, group: true},
		{name: "cascading_rules", raw: "前长禁词后", want: "前安全后", reply: "安全", rules: []map[string]any{{"find": "长禁词", "replace": "中词"}, {"find": "中词", "replace": "合规"}}},
		{name: "literal_separator_keyword", raw: "前甲/乙后", want: "前甲后", reply: "甲", rules: []map[string]any{{"find": "甲/乙", "replace": "合规"}}},
		{name: "explicit_deletion", raw: "前删除词中禁词后", want: "前中安全后", reply: "安全", rules: []map[string]any{{"find": "删除词", "replace": "", "replace_explicit": true}, {"find": "禁词", "replace": "合规"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rules := tc.rules
			if rules == nil {
				rules = []map[string]any{{"find": "禁词", "replace": "合规"}}
			}
			cfg := precisionSensitiveSettings(tc.context, tc.group, rules...)
			before, _, items := novelfetchworkshop.ApplySensitiveRulesForScopeWithDetails(tc.raw, cfg, "original")
			calls := 0
			provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
				calls++
				return tc.reply, nil
			})
			got, items := applySensitiveAIRepair(context.Background(), provider, cfg, "test-book", before, items, nil)
			if got != tc.want || calls != 1 {
				t.Fatalf("got=%q want=%q calls=%d items=%#v", got, tc.want, calls, items)
			}
			for _, item := range items {
				if item["status"] == "failed" {
					t.Fatalf("valid range failed: %#v", item)
				}
			}
		})
	}
}
func TestSensitiveUnlocatedRepairFailsBeforePaidCall(t *testing.T) {
	calls := 0
	provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
		calls++
		return "安全", nil
	})
	items := []map[string]any{{"keyword": "禁词", "before": "禁词旧行", "after": "合规旧行", "status": "done"}}
	got, items := applySensitiveAIRepair(context.Background(), provider, precisionSensitiveSettings(12, false), "book", "不同的新正文", items, nil)
	if calls != 0 || got != "不同的新正文" || items[0]["status"] != "failed" {
		t.Fatalf("unlocated repair pretended success: calls=%d got=%q items=%#v", calls, got, items)
	}
}
func TestSensitiveFormattingRunsAfterLocatedRepair(t *testing.T) {
	ctx := context.Background()
	store := novelfetchworkshop.NewMemoryStore()
	cfg := precisionSensitiveSettings(12, false, map[string]any{"find": "禁词", "replace": "合规"})
	cfg["appConfig"] = map[string]any{"layout": map[string]any{"apply_to_original": true, "apply_sensitive": true, "apply_chapter_cleanup": true, "trim_lines": true}}
	if err := store.PutConfig(ctx, "test", cfg); err != nil {
		t.Fatal(err)
	}
	provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) { return "安全正文", nil })
	got, _, items, err := processWorkshopConfiguredText(ctx, store, "test", "book", "  禁词正文。。  ", "original", true, provider)
	if err != nil || !strings.Contains(got, "安全正文") || strings.Contains(got, "合规正文") {
		t.Fatalf("formatting broke located writeback: got=%q err=%v items=%#v", got, err, items)
	}
}
func TestManualSensitiveRepairUsesExplicitIntentAndPreservesFailedBody(t *testing.T) {
	for _, tc := range []struct {
		name                string
		enabled             bool
		resolveErr, callErr error
		rules               bool
		badGroup            bool
	}{
		{name: "request_enables_ai"},
		{name: "resolver_error", enabled: true, resolveErr: errors.New("fixture missing model configuration")},
		{name: "repair_failure", enabled: true, callErr: errors.New("fixture service unavailable")},
		{name: "rules_resolver_error", enabled: true, rules: true, resolveErr: errors.New("fixture missing model configuration")},
		{name: "rules_call_error", enabled: true, rules: true, callErr: errors.New("fixture service unavailable")},
		{name: "group_invalid_output", enabled: true, badGroup: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := novelfetchworkshop.NewMemoryStore()
			cfg := precisionSensitiveSettings(12, false, map[string]any{"find": "禁词", "replace": "合规"})
			mapOrEmpty(cfg["sensitive_ai"])["enabled"] = tc.enabled
			if tc.badGroup {
				mapOrEmpty(cfg["sensitive_ai"])["context_chars"] = 0
				mapOrEmpty(cfg["sensitive_ai"])["group_hits"] = true
			}
			if err := store.PutConfig(ctx, "test", cfg); err != nil {
				t.Fatal(err)
			}
			if err := store.PutDocument(ctx, "test", novelfetchworkshop.Document{BookID: "12345678", MaxTxt: 4000}); err != nil {
				t.Fatal(err)
			}
			for _, v := range []string{"original", "original_raw"} {
				text := "禁词旧文"
				if v == "original" {
					text = "旧有效禁词正文"
				}
				if tc.badGroup && v == "original_raw" {
					text = "禁词和禁词"
				}
				if _, err := store.PutBody(ctx, "test", novelfetchworkshop.BodyRecord{BookID: "12345678", BodyRef: novelfetchworkshop.BodyRef{VersionID: v, State: "ready"}, Content: text}); err != nil {
					t.Fatal(err)
				}
			}
			before, _ := store.GetBody(ctx, "test", "12345678", "original")
			calls := 0
			provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
				calls++
				if tc.badGroup {
					return "not-json", nil
				}
				return "安全正文", tc.callErr
			})
			req := httptest.NewRequest(http.MethodPost, "/tasks/reprocess-sensitive", strings.NewReader(`{"ids":["12345678"],"sensitive_ai_enabled":true}`))
			req = req.WithContext(context.WithValue(ctx, bridgeIdentityKey{}, BridgeIdentity{Username: "test"}))
			w := httptest.NewRecorder()
			applyNovelFetchSafeRules(w, req, store, !tc.rules, func(*http.Request) (novelfetchworkshop.RewriteProvider, error) {
				if tc.resolveErr != nil {
					return nil, tc.resolveErr
				}
				return provider, nil
			})
			var result map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			after, _ := store.GetBody(ctx, "test", "12345678", "original")
			if tc.resolveErr != nil || tc.callErr != nil || tc.badGroup {
				if result["failed"] != float64(1) || result["updated"] != float64(0) || after.Content != before.Content || after.Revision != before.Revision {
					t.Fatalf("failed repair replaced body or reported success: result=%s before=%q after=%q", w.Body.String(), before.Content, after.Content)
				}
				versions := arrayOrEmpty(result["version_results"])
				if len(versions) != 1 || mapOrEmpty(versions[0])["status"] != "failed" || firstText(mapOrEmpty(versions[0])["error"]) == "" {
					t.Fatalf("missing per-version error: %s", w.Body.String())
				}
			} else if calls != 1 || after.Content != "安全正文" || result["failed"] != float64(0) {
				t.Fatalf("explicit repair ignored: calls=%d body=%q result=%s", calls, after.Content, w.Body.String())
			}
		})
	}
}

func TestSensitiveKnownFallbackIsNotReportedAsAISuccess(t *testing.T) {
	cfg := precisionSensitiveSettings(0, true, map[string]any{"find": "禁词", "replace": "合规"})
	text, _, items := novelfetchworkshop.ApplySensitiveRulesForScopeWithDetails("禁词和禁词", cfg, "original")
	calls := 0
	provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
		calls++
		return "not-json", nil
	})
	got, items := applySensitiveAIRepair(context.Background(), provider, cfg, "book", text, items, nil)
	if got != text || calls != 1 {
		t.Fatalf("known fallback changed prose/retried: got=%q calls=%d", got, calls)
	}
	for _, item := range items {
		if item["source"] != "deterministic_fallback" || item["ai_status"] != "failed" || item["fallback_used"] != true {
			t.Fatalf("fallback pretended AI success: %#v", item)
		}
	}
}
func TestSensitiveProcessingRespectsDisabledLayoutAndScope(t *testing.T) {
	for _, mode := range []string{"layout", "scope"} {
		t.Run(mode, func(t *testing.T) {
			cfg := precisionSensitiveSettings(12, false, map[string]any{"find": "禁词", "replace": "合规"})
			if mode == "layout" {
				cfg["appConfig"] = map[string]any{"layout": map[string]any{"apply_sensitive": false}}
			} else {
				group := arrayOrEmpty(mapOrEmpty(mapOrEmpty(cfg["knowledgeLibraries"])["sensitive"])["groups"])[0].(map[string]any)
				group["apply_to_original"] = false
			}
			store := novelfetchworkshop.NewMemoryStore()
			if err := store.PutConfig(context.Background(), "test", cfg); err != nil {
				t.Fatal(err)
			}
			calls := 0
			provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
				calls++
				return "安全", nil
			})
			got, _, _, err := processWorkshopConfiguredText(context.Background(), store, "test", "book", "禁词正文", "original", true, provider)
			if err != nil || calls != 0 || got != "禁词正文" {
				t.Fatalf("disabled setting ignored: got=%q calls=%d err=%v", got, calls, err)
			}
		})
	}
}

func sensitiveHoldFixture(t *testing.T, holds map[string]any) *novelfetchworkshop.MemoryStore {
	t.Helper()
	ctx := context.Background()
	s := novelfetchworkshop.NewMemoryStore()
	if err := s.PutConfig(ctx, "test", precisionSensitiveSettings(12, false, map[string]any{"find": "禁词", "replace": "合规"})); err != nil {
		t.Fatal(err)
	}
	meta := map[string]any{"originalStatus": "done", "manualOnly": true, "status": "ready", "siteSubmitDoneVersions": []any{"ai1"}, "retry_schedule": map[string]any{"rewrite": map[string]any{"attempt": 2}}}
	if len(holds) > 0 {
		meta["sensitiveManualRetryErrors"] = holds
		meta["sensitiveManualRetryRequired"] = true
		meta["sensitiveStatus"] = "failed"
		meta["sensitiveError"] = "已有失败"
	}
	if err := s.PutDocument(ctx, "test", novelfetchworkshop.Document{BookID: "12345678", MaxTxt: 4000, Meta: meta}); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"original", "original_raw", "ai1"} {
		if _, err := s.PutBody(ctx, "test", novelfetchworkshop.BodyRecord{BookID: "12345678", BodyRef: novelfetchworkshop.BodyRef{VersionID: v, State: "ready"}, Content: "已有禁词正文"}); err != nil {
			t.Fatal(err)
		}
	}
	return s
}
func runSensitiveHoldRequest(t *testing.T, s novelfetchworkshop.Store, scope string, restore, ai bool, resolveErr error) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"ids": []string{"12345678"}, "scope": scope, "sensitive_ai_enabled": ai})
	req := httptest.NewRequest(http.MethodPost, "/repair", strings.NewReader(string(raw)))
	req = req.WithContext(context.WithValue(context.Background(), bridgeIdentityKey{}, BridgeIdentity{Username: "test"}))
	w := httptest.NewRecorder()
	applyNovelFetchSafeRules(w, req, s, restore, func(*http.Request) (novelfetchworkshop.RewriteProvider, error) {
		if resolveErr != nil {
			return nil, resolveErr
		}
		return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) { return "安全正文", nil }), nil
	})
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func TestSensitiveManualHoldsAreClearedOnlyForRepairedVersions(t *testing.T) {
	for _, tc := range []struct {
		name, scope, remaining string
		restore, ai            bool
	}{
		{name: "original_success_keeps_ai1", scope: "original", remaining: "ai1", restore: true, ai: true},
		{name: "ai1_success_keeps_original", scope: "ai", remaining: "original", ai: true},
		{name: "rules_only_keeps_both", scope: "original", remaining: "both"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := sensitiveHoldFixture(t, map[string]any{"original": "原文AI处理失败", "ai1": "AI1处理失败"})
			result := runSensitiveHoldRequest(t, s, tc.scope, tc.restore, tc.ai, nil)
			if result["failed"] != float64(0) {
				t.Fatalf("repair failed: %#v", result)
			}
			doc, _ := s.GetDocument(context.Background(), "test", "12345678")
			held := mapOrEmpty(doc.Meta["sensitiveManualRetryErrors"])
			want := 1
			if tc.remaining == "both" {
				want = 2
			}
			if len(held) != want || (tc.remaining != "both" && firstText(held[tc.remaining]) == "") || doc.Meta["sensitiveManualRetryRequired"] != true || doc.Meta["sensitiveStatus"] != "failed" || doc.Meta["manualOnly"] != true || intValue(mapOrEmpty(mapOrEmpty(doc.Meta["retry_schedule"])["rewrite"])["attempt"]) != 2 {
				t.Fatalf("unrelated hold/status changed: held=%#v sensitive=%v manual=%v", held, doc.Meta["sensitiveStatus"], doc.Meta["manualOnly"])
			}
		})
	}
	t.Run("last_covered_failure_clears_own_hold_only", func(t *testing.T) {
		s := sensitiveHoldFixture(t, map[string]any{"original": "原文失败"})
		result := runSensitiveHoldRequest(t, s, "original", true, true, nil)
		doc, _ := s.GetDocument(context.Background(), "test", "12345678")
		if result["failed"] != float64(0) || doc.Meta["sensitiveManualRetryRequired"] == true || firstText(doc.Meta["sensitiveError"]) != "" || len(mapOrEmpty(doc.Meta["sensitiveManualRetryErrors"])) != 0 || doc.Meta["manualOnly"] != true {
			t.Fatalf("own hold not cleared/unrelated hold lost: result=%#v meta=%#v", result, doc.Meta)
		}
	})
}

type sensitiveCheckpointCASStore struct {
	novelfetchworkshop.Store
	calls   int
	race    bool
	failure error
}

func (s *sensitiveCheckpointCASStore) CompareAndSwapDocument(ctx context.Context, owner string, doc novelfetchworkshop.Document) (string, error) {
	s.calls++
	if s.race && s.calls == 1 {
		latest, err := s.Store.GetDocument(ctx, owner, doc.BookID)
		if err != nil {
			return "", err
		}
		latest.Meta["status"] = "stopped"
		latest.Meta["manualOnly"] = true
		latest.Meta["concurrent_preference"] = "keep"
		if err := s.Store.PutDocument(ctx, owner, latest); err != nil {
			return "", err
		}
		return "", novelfetchworkshop.ErrDocumentChanged
	}
	if s.failure != nil {
		return "", s.failure
	}
	return s.Store.CompareAndSwapDocument(ctx, owner, doc)
}
func TestSensitiveFailureCheckpointUsesCASAndReportsSaveFailure(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "concurrent_stop", true: "checkpoint_failure"}[broken], func(t *testing.T) {
			base := sensitiveHoldFixture(t, nil)
			s := &sensitiveCheckpointCASStore{Store: base, race: !broken}
			if broken {
				s.failure = errors.New("fixture sensitive checkpoint unavailable")
			}
			before, _ := base.GetBody(context.Background(), "test", "12345678", "original")
			result := runSensitiveHoldRequest(t, s, "original", true, true, errors.New("fixture model https://model.example/v1?api_key=DO_NOT_KEEP&token=DO_NOT_KEEP2"))
			doc, _ := base.GetDocument(context.Background(), "test", "12345678")
			after, _ := base.GetBody(context.Background(), "test", "12345678", "original")
			if result["ok"] != false || result["failed"] != float64(1) || result["updated"] != float64(0) || before.Content != after.Content || before.Revision != after.Revision {
				t.Fatalf("failure checkpoint pretended success/changed prose: result=%#v", result)
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "model.example") || strings.Contains(firstText(doc.Meta["sensitiveError"]), "model.example") || strings.Contains(string(encoded), "DO_NOT_KEEP") || strings.Contains(firstText(doc.Meta["sensitiveError"]), "DO_NOT_KEEP") {
				t.Fatal("credential leaked in failure")
			}
			if broken {
				if s.calls != 1 || !strings.Contains(firstText(mapOrEmpty(arrayOrEmpty(result["version_results"])[0])["error"]), "checkpoint unavailable") {
					t.Fatalf("checkpoint error hidden: calls=%d result=%#v", s.calls, result)
				}
			} else if s.calls != 2 || doc.Meta["sensitiveManualRetryRequired"] != true || doc.Meta["sensitiveStatus"] != "failed" || doc.Meta["originalStatus"] != "done" || doc.Meta["status"] != "stopped" || doc.Meta["concurrent_preference"] != "keep" {
				t.Fatalf("CAS lost stop/hold: calls=%d meta=%#v", s.calls, doc.Meta)
			}
		})
	}
}

func TestSensitiveControlledUnknownSurvivesSafeFormatting(t *testing.T) {
	cfg := precisionSensitiveSettings(0, true, map[string]any{"find": "禁词", "replace": "合规"})
	s := novelfetchworkshop.NewMemoryStore()
	if err := s.PutConfig(context.Background(), "test", cfg); err != nil {
		t.Fatal(err)
	}
	calls := 0
	provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
		calls++
		return "partial", &url.Error{Op: "Post", URL: "https://model.example/v1?api_key=DO_NOT_KEEP&token=DO_NOT_KEEP2", Err: errors.New("connection reset by peer")}
	})
	_, _, _, err := processWorkshopConfiguredText(batchfactoryv11.WithSingleAttempt(context.Background()), s, "test", "book", "禁词和禁词", "original", true, provider)
	if calls != 1 || err == nil || !novelFetchRetryResultUnknown(err) {
		t.Fatalf("unknown outcome hidden by fallback: calls=%d err=%v", calls, err)
	}
	if strings.Contains(err.Error(), "DO_NOT_KEEP") || strings.Contains(err.Error(), "model.example") {
		t.Fatal("provider URL/credential leaked")
	}
}

func TestSensitivePreparedPromptEchoPreservesEveryRune(t *testing.T) {
	line := strings.Repeat("前", 20) + "禁词" + strings.Repeat("后", 20)
	for _, tc := range []struct {
		name, raw string
		group     bool
	}{
		{name: "single", raw: line},
		{name: "group", raw: line + "\n" + strings.Repeat("甲", 20) + "禁词" + strings.Repeat("乙", 20), group: true},
		{name: "boundary_whitespace", raw: "  禁词正文  "},
		{name: "merged", raw: strings.Repeat("前", 20) + "禁词" + strings.Repeat("中", 15) + "禁词" + strings.Repeat("后", 20), group: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := precisionSensitiveSettings(12, tc.group, map[string]any{"find": "禁词", "replace": "合规"})
			mapOrEmpty(cfg["sensitive_ai"])["prompt"] = "{snippet}"
			before, _, items := novelfetchworkshop.ApplySensitiveRulesForScopeWithDetails(tc.raw, cfg, "original")
			provider := rewriteProviderFunc(func(_ context.Context, req novelfetchworkshop.RewriteRequest) (string, error) {
				if strings.Contains(req.UserPrompt, "1. 命中词：") {
					snippets := []string{}
					for _, line := range strings.Split(req.UserPrompt, "\n") {
						if strings.HasPrefix(line, "原片段：") {
							snippets = append(snippets, strings.TrimPrefix(line, "原片段："))
						}
					}
					raw, _ := json.Marshal(snippets)
					return string(raw), nil
				}
				return req.UserPrompt, nil
			})
			got, out := applySensitiveAIRepair(context.Background(), provider, cfg, "book", before, items, nil)
			if got != before {
				t.Fatalf("prepared range recropped: before_runes=%d after_runes=%d before=%q after=%q", len([]rune(before)), len([]rune(got)), before, got)
			}
			for _, item := range out {
				if item["status"] != "done" || item["ai_status"] != "done" || (item["source"] != "ai" && item["source"] != "ai_group") {
					t.Fatalf("echo repair rejected or fell back: %#v", item)
				}
			}
		})
	}
}
func TestManualSensitiveUnknownDoesNotRepeatPaidAttempt(t *testing.T) {
	for _, group := range []bool{false, true} {
		for _, message := range []string{"unexpected EOF", "broken pipe"} {
			t.Run(message+map[bool]string{false: "/single", true: "/group"}[group], func(t *testing.T) {
				s := sensitiveHoldFixture(t, nil)
				cfg, _ := s.GetConfig(context.Background(), "test")
				ai := mapOrEmpty(cfg["sensitive_ai"])
				ai["retries"] = 1
				ai["group_hits"] = group
				ai["context_chars"] = 0
				if err := s.PutConfig(context.Background(), "test", cfg); err != nil {
					t.Fatal(err)
				}
				if group {
					if _, err := s.PutBody(context.Background(), "test", novelfetchworkshop.BodyRecord{BookID: "12345678", BodyRef: novelfetchworkshop.BodyRef{VersionID: "original_raw", State: "ready"}, Content: "禁词和禁词"}); err != nil {
						t.Fatal(err)
					}
				}
				before, _ := s.GetBody(context.Background(), "test", "12345678", "original")
				calls := 0
				req := httptest.NewRequest(http.MethodPost, "/repair", strings.NewReader(`{"ids":["12345678"],"scope":"original","sensitive_ai_enabled":true}`))
				req = req.WithContext(context.WithValue(context.Background(), bridgeIdentityKey{}, BridgeIdentity{Username: "test"}))
				w := httptest.NewRecorder()
				applyNovelFetchSafeRules(w, req, s, true, func(*http.Request) (novelfetchworkshop.RewriteProvider, error) {
					return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
						calls++
						return "", errors.New(message)
					}), nil
				})
				doc, _ := s.GetDocument(context.Background(), "test", "12345678")
				after, _ := s.GetBody(context.Background(), "test", "12345678", "original")
				if calls != 1 || after.Content != before.Content || after.Revision != before.Revision || doc.Meta["sensitiveManualRetryRequired"] != true {
					t.Fatalf("unknown result was replayed or committed: calls=%d held=%v", calls, doc.Meta["sensitiveManualRetryRequired"])
				}
			})
		}
	}
}
func TestSensitiveSuccessMetadataKeepsConcurrentStopAndVersionFailure(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "partial"}[partial], func(t *testing.T) {
			s := sensitiveHoldFixture(t, nil)
			calls := 0
			req := httptest.NewRequest(http.MethodPost, "/repair", strings.NewReader(`{"ids":["12345678"],"scope":"both","sensitive_ai_enabled":true}`))
			req = req.WithContext(context.WithValue(context.Background(), bridgeIdentityKey{}, BridgeIdentity{Username: "test"}))
			w := httptest.NewRecorder()
			applyNovelFetchSafeRules(w, req, s, !partial, func(*http.Request) (novelfetchworkshop.RewriteProvider, error) {
				return rewriteProviderFunc(func(ctx context.Context, _ novelfetchworkshop.RewriteRequest) (string, error) {
					calls++
					if calls == 1 {
						latest, err := s.GetDocument(ctx, "test", "12345678")
						if err != nil {
							t.Fatal(err)
						}
						latest.Gender = "男频"
						latest.Meta["status"] = "stopped"
						latest.Meta["queueState"] = "stopped"
						latest.Meta["editor_note"] = "new user edit"
						latest.Meta["sensitiveManualRetryErrors"] = map[string]any{"ai2": "concurrent AI2 failure"}
						latest.Meta["sensitiveManualRetryRequired"] = true
						latest.Meta["sensitiveStatus"] = "failed"
						if err := s.PutDocument(ctx, "test", latest); err != nil {
							t.Fatal(err)
						}
					} else if partial {
						return "", errors.New("fixture AI1 repair failure")
					}
					return "安全正文", nil
				}), nil
			})
			var result map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			doc, _ := s.GetDocument(context.Background(), "test", "12345678")
			held := mapOrEmpty(doc.Meta["sensitiveManualRetryErrors"])
			if doc.Meta["status"] != "stopped" || doc.Meta["queueState"] != "stopped" || doc.Gender != "男频" || doc.Meta["editor_note"] != "new user edit" || firstText(held["ai2"]) == "" || doc.Meta["sensitiveManualRetryRequired"] != true {
				t.Fatalf("stale success metadata overwrote latest edits: status=%v gender=%q held=%#v", doc.Meta["status"], doc.Gender, held)
			}
			wantFailed := float64(0)
			if partial {
				wantFailed = 1
				if firstText(held["ai1"]) == "" {
					t.Fatalf("partial failure not held: %#v", held)
				}
			}
			if result["updated"] != float64(1) || result["failed"] != wantFailed {
				t.Fatalf("wrong partial counts: %#v", result)
			}
		})
	}
}

func TestSensitiveMergedRepairKeepsOriginalHitCounts(t *testing.T) {
	cfg := precisionSensitiveSettings(12, true, map[string]any{"find": "禁词", "replace": "合规"})
	text, _, items := novelfetchworkshop.ApplySensitiveRulesForScopeWithDetails("禁词和禁词", cfg, "original")
	calls := 0
	provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
		calls++
		return "安全正文", nil
	})
	_, out := applySensitiveAIRepair(context.Background(), provider, cfg, "book", text, items, nil)
	meta := map[string]any{}
	novelfetchworkshop.SetSensitiveDetails(meta, out, len(out))
	if calls != 1 || len(out) != 2 || intValue(mapOrEmpty(meta["sensitiveHits"])["hit_count"]) != 2 || intValue(mapOrEmpty(meta["sensitiveFixed"])["fixed_count"]) != 2 {
		t.Fatalf("merged group replaced occurrence counts: calls=%d hits=%d metadata=%#v", calls, len(out), meta)
	}
}
func TestSensitiveHitLimitIsAppliedBeforeContextMerging(t *testing.T) {
	cfg := precisionSensitiveSettings(12, true, map[string]any{"find": "禁词", "replace": "合规"})
	mapOrEmpty(cfg["sensitive_ai"])["max_hits_per_task"] = 1
	text, _, items := novelfetchworkshop.ApplySensitiveRulesForScopeWithDetails("禁词和禁词", cfg, "original")
	calls := 0
	provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
		calls++
		return "安全和", nil
	})
	got, out := applySensitiveAIRepair(context.Background(), provider, cfg, "book", text, items, nil)
	if calls != 1 || got != "安全和合规" || len(out) != 2 || out[0]["status"] != "done" || out[1]["status"] != "skipped_limit" {
		t.Fatalf("hit cap was counted after merging: calls=%d text=%q items=%#v", calls, got, out)
	}
}
