package httpapi

import (
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
	"strings"
)

// Clone only structured views. Prose, titles and user configuration strings
// are untouched; only error values and diagnostic log messages are filtered.
func safeNovelFetchErrorView(value any) any { return safeNovelFetchErrorNode(value, false, false) }
func safeNovelFetchErrorNode(value any, errorField, logField bool) any {
	switch v := value.(type) {
	case map[string]map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = child
		}
		return safeNovelFetchErrorNode(out, errorField, logField)
	case map[string]string:
		out := make(map[string]any, len(v))
		for key, child := range v {
			out[key] = child
		}
		return safeNovelFetchErrorNode(out, errorField, logField)
	case []map[string]any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = child
		}
		return safeNovelFetchErrorNode(out, errorField, logField)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			lower := strings.ToLower(key)
			isError := errorField || novelFetchIsErrorField(key)
			isLog := lower == "logs" || lower == "steps" || lower == "progress"
			diagnostic := logField && (lower == "message" || lower == "detail" || lower == "reason")
			out[key] = safeNovelFetchErrorNode(child, isError || diagnostic, isLog)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = safeNovelFetchErrorNode(item, errorField, logField)
		}
		return out
	case string:
		if errorField || logField {
			return batchfactoryv11.SafeProviderErrorText(v)
		}
		return v
	default:
		return value
	}
}
func safeNovelFetchDocumentErrors(document novelfetchworkshop.Document) novelfetchworkshop.Document {
	if document.Meta != nil {
		document.Meta = safeNovelFetchErrorView(document.Meta).(map[string]any)
	}
	if document.Logs != nil {
		document.Logs = safeNovelFetchErrorNode(document.Logs, false, true).([]any)
	}
	return document
}

func novelFetchIsErrorField(key string) bool {
	lower := strings.ToLower(key)
	return lower == "error" || lower == "errors" || lower == "errormessage" || lower == "error_message" || strings.HasSuffix(lower, "_error") || strings.HasSuffix(lower, "_errors") || strings.HasSuffix(key, "Error") || strings.HasSuffix(key, "Errors")
}
