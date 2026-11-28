package novelfetchworkshop

import (
	"encoding/json"
	"fmt"
	"math"
	"qiantie/backend/internal/batchfactoryv11"
	"strings"
	"time"
)

func rewriteAISettings(settings map[string]any) map[string]any {
	ai := mapOrEmpty(settings["ai"])
	if configured := mapOrEmpty(mapOrEmpty(settings["appConfig"])["ai"]); len(configured) > 0 {
		ai = configured
	}
	return ai
}

// AIRetryMaxFailures includes the first failure and caps the existing round at
// three. Explicit zero retries means one failure before a manual hold.
func AIRetryMaxFailures(settings map[string]any) int {
	retries := ruleInt(rewriteAISettings(settings)["retry_times"], 2)
	if retries < 0 {
		retries = 0
	}
	if retries > 2 {
		retries = 2
	}
	return retries + 1
}

// AIRequestParameters validates the shared account AI options. Classifiers
// keep their explicit protocol, token budget and independent deadline.
func AIRequestParameters(settings map[string]any) (time.Duration, map[string]any, error) {
	ai := rewriteAISettings(settings)
	if _, err := rewriteEffectiveTemperature(settings); err != nil {
		return 0, nil, err
	}

	if _, err := rewriteSettingInt(ai, "max_tokens", rewriteMaxTokens, 1, 1000000); err != nil {
		return 0, nil, err
	}
	seconds, err := rewriteSettingInt(ai, "timeout_seconds", 180, 1, 900)
	if err != nil {
		return 0, nil, err
	}
	extra := map[string]any{}
	for _, key := range []string{"top_p", "presence_penalty", "frequency_penalty"} {
		if value, exists := ai[key]; exists && !rewriteParameterMissing(value) {
			extra[key] = value
		}
	}
	enabled, disabled := ai["enable_thinking"] == true, ai["disable_thinking"] == true
	if enabled && disabled {
		return 0, nil, batchfactoryv11.NewConfigRequiredError("启用思考与禁用思考不能同时开启")
	}
	if raw, exists := ai["extra_body_json"]; exists && raw != nil {
		var parsed map[string]any
		switch value := raw.(type) {
		case string:
			if strings.TrimSpace(value) == "" {
				break
			}
			if err := json.Unmarshal([]byte(value), &parsed); err != nil || parsed == nil {
				return 0, nil, batchfactoryv11.NewConfigRequiredError("额外 JSON 必须是对象")
			}
		case map[string]any:
			parsed = value
		default:
			return 0, nil, batchfactoryv11.NewConfigRequiredError("额外 JSON 必须是对象")
		}
		for key, value := range parsed {
			extra[key] = value
		}
	}
	if enabled || disabled {
		kind := "enabled"
		if disabled {
			kind = "disabled"
		}
		if saved, ok := extra["thinking"]; ok {
			object, ok := saved.(map[string]any)
			if !ok || object["type"] != kind {
				return 0, nil, batchfactoryv11.NewConfigRequiredError("思考开关与额外 JSON 的 thinking 冲突")
			}
		}
		extra["thinking"] = map[string]any{"type": kind}
	}
	if err := batchfactoryv11.ValidateTextExtraBody(extra); err != nil {
		return 0, nil, fmt.Errorf("AI 参数配置错误：%w", err)
	}
	return time.Duration(seconds) * time.Second, extra, nil
}

func rewriteEffectiveTemperature(settings map[string]any) (float64, error) {
	rewrite := mapOrEmpty(settings["rewrite"])
	if configured := mapOrEmpty(mapOrEmpty(settings["appConfig"])["rewrite"]); len(configured) > 0 {
		rewrite = configured
	}
	value, exists := rewrite["temperature"]
	if !exists {
		value, exists = rewriteAISettings(settings)["temperature"]
	}
	if !exists || value == nil || value == "" {
		return rewriteTemperature, nil
	}
	raw, err := json.Marshal(value)
	var number float64
	if err != nil || json.Unmarshal(raw, &number) != nil {
		return 0, batchfactoryv11.NewConfigRequiredError("改文温度必须是有效数值")
	}
	if err := batchfactoryv11.ApplyTextRequestParameters(map[string]any{}, batchfactoryv11.TextCompletionRequest{Temperature: number, OmitMaxTokens: true}, nil); err != nil {
		return 0, err
	}
	return number, nil
}

func rewriteParameterMissing(value any) bool {
	if value == nil {
		return true
	}
	text, ok := value.(string)
	return ok && strings.TrimSpace(text) == ""
}

func rewriteSettingInt(settings map[string]any, key string, fallback, min, max int) (int, error) {
	value, exists := settings[key]
	if !exists || rewriteParameterMissing(value) {
		return fallback, nil
	}
	raw, err := json.Marshal(value)
	var number float64
	if err != nil || json.Unmarshal(raw, &number) != nil || math.IsNaN(number) || math.IsInf(number, 0) || number != math.Trunc(number) || number < float64(min) || number > float64(max) {
		return 0, batchfactoryv11.NewConfigRequiredError(fmt.Sprintf("AI %s 必须是 %d 到 %d 的整数", key, min, max))
	}
	return int(number), nil
}
