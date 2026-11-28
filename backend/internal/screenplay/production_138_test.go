package screenplay

import (
	"context"
	"fmt"
	"qiantie/backend/internal/yzcmusage"
	"strings"
	"testing"
)

func TestProduction138SavedChapterReachesOneBatchAndWaits(t *testing.T) {
	s, store, g, _ := newStoryboard138(t)
	g.responses = []string{storyboard138Response("1-1-001")}
	p, _ := store.GetProject(context.Background(), "alice", "p1")
	p.CreativeConfig = &CreativeConfig{StoryboardPromptID: "luoshui_storyboard_138"}
	if _, err := store.SaveCreativeConfig(context.Background(), "alice", "p1", *p.CreativeConfig, p.Revision); err != nil {
		t.Fatal(err)
	}
	b := NewBatch("production138", "p1", "alice", "production", PermissionBypass)
	for _, d := range StepDefinitions[:5] {
		b.Step(d.Key).Status = StepDone
	}
	b.Step("5_storyboard").TotalEpisodes = 1
	if err := store.CreateBatch(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	e := BatchExecutor{Store: store, Generator: g, Authoring: s.Authoring}
	_ = s
	out, err := e.RunNext(context.Background(), "alice", "p1")
	if err != nil || g.callCount() != 1 || out.Batch.Status != BatchWaitingUser {
		t.Fatalf("must call once then wait: %+v err=%v calls=%d", out, err, g.callCount())
	}
}

func production138Fixture(t *testing.T) (*ProductionRuntime, *StoryboardService, *MemoryStore, *fakeGenerator, context.Context) {
	t.Helper()
	s, store, g, _ := newStoryboard138(t)
	ctx := WithProductionAuthorization(context.Background(), "alice", "team-a", "member")
	p, _ := store.GetProject(ctx, "alice", "p1")
	if _, err := store.SaveCreativeConfig(ctx, "alice", "p1", CreativeConfig{StoryboardPromptID: "luoshui_storyboard_138"}, p.Revision); err != nil {
		t.Fatal(err)
	}
	b := NewBatch("production138", "p1", "alice", "production", PermissionBypass)
	for _, d := range StepDefinitions[:5] {
		b.Step(d.Key).Status = StepDone
	}
	b.Step("5_storyboard").TotalEpisodes = 1
	if err := store.CreateBatch(ctx, b); err != nil {
		t.Fatal(err)
	}
	r := &ProductionRuntime{Store: store, Repository: NewMemoryProductionRepository(), RedisDo: func(_ context.Context, args ...string) (any, error) {
		if args[0] == "EVAL" {
			return "1", nil
		}
		return "OK", nil
	}, Executor: func(string) BatchExecutor { return BatchExecutor{Store: store, Generator: g, Authoring: s.Authoring} }}
	return r, s, store, g, ctx
}
func TestProduction138RuntimeTwoBatchesExplicitContinueAndAttribution(t *testing.T) {
	r, _, store, g, ctx := production138Fixture(t)
	g.responses = []string{storyboard138Response("1-1-001"), storyboard138Response("1-1-002", "1-1-003")}
	first, err := r.Start(ctx, "alice", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.RunOne(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	waiting, _ := r.Status(ctx, "alice", "p1")
	if waiting.Status != "blocked" || waiting.Batch.Step("5_storyboard").Status != StepBlocked || g.callCount() != 1 {
		t.Fatalf("not durable wait: %+v calls=%d", waiting, g.callCount())
	}
	key := waiting.Batch.Step("5_storyboard").Episodes[1].StoryboardGeneration.RequestKey
	if _, err = r.Start(ctx, "alice", "p1"); err == nil {
		t.Fatal("start bypassed waiting")
	}
	if err = r.RunOne(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if g.callCount() != 1 {
		t.Fatal("restart paid")
	}
	if _, err = r.ContinueStoryboard138(ctx, "alice", "p1", key, false); err == nil {
		t.Fatal("unconfirmed")
	}
	foreign := WithProductionAuthorization(context.Background(), "alice", "team-a", "other-member")
	if _, err = r.ContinueStoryboard138(foreign, "alice", "p1", key, true); err == nil {
		t.Fatal("changed actual actor")
	}
	next, err := r.ContinueStoryboard138(ctx, "alice", "p1", key, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.ContinueStoryboard138(ctx, "alice", "p1", key, true); err == nil {
		t.Fatal("duplicate click advanced")
	}
	if err = r.RunOne(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	done, _ := r.Status(ctx, "alice", "p1")
	if g.callCount() != 2 || done.Batch.Step("5_storyboard").Status != StepDone || done.Batch.Step("6_storyboard_doctor").Status != StepPending {
		t.Fatalf("not final one advance %+v calls=%d", done, g.callCount())
	}
	if done.Batch.Step("5_storyboard").Episodes[1].StoryboardGeneration.Status != "succeeded" {
		t.Fatal("stale UI waiting")
	}
	journal, err := store.LoadStoryboardGenerations(ctx, "alice", "p1")
	if err != nil {
		t.Fatal(err)
	}
	firstAttempt := journal.Attempts[key]
	if firstAttempt == nil || firstAttempt.PromptID != "luoshui_storyboard_138" || firstAttempt.PromptDigest == "" || firstAttempt.ParserVersion == "" || firstAttempt.OutputProtocol == "" {
		t.Fatal("unfrozen production contract", firstAttempt)
	}
	if !g.requests[0].SingleAttempt || strings.Contains(g.requests[1].UserPrompt, `"description":"甲开窗"`) || !strings.Contains(g.requests[1].UserPrompt, "甲离开") {
		t.Fatal("production repeated covered text")
	}
	a := done.Batch.ProductionAuthorization
	if a.Actor != "member" || a.Subject != "alice" || a.TeamID != "team-a" {
		t.Fatal("actor lost", a)
	}
	boards, _ := store.ListStoryboards(ctx, "alice", "p1", "prompt_138")
	if len(boards) != 1 || len(boards[0].Shots) != 2 {
		t.Fatal("incomplete publish", boards)
	}
}
func TestProduction138LegacyJobAndNoAuthorizationFailClosed(t *testing.T) {
	r, _, _, g, ctx := production138Fixture(t)
	if _, err := r.Start(context.Background(), "alice", "p1"); err == nil {
		t.Fatal("missing identity accepted")
	}
	_ = r.Repository.SetProductionState(ctx, "alice", "p1", "running", "")
	j, _ := r.Status(ctx, "alice", "p1")
	if err := r.RunOne(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	out, _ := r.Status(ctx, "alice", "p1")
	if out.Status != "blocked" || g.callCount() != 0 {
		t.Fatal("legacy charged", out, g.callCount())
	}
}
func TestProduction138SourceEditAfterApprovalDoesNotCall(t *testing.T) {
	r, s, _, g, ctx := production138Fixture(t)
	g.responses = []string{storyboard138Response("1-1-001")}
	j, err := r.Start(ctx, "alice", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.RunOne(ctx, j); err != nil {
		t.Fatal(err)
	}
	j, _ = r.Status(ctx, "alice", "p1")
	key := j.Batch.Step("5_storyboard").Episodes[1].StoryboardGeneration.RequestKey
	next, err := r.ContinueStoryboard138(ctx, "alice", "p1", key, true)
	if err != nil {
		t.Fatal(err)
	}
	state, _ := s.Authoring.Repository.Load(ctx, "alice", "p1")
	state.Chapters[0].ScriptText += "改稿"
	_, _ = s.Authoring.Repository.Save(ctx, state, state.Revision)
	_ = r.RunOne(ctx, next)
	if g.callCount() != 1 {
		t.Fatal("edited source paid")
	}
}

func TestProduction138UnknownReceiptRecoveryIsFreeAndWaits(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			r, s, base, _, ctx := production138Fixture(t)
			fault := &storyboard138FailStore{MemoryStore: base}
			r.Store = fault
			calls := 0
			generator := storyboardGenerationTestFunc(func(c context.Context, req TextRequest) (TextResponse, error) {
				calls++
				team, actor := yzcmusage.AttributionForOwner(c, "alice")
				if team != "team-a" || actor != "member" || !req.SingleAttempt {
					t.Fatalf("attribution/contract lost team=%s actor=%s request=%+v", team, actor, req)
				}
				fault.fail = true
				if complete {
					return TextResponse{Content: storyboard138Response("1-1-001", "1-1-002", "1-1-003")}, nil
				}
				return TextResponse{Content: storyboard138Response("1-1-001")}, nil
			})
			r.Executor = func(string) BatchExecutor {
				return BatchExecutor{Store: fault, Authoring: s.Authoring, Generator: generator}
			}
			j, err := r.Start(ctx, "alice", "p1")
			if err != nil {
				t.Fatal(err)
			}
			_ = r.RunOne(context.Background(), j)
			fault.fail = false
			waiting, _ := r.Status(ctx, "alice", "p1")
			if waiting.Status != "blocked" {
				t.Fatalf("unknown not blocked: %+v", waiting)
			}
			progress := waiting.Batch.Step("5_storyboard").Episodes[1].StoryboardGeneration
			if progress == nil || progress.Status != "unknown" {
				t.Fatalf("unknown descriptor %+v", progress)
			}
			if _, err = r.ContinueStoryboard138(ctx, "alice", "p1", progress.RequestKey, true); err == nil {
				t.Fatal("unknown retry approved")
			}
			recovered, err := r.RecoverProductionStoryboard138(ctx, "alice", "p1", progress.RequestKey)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatal("recovery called provider", calls)
			}
			if complete {
				if recovered.Batch.Step("5_storyboard").Status != StepDone || recovered.Status != "running" {
					t.Fatalf("complete recovery %+v", recovered)
				}
			} else {
				if recovered.Status != "blocked" || recovered.Batch.Step("5_storyboard").Episodes[1].StoryboardGeneration.Status != "awaiting_continue" {
					t.Fatalf("partial recovery %+v", recovered)
				}
			}
		})
	}
}
func TestProduction138TemplateAndAssetChangeDoNotPay(t *testing.T) {
	for _, kind := range []string{"template", "asset"} {
		t.Run(kind, func(t *testing.T) {
			r, _, store, g, ctx := production138Fixture(t)
			g.responses = []string{storyboard138Response("1-1-001")}
			j, err := r.Start(ctx, "alice", "p1")
			if err != nil {
				t.Fatal(err)
			}
			if err = r.RunOne(ctx, j); err != nil {
				t.Fatal(err)
			}
			j, _ = r.Status(ctx, "alice", "p1")
			key := j.Batch.Step("5_storyboard").Episodes[1].StoryboardGeneration.RequestKey
			next, err := r.ContinueStoryboard138(ctx, "alice", "p1", key, true)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "asset" {
				_, _ = store.MergeAssets(ctx, "alice", "p1", []Asset{{ID: "changed", Type: AssetCharacter, Name: "新角色", Description: "改动"}})
			} else {
				p, _ := store.GetProject(ctx, "alice", "p1")
				cfg := *p.CreativeConfig
				cfg.StoryboardPromptID = "storyboard_xiaoyunque_v3"
				_, _ = store.SaveCreativeConfig(ctx, "alice", "p1", cfg, p.Revision)
			}
			_ = r.RunOne(ctx, next)
			if g.callCount() != 1 {
				t.Fatal("changed inputs paid", kind, g.callCount())
			}
		})
	}
}

func TestProduction138PartialPreservesFinalBoardAndAssets(t *testing.T) {
	r, _, store, g, ctx := production138Fixture(t)
	g.responses = []string{storyboard138Response("1-1-001")}
	old, err := store.SaveStoryboard(ctx, Storyboard{ID: "old-final", OwnerUsername: "alice", ProjectID: "p1", EpisodeNo: 1, Engine: "prompt_138", ShotCount: 1, Status: "done", Shots: []StoryboardShot{{ID: "old-shot", Seq: 1, Scene: "家"}}})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.MutateBatch(ctx, "alice", "p1", func(b *Batch) error { b.Step("5_storyboard").Status = StepStale; return nil })
	before, _ := store.ListAssets(ctx, "alice", "p1", "")
	j, err := r.Start(ctx, "alice", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.RunOne(ctx, j); err != nil {
		t.Fatal(err)
	}
	after, _ := store.GetStoryboard(ctx, "alice", "p1", 1, "prompt_138")
	assets, _ := store.ListAssets(ctx, "alice", "p1", "")
	if after.ID != old.ID || after.Revision != old.Revision || len(assets) != len(before) {
		t.Fatal("partial overwrote published outputs")
	}
}
func TestProduction138LateWorkerCannotPublishOrAdvanceNewRun(t *testing.T) {
	r, s, store, _, ctx := production138Fixture(t)
	r.Executor = func(string) BatchExecutor {
		return BatchExecutor{Store: store, Authoring: s.Authoring, Generator: storyboardGenerationTestFunc(func(context.Context, TextRequest) (TextResponse, error) {
			_, _ = store.MutateBatch(context.Background(), "alice", "p1", func(b *Batch) error { b.ProductionAuthorization.RunID = "new-run"; return nil })
			return TextResponse{Content: storyboard138Response("1-1-001", "1-1-002", "1-1-003")}, nil
		})}
	}
	j, err := r.Start(ctx, "alice", "p1")
	if err != nil {
		t.Fatal(err)
	}
	_ = r.RunOne(context.Background(), j)
	boards, _ := store.ListStoryboards(ctx, "alice", "p1", "prompt_138")
	if len(boards) != 0 {
		t.Fatal("old worker published")
	}
	current, _ := r.Status(ctx, "alice", "p1")
	if current.Units != 0 || current.Batch.ProductionAuthorization.RunID != "new-run" {
		t.Fatal("old worker overwrote new run", current)
	}
}

func TestProduction138PauseWaitingStillRequiresExplicitContinue(t *testing.T) {
	r, _, _, g, ctx := production138Fixture(t)
	g.responses = []string{storyboard138Response("1-1-001"), storyboard138Response("1-1-002", "1-1-003")}
	j, err := r.Start(ctx, "alice", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.RunOne(ctx, j); err != nil {
		t.Fatal(err)
	}
	paused, err := r.Pause(ctx, "alice", "p1")
	if err != nil {
		t.Fatal(err)
	}
	key := paused.Batch.Step("5_storyboard").Episodes[1].StoryboardGeneration.RequestKey
	if err = r.RunOne(ctx, j); err != nil {
		t.Fatal(err)
	}
	if g.callCount() != 1 {
		t.Fatal("paused auto resubmitted")
	}
	next, err := r.ContinueStoryboard138(ctx, "alice", "p1", key, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = r.RunOne(ctx, next); err != nil {
		t.Fatal(err)
	}
	if g.callCount() != 2 {
		t.Fatal("explicit resume did not run exactly once")
	}
}
func TestProduction138CancelledReturnedBodyCanRecoverFree(t *testing.T) {
	r, s, store, _, ctx := production138Fixture(t)
	calls := 0
	r.Executor = func(string) BatchExecutor {
		return BatchExecutor{Store: store, Authoring: s.Authoring, Generator: storyboardGenerationTestFunc(func(context.Context, TextRequest) (TextResponse, error) {
			calls++
			if _, err := r.Pause(ctx, "alice", "p1"); err != nil {
				t.Fatal(err)
			}
			return TextResponse{Content: storyboard138Response("1-1-001", "1-1-002", "1-1-003")}, nil
		})}
	}
	j, err := r.Start(ctx, "alice", "p1")
	if err != nil {
		t.Fatal(err)
	}
	_ = r.RunOne(context.Background(), j)
	paused, _ := r.Status(ctx, "alice", "p1")
	if paused.Status != "paused" {
		t.Fatal("cancel did not remain paused", paused)
	}
	progress := paused.Batch.Step("5_storyboard").Episodes[1].StoryboardGeneration
	if progress == nil {
		t.Fatal("cancel lost returned request")
	}
	recovered, err := r.RecoverProductionStoryboard138(ctx, "alice", "p1", progress.RequestKey)
	if err != nil {
		t.Fatal(err, progress)
	}
	if calls != 1 || recovered.Batch.Step("5_storyboard").Status != StepDone || recovered.Status != "paused" || recovered.Batch.Status != BatchPaused {
		t.Fatal("cancel free recovery must retain pause", calls, recovered)
	}
	if err := r.RunOne(context.Background(), j); err != nil {
		t.Fatal(err)
	}
	jobs, err := r.Repository.ListRunningProductionJobs(ctx)
	if err != nil || len(jobs) != 0 || calls != 1 {
		t.Fatal("free recovery resumed paid work", jobs, calls, err)
	}
}

func TestProduction138ExistingPartialKeepsOriginalRequestKey(t *testing.T) {
	r, s, _, g, ctx := production138Fixture(t)
	g.responses = []string{storyboard138Response("1-1-001")}
	_, err := s.Generate(ctx, StoryboardRequest{Owner: "alice", ProjectID: "p1", PromptID: "luoshui_storyboard_138", Engine: "prompt_138", IdempotencyKey: "manual-original", EpisodeNo: 1, ChapterID: "c138", Confirmed: true})
	if err == nil {
		t.Fatal("fixture expected partial")
	}
	j, err := r.Start(ctx, "alice", "p1")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.RunOne(ctx, j); err != nil {
		t.Fatal(err)
	}
	saved, _ := r.Status(ctx, "alice", "p1")
	progress := saved.Batch.Step("5_storyboard").Episodes[1].StoryboardGeneration
	if progress == nil || progress.RequestKey != "manual-original" || g.callCount() != 1 {
		t.Fatal("lost actual original key", progress, g.callCount())
	}
}
