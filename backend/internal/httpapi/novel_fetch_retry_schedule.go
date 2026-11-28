package httpapi

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
)

// retry_schedule 复刻参考实现 task_retry_schedule：每个处理阶段单独记录
// 重试次数、下次重试时间、最后一次错误和是否需要人工处理。阶段化记录让
// 原文、敏感词、分类、改文四类失败不会互相覆盖退避时间，也让运维能直接
// 看到任务卡在哪一步。
const (
	novelFetchRetryStageOriginalFetch = "original_fetch"
	novelFetchRetryStageSensitive     = "sensitive"
	novelFetchRetryStageClassify      = "classify"
	novelFetchRetryStageRewrite       = "rewrite"
	novelFetchRetryStagePipeline      = "pipeline"
)

// 与参考实现 TASK_RETRY_DELAYS_SECONDS 一致（30/120/300/900/1800 秒）。
var novelFetchTaskRetryDelays = []time.Duration{
	30 * time.Second,
	2 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
}

// A confirmed malformed response gets one delayed retry within the same round.
var novelFetchAIOutputRetryDelays = []time.Duration{30 * time.Second}

var novelFetchAIRetryDelayPattern = regexp.MustCompile(`(?:约|请)\s*(\d+(?:\.\d+)?)\s*秒后重试`)

// novelFetchAIRetryDelayHintSeconds 复刻参考实现 ai_retry_delay_hint_seconds：
// 如果错误文本里带有“约 X 秒后重试”的明确等待时间，调度就至少等待该时间，
// 避免在服务端限流窗口内反复请求。Go 侧目前没有熔断文案，取不到时返回 0。
func novelFetchAIRetryDelayHintSeconds(failure error) time.Duration {
	if failure == nil {
		return 0
	}
	match := novelFetchAIRetryDelayPattern.FindStringSubmatch(failure.Error())
	if len(match) < 2 {
		return 0
	}
	seconds, err := strconv.ParseFloat(match[1], 64)
	if err != nil || seconds <= 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

// A typed validation failure is a confirmed bad output and shares the single
// delayed repair budget with legacy output-rejection markers.
func novelFetchAIErrorIsOutputRejection(failure error) bool {
	if failure == nil {
		return false
	}
	if errors.Is(failure, novelfetchworkshop.ErrRewriteValidation) || errors.Is(failure, batchfactoryv11.ErrTextEmpty) {
		return true
	}
	text := strings.ToLower(failure.Error())
	for _, marker := range []string{"ai 返回异常", "ai_output_rejected", "invalid_json", "incomplete_prose", "文本模型返回了空正文"} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// novelFetchParseRetryTime 兼容 RFC3339Nano（Go 写入）和参考实现不带时区的
// ISO 秒级时间。
func novelFetchParseRetryTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

// novelFetchRetrySchedule 读取并复制阶段重试表。文档持久化后嵌套值会变成
// map[string]any，这里同时兼容两种形态。
func novelFetchRetrySchedule(meta map[string]any) map[string]map[string]any {
	schedule := map[string]map[string]any{}
	if meta == nil {
		return schedule
	}
	switch raw := meta["retry_schedule"].(type) {
	case map[string]map[string]any:
		for stage, state := range raw {
			key := strings.TrimSpace(stage)
			if key == "" || state == nil {
				continue
			}
			schedule[key] = copyAnyMap(state)
		}
	case map[string]any:
		for stage, value := range raw {
			key := strings.TrimSpace(stage)
			if key == "" {
				continue
			}
			if state, ok := value.(map[string]any); ok {
				schedule[key] = copyAnyMap(state)
			}
		}
	}
	return schedule
}

// refreshNovelFetchTaskRetrySummary 复刻参考实现 refresh_task_retry_summary：
// 把最早到期的阶段汇总成 retry_stage/retry_count/next_retry_at 等字段，供
// 前端和外部排障直接读取。
func refreshNovelFetchTaskRetrySummary(meta map[string]any) {
	if meta == nil {
		return
	}
	schedule := novelFetchRetrySchedule(meta)
	if len(schedule) == 0 {
		for _, key := range []string{"retry_schedule", "retry_stage", "retry_count", "next_retry_at", "last_retry_error"} {
			delete(meta, key)
		}
		return
	}
	stages := make([]string, 0, len(schedule))
	for stage := range schedule {
		stages = append(stages, stage)
	}
	sort.SliceStable(stages, func(i, j int) bool {
		left, leftOK := novelFetchParseRetryTime(firstText(schedule[stages[i]]["next_retry_at"]))
		right, rightOK := novelFetchParseRetryTime(firstText(schedule[stages[j]]["next_retry_at"]))
		switch {
		case leftOK && rightOK:
			return left.Before(right)
		case leftOK:
			return true
		case rightOK:
			return false
		default:
			return stages[i] < stages[j]
		}
	})
	stage := stages[0]
	state := schedule[stage]
	meta["retry_schedule"] = schedule
	meta["retry_stage"] = stage
	meta["retry_count"] = intValue(state["attempt"])
	meta["next_retry_at"] = firstText(state["next_retry_at"])
	meta["last_retry_error"] = firstText(state["error"])
}

// refreshNovelFetchAIManualRetryHold 复刻参考实现 refresh_ai_manual_retry_hold。
func refreshNovelFetchAIManualRetryHold(meta map[string]any) {
	if meta == nil {
		return
	}
	schedule := novelFetchRetrySchedule(meta)
	stages := make([]string, 0, len(schedule))
	for stage, state := range schedule {
		if state["manual_only"] == true || state["manualOnly"] == true {
			stages = append(stages, stage)
		}
	}
	if len(stages) == 0 {
		delete(meta, "manual_ai_retry_required")
		delete(meta, "manual_ai_retry_stage")
		delete(meta, "manual_ai_retry_error")
		return
	}
	sort.Strings(stages)
	stage := stages[0]
	meta["manual_ai_retry_required"] = true
	meta["manual_ai_retry_stage"] = stage
	meta["manual_ai_retry_error"] = firstText(schedule[stage]["error"])
}

func novelFetchRetryStateIsManual(state map[string]any) bool {
	return state["manual_only"] == true || state["manualOnly"] == true
}

func novelFetchRetryScheduleHasManualHold(meta map[string]any) bool {
	for _, state := range novelFetchRetrySchedule(meta) {
		if novelFetchRetryStateIsManual(state) {
			return true
		}
	}
	return false
}

// scheduleNovelFetchTaskRetry 复刻参考实现 schedule_task_retry：按阶段累计
// 尝试次数并写入下一次可重试时间。模型确定性失败写 manual_only，扫描器
// 不再自动接管，等待用户点击重试。
func scheduleNovelFetchTaskRetry(meta map[string]any, stage string, failure error, retryAfter time.Duration, now time.Time) map[string]any {
	if meta == nil {
		return nil
	}
	stage = strings.TrimSpace(stage)
	if stage == "" {
		stage = novelFetchRetryStagePipeline
	}
	schedule := novelFetchRetrySchedule(meta)
	previous := schedule[stage]
	attempt := intValue(previous["attempt"]) + 1
	errorText := ""
	if failure != nil {
		errorText = batchfactoryv11.SafeProviderErrorText(strings.TrimSpace(failure.Error()))
	}
	firstAttempt := firstText(previous["first_attempt_at"])
	if firstAttempt == "" && intValue(previous["attempt"]) == 0 {
		firstAttempt = now.UTC().Format(time.RFC3339Nano)
	}
	outputRejection := novelFetchAIErrorIsOutputRejection(failure)
	outputRejections := intValue(previous["output_rejections"])
	if outputRejection {
		outputRejections++
	}
	limit := novelFetchRetryLimit(stage)
	if novelFetchRetryIsAIStage(stage) {
		if configured := intValue(meta["ai_retry_max_failures"]); configured > 0 && configured < limit {
			limit = configured
		}
		if saved := intValue(previous["max_failures"]); saved > 0 && saved < limit {
			limit = saved
		}
	}
	budget := map[string]any{"attempt": attempt, "first_attempt_at": firstAttempt, "output_rejections": outputRejections, "max_failures": limit}
	if novelFetchRetryStateExhausted(stage, previous, now) || novelFetchRetryStateExhausted(stage, budget, now) || (stage != novelFetchRetryStageOriginalFetch && novelFetchAIManualRetryRequired(failure)) || novelFetchRetryConfigurationFailure(failure) {
		state := map[string]any{
			"attempt":           attempt,
			"next_retry_at":     "",
			"error":             errorText,
			"manual_only":       true,
			"updated_at":        now.UTC().Format(time.RFC3339Nano),
			"first_attempt_at":  firstAttempt,
			"output_rejections": outputRejections,
			"max_failures":      limit,
		}
		schedule[stage] = state
		meta["retry_schedule"] = schedule
		refreshNovelFetchTaskRetrySummary(meta)
		refreshNovelFetchAIManualRetryHold(meta)
		return state
	}
	delays := novelFetchTaskRetryDelays
	if outputRejection {
		delays = novelFetchAIOutputRetryDelays
	}
	index := attempt - 1
	if index < 0 {
		index = 0
	}
	if index >= len(delays) {
		index = len(delays) - 1
	}
	delay := delays[index]
	if retryAfter > delay {
		delay = retryAfter
	}
	state := map[string]any{
		"attempt":           attempt,
		"next_retry_at":     now.Add(delay).UTC().Format(time.RFC3339Nano),
		"error":             errorText,
		"updated_at":        now.UTC().Format(time.RFC3339Nano),
		"first_attempt_at":  firstAttempt,
		"output_rejections": outputRejections,
		"max_failures":      limit,
	}
	if outputRejection && delay <= 0 {
		state["immediate"] = true
	}
	schedule[stage] = state
	meta["retry_schedule"] = schedule
	refreshNovelFetchTaskRetrySummary(meta)
	refreshNovelFetchAIManualRetryHold(meta)
	return state
}

// clearNovelFetchTaskRetry 复刻参考实现 clear_task_retry。stage 为空时清空
// 全部阶段，用于任务成功完成或用户主动重试。
func clearNovelFetchTaskRetry(meta map[string]any, stage string) {
	if meta == nil {
		return
	}
	schedule := novelFetchRetrySchedule(meta)
	if key := strings.TrimSpace(stage); key != "" {
		delete(schedule, key)
	} else {
		schedule = map[string]map[string]any{}
	}
	if len(schedule) == 0 {
		delete(meta, "retry_schedule")
	} else {
		meta["retry_schedule"] = schedule
	}
	refreshNovelFetchTaskRetrySummary(meta)
	refreshNovelFetchAIManualRetryHold(meta)
}

// releaseNovelFetchAIManualRetryHold 复刻参考实现
// release_ai_manual_retry_hold：用户主动重试时释放人工保持的阶段。
func releaseNovelFetchAIManualRetryHold(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	schedule := novelFetchRetrySchedule(meta)
	released := false
	for stage, state := range schedule {
		if novelFetchRetryStateIsManual(state) {
			delete(schedule, stage)
			released = true
		}
	}
	if !released && meta["manual_ai_retry_required"] != true {
		return false
	}
	if len(schedule) == 0 {
		delete(meta, "retry_schedule")
	} else {
		meta["retry_schedule"] = schedule
	}
	refreshNovelFetchTaskRetrySummary(meta)
	refreshNovelFetchAIManualRetryHold(meta)
	return true
}

// A missing schedule permits first execution. Every persisted stage must have
// a valid due time and remaining budget; invalid dates require manual handling.
func novelFetchTaskRetryDue(meta map[string]any, stage string, now time.Time) bool {
	schedule := novelFetchRetrySchedule(meta)
	states := make(map[string]map[string]any, len(schedule))
	if key := strings.TrimSpace(stage); key != "" {
		if state, ok := schedule[key]; ok {
			states[key] = state
		}
	} else {
		for key, state := range schedule {
			states[key] = state
		}
	}
	if len(states) == 0 {
		return true
	}
	for key, state := range states {
		if novelFetchRetryStateExhausted(key, state, now) {
			return false
		}
		dueAt, ok := novelFetchParseRetryTime(firstText(state["next_retry_at"]))
		if !ok || dueAt.After(now) {
			return false
		}
	}
	return true
}

// novelFetchRetryStageForError 把处理链路返回的错误归到对应阶段。
func novelFetchRetryStageForError(failure error) string {
	if failure == nil {
		return novelFetchRetryStagePipeline
	}
	text := strings.ToLower(failure.Error())
	switch {
	case strings.Contains(text, "敏感词"):
		return novelFetchRetryStageSensitive
	case strings.Contains(text, "分类"), strings.Contains(text, "性别"), strings.Contains(text, "题材"):
		return novelFetchRetryStageClassify
	default:
		return novelFetchRetryStageRewrite
	}
}

// scheduleNovelFetchDocumentRetry 只更新重试表，不改写任务状态，便于调用方
// 继续维护自己更精确的状态文案（例如 waiting_classification）。
func scheduleNovelFetchDocumentRetry(ctx context.Context, store novelfetchworkshop.Store, owner, bookID, stage string, failure error, now time.Time) error {
	document, err := store.GetDocument(ctx, owner, bookID)
	if err != nil {
		return err
	}
	if document.Meta == nil {
		document.Meta = map[string]any{}
	}
	// Classification already records its one-shot failure with the result CAS.
	// The worker may observe that same failure; do not count it a second time.
	if stage == novelFetchRetryStageClassify && novelFetchRetryStateIsManual(novelFetchRetrySchedule(document.Meta)[stage]) {
		return nil
	}
	if novelFetchRetryIsAIStage(stage) {
		if err := applyNovelFetchAIStageBudget(ctx, store, owner, document.Meta); err != nil {
			return err
		}
	}
	scheduleNovelFetchTaskRetry(document.Meta, stage, failure, novelFetchAIRetryDelayHintSeconds(failure), now)
	return store.PutDocument(ctx, owner, document)
}

// novelFetchResolvedPlatformID 复刻参考实现 web_platform_id：优先使用显式
// 平台 ID；只有平台名称时按平台目录自动换算出 ID；都取不到才回退默认值。
// 直接回退 "2" 会把只带名称的任务发到错误书城。
func novelFetchResolvedPlatformID(settings map[string]any, meta map[string]any) string {
	if id := strings.TrimSpace(firstText(meta["platformId"], meta["platform_id"])); id != "" {
		return id
	}
	name := strings.TrimSpace(firstText(meta["platformName"], meta["platform_name"]))
	if name != "" {
		if settings == nil {
			settings = map[string]any{}
		}
		for _, raw := range novelFetchPlatformCatalog(settings) {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(fmt.Sprint(row["name"])), name) {
				continue
			}
			if id := strings.TrimSpace(fmt.Sprint(row["id"])); id != "" && id != "<nil>" {
				return id
			}
		}
	}
	return "2"
}
