package screenplay

import (
	"context"
	"fmt"
	"qiantie/backend/internal/yzcmusage"
	"time"
)

// ProductionAuthorization is frozen from trusted authentication, never request JSON.
// RunID separates a restarted production from late results of an older worker.
type ProductionAuthorization struct {
	Subject string `json:"subject"`
	TeamID  string `json:"team_id,omitempty"`
	Actor   string `json:"actor"`
	RunID   string `json:"run_id"`
}
type productionAuthorizationKey struct{}

func WithProductionAuthorization(ctx context.Context, subject, team, actor string) context.Context {
	return context.WithValue(ctx, productionAuthorizationKey{}, ProductionAuthorization{Subject: subject, TeamID: team, Actor: actor})
}

type ProductionStoryboardGeneration struct {
	RequestKey string `json:"request_key"`
	RetryOf    string `json:"retry_of,omitempty"`
	ChapterID  string `json:"chapter_id"`
	Status     string `json:"status"`
}

func (e BatchExecutor) executeStoryboard138(ctx context.Context, owner string, p Project, unit Unit, regenerate bool, recovery ...*ProductionStoryboardGeneration) (batchUnitOutcome, error) {
	if e.Authoring == nil || e.Authoring.Receipts == nil {
		return batchUnitOutcome{}, fmt.Errorf("%w: 138生产需要已保存章节与回包目录", ErrInvalidState)
	}
	b, err := e.Store.GetBatch(ctx, owner, p.ID)
	if err != nil {
		return batchUnitOutcome{}, err
	}
	state, err := e.Authoring.Load(ctx, owner, p.ID)
	if err != nil {
		return batchUnitOutcome{}, err
	}
	n := unit.Episode
	if n <= 0 {
		n = 1
	}
	if n > len(state.Chapters) || state.Chapters[n-1].ID == "" {
		return batchUnitOutcome{}, fmt.Errorf("%w: 第%d集缺少已保存专业章节", ErrInvalidArgument, n)
	}
	ep := b.Step("5_storyboard").Episodes[unit.Episode]
	key := "batch-" + contentDigest(fmt.Sprintf("%s:%d:%d:%d", b.ID, b.PlanVersion, n, p.Revision))
	req := StoryboardRequest{Owner: owner, ProjectID: p.ID, Engine: StoryboardEngine("prompt_138"), PromptID: "luoshui_storyboard_138", EpisodeNo: n, ChapterID: state.Chapters[n-1].ID, IdempotencyKey: key, Style: p.ArtStyle, Synopsis: p.Synopsis, Model: p.ModelID, Confirmed: true}
	var progress *ProductionStoryboardGeneration
	if ep != nil {
		progress = ep.StoryboardGeneration
	}
	if len(recovery) > 0 {
		progress = recovery[0]
		req.recoveryOnly = true
	}
	if progress != nil {
		g := progress
		req.IdempotencyKey = g.RequestKey
		req.RetryOf = g.RetryOf
		req.ConfirmedRetry = g.RetryOf != ""
		req.ChapterID = g.ChapterID
	} else if board, ok := existingBatchStoryboard(ctx, e.Store, owner, p.ID, n); ok && !regenerate {
		return batchUnitOutcome{artifacts: []string{"storyboard://" + board.ID}}, nil
	}
	service := &StoryboardService{Store: e.Store, Generator: e.Generator, Authoring: e.Authoring}
	result, err := service.Generate(ctx, req)
	if err != nil && result.GenerationAttempt == nil {
		if gs, ok := e.Store.(StoryboardGenerationStore); ok {
			if state, loadErr := gs.LoadStoryboardGenerations(context.WithoutCancel(ctx), owner, p.ID); loadErr == nil {
				if attempt := state.Attempts[req.IdempotencyKey]; attempt != nil {
					summary := attempt.Summary()
					result.GenerationAttempt = &summary
				}
			}
		}
	}
	if result.GenerationAttempt != nil && result.GenerationAttempt.Status != "succeeded" {
		status := result.GenerationAttempt.Status
		for _, episode := range result.GenerationAttempt.Episodes {
			if episode.Status == "awaiting_continue" {
				status = "awaiting_continue"
			}
		}
		if result.GenerationAttempt.CanRecover {
			status = "unknown"
		}
		if err == nil {
			err = fmt.Errorf("%w: 分镜批次尚未完整", ErrPartialFailure)
		}
		return batchUnitOutcome{storyboardGeneration: &ProductionStoryboardGeneration{RequestKey: result.GenerationAttempt.RequestKey, RetryOf: result.GenerationAttempt.RetryOf, ChapterID: result.GenerationAttempt.ChapterID, Status: status}}, err
	}
	if err != nil {
		return batchUnitOutcome{}, err
	}
	artifacts := []string{}
	for _, board := range result.Storyboards {
		artifacts = append(artifacts, "storyboard://"+board.ID)
	}
	if len(artifacts) == 0 {
		return batchUnitOutcome{}, ErrArtifactInvalid
	}
	return batchUnitOutcome{artifacts: artifacts}, nil
}

// ContinueStoryboard138 records one explicit approval. The background worker
// reuses this key on every replay; repeated clicks cannot approve another batch.
func (r *ProductionRuntime) ContinueStoryboard138(ctx context.Context, owner, id, key string, confirmed bool) (ProductionJob, error) {
	if r == nil || r.Store == nil || r.Repository == nil {
		return ProductionJob{}, ErrInvalidState
	}
	if !confirmed || key == "" {
		return ProductionJob{}, ErrInvalidArgument
	}
	token := newScreenplayID("continue")
	control := "screenplay-production-control:" + owner + ":" + id
	if err := r.Store.AcquireLock(ctx, control, token, 30*time.Second); err != nil {
		return ProductionJob{}, err
	}
	defer r.Store.ReleaseLock(context.Background(), control, token)
	j, err := r.Repository.GetProductionJob(ctx, owner, id)
	if err != nil {
		return ProductionJob{}, err
	}
	if j.Status != "blocked" && j.Status != "paused" {
		return ProductionJob{}, fmt.Errorf("%w: 任务不在分镜等待状态", ErrInvalidState)
	}
	gs, ok := r.Store.(StoryboardGenerationStore)
	if !ok {
		return ProductionJob{}, ErrInvalidState
	}
	saved, err := gs.LoadStoryboardGenerations(ctx, owner, id)
	if err != nil {
		return ProductionJob{}, err
	}
	previous := saved.Attempts[key]
	if previous == nil || !previous.Summary().CanRetry || !hasAwaiting138(previous) {
		return ProductionJob{}, ErrInvalidState
	}
	auth, _ := ctx.Value(productionAuthorizationKey{}).(ProductionAuthorization)
	_, err = r.Store.MutateBatch(ctx, owner, id, func(b *Batch) error {
		a := b.ProductionAuthorization
		if a == nil || a.Subject != auth.Subject || a.TeamID != auth.TeamID || a.Actor != auth.Actor || auth.Actor == "" {
			return fmt.Errorf("%w: 缺少原生产发起人的可信授权", ErrAccessDenied)
		}
		st := b.Step("5_storyboard")
		if st == nil || b.CurrentStep != "5_storyboard" {
			return ErrInvalidState
		}
		ep := st.Episodes[b.CurrentEpisode]
		if ep == nil || ep.StoryboardGeneration == nil {
			return ErrInvalidState
		}
		g := ep.StoryboardGeneration
		// The same approval may finish a failed job-state write, never grant another batch.
		if st.Status == StepPending && g.Status == "approved" && g.RetryOf == key {
			return nil
		}
		if st.Status != StepBlocked {
			return ErrInvalidState
		}
		if g.RequestKey != key || g.Status != "awaiting_continue" {
			return ErrRevisionConflict
		}
		g.RetryOf = key
		g.RequestKey = "batch-continue-" + contentDigest(a.RunID+":"+key)
		g.Status = "approved"
		ep.Status = StepPending
		ep.Reason = ""
		st.Status = StepPending
		st.Error = ""
		b.Status = BatchRunning
		return nil
	})
	if err != nil {
		return ProductionJob{}, err
	}
	if err = r.Repository.SetProductionState(ctx, owner, id, "running", ""); err != nil {
		return ProductionJob{}, err
	}
	return r.Status(ctx, owner, id)
}

func ProductionAuthorizationFromContext(ctx context.Context) (ProductionAuthorization, bool) {
	a, ok := ctx.Value(productionAuthorizationKey{}).(ProductionAuthorization)
	return a, ok
}
func productionUsageContext(ctx context.Context, a *ProductionAuthorization) context.Context {
	ctx = WithProductionAuthorization(ctx, a.Subject, a.TeamID, a.Actor)
	ctx = yzcmusage.WithAttribution(ctx, a.Subject, a.TeamID, a.Actor)
	return yzcmusage.WithIdentity(ctx, a.Subject, "screenplay")
}

// RecoverProductionStoryboard138 parses only already returned receipts. Missing
// batches wait for a later explicit continue; no model is supplied here.
func (r *ProductionRuntime) RecoverProductionStoryboard138(ctx context.Context, owner, id, key string) (ProductionJob, error) {
	if r == nil || r.Store == nil || r.Repository == nil || r.Executor == nil {
		return ProductionJob{}, ErrInvalidState
	}
	token := newScreenplayID("recover-production")
	for _, lock := range []string{"screenplay-production-control:" + owner + ":" + id, "screenplay-batch:" + owner + ":" + id, "authoring:" + owner + ":" + id} {
		if err := r.Store.AcquireLock(ctx, lock, token, 15*time.Minute); err != nil {
			return ProductionJob{}, err
		}
		defer r.Store.ReleaseLock(context.Background(), lock, token)
	}
	j, err := r.Repository.GetProductionJob(ctx, owner, id)
	if err != nil {
		return ProductionJob{}, err
	}
	if j.Status != "blocked" && j.Status != "paused" {
		return ProductionJob{}, ErrInvalidState
	}
	b, err := r.Store.GetBatch(ctx, owner, id)
	if err != nil {
		return ProductionJob{}, err
	}
	auth, _ := ctx.Value(productionAuthorizationKey{}).(ProductionAuthorization)
	a := b.ProductionAuthorization
	if a == nil || a.Actor == "" || a.Actor != auth.Actor || a.Subject != auth.Subject || a.TeamID != auth.TeamID {
		return ProductionJob{}, ErrAccessDenied
	}
	st := b.Step("5_storyboard")
	if st == nil {
		return ProductionJob{}, ErrInvalidState
	}
	ep := st.Episodes[b.CurrentEpisode]
	if b.CurrentStep != "5_storyboard" || ep == nil || ep.StoryboardGeneration == nil || ep.StoryboardGeneration.RequestKey != key {
		return ProductionJob{}, ErrRevisionConflict
	}
	g := ep.StoryboardGeneration
	if g.Status != "unknown" && g.Status != "calling" && g.Status != "failed" && g.Status != "running" && g.Status != "cancelled" {
		return ProductionJob{}, ErrInvalidState
	}
	executor := r.Executor(owner)
	service := &StoryboardService{Store: r.Store, Authoring: executor.Authoring}
	if _, err = service.RecoverStoryboard138(ctx, owner, id, key); err != nil {
		return ProductionJob{}, err
	}
	recoveryKey := "batch-recover-" + contentDigest(a.RunID+":"+key)
	p, err := r.Store.GetProject(ctx, owner, id)
	if err != nil {
		return ProductionJob{}, err
	}
	executor.Generator = nil
	outcome, runErr := executor.executeStoryboard138(ctx, owner, p, Unit{StepKey: "5_storyboard", Episode: b.CurrentEpisode}, true, &ProductionStoryboardGeneration{RequestKey: recoveryKey, RetryOf: key, ChapterID: g.ChapterID})
	_, err = r.Store.MutateBatch(ctx, owner, id, func(b *Batch) error {
		st := b.Step("5_storyboard")
		ep := st.Episodes[b.CurrentEpisode]
		if outcome.storyboardGeneration != nil {
			ep.StoryboardGeneration = outcome.storyboardGeneration
			ep.Status = StepBlocked
			st.Status = StepBlocked
			b.Status = BatchWaitingUser
			if j.Status == "paused" {
				b.Status = BatchPaused
			}
			return nil
		}
		if runErr != nil {
			return runErr
		}
		ep.StoryboardGeneration = &ProductionStoryboardGeneration{RequestKey: recoveryKey, RetryOf: key, ChapterID: g.ChapterID, Status: "succeeded"}
		CompleteStep(b, Unit{StepKey: "5_storyboard", Episode: b.CurrentEpisode}, outcome.artifacts)
		if j.Status == "paused" {
			b.Status = BatchPaused
		}
		return nil
	})
	if err != nil {
		return ProductionJob{}, err
	}
	nextStatus := "blocked"
	if outcome.storyboardGeneration == nil {
		nextStatus = "running"
	}
	if j.Status == "paused" {
		nextStatus = "paused"
	}
	if err = r.Repository.SetProductionState(ctx, owner, id, nextStatus, ""); err != nil {
		return ProductionJob{}, err
	}
	return r.Status(ctx, owner, id)
}

func production138Selected(ctx context.Context, store Store, owner string, p Project, episode int) bool {
	if episode <= 0 {
		episode = 1
	}
	if p.CreativeConfig != nil && p.CreativeConfig.StoryboardPromptID == "luoshui_storyboard_138" {
		return true
	}
	if b, err := store.GetBatch(ctx, owner, p.ID); err == nil {
		if st := b.Step("5_storyboard"); st != nil {
			if ep := st.Episodes[episode]; ep != nil && ep.StoryboardGeneration != nil {
				return true
			}
		}
	}
	return false
}

func productionHasAuthorization(ctx context.Context, store Store, owner, id string) bool {
	b, err := store.GetBatch(ctx, owner, id)
	return err == nil && b.ProductionAuthorization != nil
}
