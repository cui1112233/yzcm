package novelfetchworkshop

import "strings"

// The reference editor stores prose separately from prompt/template fields.
// Keep this compatibility local to reference selection, not generic templates.
func rewriteReferenceText(item map[string]any) string {
	if text := rewriteString(item["reference_text"]); text != "" {
		return text
	}
	return rewriteItemText(item)
}

func highImitationSystemPrompt(library map[string]any) string {
	for _, item := range arrayMap(library["prompts"]) {
		if item["enabled"] == false {
			continue
		}
		for _, key := range []string{"system_prompt", "content", "prompt"} {
			if text := rewriteString(item[key]); text != "" {
				return text
			}
		}
	}
	return ""
}

// Explicit saved choices win. Automatic selection prefers the same style,
// then an unscoped reference, and never borrows an unrelated genre's first row.
func selectRewriteReference(library map[string]any, fields []string, document Document, idKey, nameKey string) map[string]any {
	items := []map[string]any{}
	for _, field := range fields {
		for _, item := range arrayMap(library[field]) {
			// SaveKnowledge also mirrors typed prompt rows into items. They
			// remain system instructions, never example prose for imitation.
			if rewriteString(item["collection"]) == "prompts" {
				continue
			}
			if item["enabled"] != false && rewriteReferenceText(item) != "" {
				items = append(items, item)
			}
		}
	}
	id, name := documentMetaText(document, idKey), documentMetaText(document, nameKey)
	for _, item := range items {
		if id != "" && rewriteString(item["id"]) == id || name != "" && rewriteItemName(item) == name {
			return item
		}
	}
	style := documentMetaText(document, "style", "genre", "classification")
	if style == "" {
		style = strings.TrimSpace(document.BookInfo.Genre)
	}
	gender := strings.TrimSpace(document.Gender)
	if gender == "" {
		gender = strings.TrimSpace(document.BookInfo.Category)
	}
	var generic map[string]any
	for _, item := range items {
		itemGender := rewriteString(item["gender"])
		if itemGender != "" && gender != "" && itemGender != gender {
			continue
		}
		itemStyle := rewriteString(item["style"])
		if style != "" && itemStyle == style {
			return item
		}
		if generic == nil && (itemStyle == "" || itemStyle == "通用" || itemStyle == "不限" || itemStyle == "全部") {
			generic = item
		}
	}
	return generic
}
