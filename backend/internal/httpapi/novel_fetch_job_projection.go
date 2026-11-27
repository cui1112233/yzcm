package httpapi

import (
	"context"
	"strings"

	"qiantie/backend/internal/novelfetchworkshop"
)

// Historical run rows are audit records, not proof that a queue still owns work.
// This projection never rewrites history, stops work or enqueues a retry.
func verifiedWorkshopJobView(ctx context.Context, store novelfetchworkshop.Store, queue novelfetchworkshop.ProcessQueue, owner string, view map[string]any) map[string]any {
	view = manualWorkshopJobView(ctx, store, owner, view)
	switch strings.ToLower(firstText(view["status"])) {
	case "queued", "running", "processing", "waiting_retry":
	default:
		return view
	}
	checker, ok := queue.(interface {
		ProcessJobExists(context.Context, string) (bool, error)
	})
	if !ok || firstText(view["id"]) == "" {
		return view
	}
	exists, err := checker.ProcessJobExists(ctx, firstText(view["id"]))
	checked := cloneAnyMap(view)
	if err != nil {
		checked["queue_verified"] = false
		checked["queue_check_error"] = "队列状态暂时无法核对，请稍后刷新。"
		return checked
	}
	checked["queue_verified"] = true
	if !exists {
		checked["historical_status"] = view["status"]
		checked["status"], checked["queue_state"] = "failed", "failed"
		checked["status_reason"] = "queue_missing"
		checked["error"] = "原队列任务已不存在，处理已中断；请确认书籍任务后手动重试。"
	}
	return checked
}
