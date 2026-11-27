package httpapi

import (
	"errors"
	"qiantie/backend/internal/batchfactoryv11"
	"time"
)

func (c *BatchAutomationCoordinator) scheduleMultiModeSourceWait(entry map[string]any, err error, persist func()) bool {
	if !errors.Is(err, batchfactoryv11.ErrMultiModeSourcePending) {
		return false
	}
	entry["status"], entry["stage"], entry["message"], entry["updatedAt"] = "scheduled", "fetch_original", "等待共享原文读取任务回填，完成后继续各自生产", c.now()
	entry["retryRequested"] = true
	entry["retryAt"] = c.now().Add(time.Minute)
	delete(entry, "needsManual")
	delete(entry, "error")
	persist()
	return true
}
