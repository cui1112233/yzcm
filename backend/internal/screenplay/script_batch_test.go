package screenplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"qiantie/backend/internal/prompt"
	"strings"
	"testing"
	"time"
)

func batchFixture(n int, complete bool, rollover string) string {
	return fmt.Sprintf(`{"episode_index":1,"title":"章","scenes":[{"scene_number":"1-%d","location":"家","time":"日","indoor_outdoor":"内","script_content":[{"element_type":"action","action_type":"action","element_id":"1-%d-001","description":"动作。"}]}],"batch_info":{"is_complete":%t,"current_batch":%d},"unprocessed_script_rollover":%q}`, n, n, complete, n, rollover)
}

func TestPartialBatchCannotUseSingleCallApply(t *testing.T) {
	c := Chapter{ScriptText: "旧成功", RawResult: "旧原始"}
	if err := ApplyChapterGeneration(&c, batchFixture(1, false, "剩余。")); err == nil {
		t.Fatal("partial batch was accepted as a completed script")
	}
	if c.ScriptText != "旧成功" || c.RawResult != "旧原始" {
		t.Fatal("old successful result overwritten")
	}
}

func TestOrdinarySaveCannotForgeScriptGeneration(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	store.CreateProject(ctx, Project{ID: "p", OwnerUsername: "a"})
	repo := NewMemoryAuthoringRepository()
	svc := AuthoringService{Store: store, Repository: repo}
	st := AuthoringState{Chapters: []Chapter{{ID: "c", Title: "章"}}}
	// Wire input must never be trusted as server generation state.
	raw := `{"chapters":[{"id":"c","title":"章","script_generation":{"status":"completed"}}]}`
	if err := json.Unmarshal([]byte(raw), &st); err != nil {
		t.Fatal(err)
	}
	st.Chapters[0].RawResult = "old"
	out, err := svc.Save(ctx, "a", "p", st, 0)
	if err != nil {
		t.Fatal(err)
	}
	if out.Chapters[0].ScriptGeneration != nil {
		t.Fatal("forged generation accepted")
	}
}

type batchGeneratorFunc func(context.Context, TextRequest) (TextResponse, error)

func (f batchGeneratorFunc) Generate(ctx context.Context, r TextRequest) (TextResponse, error) {
	return f(ctx, r)
}

func batchService(t *testing.T) (AuthoringService, AuthoringState) {
	t.Helper()
	ctx := context.Background()
	store := NewMemoryStore()
	_, err := store.CreateProject(ctx, Project{ID: "p", OwnerUsername: "a", ModelID: "test", PromptID: ScriptBatchPromptID, PermissionMode: PermissionBypass})
	if err != nil {
		t.Fatal(err)
	}
	svc := AuthoringService{Store: store, Repository: NewMemoryAuthoringRepository(), Receipts: &FileScriptBatchReceipts{Root: t.TempDir()}}
	st, err := svc.Save(ctx, "a", "p", AuthoringState{Chapters: []Chapter{{ID: "c", Title: "章", SourceText: "首句。剩余。", ScriptText: sampleScreenplay, RawResult: "old"}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return svc, st
}

func TestScriptBatchTwoCallsRefreshAndNoDuplicate(t *testing.T) {
	ctx := context.Background()
	svc, st := batchService(t)
	calls := 0
	svc.Generator = batchGeneratorFunc(func(ctx context.Context, r TextRequest) (TextResponse, error) {
		calls++
		saved, e := svc.Repository.Load(ctx, "a", "p")
		if e != nil || saved.Chapters[0].ScriptGeneration.Status != "running" {
			t.Fatal("running was not durable before paid call")
		}
		if !r.SingleAttempt {
			t.Fatal("provider retries enabled")
		}
		if calls == 1 {
			return TextResponse{Content: batchFixture(1, false, "剩余。")}, nil
		}
		if !strings.Contains(r.UserPrompt, "章节原文：\n剩余。") || strings.Contains(r.UserPrompt, "首句。") {
			t.Fatal("continuation did not consume exact rollover")
		}
		return TextResponse{Content: batchFixture(2, true, "")}, nil
	})
	first, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	if first.Chapters[0].ScriptGeneration.Status != "awaiting_continue" || first.Chapters[0].ScriptText != sampleScreenplay || first.Chapters[0].RawResult != "old" {
		t.Fatal("partial overwrote successful script")
	}
	if _, e = svc.Store.ActiveDocument(ctx, "a", "p"); !errors.Is(e, ErrNoActiveScript) {
		t.Fatal("partial published", e)
	}
	if _, e = svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", first.Revision, false); e == nil || calls != 1 {
		t.Fatal("duplicate start paid")
	}
	refreshed, e := svc.Load(ctx, "a", "p")
	if e != nil {
		t.Fatal(e)
	}
	done, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "continue", "", refreshed.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	var raw struct {
		Scenes []json.RawMessage `json:"scenes"`
	}
	json.Unmarshal([]byte(done.Chapters[0].RawResult), &raw)
	if len(raw.Scenes) != 2 || done.Chapters[0].ScriptGeneration.Status != "completed" || len(done.Chapters[0].ScriptGeneration.Batches) != 2 {
		t.Fatal("not merged")
	}
	if _, e = svc.Store.ActiveDocument(ctx, "a", "p"); e != nil {
		t.Fatal(e)
	}
	if _, e = svc.GenerateScriptBatch(ctx, "a", "p", "c", "continue", "", done.Revision, false); e == nil || calls != 2 {
		t.Fatal("completed duplicate paid")
	}
	if _, e = svc.GenerateScriptBatch(ctx, "b", "p", "c", "restart", "", done.Revision, true); e == nil {
		t.Fatal("cross user accepted")
	}
}

func TestScriptBatchRejectInvalidRolloverAndIDs(t *testing.T) {
	tests := map[string]string{
		"no progress": batchFixture(1, false, "首句。剩余。"), "not suffix": batchFixture(1, false, "假剩余。"), "mid sentence": batchFixture(1, false, "余。"),
		"missing boolean": strings.Replace(batchFixture(1, false, "剩余。"), `"is_complete":false,`, "", 1), "null boolean": strings.Replace(batchFixture(1, false, "剩余。"), `"is_complete":false`, `"is_complete":null`, 1),
		"wrong batch": strings.Replace(batchFixture(1, false, "剩余。"), `"current_batch":1`, `"current_batch":2`, 1), "nonempty completed": batchFixture(1, true, "剩余。"),
		"bad element": strings.Replace(batchFixture(1, false, "剩余。"), "1-1-001", "1-1-002", 1), "bad scene": strings.Replace(batchFixture(1, false, "剩余。"), `"scene_number":"1-1"`, `"scene_number":"1-2"`, 1),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, e := validateScriptBatch(&ScriptGeneration{}, "首句。剩余。", raw); e == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
	g := &ScriptGeneration{Batches: []ScriptBatch{{Valid: true, RawResult: batchFixture(1, false, "剩余。")}}}
	if _, e := validateScriptBatch(g, "剩余。", strings.Replace(batchFixture(2, true, ""), "1-2-001", "1-1-001", 1)); e == nil {
		t.Fatal("cross batch duplicate accepted")
	}
	if _, e := validateScriptBatch(g, "剩余。", strings.ReplaceAll(batchFixture(2, true, ""), `"episode_index":1`, `"episode_index":2`)); e == nil {
		t.Fatal("episode change accepted")
	}
}

func TestScriptBatchCancelledReturnedResultIsSaved(t *testing.T) {
	svc, st := batchService(t)
	svc.Repository = contextAwareBatchRepository{svc.Repository}
	ctx, cancel := context.WithCancel(context.Background())
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) {
		cancel()
		return TextResponse{Content: batchFixture(1, false, "剩余。")}, nil
	})
	_, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	saved, e := svc.Repository.Load(context.Background(), "a", "p")
	if e != nil {
		t.Fatal(e)
	}
	g := saved.Chapters[0].ScriptGeneration
	if g.Status != "awaiting_continue" || len(g.Batches) != 1 || g.Batches[0].RawResult == "" {
		t.Fatal("cancel discarded returned content")
	}
}

type contextAwareBatchRepository struct{ AuthoringRepository }

func (r contextAwareBatchRepository) Save(ctx context.Context, st AuthoringState, v int64) (AuthoringState, error) {
	if err := ctx.Err(); err != nil {
		return AuthoringState{}, err
	}
	return r.AuthoringRepository.Save(ctx, st, v)
}

func TestScriptBatchConcurrentRevisionMergeAndRunningDuplicate(t *testing.T) {
	ctx := context.Background()
	svc, st := batchService(t)
	calls := 0
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) {
		calls++
		live, e := svc.Repository.Load(ctx, "a", "p")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", live.Revision, false); e == nil {
			t.Fatal("in-flight duplicate allowed")
		}
		forged := cloneAuthoring(live)
		forged.Chapters[0].SourceText = "racing edit"
		if _, e = svc.Save(ctx, "a", "p", forged, forged.Revision); e == nil {
			t.Fatal("in-flight chapter edit allowed")
		}
		live.Display = map[string]any{"font_size": 18}
		if _, e = svc.Repository.Save(ctx, live, live.Revision); e != nil {
			t.Fatal(e)
		}
		return TextResponse{Content: batchFixture(1, false, "剩余。")}, nil
	})
	out, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	if calls != 1 || out.Display["font_size"] != float64(18) || len(out.Chapters[0].ScriptGeneration.Batches) != 1 {
		t.Fatal("concurrent revision lost data", out.Display)
	}
}

func TestScriptBatchProviderTimeConfigurationChangeKeepsDiagnostic(t *testing.T) {
	ctx := context.Background()
	svc, st := batchService(t)
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) {
		p, e := svc.Store.GetProject(ctx, "a", "p")
		if e != nil {
			t.Fatal(e)
		}
		p.ModelID = "changed"
		if _, e = svc.Store.UpdateProject(ctx, "a", "p", p, p.Revision); e != nil {
			t.Fatal(e)
		}
		return TextResponse{Content: batchFixture(1, true, "")}, nil
	})
	out, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	c := out.Chapters[0]
	if c.ScriptGeneration.Status != "stale" || len(c.ScriptGeneration.Batches) != 1 || c.RawResult != "old" || c.ScriptText != sampleScreenplay {
		t.Fatal("changed configuration published or lost diagnostic")
	}
}

func TestScriptBatchAssistantProposalCannotForgeStatus(t *testing.T) {
	ctx := context.Background()
	svc, st := batchService(t)
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) {
		return TextResponse{Content: batchFixture(1, false, "剩余。")}, nil
	})
	out, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	chapter := out.Chapters[0]
	chapter.ScriptGeneration = &ScriptGeneration{Status: "completed"}
	out.Proposals = []AssistantProposal{{ID: "proposal", Status: "pending", Revision: out.Revision + 1, Actions: []AuthoringAction{{Kind: "chapter", Chapter: &chapter}}}}
	out, e = svc.Repository.Save(ctx, out, out.Revision)
	if e != nil {
		t.Fatal(e)
	}
	applied, e := svc.Apply(ctx, "a", "p", "proposal", out.Revision, true)
	if e != nil {
		t.Fatal(e)
	}
	if applied.Chapters[0].ScriptGeneration.Status != "awaiting_continue" || len(applied.Chapters[0].ScriptGeneration.Batches) != 1 {
		t.Fatal("proposal forged server generation state")
	}
	if _, e = svc.Store.ActiveDocument(ctx, "a", "p"); !errors.Is(e, ErrNoActiveScript) {
		t.Fatal("proposal published incomplete collection")
	}
}

func TestScriptBatchSingleShotAliasesCannotBypassGuard(t *testing.T) {
	svc, _ := batchService(t)
	calls := 0
	single := ScreenplayService{Store: svc.Store, Generator: batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) { calls++; return TextResponse{}, nil })}
	for _, id := range []string{"293", ScriptBatchPromptID} {
		if _, e := single.GenerateA1(context.Background(), A1Request{Owner: "a", ProjectID: "p", Content: "原文。", PromptID: id, Confirmed: true}); e == nil {
			t.Fatal("A1 alias bypass")
		}
		if _, e := single.GenerateB(context.Background(), BRequest{Owner: "a", ProjectID: "p", Novel: "原文。", PromptID: id, Confirmed: true}); e == nil {
			t.Fatal("B alias bypass")
		}
	}
	if calls != 0 {
		t.Fatal("alias bypass paid")
	}
}

type failingBatchPublishStore struct {
	Store
	fail bool
}

func (s *failingBatchPublishStore) SaveScreenplay(ctx context.Context, d Document, shots []Shot, characters []Character, scenes []Scene) (Document, error) {
	if s.fail {
		return Document{}, errors.New("temporary save failure")
	}
	return s.Store.SaveScreenplay(ctx, d, shots, characters, scenes)
}

func TestScriptBatchPublicationRetryNeverCallsModel(t *testing.T) {
	ctx := context.Background()
	svc, st := batchService(t)
	store := &failingBatchPublishStore{Store: svc.Store, fail: true}
	svc.Store = store
	calls := 0
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) {
		calls++
		return TextResponse{Content: batchFixture(1, true, "")}, nil
	})
	_, _ = svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false)
	saved, e := svc.Load(ctx, "a", "p")
	if e != nil {
		t.Fatal(e)
	}
	store.fail = false
	saved, e = svc.GenerateScriptBatch(ctx, "a", "p", "c", "publish", "", saved.Revision, false)
	if e != nil {
		t.Fatal("cannot retry completed publication without another model call", e)
	}
	if calls != 1 {
		t.Fatal("publication retry charged model")
	}
	if _, e = store.ActiveDocument(ctx, "a", "p"); e != nil {
		t.Fatal(e)
	}
	_, e = svc.GenerateScriptBatch(ctx, "a", "p", "c", "publish", "", saved.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	docs, _ := store.ListDocuments(ctx, "a", "p")
	if len(docs) != 1 {
		t.Fatal("publication retry created duplicate versions")
	}
}

func TestScriptBatchFailureUnknownRequiresExplicitRestart(t *testing.T) {
	svc, st := batchService(t)
	calls := 0
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) {
		calls++
		return TextResponse{Content: "partial wire response"}, context.DeadlineExceeded
	})
	out, e := svc.GenerateScriptBatch(context.Background(), "a", "p", "c", "start", "", st.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	g := out.Chapters[0].ScriptGeneration
	if g.Status != "unknown" || g.Batches[0].RawResult != "partial wire response" || out.Chapters[0].RawResult != "old" {
		t.Fatal("failure lost diagnostic or old success")
	}
	for _, action := range []string{"start", "continue", "restart"} {
		if _, e = svc.GenerateScriptBatch(context.Background(), "a", "p", "c", action, "", out.Revision, false); e == nil {
			t.Fatal("implicit paid retry", action)
		}
	}
	if calls != 1 {
		t.Fatal("retried automatically")
	}
	restarted, e := svc.GenerateScriptBatch(context.Background(), "a", "p", "c", "restart", "", out.Revision, true)
	if e != nil {
		t.Fatal(e)
	}
	if len(restarted.Chapters[0].ScriptGeneration.History) != 1 || calls != 2 {
		t.Fatal("explicit restart lost history")
	}
}

func TestScriptBatchSourceConfigMismatchAndEditProtection(t *testing.T) {
	svc, st := batchService(t)
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) {
		return TextResponse{Content: batchFixture(1, false, "剩余。")}, nil
	})
	out, e := svc.GenerateScriptBatch(context.Background(), "a", "p", "c", "start", "", st.Revision, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.GenerateScriptBatch(context.Background(), "a", "p", "c", "continue", "changed instruction", out.Revision, false); e == nil {
		t.Fatal("config mismatch accepted")
	}
	out.Chapters[0].SourceText = "新原文。"
	out.Chapters[0].ScriptGeneration = &ScriptGeneration{Status: "completed"}
	saved, e := svc.Save(context.Background(), "a", "p", out, out.Revision)
	if e != nil {
		t.Fatal(e)
	}
	if saved.Chapters[0].ScriptGeneration.Status != "stale" || len(saved.Chapters[0].ScriptGeneration.Batches) != 1 {
		t.Fatal("ordinary edit forged generation")
	}
	if _, e = svc.GenerateScriptBatch(context.Background(), "a", "p", "c", "continue", "", saved.Revision, false); e == nil {
		t.Fatal("source mismatch paid")
	}
}

func TestSingleShotA1BRejectBatchBeforeModel(t *testing.T) {
	svc, _ := batchService(t)
	calls := 0
	single := ScreenplayService{Store: svc.Store, Generator: batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) { calls++; return TextResponse{}, nil })}
	if _, e := single.GenerateA1(context.Background(), A1Request{Owner: "a", ProjectID: "p", Content: "原文。", Confirmed: true}); e == nil {
		t.Fatal("A1 accepted batch")
	}
	if _, e := single.GenerateB(context.Background(), BRequest{Owner: "a", ProjectID: "p", Novel: "原文。", Confirmed: true}); e == nil {
		t.Fatal("B accepted batch")
	}
	if calls != 0 {
		t.Fatal("unsupported entry paid")
	}
}

func TestScriptBatchExpiredRunningLoadsUnknownAndCannotContinue(t *testing.T) {
	ctx := context.Background()
	svc, st := batchService(t)
	st.Chapters[0].ScriptGeneration = &ScriptGeneration{ID: "old", Status: "running", Deadline: time.Now().Add(-time.Minute)}
	saved, e := svc.Repository.Save(ctx, st, st.Revision)
	if e != nil {
		t.Fatal(e)
	}
	loaded, e := svc.Load(ctx, "a", "p")
	if e != nil {
		t.Fatal(e)
	}
	if loaded.Chapters[0].ScriptGeneration.Status != "unknown" {
		t.Fatal("expired running did not become uncertain")
	}
	calls := 0
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) { calls++; return TextResponse{}, nil })
	if _, e = svc.GenerateScriptBatch(ctx, "a", "p", "c", "continue", "", saved.Revision, false); e == nil || calls != 0 {
		t.Fatal("expired running automatically resumed")
	}
}

func TestScriptBatchExactFrozenPromptAndLimits(t *testing.T) {
	snapshot, e := prompt.ResolveCreativePrompt(ScriptBatchPromptID, "script")
	if e != nil {
		t.Fatal(e)
	}
	p := Project{PromptID: ScriptBatchPromptID, CreativeConfig: &CreativeConfig{ScriptPromptID: ScriptBatchPromptID, PromptSnapshots: map[string]CreativePromptSnapshot{"script": snapshot}}}
	snapshot.Supported = false
	snapshot.ParserVersion = "script_scene_batch_v1"
	p.CreativeConfig.PromptSnapshots["script"] = snapshot
	if _, e = BuildChapterTextRequest(p, Chapter{SourceText: "原文。"}, "", ""); e != nil {
		t.Fatal("matching old frozen snapshot not upgraded", e)
	}
	snapshot.Body += "tampered"
	snapshot.Supported = true
	p.CreativeConfig.PromptSnapshots["script"] = snapshot
	if _, e = BuildChapterTextRequest(p, Chapter{SourceText: "原文。"}, "", ""); e == nil {
		t.Fatal("nonmatching body enabled")
	}
	if _, e = decodeScriptBatch(strings.Repeat("x", maxScriptBatchBytes+1)); e == nil {
		t.Fatal("oversized response accepted")
	}
	ctx := context.Background()
	svc, st := batchService(t)
	st.Chapters[0].SourceText = strings.Repeat("字", maxScriptBatchBytes/3+1)
	st, e = svc.Save(ctx, "a", "p", st, st.Revision)
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) { calls++; return TextResponse{}, nil })
	if _, e = svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false); e == nil || calls != 0 {
		t.Fatal("oversized source called model")
	}
}

func TestScriptBatchRejectsActionWithoutRecoveredType(t *testing.T) {
	raw := strings.Replace(batchFixture(1, true, ""), `"action_type":"action",`, "", 1)
	if _, err := validateScriptBatch(&ScriptGeneration{}, "原文。", raw); err == nil {
		t.Fatal("293 action without mandatory action_type accepted")
	}
}

func TestScriptBatchLateReturnIsRetainedInRestartHistory(t *testing.T) {
	ctx := context.Background()
	svc, st := batchService(t)
	svc.Generator = batchGeneratorFunc(func(context.Context, TextRequest) (TextResponse, error) {
		live, e := svc.Repository.Load(ctx, "a", "p")
		if e != nil {
			t.Fatal(e)
		}
		previous := live.Chapters[0].ScriptGeneration
		live.Chapters[0].ScriptGeneration = &ScriptGeneration{ID: "new-run", Status: "unknown", History: []*ScriptGeneration{previous}}
		if _, e = svc.Repository.Save(ctx, live, live.Revision); e != nil {
			t.Fatal(e)
		}
		return TextResponse{Content: batchFixture(1, true, "")}, nil
	})
	out, e := svc.GenerateScriptBatch(ctx, "a", "p", "c", "start", "", st.Revision, false)
	if e != nil {
		t.Fatal("late returned body lost after restart", e)
	}
	g := out.Chapters[0].ScriptGeneration
	if g.ID != "new-run" || g.Status != "unknown" || len(g.History[0].Batches) != 1 || g.History[0].Status != "stale" || out.Chapters[0].RawResult != "old" {
		t.Fatal("late response overwrote new run or lost raw diagnostic")
	}
}
