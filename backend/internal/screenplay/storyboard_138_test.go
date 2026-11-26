package screenplay

import (
	"context"
	"encoding/json"
	"fmt"
	"qiantie/backend/internal/prompt"
	"strings"
	"testing"
)

const storyboard138Source = `{"episode_index":1,"title":"窗外","scenes":[{"scene_number":"1-1","location":"家","script_content":[{"element_id":"1-1-001","element_type":"action","description":"甲开窗"},{"element_id":"1-1-002","element_type":"action","description":"甲关窗"},{"element_id":"1-1-003","element_type":"action","description":"甲离开"}]}]}`

func storyboard138Response(ids ...string) string {
	raw, _ := json.Marshal(map[string]any{"storyboard": []any{map[string]any{"id": 1, "source_elements": ids, "characters": "甲", "scene": "家", "props": "无", "image_prompt": "开窗四格", "video_desc": "Grid 1-6 开窗", "voice_actor": "无", "emotion": "无", "line": "无"}}})
	return string(raw) + "\n【分镜自检】未处理剧情：模型自称全部完成"
}
func newStoryboard138(t *testing.T) (*StoryboardService, *MemoryStore, *fakeGenerator, StoryboardRequest) {
	t.Helper()
	s, store, g := newStoryboardService(t, PermissionBypass)
	s.Authoring = &AuthoringService{Store: store, Repository: NewMemoryAuthoringRepository(), Receipts: &FileScriptBatchReceipts{Root: t.TempDir()}}
	script, e := professionalScriptMarkdown(storyboard138Source)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.Authoring.Save(context.Background(), "alice", "p1", AuthoringState{Chapters: []Chapter{{ID: "c138", Title: "窗外", ScriptText: script, RawResult: storyboard138Source}}}, 0)
	if e != nil {
		t.Fatal(e)
	}
	in := generationTestRequest("138-first")
	in.PromptID = "luoshui_storyboard_138"
	in.ChapterID = "c138"
	in.Content = ""
	return s, store, g, in
}
func TestStoryboard138TwoBatchesSaveReopenAndOnlyPublishComplete(t *testing.T) {
	s, store, g, in := newStoryboard138(t)
	g.responses = []string{storyboard138Response("1-1-001"), storyboard138Response("1-1-002", "1-1-003")}
	first, e := s.Generate(context.Background(), in)
	if e == nil || first.GenerationAttempt == nil || first.GenerationAttempt.Status != "partial" || g.callCount() != 1 {
		t.Fatalf("partial not saved: %+v %v calls=%d", first, e, g.callCount())
	}
	boards, _ := store.ListStoryboards(context.Background(), "alice", "p1", "prompt_138")
	if len(boards) != 0 {
		t.Fatal("partial published")
	}
	_, _ = s.Generate(context.Background(), in)
	if g.callCount() != 1 {
		t.Fatal("duplicate paid")
	}
	s = &StoryboardService{Store: store, Generator: g, Authoring: s.Authoring}
	in.RetryOf = in.IdempotencyKey
	in.IdempotencyKey = "138-next"
	in.ConfirmedRetry = true
	out, e := s.Generate(context.Background(), in)
	if e != nil || len(out.Storyboards) != 1 || len(out.Storyboards[0].Shots) != 2 || g.callCount() != 2 {
		t.Fatalf("continuation %+v %v calls=%d", out, e, g.callCount())
	}
	if strings.Contains(g.requests[1].UserPrompt, `"description":"甲开窗"`) || !strings.Contains(g.requests[1].UserPrompt, "甲离开") {
		t.Fatal("not exact remaining source", g.requests[1].UserPrompt)
	}
}
func TestStoryboard138RejectsUnnumberedAndStaleBeforeModel(t *testing.T) {
	for _, kind := range []string{"raw", "edited", "missing", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			s, _, g, in := newStoryboard138(t)
			state, _ := s.Authoring.Repository.Load(context.Background(), "alice", "p1")
			switch kind {
			case "raw":
				in.ChapterID = ""
				in.Content = "无编号原文"
			case "edited":
				state.Chapters[0].ScriptText = "改过的剧本"
			case "missing":
				state.Chapters[0].RawResult = strings.ReplaceAll(storyboard138Source, `"element_id":"1-1-001",`, "")
			case "duplicate":
				state.Chapters[0].RawResult = strings.ReplaceAll(storyboard138Source, "1-1-002", "1-1-001")
			}
			if kind == "missing" || kind == "duplicate" {
				state.Chapters[0].ScriptText, _ = professionalScriptMarkdown(state.Chapters[0].RawResult)
			}
			if kind != "raw" {
				_, _ = s.Authoring.Repository.Save(context.Background(), state, state.Revision)
			}
			_, e := s.Generate(context.Background(), in)
			if e == nil || g.callCount() != 0 {
				t.Fatal("invalid source billed", e, g.callCount())
			}
		})
	}
}
func TestStoryboard138CoverageRejectsUnknownGapNoProgressAllowsSplit(t *testing.T) {
	for _, ids := range [][]string{{"1-1-002"}, {"1-1-001", "1-1-003"}, {"foreign"}, {}} {
		t.Run(strings.Join(ids, "+"), func(t *testing.T) {
			s, store, g, in := newStoryboard138(t)
			g.responses = []string{storyboard138Response(ids...)}
			out, e := s.Generate(context.Background(), in)
			if e == nil {
				t.Fatal("invalid coverage accepted", out)
			}
			boards, _ := store.ListStoryboards(context.Background(), "alice", "p1", "prompt_138")
			if len(boards) > 0 {
				t.Fatal("invalid published")
			}
		})
	}
}

type storyboard138FailStore struct {
	*MemoryStore
	fail bool
}

func (s *storyboard138FailStore) MutateStoryboardGenerations(ctx context.Context, o, p string, fn func(*StoryboardGenerationState) error) (StoryboardGenerationState, error) {
	if s.fail {
		return StoryboardGenerationState{}, fmt.Errorf("database unavailable")
	}
	return s.MemoryStore.MutateStoryboardGenerations(ctx, o, p, fn)
}
func TestStoryboard138ReturnedBodyRecoversWithoutAnotherModelCall(t *testing.T) {
	s, base, _, in := newStoryboard138(t)
	store := &storyboard138FailStore{MemoryStore: base}
	s.Store = store
	calls := 0
	s.Generator = storyboardGenerationTestFunc(func(context.Context, TextRequest) (TextResponse, error) {
		calls++
		store.fail = true
		return TextResponse{Content: storyboard138Response("1-1-001", "1-1-002", "1-1-003")}, nil
	})
	_, e := s.Generate(context.Background(), in)
	if e == nil {
		t.Fatal("expected save fault")
	}
	store.fail = false
	recovered := &StoryboardService{Store: store, Authoring: s.Authoring}
	if _, e = recovered.RecoverStoryboard138(context.Background(), "alice", "p1", in.IdempotencyKey); e != nil {
		t.Fatal("returned body cannot recover", e)
	}
	s.Generator = storyboardGenerationTestFunc(func(context.Context, TextRequest) (TextResponse, error) {
		t.Fatal("recovery billed twice")
		return TextResponse{}, nil
	})
	in.RetryOf = in.IdempotencyKey
	in.IdempotencyKey = "recovered"
	in.ConfirmedRetry = true
	out, e := s.Generate(context.Background(), in)
	if e != nil || len(out.Storyboards) != 1 || calls != 1 {
		t.Fatal(out, e, calls)
	}
}
func TestStoryboard138SplitReuseAndNoProgress(t *testing.T) {
	s, _, g, in := newStoryboard138(t)
	g.responses = []string{storyboard138Response("1-1-001", "1-1-001"), storyboard138Response("1-1-001"), storyboard138Response("1-1-001", "1-1-002", "1-1-003")}
	out, e := s.Generate(context.Background(), in)
	if e == nil || out.GenerationAttempt.Status != "partial" {
		t.Fatal(out, e)
	}
	in.RetryOf = in.IdempotencyKey
	in.IdempotencyKey = "no-progress"
	in.ConfirmedRetry = true
	out, e = s.Generate(context.Background(), in)
	if e == nil || len(out.Storyboards) != 0 || len(out.FailedScenes) != 1 || out.FailedScenes[0].Scene != 1 {
		t.Fatal(out, e)
	}
	in.RetryOf = in.IdempotencyKey
	in.IdempotencyKey = "split-finish"
	out, e = s.Generate(context.Background(), in)
	if e != nil || len(out.Storyboards) != 1 || g.callCount() != 3 {
		t.Fatal(out, e, g.callCount())
	}
}
func TestStoryboard138ConfigChangeDuringCallKeepsReceiptAndOldBoard(t *testing.T) {
	s, store, _, in := newStoryboard138(t)
	old, _ := store.SaveStoryboard(context.Background(), Storyboard{ID: "old", OwnerUsername: "alice", ProjectID: "p1", EpisodeNo: 1, Engine: "prompt_138", Shots: []StoryboardShot{{Seq: 1, VideoDesc: "原分镜"}}})
	s.Generator = storyboardGenerationTestFunc(func(context.Context, TextRequest) (TextResponse, error) {
		p, _ := store.GetProject(context.Background(), "alice", "p1")
		cfg := EffectiveCreativeConfig(p)
		cfg.AspectRatio = "16:9"
		_, _ = store.SaveCreativeConfig(context.Background(), "alice", "p1", cfg, p.Revision)
		return TextResponse{Content: storyboard138Response("1-1-001", "1-1-002", "1-1-003")}, nil
	})
	_, e := s.Generate(context.Background(), in)
	if e == nil {
		t.Fatal("stale config published")
	}
	after, _ := store.GetStoryboard(context.Background(), "alice", "p1", 1, "prompt_138")
	if after.ID != old.ID || after.Revision != old.Revision {
		t.Fatal("old board replaced")
	}
	st, _ := store.LoadStoryboardGenerations(context.Background(), "alice", "p1")
	c := st.Attempts[in.IdempotencyKey].Episodes[1].Calls[phase138(1, 1)]
	if c.RawResponse == "" || c.ReceiptKey == nil {
		t.Fatal("paid result lost")
	}
}

func TestStoryboard138CancelAndUnknownNeverAutomaticallyResubmit(t *testing.T) {
	for _, mode := range []string{"cancel", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, store, _, in := newStoryboard138(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			s.Generator = storyboardGenerationTestFunc(func(context.Context, TextRequest) (TextResponse, error) {
				calls++
				if mode == "cancel" {
					cancel()
					return TextResponse{Content: storyboard138Response("1-1-001", "1-1-002", "1-1-003")}, nil
				}
				return TextResponse{}, fmt.Errorf("transport lost")
			})
			out, e := s.Generate(ctx, in)
			if e == nil || len(out.Storyboards) != 0 {
				t.Fatal(out, e)
			}
			_, _ = s.Generate(context.Background(), in)
			if calls != 1 {
				t.Fatal("duplicate request called provider")
			}
			in.RetryOf = in.IdempotencyKey
			in.IdempotencyKey = "retry-" + mode
			in.ConfirmedRetry = true
			out, e = s.Generate(context.Background(), in)
			if mode == "cancel" {
				if e != nil || len(out.Storyboards) != 1 || calls != 1 {
					t.Fatal("cancelled received body not reused", out, e, calls)
				}
			} else {
				if e == nil || calls != 1 {
					t.Fatal("unknown rebilled", out, e, calls)
				}
			}
			state, _ := store.LoadStoryboardGenerations(context.Background(), "alice", "p1")
			if state.Attempts["138-first"].Episodes[1].Calls[phase138(1, 1)].ReceiptKey == nil {
				t.Fatal("missing receipt")
			}
		})
	}
}
func TestStoryboard138ConcurrentClicksReserveOnlyOneCall(t *testing.T) {
	s, _, _, in := newStoryboard138(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	calls := 0
	s.Generator = storyboardGenerationTestFunc(func(context.Context, TextRequest) (TextResponse, error) {
		calls++
		close(entered)
		<-release
		return TextResponse{Content: storyboard138Response("1-1-001")}, nil
	})
	go func() { _, e := s.Generate(context.Background(), in); done <- e }()
	<-entered
	_, e := s.Generate(context.Background(), in)
	if e == nil {
		t.Fatal("concurrent same key accepted")
	}
	other := in
	other.IdempotencyKey = "other-click"
	_, e = s.Generate(context.Background(), other)
	if e == nil {
		t.Fatal("concurrent other key accepted")
	}
	close(release)
	<-done
	if calls != 1 {
		t.Fatal(calls)
	}
}
func TestStoryboard138PartialPreservesExistingBoardAndDisablesAssetSync(t *testing.T) {
	s, store, g, in := newStoryboard138(t)
	p, _ := store.GetProject(context.Background(), "alice", "p1")
	cfg := EffectiveCreativeConfig(p)
	cfg.StoryboardAutoSyncAssets = true
	_, _ = store.SaveCreativeConfig(context.Background(), "alice", "p1", cfg, p.Revision)
	old, _ := store.SaveStoryboard(context.Background(), Storyboard{ID: "old-media-board", OwnerUsername: "alice", ProjectID: "p1", EpisodeNo: 1, Engine: "prompt_138", Shots: []StoryboardShot{{ID: "media-shot", Seq: 1, VideoDesc: "已关联图片视频"}}})
	g.responses = []string{storyboard138Response("1-1-001")}
	out, e := s.Generate(context.Background(), in)
	after, _ := store.GetStoryboard(context.Background(), "alice", "p1", 1, "prompt_138")
	assets, _ := store.ListAssets(context.Background(), "alice", "p1", "")
	if e == nil || !out.PartialFailure || after.ID != old.ID || after.Revision != old.Revision || after.Shots[0].ID != "media-shot" || g.callCount() != 1 || len(assets) > 0 {
		t.Fatal("partial touched board/media or synchronized assets", out, e, after, g.callCount())
	}
}
func TestStoryboard138TwoScenesEachAddsAtMostOneBatch(t *testing.T) {
	s, _, g, in := newStoryboard138(t)
	st, _ := s.Authoring.Repository.Load(context.Background(), "alice", "p1")
	// Use structured JSON construction so scene insertion cannot alter an element.
	var doc map[string]any
	_ = json.Unmarshal([]byte(storyboard138Source), &doc)
	var second map[string]any
	_ = json.Unmarshal([]byte(`{"scene_number":"1-2","location":"路口","script_content":[{"element_id":"1-2-001","element_type":"action","description":"乙到达"},{"element_id":"1-2-002","element_type":"action","description":"乙离开"}]}`), &second)
	doc["scenes"] = append(doc["scenes"].([]any), second)
	body, _ := json.Marshal(doc)
	raw := string(body)
	st.Chapters[0].RawResult = raw
	st.Chapters[0].ScriptText, _ = professionalScriptMarkdown(raw)
	_, _ = s.Authoring.Repository.Save(context.Background(), st, st.Revision)
	g.responses = []string{storyboard138Response("1-1-001"), storyboard138Response("1-2-001"), storyboard138Response("1-1-002", "1-1-003"), storyboard138Response("1-2-002")}
	out, e := s.Generate(context.Background(), in)
	if e == nil || g.callCount() != 2 || out.GenerationAttempt.Status != "partial" {
		t.Fatal(out, e, g.callCount())
	}
	in.RetryOf = in.IdempotencyKey
	in.IdempotencyKey = "two-scenes-next"
	in.ConfirmedRetry = true
	out, e = s.Generate(context.Background(), in)
	if e != nil || g.callCount() != 4 || len(out.Storyboards) != 1 || len(out.Storyboards[0].Shots) != 4 {
		t.Fatal(out, e, g.callCount())
	}
	for _, r := range g.requests[:2] {
		if strings.Contains(r.UserPrompt, "1-1-001") && strings.Contains(r.UserPrompt, "1-2-001") {
			t.Fatal("scene source leaked")
		}
	}
}
func TestStoryboard138BoundsOversizedResponseBeforeDatabase(t *testing.T) {
	s, store, g, in := newStoryboard138(t)
	g.responses = []string{strings.Repeat("x", max138Bytes+1)}
	_, e := s.Generate(context.Background(), in)
	if e == nil {
		t.Fatal("oversized accepted")
	}
	state, _ := store.LoadStoryboardGenerations(context.Background(), "alice", "p1")
	call := state.Attempts[in.IdempotencyKey].Episodes[1].Calls[phase138(1, 1)]
	if call.RawResponse != "" || call.ReceiptKey == nil {
		t.Fatal("oversized body persisted")
	}
	receipt, e := storyboard138Receipts(s.Authoring).Load(context.Background(), *call.ReceiptKey)
	if e != nil || !receipt.Oversized || len(receipt.Preview) > 2048 || receipt.RawResult != "" {
		t.Fatal(receipt, e)
	}
	_, _ = s.Generate(context.Background(), in)
	if g.callCount() != 1 {
		t.Fatal("oversized auto rebilled")
	}
}
func TestStoryboard138CannotPublishChapterIntoDifferentEpisode(t *testing.T) {
	s, _, g, in := newStoryboard138(t)
	in.EpisodeNo = 2
	_, e := s.Generate(context.Background(), in)
	if e == nil || g.callCount() != 0 {
		t.Fatal("chapter generated into another episode", e, g.callCount())
	}
}

type storyboard138ReadyFail struct{ ScriptBatchReceipts }

func (r storyboard138ReadyFail) Save(ctx context.Context, v ScriptBatchReceipt) error {
	if v.Ready {
		return fmt.Errorf("receipt volume unavailable after billing")
	}
	return r.ScriptBatchReceipts.Save(ctx, v)
}
func TestStoryboard138ReceiptWriteFailureFallsBackToDatabaseWithoutRebilling(t *testing.T) {
	s, store, g, in := newStoryboard138(t)
	s.Authoring.Receipts = storyboard138ReadyFail{ScriptBatchReceipts: s.Authoring.Receipts}
	g.responses = []string{storyboard138Response("1-1-001", "1-1-002", "1-1-003")}
	_, e := s.Generate(context.Background(), in)
	if e == nil {
		t.Fatal("expected file write diagnostic")
	}
	state, _ := store.LoadStoryboardGenerations(context.Background(), "alice", "p1")
	call := state.Attempts[in.IdempotencyKey].Episodes[1].Calls[phase138(1, 1)]
	if call.RawResponse == "" || call.Status != "received" {
		t.Fatal("database was available but returned result lost")
	}
	in.RetryOf = in.IdempotencyKey
	in.IdempotencyKey = "database-recovery"
	in.ConfirmedRetry = true
	out, e := s.Generate(context.Background(), in)
	if e != nil || len(out.Storyboards) != 1 || g.callCount() != 1 {
		t.Fatal(out, e, g.callCount())
	}
}
func TestStoryboard138MergedOutputPassesFinalContractWithFreshBatchIDs(t *testing.T) {
	s, _, g, in := newStoryboard138(t)
	g.responses = []string{storyboard138Response("1-1-001"), storyboard138Response("1-1-002", "1-1-003")}
	_, _ = s.Generate(context.Background(), in)
	in.RetryOf = in.IdempotencyKey
	in.IdempotencyKey = "merged-contract"
	in.ConfirmedRetry = true
	out, e := s.Generate(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	p, _ := prompt.ResolveCreativePrompt("138", "storyboard")
	p.ResponseMode = "json_object"
	if e = validateSelectedStoryboardProtocol(p, "", out.Storyboards[0].Shots); e != nil {
		t.Fatal("merged output violates final contract", e)
	}
}
func TestStoryboard138CompleteThenSyncAssetsOnlyOnce(t *testing.T) {
	s, store, g, in := newStoryboard138(t)
	p, _ := store.GetProject(context.Background(), "alice", "p1")
	cfg := EffectiveCreativeConfig(p)
	cfg.StoryboardAutoSyncAssets = true
	_, _ = store.SaveCreativeConfig(context.Background(), "alice", "p1", cfg, p.Revision)
	g.responses = []string{storyboard138Response("1-1-001", "1-1-002", "1-1-003"), `{"characters":[{"name":"甲","description":"角色","prompt":"角色形象"}],"scenes":[{"name":"家","description":"房间","prompt":"房间图"}],"props":[]}`}
	out, e := s.Generate(context.Background(), in)
	if e != nil || len(out.Storyboards) != 1 || g.callCount() != 2 {
		t.Fatal(out, e, g.callCount())
	}
	_, e = s.Generate(context.Background(), in)
	if e != nil || g.callCount() != 2 {
		t.Fatal("completed sync repeated", e, g.callCount())
	}
}

type storyboard138PauseReceipt struct {
	ScriptBatchReceipts
	ready   chan struct{}
	release chan struct{}
}

func (r storyboard138PauseReceipt) Save(ctx context.Context, v ScriptBatchReceipt) error {
	if e := r.ScriptBatchReceipts.Save(ctx, v); e != nil {
		return e
	}
	if v.Ready {
		close(r.ready)
		<-r.release
	}
	return nil
}
func TestStoryboard138RecoveryFencesLateWorkerBeforePublication(t *testing.T) {
	s, store, g, in := newStoryboard138(t)
	ready, release := make(chan struct{}), make(chan struct{})
	s.Authoring.Receipts = storyboard138PauseReceipt{ScriptBatchReceipts: s.Authoring.Receipts, ready: ready, release: release}
	g.responses = []string{storyboard138Response("1-1-001", "1-1-002", "1-1-003")}
	done := make(chan error, 1)
	go func() { _, e := s.Generate(context.Background(), in); done <- e }()
	<-ready
	recovered := &StoryboardService{Store: store, Authoring: s.Authoring}
	if _, e := recovered.RecoverStoryboard138(context.Background(), "alice", "p1", in.IdempotencyKey); e != nil {
		close(release)
		<-done
		t.Fatal(e)
	}
	close(release)
	if e := <-done; e == nil {
		t.Fatal("superseded worker published")
	}
	boards, _ := store.ListStoryboards(context.Background(), "alice", "p1", "prompt_138")
	if len(boards) > 0 {
		t.Fatal("late original worker overwrote board")
	}
	in.RetryOf = in.IdempotencyKey
	in.IdempotencyKey = "late-recovered"
	in.ConfirmedRetry = true
	out, e := s.Generate(context.Background(), in)
	if e != nil || len(out.Storyboards) != 1 || g.callCount() != 1 {
		t.Fatal(out, e, g.callCount())
	}
}

func TestStoryboard138RequestTextCannotBypassSavedScript(t *testing.T) {
	for _, shape := range []string{"chapter", "content", "content_episode", "episodes", "episodes_chapter"} {
		t.Run(shape, func(t *testing.T) {
			s, store, g, in := newStoryboard138(t)
			state, _ := s.Authoring.Repository.Load(context.Background(), "alice", "p1")
			old := state.Chapters[0].ScriptText
			state.Chapters[0].ScriptText = "人工改过的当前剧本"
			_, e := s.Authoring.Repository.Save(context.Background(), state, state.Revision)
			if e != nil {
				t.Fatal(e)
			}
			in.Content = old
			switch shape {
			case "chapter":
			case "content":
				in.ChapterID = ""
			case "content_episode":
				in.ChapterID = ""
				in.EpisodeNo = 1
			case "episodes":
				in.ChapterID = ""
				in.Content = ""
				in.Episodes = map[int]string{1: old}
			case "episodes_chapter":
				in.Content = ""
				in.Episodes = map[int]string{1: old}
			}
			g.responses = []string{storyboard138Response("1-1-001", "1-1-002", "1-1-003")}
			out, e := s.Generate(context.Background(), in)
			boards, _ := store.ListStoryboards(context.Background(), "alice", "p1", "prompt_138")
			if e == nil || g.callCount() != 0 || len(boards) != 0 {
				t.Fatalf("request %s bypassed saved script: err=%v calls=%d boards=%d status=%+v", shape, e, g.callCount(), len(boards), out.GenerationAttempt)
			}
		})
	}
}
func TestStoryboard138RejectsCallerReplacementOfCurrentSavedScript(t *testing.T) {
	for _, shape := range []string{"content", "episodes"} {
		t.Run(shape, func(t *testing.T) {
			s, _, g, in := newStoryboard138(t)
			in.ChapterID = ""
			if shape == "content" {
				in.Content = "用户传入的另一份剧本"
			} else {
				in.Episodes = map[int]string{1: "用户传入的另一份剧本"}
			}
			_, e := s.Generate(context.Background(), in)
			if e == nil || g.callCount() != 0 {
				t.Fatal("replacement accepted", e, g.callCount())
			}
		})
	}
}
func TestStoryboard138SavedChapterContentWithoutIDRemainsSupported(t *testing.T) {
	for _, shape := range []string{"content", "episodes"} {
		t.Run(shape, func(t *testing.T) {
			s, _, g, in := newStoryboard138(t)
			state, _ := s.Authoring.Repository.Load(context.Background(), "alice", "p1")
			in.ChapterID = ""
			if shape == "content" {
				in.Content = state.Chapters[0].ScriptText
			} else {
				in.Episodes = map[int]string{1: state.Chapters[0].ScriptText}
			}
			g.responses = []string{storyboard138Response("1-1-001", "1-1-002", "1-1-003")}
			out, e := s.Generate(context.Background(), in)
			if e != nil || g.callCount() != 1 || len(out.Storyboards) != 1 {
				t.Fatal(out, e, g.callCount())
			}
		})
	}
}
func TestStoryboard138SavedChapterIdentityIsPartOfContinuation(t *testing.T) {
	s, _, g, in := newStoryboard138(t)
	st, _ := s.Authoring.Repository.Load(context.Background(), "alice", "p1")
	in.ChapterID = ""
	in.Content = st.Chapters[0].ScriptText
	g.responses = []string{storyboard138Response("1-1-001"), storyboard138Response("1-1-002", "1-1-003")}
	_, _ = s.Generate(context.Background(), in)
	st, _ = s.Authoring.Repository.Load(context.Background(), "alice", "p1")
	st.Chapters[0].ID = "replacement-chapter-with-identical-text"
	_, _ = s.Authoring.Repository.Save(context.Background(), st, st.Revision)
	in.RetryOf = in.IdempotencyKey
	in.IdempotencyKey = "replacement-source"
	in.ConfirmedRetry = true
	_, e := s.Generate(context.Background(), in)
	if e == nil || g.callCount() != 1 {
		t.Fatal("same text from another saved chapter resumed old result", e, g.callCount())
	}
}
func TestStoryboard138CurrentSavedScriptChangeDuringCallRejectsOldCallerText(t *testing.T) {
	for _, shape := range []string{"chapter", "content", "episodes"} {
		t.Run(shape, func(t *testing.T) {
			s, store, _, in := newStoryboard138(t)
			st, _ := s.Authoring.Repository.Load(context.Background(), "alice", "p1")
			old := st.Chapters[0].ScriptText
			if shape == "content" {
				in.ChapterID = ""
				in.Content = old
			}
			if shape == "episodes" {
				in.ChapterID = ""
				in.Episodes = map[int]string{1: old}
			}
			calls := 0
			s.Generator = storyboardGenerationTestFunc(func(context.Context, TextRequest) (TextResponse, error) {
				calls++
				current, _ := s.Authoring.Repository.Load(context.Background(), "alice", "p1")
				current.Chapters[0].ScriptText = "生成期间人工更新"
				_, _ = s.Authoring.Repository.Save(context.Background(), current, current.Revision)
				return TextResponse{Content: storyboard138Response("1-1-001", "1-1-002", "1-1-003")}, nil
			})
			out, e := s.Generate(context.Background(), in)
			boards, _ := store.ListStoryboards(context.Background(), "alice", "p1", "prompt_138")
			if e == nil || calls != 1 || len(boards) != 0 {
				t.Fatal("changed saved script published", out, e, calls, len(boards))
			}
			state, _ := store.LoadStoryboardGenerations(context.Background(), "alice", "p1")
			if state.Attempts[in.IdempotencyKey].Episodes[1].Calls[phase138(1, 1)].RawResponse == "" {
				t.Fatal("paid reply lost after source edit")
			}
		})
	}
}
