package batchfactoryv11

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"time"
)

// ValidateTextExtraBody prevents saved JSON from replacing request identity,
// credentials or the caller's output protocol. Explicit sampling fields live
// on TextCompletionRequest and cannot be overridden through extra JSON.
func ValidateTextExtraBody(extra map[string]any) error {
	for key, value := range extra {
		normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), "_", ""))
		switch normalized {
		case "model", "messages", "apikey", "authorization", "headers", "cookie", "credentials", "baseurl", "endpoint", "url", "responseformat", "temperature", "maxtokens", "maxcompletiontokens", "maxoutputtokens", "n", "bestof", "tools", "functions", "toolchoice", "functioncall":
			return NewConfigRequiredError("额外 JSON 不允许覆盖受保护字段：" + key)
		case "stream":
			if enabled, ok := value.(bool); !ok || enabled {
				return NewConfigRequiredError("当前测书不支持流式响应（stream）")
			}
		}
		if err := validateTextParameter(key, value); err != nil {
			return err
		}
	}
	if _, err := json.Marshal(extra); err != nil {
		return NewConfigRequiredError("模型额外参数必须是有效 JSON 对象")
	}
	return nil
}

func validateTextParameter(key string, value any) error {
	var min, max float64
	switch key {
	case "top_p":
		min, max = 0, 1
	case "presence_penalty", "frequency_penalty":
		min, max = -2, 2
	case "temperature":
		min, max = 0, 2
	case "max_tokens":
		min, max = 1, 1000000
	case "thinking":
		object, ok := value.(map[string]any)
		if !ok || (object["type"] != "enabled" && object["type"] != "disabled" && object["type"] != "adaptive") {
			return NewConfigRequiredError("thinking 必须是包含有效 type 的对象")
		}
		return nil
	case "enable_thinking":
		if _, ok := value.(bool); !ok {
			return NewConfigRequiredError("enable_thinking 必须为布尔值")
		}
		return nil
	default:
		return nil
	}
	raw, err := json.Marshal(value)
	var number float64
	if err != nil || json.Unmarshal(raw, &number) != nil || math.IsNaN(number) || math.IsInf(number, 0) || number < min || number > max || (key == "max_tokens" && number != math.Trunc(number)) {
		return NewConfigRequiredError("模型参数无效：" + key)
	}
	return nil
}

// ApplyTextRequestParameters merges channel defaults first, then explicit
// business parameters. model/messages/response_format stay with the caller.
func ApplyTextRequestParameters(payload map[string]any, input TextCompletionRequest, defaults map[string]any) error {
	merged := map[string]any{}
	for key, value := range defaults {
		if key == "temperature" || key == "max_tokens" {
			continue
		}
		if input.ExtraBody == nil {
			// Legacy callers never applied protocol overrides from channel defaults.
			if err := ValidateTextExtraBody(map[string]any{key: value}); err != nil {
				continue
			}
		}
		merged[key] = value
	}
	if err := ValidateTextExtraBody(merged); err != nil {
		return err
	}
	if err := ValidateTextExtraBody(input.ExtraBody); err != nil {
		return err
	}
	if _, ok := input.ExtraBody["thinking"]; ok {
		delete(merged, "enable_thinking")
	}
	if _, ok := input.ExtraBody["enable_thinking"]; ok {
		delete(merged, "thinking")
	}
	for key, value := range input.ExtraBody {
		merged[key] = value
	}
	if flag, ok := merged["enable_thinking"].(bool); ok {
		if thinking, ok := merged["thinking"].(map[string]any); ok && ((flag && thinking["type"] == "disabled") || (!flag && thinking["type"] == "enabled")) {
			return NewConfigRequiredError("启用思考与禁用思考不能同时生效")
		}
	}
	if !input.OmitTemperature {
		if err := validateTextParameter("temperature", input.Temperature); err != nil {
			return err
		}
	}
	if !input.OmitMaxTokens && input.MaxTokens != 0 {
		if err := validateTextParameter("max_tokens", input.MaxTokens); err != nil {
			return err
		}
	}
	for key, value := range merged {
		payload[key] = value
	}
	// Streaming is deliberately unsupported for the complete-body save path.
	delete(payload, "stream")
	ApplyTextParameterOmissions(payload, input)
	return nil
}

// TextCompletionHTTPClient changes only an explicitly requested per-call limit.
// The request context remains authoritative when its deadline is earlier.
func TextCompletionHTTPClient(ctx context.Context, client *http.Client, req *http.Request, timeout time.Duration) *http.Client {
	if timeout > 0 {
		copy := *client
		copy.Timeout = timeout
		client = &copy
	}
	return SingleAttemptClient(ctx, client, req)
}
