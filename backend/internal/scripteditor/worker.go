package scripteditor

import (
	"context"
	"time"
)

func StartWorker(ctx context.Context, service *Service, interval time.Duration) {
	if service == nil || service.Store == nil {
		return
	}
	if interval <= 0 {
		interval = time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if service.JobQueue != nil {
				if id, err := service.JobQueue.Dequeue(ctx, 0); err == nil && id != "" {
					if job, getErr := findRunnableByID(ctx, service.Store, id); getErr == nil {
						go service.RunJob(ctx, job.OwnerUsername, job.ID)
					}
				}
			}
			if rows, err := service.Store.ListRunnableJobs(ctx, 32); err == nil {
				for _, row := range rows {
					row := row
					go service.RunJob(ctx, row.OwnerUsername, row.ID)
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func findRunnableByID(ctx context.Context, store Store, id string) (Job, error) {
	rows, err := store.ListRunnableJobs(ctx, 128)
	if err != nil {
		return Job{}, err
	}
	for _, row := range rows {
		if row.ID == id {
			return row, nil
		}
	}
	return Job{}, ErrNotFound
}
