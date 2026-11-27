package scripteditor

import (
	"context"
	"encoding/json"
	"strings"
)

type LinkedProductionJob struct {
	Stage    string `json:"stage"`
	Status   string `json:"status"`
	JobID    string `json:"jobId,omitempty"`
	TargetID string `json:"targetId,omitempty"`
}
type ProductionStatus struct {
	Total                     int                   `json:"total"`
	Running                   int                   `json:"running"`
	Succeeded                 int                   `json:"succeeded"`
	Failed                    int                   `json:"failed"`
	ResultPendingConfirmation int                   `json:"resultPendingConfirmation"`
	Jobs                      []LinkedProductionJob `json:"jobs"`
}

func AggregateProduction(rows []LinkedProductionJob) ProductionStatus {
	result := ProductionStatus{Jobs: rows, Total: len(rows)}
	for _, row := range rows {
		switch row.Status {
		case "queued", "running", "pending":
			result.Running++
		case "succeeded", "synced":
			result.Succeeded++
		case "failed":
			result.Failed++
		case "may_have_sent", "awaiting_confirmation":
			result.ResultPendingConfirmation++
		}
	}
	return result
}

func (s *Service) Production(ctx context.Context, owner, workID string) (ProductionStatus, error) {
	if _, err := s.Store.GetWork(ctx, owner, workID); err != nil {
		return ProductionStatus{}, err
	}
	rows := []LinkedProductionJob{}
	jobs, err := s.Store.ListJobs(ctx, owner, workID)
	if err != nil {
		return ProductionStatus{}, err
	}
	for _, job := range jobs {
		status := string(job.Status)
		var result JobResult
		_ = json.Unmarshal(job.Result, &result)
		for _, call := range result.Calls {
			if call.State == JobCallMayHaveSent {
				status = "may_have_sent"
				break
			}
		}
		rows = append(rows, LinkedProductionJob{Stage: stageFromType(job.Type), Status: status, JobID: job.ID})
	}
	if links, ok := s.Store.(BindingStore); ok {
		linked, linkErr := links.ListJobLinks(ctx, owner, workID, "")
		if linkErr != nil {
			return ProductionStatus{}, linkErr
		}
		for _, link := range linked {
			rows = append(rows, LinkedProductionJob{Stage: stageFromType(link.TargetType), Status: string(link.SyncState), JobID: link.JobID, TargetID: link.TargetID})
		}
	}
	return AggregateProduction(rows), nil
}

func stageFromType(value string) string {
	value = strings.ToLower(value)
	for _, stage := range []string{"image", "video", "voice", "audio", "subtitle", "cut", "export"} {
		if strings.Contains(value, stage) {
			if stage == "audio" {
				return "voice"
			}
			return stage
		}
	}
	return "export"
}
