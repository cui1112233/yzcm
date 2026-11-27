package novelfetchworkshop

import "testing"

func repairRangeSettings(rules ...map[string]any) map[string]any {
	values := []any{}
	for _, r := range rules {
		values = append(values, r)
	}
	return map[string]any{"knowledgeLibraries": map[string]any{"sensitive": map[string]any{"groups": []any{map[string]any{"enabled": true, "rules": values}}}}}
}
func TestSensitiveRepairRangesUseRunesAcrossRules(t *testing.T) {
	cfg := repairRangeSettings(map[string]any{"find": "长禁词", "replace": "中"}, map[string]any{"find": "禁词", "replace": "替换词"})
	text, _, items := ApplySensitiveRulesForScopeWithDetails("起😀长禁词后，禁词终", cfg, "original")
	planned, direct, err := PrepareSensitiveAIRepairItems(text, items, 0)
	if err != nil || len(planned) != 2 || len(direct) != 2 {
		t.Fatalf("plan=%#v direct=%#v err=%v", planned, direct, err)
	}
	delta := 0
	for i, output := range []string{"甲", "乙"} {
		text, delta, err = ApplySensitiveAIRepairItem(text, delta, planned[i], output)
		if err != nil {
			t.Fatal(err)
		}
	}
	if text != "起😀甲后，乙终" {
		t.Fatalf("rune/rule mapping damaged outside text: %q", text)
	}
}
func TestSensitiveRepairRangesMergeDifferentKeywordContexts(t *testing.T) {
	cfg := repairRangeSettings(map[string]any{"find": "甲词", "replace": "替甲"}, map[string]any{"find": "乙词", "replace": "替乙"})
	text, _, items := ApplySensitiveRulesForScopeWithDetails("起甲词中乙词终", cfg, "original")
	planned, _, err := PrepareSensitiveAIRepairItems(text, items, 12)
	if err != nil || len(planned) != 1 || planned[0]["keyword"] != "甲词、乙词" || planned[0]["repair_text"] != text {
		t.Fatalf("overlapping context plan=%#v err=%v", planned, err)
	}
}
func TestSensitiveRepairRangeMismatchDoesNotMutateText(t *testing.T) {
	text, _, items := ApplySensitiveRulesForScopeWithDetails("前禁词后", repairRangeSettings(map[string]any{"find": "禁词", "replace": "合规"}), "original")
	planned, _, err := PrepareSensitiveAIRepairItems(text, items, 0)
	if err != nil {
		t.Fatal(err)
	}
	changed := "另" + text
	got, delta, err := ApplySensitiveAIRepairItem(changed, 0, planned[0], "安全")
	if err == nil || got != changed || delta != 0 {
		t.Fatalf("mismatched position accepted: got=%q delta=%d err=%v", got, delta, err)
	}
}
func TestSensitiveRepairExplicitAndCascadedDeletionStayDeleted(t *testing.T) {
	cfg := repairRangeSettings(map[string]any{"find": "原有长词", "replace": "临时词"}, map[string]any{"find": "临时词", "replace": "", "replace_explicit": true})
	text, _, items := ApplySensitiveRulesForScopeWithDetails("前原有长词后", cfg, "original")
	planned, direct, err := PrepareSensitiveAIRepairItems(text, items, 12)
	if err != nil || text != "前后" || len(planned) != 0 || len(direct) != 2 {
		t.Fatalf("deletion became an AI repair: text=%q planned=%#v direct=%#v err=%v", text, planned, direct, err)
	}
}

func TestSensitiveDetailsRefreshVisibleCountsFromOriginalHits(t *testing.T) {
	meta := map[string]any{"sensitiveHitCount": 7, "sensitive_hit_count": 7, "sensitiveFixedCount": 7, "sensitive_fixed_count": 7, "sensitiveFailedCount": 4, "sensitive_failed_count": 4}
	items := []map[string]any{{"keyword": "词", "status": "done", "source": "ai"}, {"keyword": "词", "status": "skipped_limit", "source": "system_rule"}}
	SetSensitiveDetails(meta, items, 2)
	for _, key := range []string{"sensitiveHitCount", "sensitive_hit_count", "sensitiveFixedCount", "sensitive_fixed_count"} {
		if meta[key] != 2 {
			t.Fatalf("stale visible count %s=%v", key, meta[key])
		}
	}
	for _, key := range []string{"sensitiveFailedCount", "sensitive_failed_count"} {
		if meta[key] != 0 {
			t.Fatalf("stale failure count %s=%v", key, meta[key])
		}
	}
}
