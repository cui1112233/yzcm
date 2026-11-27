package novelfetchworkshop

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

type sensitiveRuleEdit struct{ start, end, size int }

func sensitiveMovedPoint(point int, right bool, edits []sensitiveRuleEdit) int {
	delta := 0
	for _, e := range edits {
		if point <= e.start {
			return point + delta
		}
		if point < e.end {
			if right {
				return e.start + delta + e.size
			}
			return e.start + delta
		}
		delta += e.size - (e.end - e.start)
	}
	return point + delta
}
func sensitiveRepairInt(value any) int {
	switch n := value.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return -1
}
func moveSensitiveRepairItems(items []map[string]any, edits []sensitiveRuleEdit) {
	for _, item := range items {
		item["repair_start"] = sensitiveMovedPoint(sensitiveRepairInt(item["repair_start"]), false, edits)
		item["repair_end"] = sensitiveMovedPoint(sensitiveRepairInt(item["repair_end"]), true, edits)
	}
}
func copySensitiveItem(item map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range item {
		out[k] = v
	}
	return out
}

// PrepareSensitiveAIRepairItems creates an execution plan separately from the
// original occurrence records. The hit limit is applied before merging ranges.
// Explicit deletions remain local, and skipped hits stay directly replaced.
func PrepareSensitiveAIRepairItems(text string, items []map[string]any, contextChars int, limits ...int) ([]map[string]any, []map[string]any, error) {
	if contextChars < 0 {
		contextChars = 0
	}
	limit := len(items)
	if len(limits) > 0 && limits[0] > 0 && limits[0] < limit {
		limit = limits[0]
	}
	runes := []rune(text)
	type repairSpan struct {
		start, end int
		item       map[string]any
		keywords   []string
		members    []int
		legacy     bool
	}
	spans := []repairSpan{}
	skipped := []repairSpan{}
	records := make([]map[string]any, 0, len(items))
	legacyCursor := 0
	for index, original := range items {
		record := copySensitiveItem(original)
		record["hit_index"] = index + 1
		records = append(records, record)
		if record["repair_delete"] == true {
			continue
		}
		isSkipped := index >= limit
		if isSkipped {
			record["status"] = "skipped_limit"
			record["ai_status"] = "skipped_limit"
			record["error"] = fmt.Sprintf("超过敏感词 AI 最大命中数 %d，保留本地替换", limit)
		}
		start, hasPosition := record["repair_start"]
		left, right := sensitiveRepairInt(start), sensitiveRepairInt(record["repair_end"])
		legacy := !hasPosition
		if legacy {
			target := stringValue(record["after"], record["before"])
			if target == "" {
				if isSkipped {
					continue
				}
				return nil, nil, fmt.Errorf("敏感词修复片段无法定位")
			}
			at := strings.Index(text[legacyCursor:], target)
			if at < 0 {
				if isSkipped {
					continue
				}
				return nil, nil, fmt.Errorf("敏感词修复片段无法定位")
			}
			at += legacyCursor
			legacyCursor = at + len(target)
			left = len([]rune(text[:at]))
			right = left + len([]rune(target))
		} else if left < 0 || right < left || right > len(runes) || string(runes[left:right]) != stringValue(record["repair_text"]) {
			return nil, nil, fmt.Errorf("敏感词修复片段位置已失效")
		}
		span := repairSpan{start: left, end: right, item: copySensitiveItem(record), keywords: []string{stringValue(record["keyword"])}, members: []int{index}, legacy: legacy}
		if isSkipped {
			skipped = append(skipped, span)
		} else {
			spans = append(spans, span)
		}
	}
	for index := range spans {
		span := &spans[index]
		if span.legacy {
			continue
		}
		left, right := span.start, span.end
		for n := 0; n < contextChars && left > 0 && runes[left-1] != '\n'; n++ {
			left--
		}
		for n := 0; n < contextChars && right < len(runes) && runes[right] != '\n'; n++ {
			right++
		}
		// Context must not let an included hit overwrite a separately capped hit.
		for _, excluded := range skipped {
			if excluded.end <= span.start && excluded.end > left {
				left = excluded.end
			}
			if excluded.start >= span.end && excluded.start < right {
				right = excluded.start
			}
		}
		span.start, span.end = left, right
	}
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	merged := []repairSpan{}
	for _, span := range spans {
		if len(merged) > 0 && span.start < merged[len(merged)-1].end && !span.legacy && !merged[len(merged)-1].legacy {
			last := &merged[len(merged)-1]
			if span.end > last.end {
				last.end = span.end
			}
			last.members = append(last.members, span.members...)
			for _, keyword := range span.keywords {
				found := false
				for _, old := range last.keywords {
					if keyword == old {
						found = true
						break
					}
				}
				if !found {
					last.keywords = append(last.keywords, keyword)
				}
			}
		} else {
			merged = append(merged, span)
		}
	}
	repairs := make([]map[string]any, 0, len(merged))
	for index, span := range merged {
		item := span.item
		fragment := string(runes[span.start:span.end])
		item["repair_start"], item["repair_end"], item["repair_text"] = span.start, span.end, fragment
		item["after"] = fragment
		if !span.legacy {
			item["before"] = fragment
		}
		item["keyword"] = strings.Join(span.keywords, "、")
		item["repair_keywords"] = append([]string(nil), span.keywords...)
		item["repair_member_indices"] = append([]int(nil), span.members...)
		item["hit_index"] = index + 1
		repairs = append(repairs, item)
	}
	return repairs, records, nil
}

// ApplySensitiveAIRepairItem never guesses a fallback substring. Earlier edits
// affect only the accumulated rune delta; the planned target must still match.
func ApplySensitiveAIRepairItem(text string, delta int, item map[string]any, output string) (string, int, error) {
	left, right := sensitiveRepairInt(item["repair_start"])+delta, sensitiveRepairInt(item["repair_end"])+delta
	runes := []rune(text)
	expected := stringValue(item["repair_text"])
	if left < 0 || right < left || right > len(runes) || string(runes[left:right]) != expected {
		return text, delta, fmt.Errorf("敏感词修复片段位置已失效")
	}
	expectedRunes := []rune(expected)
	leading, trailing := 0, len(expectedRunes)
	for leading < len(expectedRunes) && unicode.IsSpace(expectedRunes[leading]) {
		leading++
	}
	for trailing > leading && unicode.IsSpace(expectedRunes[trailing-1]) {
		trailing--
	}
	prefix, suffix := string(expectedRunes[:leading]), string(expectedRunes[trailing:])
	if prefix != "" && !strings.HasPrefix(output, prefix) {
		output = prefix + output
	}
	if suffix != "" && !strings.HasSuffix(output, suffix) {
		output += suffix
	}
	item["after"] = output
	return string(runes[:left]) + output + string(runes[right:]), delta + len([]rune(output)) - (right - left), nil
}

// SafeSensitiveModelError shares the established provider error formatter.
// Never return an endpoint, response body, or credential from a model failure.
func SafeSensitiveModelError(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	// Some existing adapters report a bare status instead of the HTTP prefix.
	// Normalize only the numeric status; never retain the provider's payload.
	if fields := strings.Fields(message); len(fields) > 0 {
		status := fields[0]
		if len(status) == 3 && (status[0] == '4' || status[0] == '5') && status[1] >= '0' && status[1] <= '9' && status[2] >= '0' && status[2] <= '9' {
			return safeStyleError(fmt.Errorf("HTTP %s", status))
		}
	}
	if strings.Contains(message, "重新填写 Key") {
		return "文本模型需要在 API 配置中重新填写 Key"
	}
	if strings.Contains(message, "未配置") {
		return "敏感词 AI 修复模型未配置，请检查 API 配置"
	}
	for _, marker := range []string{"JSON", "json", "invalid character", "返回数量", "响应格式", "不完整"} {
		if strings.Contains(message, marker) {
			return "敏感词 AI 返回格式不符合要求"
		}
	}
	return safeStyleError(err)
}
