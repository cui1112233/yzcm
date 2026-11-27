package batchfactoryv11

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type memoryMultiModeTarget struct {
	BatchID, BookID, SourceKey, Status string
	Revision                           int64
}
type memoryMultiModeGroup struct {
	Owner, Digest, RequestID string
	Result                   MultiModeCreationResult
	Sources                  []MultiModeSourceJob
	Targets                  []memoryMultiModeTarget
}

func (s *MemoryStore) CreateMultiModeBatches(ctx context.Context, owner string, input CreateMultiModeInput) (MultiModeCreationResult, error) {
	input, digest, err := normalizeMultiModeInput(input)
	if err != nil {
		return MultiModeCreationResult{}, err
	}
	if strings.TrimSpace(owner) == "" {
		return MultiModeCreationResult{}, ErrInvalid
	}
	s.multiModeMu.Lock()
	defer s.multiModeMu.Unlock()
	if s.multiModeGroups == nil {
		s.multiModeGroups = map[string]*memoryMultiModeGroup{}
	}
	for _, g := range s.multiModeGroups {
		if g.Owner == owner && g.RequestID == input.RequestID {
			if g.Digest != digest {
				return MultiModeCreationResult{}, ErrConflict
			}
			return s.readMemoryMultiMode(ctx, g)
		}
	}
	groupID, err := newID("group")
	if err != nil {
		return MultiModeCreationResult{}, err
	}
	g := &memoryMultiModeGroup{Owner: owner, Digest: digest, RequestID: input.RequestID}
	g.Result.GroupID = groupID
	g.Sources = newMultiModeSources(input, groupID)
	for _, project := range input.Projects {
		batch, createErr := s.CreateBatch(ctx, owner, multiModeBatchInput(input, project, groupID))
		if createErr != nil {
			return MultiModeCreationResult{}, createErr
		}
		g.Result.Batches = append(g.Result.Batches, batch)
		g.Result.Projects = append(g.Result.Projects, MultiModeProject{Mode: project.Mode, BatchID: batch.ID, PresetID: project.PresetID, ExecutionMode: project.ExecutionMode})
		for i, book := range batch.Books {
			g.Targets = append(g.Targets, memoryMultiModeTarget{BatchID: batch.ID, BookID: book.ID, SourceKey: multiModeSourceKey(input.Books[i]), Status: "pending", Revision: book.Revision})
		}
	}
	s.multiModeGroups[groupID] = g
	return s.readMemoryMultiMode(ctx, g)
}
func (s *MemoryStore) readMemoryMultiMode(ctx context.Context, g *memoryMultiModeGroup) (MultiModeCreationResult, error) {
	result := MultiModeCreationResult{GroupID: g.Result.GroupID, Projects: append([]MultiModeProject(nil), g.Result.Projects...)}
	for _, p := range result.Projects {
		batch, err := s.GetBatch(ctx, g.Owner, p.BatchID)
		if err != nil {
			return result, err
		}
		raw, _ := json.Marshal(batch)
		var copy Batch
		_ = json.Unmarshal(raw, &copy)
		result.Batches = append(result.Batches, copy)
	}
	return result, nil
}
func (s *MemoryStore) GetMultiModeCreation(ctx context.Context, o, g string) (MultiModeCreationResult, error) {
	s.multiModeMu.Lock()
	defer s.multiModeMu.Unlock()
	group, ok := s.multiModeGroups[g]
	if !ok || group.Owner != o {
		return MultiModeCreationResult{}, ErrNotFound
	}
	return s.readMemoryMultiMode(ctx, group)
}
func (s *MemoryStore) ListMultiModeSources(_ context.Context, o, g string) ([]MultiModeSourceJob, error) {
	s.multiModeMu.Lock()
	defer s.multiModeMu.Unlock()
	group, ok := s.multiModeGroups[g]
	if !ok || group.Owner != o {
		return nil, ErrNotFound
	}
	raw, _ := json.Marshal(group.Sources)
	var out []MultiModeSourceJob
	_ = json.Unmarshal(raw, &out)
	return out, nil
}
func (s *MemoryStore) ClaimMultiModeSource(_ context.Context, o, g, k string) (MultiModeSourceJob, bool, error) {
	s.multiModeMu.Lock()
	defer s.multiModeMu.Unlock()
	group, ok := s.multiModeGroups[g]
	if !ok || group.Owner != o {
		return MultiModeSourceJob{}, false, ErrNotFound
	}
	for i := range group.Sources {
		job := &group.Sources[i]
		if job.Key != k {
			continue
		}
		if job.Status == "ready" || job.Status == "fetching" && time.Now().Before(job.LeaseUntil) {
			return *job, false, nil
		}
		token, err := newID("source")
		if err != nil {
			return *job, false, err
		}
		job.Status, job.Token, job.LeaseUntil = "fetching", token, time.Now().Add(10*time.Minute)
		return *job, true, nil
	}
	return MultiModeSourceJob{}, false, ErrNotFound
}
func (s *MemoryStore) CompleteMultiModeSource(_ context.Context, o, g, k, token string, snapshot MultiModeSourceSnapshot, workErr error) error {
	s.multiModeMu.Lock()
	defer s.multiModeMu.Unlock()
	group, ok := s.multiModeGroups[g]
	if !ok || group.Owner != o {
		return ErrNotFound
	}
	for i := range group.Sources {
		job := &group.Sources[i]
		if job.Key != k {
			continue
		}
		if job.Token != token || job.Status != "fetching" {
			return ErrConflict
		}
		job.Status = "ready"
		job.Snapshot = snapshot
		job.Error = ""
		if workErr != nil {
			job.Status, job.Error = "failed", workErr.Error()
			if errors.Is(workErr, ErrMultiModeSourcePending) {
				job.Status = "pending"
			}
		}
		return nil
	}
	return ErrNotFound
}
func (s *MemoryStore) DeliverMultiModeSources(ctx context.Context, o, g string) error {
	s.multiModeMu.Lock()
	defer s.multiModeMu.Unlock()
	group, ok := s.multiModeGroups[g]
	if !ok || group.Owner != o {
		return ErrNotFound
	}
	for i := range group.Targets {
		target := &group.Targets[i]
		if target.Status != "pending" {
			continue
		}
		var source *MultiModeSourceJob
		for j := range group.Sources {
			if group.Sources[j].Key == target.SourceKey && group.Sources[j].Status == "ready" {
				source = &group.Sources[j]
				break
			}
		}
		if source == nil {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		s.mu.Lock()
		owned, exists := s.batches[target.BatchID]
		if !exists || owned.Owner != o {
			s.mu.Unlock()
			return ErrNotFound
		}
		for j := range owned.Value.Books {
			book := &owned.Value.Books[j]
			if book.ID != target.BookID {
				continue
			}
			manual := book.Revision != target.Revision
			metadata := multiModeSourceMetadata(source.Snapshot.Metadata)
			if manual {
				for key, value := range book.SourceMetadata {
					if value != "" {
						metadata[key] = value
					}
				}
			}
			preserveMultiModeControls(metadata, book.SourceMetadata)
			metadata["multimodeGroupId"] = g
			metadata["multimodeSourceKey"] = target.SourceKey
			metadata["multiModeSourceState"] = "delivered"
			metadata["rawSourceStatus"] = "verified"
			metadata["contentPending"] = false
			book.SourceMetadata = metadata
			if !manual || strings.TrimSpace(book.SourceText) == "" && strings.TrimSpace(book.TxtText) == "" {
				book.SourceText = source.Snapshot.OriginalRaw
				book.TxtText = source.Snapshot.OriginalRaw
			}
			if source.Snapshot.Title != "" && !manual {
				book.Title = source.Snapshot.Title
			}
			book.Revision++
			target.Status = "delivered"
		}
		s.batches[target.BatchID] = owned
		s.mu.Unlock()
	}
	return nil
}
