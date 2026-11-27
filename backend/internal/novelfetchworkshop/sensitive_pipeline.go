package novelfetchworkshop

import "strings"

func sensitivePipelineConfig(settings map[string]any) (map[string]any, map[string]any) {
	app := mapOrEmpty(settings["appConfig"])
	layout := mapOrEmpty(settings["layout"])
	if configured := mapOrEmpty(app["layout"]); len(configured) > 0 {
		layout = configured
	}
	knowledge := mapOrEmpty(settings["knowledgeLibraries"])
	if len(knowledge) == 0 {
		knowledge = mapOrEmpty(app["knowledge"])
	}
	return layout, knowledge
}

// ProcessSensitiveTextBeforeFormatting keeps AI positions stable until repair.
func ProcessSensitiveTextBeforeFormatting(text, scope string, settings map[string]any) (string, []map[string]any, []map[string]any) {
	layout, _ := sensitivePipelineConfig(settings)
	raw := normalizeRuleNewlines(text)
	trace := []map[string]any{{"stage": "raw", "title": "抓取原文/raw", "chars": len([]rune(raw)), "lines": len(strings.Split(raw, "\n")), "changed": false, "samples": []map[string]any{}}}
	if (scope == "original" && !ruleBool(layout, "apply_to_original", true)) || (scope == "ai" && !ruleBool(layout, "apply_to_ai", true)) || !ruleBool(layout, "apply_sensitive", true) {
		return raw, trace, []map[string]any{}
	}
	processed, _, items := ApplySensitiveRulesForScopeWithDetails(raw, settings, scope)
	return processed, append(trace, ruleTraceStage("sensitive", "敏感词替换", raw, processed)), items
}

// FormatConfiguredTextAfterSensitive runs the existing formatter, without a
// second sensitive replacement pass that could expand user replacements.
func FormatConfiguredTextAfterSensitive(text, scope string, settings map[string]any) (string, []map[string]any) {
	layout, knowledge := sensitivePipelineConfig(settings)
	return PreviewRules(text, scope, layout, knowledge)
}
