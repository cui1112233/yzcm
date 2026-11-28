package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	txt "qiantie/backend/internal/batchfactorytext"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/localartifact"
	"time"
)

// Keep the shared workflow, but give later phases their own usable budget.
func automationTextStageTimeout(stage string) time.Duration {
	switch stage {
	case "text_render", "text_publish":
		return 45 * time.Minute
	default:
		return 10 * time.Minute
	}
}

// Only local, reusable work is automatically resumed. Processing may call a model,
// and an upload timeout may hide a committed remote publication: neither is retried here.
func scheduleTextLocalRecovery(entry map[string]any, stage string, err error, now time.Time) bool {
	if errors.Is(err, txt.ErrDuplicatePageImage) {
		attempt := automationNumberValue(entry["textRecoveryAttempts"], 0)
		if attempt >= 3 {
			return false
		}
		const delay = 5 * time.Second
		entry["textRecoveryAttempts"] = attempt + 1
		entry["status"], entry["stage"], entry["error"], entry["updatedAt"] = "scheduled", "text_render", err.Error(), now
		entry["retryRequested"], entry["retryAt"] = true, now.Add(delay)
		entry["message"] = "检测到空白或重复文字页，正在重新渲染可读文字；不会复用错误合集"
		delete(entry, "textPendingPublishCollectionId")
		delete(entry, "textCollectionId")
		delete(entry, "needsManual")
		return true
	}
	switch stage {
	case "text_render", "text_validate", "text_collection":
	default:
		return false
	}
	retryable := retryableTextLocalRead(err) || errors.Is(err, txt.ErrConflict) || errors.Is(err, batchfactoryv11.ErrConflict)
	attempt := automationNumberValue(entry["textRecoveryAttempts"], 0)
	if !retryable || attempt >= 3 {
		return false
	}
	delay := 30 * time.Second * time.Duration(1<<attempt)
	entry["textRecoveryAttempts"] = attempt + 1
	entry["status"], entry["stage"], entry["error"], entry["updatedAt"] = "scheduled", stage, err.Error(), now
	entry["retryRequested"], entry["retryAt"] = true, now.Add(delay)
	entry["message"] = fmt.Sprintf("%s；已有图片保留，%d秒后从未完成步骤续跑（%d/3）", err.Error(), int(delay.Seconds()), attempt+1)
	delete(entry, "needsManual")
	return true
}

func retryableTextLocalRead(err error) bool {
	var temporary net.Error
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, localartifact.ErrRestoreTimeout) || (errors.As(err, &temporary) && (temporary.Timeout() || temporary.Temporary()))
}

func automationProgressStalledInStage(last, now time.Time, stage string) bool {
	if !automationProgressStalled(last, now) {
		return false
	}
	// Let the text phase's own deadline fire first; an upload may not emit page heartbeats.
	if stage == "text_render" || stage == "text_publish" {
		return now.Sub(last) >= automationTextStageTimeout(stage)+automationStallWatchInterval
	}
	return true
}
