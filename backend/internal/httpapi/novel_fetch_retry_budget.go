package httpapi

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
)

// attempt counts failed executions, including the first execution. A normal
// round allows three failures (initial execution plus at most two retries).
// The existing original-fetch policy allows five retries after its first call.
const novelFetchStageMaxFailures = 3
const novelFetchRetryRoundLifetime = 24 * time.Hour
const novelFetchRetryStageUpload = "upload"

func novelFetchRetryResultUnknown(failure error) bool {
	if failure == nil {
		return false
	}
	if errors.Is(failure, context.DeadlineExceeded) || errors.Is(failure, context.Canceled) {
		return true
	}
	var network net.Error
	if errors.As(failure, &network) {
		return true
	}
	text := strings.ToLower(failure.Error())
	for _, marker := range []string{"超时", "timeout", "timed out", "deadline exceeded", "context canceled", "connection reset", "connection refused", "broken pipe", "unexpected eof", "结果未知", "响应丢失", "未确认"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func novelFetchRetryConfigurationFailure(failure error) bool {
	if failure == nil {
		return false
	}
	text := strings.ToLower(failure.Error())
	for _, marker := range []string{"未配置", "配置不可用", "配置错误", "未登录", "登录已失效", "unauthorized", "forbidden", "invalid api key", "http 401", "http 403", "not found", "来源无效", "参数无效"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

func novelFetchRetryLimit(stage string) int {
	if stage == novelFetchRetryStageOriginalFetch {
		return novelFetchOriginalFetchMaxRetries + 1
	}
	if stage == novelFetchRetryStageClassify {
		return 1
	}
	return novelFetchStageMaxFailures
}

func novelFetchRetryStateExhausted(stage string, state map[string]any, now time.Time) bool {
	if novelFetchRetryStateIsManual(state) || intValue(state["attempt"]) >= novelFetchRetryLimit(stage) {
		return true
	}
	if intValue(state["output_rejections"]) > 1 {
		return true
	}
	if raw := firstText(state["first_attempt_at"]); raw != "" {
		first, ok := novelFetchParseRetryTime(raw)
		if !ok || first.After(now) || !now.Before(first.Add(novelFetchRetryRoundLifetime)) {
			return true
		}
	}
	return false
}
