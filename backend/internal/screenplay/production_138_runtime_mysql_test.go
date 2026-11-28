package screenplay

import (
	"context"
	"fmt"
	"testing"
)

type production138SQLFault struct {
	*MySQLStore
	fail bool
}

func (s *production138SQLFault) MutateStoryboardGenerations(ctx context.Context, o, p string, fn func(*StoryboardGenerationState) error) (StoryboardGenerationState, error) {
	if s.fail {
		return StoryboardGenerationState{}, fmt.Errorf("injected generation journal outage")
	}
	return s.MySQLStore.MutateStoryboardGenerations(ctx, o, p, fn)
}
func TestProduction138MySQLRuntimeTwoBatchesRestartAndFreeRecovery(t *testing.T) {
	db := newProduction138MySQLDatabase(t)
	for _, kind := range []string{"two-batches", "unknown-partial", "unknown-complete", "unknown-complete-paused", "late-run"} {
		t.Run(kind, func(t *testing.T) {
			ctx := WithProductionAuthorization(context.Background(), "alice", "team-a", "member")
			store := NewMySQLStore(db)
			id := "p-" + kind
			p, err := store.CreateProject(ctx, Project{ID: id, OwnerUsername: "alice", PermissionMode: PermissionBypass})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = store.SaveCreativeConfig(ctx, "alice", id, CreativeConfig{StoryboardPromptID: "luoshui_storyboard_138"}, p.Revision); err != nil {
				t.Fatal(err)
			}
			receipts := &FileScriptBatchReceipts{Root: t.TempDir()}
			authors := &AuthoringService{Store: store, Repository: store.AuthoringRepository(), Receipts: receipts}
			text, err := professionalScriptMarkdown(storyboard138Source)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = authors.Save(ctx, "alice", id, AuthoringState{Chapters: []Chapter{{ID: "c138", Title: "窗外", ScriptText: text, RawResult: storyboard138Source}}}, 0); err != nil {
				t.Fatal(err)
			}
			b := NewBatch("batch-"+kind, id, "alice", "production", PermissionBypass)
			for _, d := range StepDefinitions[:5] {
				b.Step(d.Key).Status = StepDone
			}
			b.Step("5_storyboard").TotalEpisodes = 1
			if err = store.CreateBatch(ctx, b); err != nil {
				t.Fatal(err)
			}
			fault := &production138SQLFault{MySQLStore: store}
			calls := 0
			generator := storyboardGenerationTestFunc(func(c context.Context, req TextRequest) (TextResponse, error) {
				calls++
				if !req.SingleAttempt {
					t.Fatal("not single attempt")
				}
				if kind == "late-run" {
					_, err := store.MutateBatch(c, "alice", id, func(b *Batch) error { b.ProductionAuthorization.RunID = "next-run"; return nil })
					if err != nil {
						t.Fatal(err)
					}
				}
				if kind == "unknown-partial" || kind == "unknown-complete" || kind == "unknown-complete-paused" {
					fault.fail = true
				}
				if kind == "unknown-complete" || kind == "unknown-complete-paused" || kind == "late-run" {
					return TextResponse{Content: storyboard138Response("1-1-001", "1-1-002", "1-1-003")}, nil
				}
				if calls == 1 {
					return TextResponse{Content: storyboard138Response("1-1-001")}, nil
				}
				return TextResponse{Content: storyboard138Response("1-1-002", "1-1-003")}, nil
			})
			redis := func(_ context.Context, args ...string) (any, error) {
				if args[0] == "EVAL" {
					return "1", nil
				}
				return "OK", nil
			}
			runtime := &ProductionRuntime{Store: fault, Repository: store, RedisDo: redis, Executor: func(string) BatchExecutor {
				return BatchExecutor{Store: fault, Authoring: authors, Generator: generator}
			}}
			j, err := runtime.Start(ctx, "alice", id)
			if err != nil {
				t.Fatal(err)
			}
			_ = runtime.RunOne(context.Background(), j)
			fault.fail = false
			// Recreate repositories/runtime, retaining only durable database rows and receipt directory.
			fresh := NewMySQLStore(db)
			authors = &AuthoringService{Store: fresh, Repository: fresh.AuthoringRepository(), Receipts: receipts}
			runtime = &ProductionRuntime{Store: fresh, Repository: fresh, RedisDo: redis, Executor: func(string) BatchExecutor {
				return BatchExecutor{Store: fresh, Authoring: authors, Generator: generator}
			}}
			saved, err := runtime.Status(ctx, "alice", id)
			if err != nil {
				t.Fatal(err)
			}
			if saved.Batch.ProductionAuthorization == nil || saved.Batch.ProductionAuthorization.Actor != "member" || saved.Batch.ProductionAuthorization.TeamID != "team-a" {
				t.Fatal("durable auth missing", saved.Batch)
			}
			if kind == "late-run" {
				boards, _ := fresh.ListStoryboards(ctx, "alice", id, "prompt_138")
				if len(boards) != 0 || saved.Units != 0 || saved.Batch.ProductionAuthorization.RunID != "next-run" {
					t.Fatal("late old run wrote new result")
				}
				return
			}
			if saved.Status != "blocked" || calls != 1 {
				t.Fatalf("not waiting %+v calls=%d", saved, calls)
			}
			progress := saved.Batch.Step("5_storyboard").Episodes[1].StoryboardGeneration
			if progress == nil {
				t.Fatal("durable request key missing")
			}
			if err = runtime.RunOne(ctx, j); err != nil {
				t.Fatal(err)
			}
			if calls != 1 {
				t.Fatal("restart resubmitted")
			}
			if kind != "two-batches" {
				if progress.Status != "unknown" {
					t.Fatal("unknown lost", progress)
				}
				if kind == "unknown-complete-paused" {
					if _, err = runtime.Pause(ctx, "alice", id); err != nil {
						t.Fatal(err)
					}
				}
				saved, err = runtime.RecoverProductionStoryboard138(ctx, "alice", id, progress.RequestKey)
				if err != nil {
					t.Fatal(err)
				}
				if calls != 1 {
					t.Fatal("recovery charged")
				}
				if kind == "unknown-complete-paused" {
					if saved.Status != "paused" || saved.Batch.Status != BatchPaused || saved.Batch.Step("5_storyboard").Status != StepDone {
						t.Fatal("recovery undid explicit pause", saved)
					}
					if err = runtime.RunOne(ctx, j); err != nil {
						t.Fatal(err)
					}
					jobs, err := fresh.ListRunningProductionJobs(ctx)
					for _, job := range jobs {
						if job.ProjectID == id {
							t.Fatal("paused recovery scheduled paid work")
						}
					}
					if err != nil || calls != 1 {
						t.Fatal("paused recovery called model", calls, err)
					}
					return
				}
				if kind == "unknown-complete" {
					if saved.Batch.Step("5_storyboard").Status != StepDone {
						t.Fatal("complete receipt not published")
					}
					return
				}
				if saved.Batch.Step("5_storyboard").Episodes[1].StoryboardGeneration.Status != "awaiting_continue" {
					t.Fatal("partial recovery not waiting")
				}
				return
			}
			next, err := runtime.ContinueStoryboard138(ctx, "alice", id, progress.RequestKey, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = runtime.ContinueStoryboard138(ctx, "alice", id, progress.RequestKey, true); err == nil {
				t.Fatal("duplicate continuation")
			}
			if err = runtime.RunOne(context.Background(), next); err != nil {
				t.Fatal(err)
			}
			final, err := runtime.Status(ctx, "alice", id)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 2 || final.Batch.Step("5_storyboard").Status != StepDone || final.Batch.Step("6_storyboard_doctor").Status != StepPending {
				t.Fatal("not exactly two batches then one advance", calls, final.Batch)
			}
		})
	}
}
