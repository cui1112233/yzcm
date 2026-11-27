package scripteditor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type recordingJobQueue struct{ ids []string }

func (q *recordingJobQueue) Enqueue(_ context.Context, id string) error {
	q.ids = append(q.ids, id)
	return nil
}
func (q *recordingJobQueue) Dequeue(context.Context, time.Duration) (string, error) {
	return "", ErrQueueEmpty
}

type episodeGenerator struct{ calls []string }

func (g *episodeGenerator) Generate(_ context.Context, _, user, _ string) (string, error) {
	g.calls = append(g.calls, user)
	if strings.Contains(user, "第2集") {
		return "", errors.New("provider unavailable")
	}
	return "第1集\n\n生成正文", nil
}

func newJobFixture(t *testing.T) (*Service, *recordingJobQueue, Work, Chapter) {
	t.Helper()
	store := newDeterministicMemoryStore()
	work, err := store.CreateWork(context.Background(), "alice", CreateWorkInput{Title: "测试作品", Type: WorkNovel})
	if err != nil {
		t.Fatal(err)
	}
	chapter, err := store.CreateChapter(context.Background(), "alice", work.ID, CreateChapterInput{Title: "第一章", Content: "原文"})
	if err != nil {
		t.Fatal(err)
	}
	queue := &recordingJobQueue{}
	service := NewService(store)
	service.JobQueue = queue
	return service, queue, work, chapter
}

func TestCreateGenerationJobIsIdempotentAndRetryOnlyUsesFailedEpisodes(t *testing.T) {
	ctx := context.Background()
	service, queue, work, chapter := newJobFixture(t)
	input := CreateGenerationJobInput{RequestKey: "generate-1", Episodes: []int{1, 2}, ChapterIDs: []string{chapter.ID}, ModelID: "text-model"}
	first, err := service.CreateGenerationJob(ctx, "alice", work.ID, input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.CreateGenerationJob(ctx, "alice", work.ID, input)
	if err != nil || second.ID != first.ID || len(queue.ids) != 1 {
		t.Fatalf("first=%#v second=%#v queue=%v err=%v", first, second, queue.ids, err)
	}

	result, _ := json.Marshal(JobResult{CompletedEpisodes: []int{1}, FailedEpisodes: []JobEpisodeFailure{{Episode: 2, Error: "失败"}}})
	first.Status, first.Result = JobPartial, result
	first, err = service.Store.UpdateJob(ctx, first, first.Revision)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := service.RetryJob(ctx, "alice", work.ID, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	var retriedInput JobInput
	if err := json.Unmarshal(retried.Input, &retriedInput); err != nil {
		t.Fatal(err)
	}
	if len(retriedInput.Episodes) != 1 || retriedInput.Episodes[0] != 2 || retried.RequestKey == first.RequestKey {
		t.Fatalf("retried=%#v input=%#v", retried, retriedInput)
	}
}

func TestRunJobKeepsSuccessfulEpisodesAndMarksPartial(t *testing.T) {
	ctx := context.Background()
	service, _, work, chapter := newJobFixture(t)
	job, err := service.CreateGenerationJob(ctx, "alice", work.ID, CreateGenerationJobInput{RequestKey: "partial-1", Episodes: []int{1, 2}, ChapterIDs: []string{chapter.ID}, ModelID: "text-model"})
	if err != nil {
		t.Fatal(err)
	}
	generator := &episodeGenerator{}
	service.GeneratorResolver = func(context.Context, string, string) (ScriptGenerator, error) { return generator, nil }
	job, err = service.RunJob(ctx, "alice", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobPartial || job.ProgressCurrent != 1 || job.ProgressTotal != 2 {
		t.Fatalf("job=%#v", job)
	}
	var result JobResult
	_ = json.Unmarshal(job.Result, &result)
	if len(result.CompletedEpisodes) != 1 || result.CompletedEpisodes[0] != 1 || len(result.FailedEpisodes) != 1 || result.FailedEpisodes[0].Episode != 2 {
		t.Fatalf("result=%#v", result)
	}
	scripts, _ := service.Store.ListScripts(ctx, "alice", work.ID)
	if len(scripts) != 1 || scripts[0].Episode != 1 {
		t.Fatalf("scripts=%#v", scripts)
	}
}

func TestRunJobDoesNotRepeatUncertainModelCall(t *testing.T) {
	ctx := context.Background()
	service, _, work, chapter := newJobFixture(t)
	job, _ := service.CreateGenerationJob(ctx, "alice", work.ID, CreateGenerationJobInput{RequestKey: "uncertain-1", Episodes: []int{1}, ChapterIDs: []string{chapter.ID}, ModelID: "text-model"})
	result, _ := json.Marshal(JobResult{Calls: map[string]JobCall{"episode-1": {Fingerprint: "abc", State: JobCallMayHaveSent}}})
	job.Status, job.Result = JobRunning, result
	job.LeaseUntil = time.Now().Add(-time.Minute)
	job, _ = service.Store.UpdateJob(ctx, job, job.Revision)
	generator := &episodeGenerator{}
	service.GeneratorResolver = func(context.Context, string, string) (ScriptGenerator, error) { return generator, nil }
	job, err := service.RunJob(ctx, "alice", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != JobFailed || job.ErrorCode != "RESULT_UNKNOWN" || len(generator.calls) != 0 {
		t.Fatalf("job=%#v calls=%d", job, len(generator.calls))
	}
}

func TestApplyPolishJobCreatesNewVersionOnlyAfterUserAction(t *testing.T) {
	ctx := context.Background()
	service, _, work, _ := newJobFixture(t)
	script, _ := service.Store.CreateScript(ctx, "alice", work.ID, CreateScriptInput{Episode: 1, Content: "原稿"})
	job, err := service.CreateReviewJob(ctx, "alice", work.ID, CreateReviewJobInput{RequestKey: "polish-1", ScriptID: script.ID, Kind: "polish", ModelID: "text-model"})
	if err != nil {
		t.Fatal(err)
	}
	service.GeneratorResolver = func(context.Context, string, string) (ScriptGenerator, error) {
		return ScriptGeneratorFunc(func(context.Context, string, string, string) (string, error) { return "润色后的完整剧本", nil }), nil
	}
	job, err = service.RunJob(ctx, "alice", job.ID)
	if err != nil || job.Status != JobSucceeded {
		t.Fatalf("job=%#v err=%v", job, err)
	}
	before, _ := service.Store.ListScripts(ctx, "alice", work.ID)
	if len(before) != 1 {
		t.Fatalf("worker overwrote script: %#v", before)
	}
	created, err := service.ApplyJobResult(ctx, "alice", work.ID, job.ID)
	if err != nil || created.Version != 2 || created.Content != "润色后的完整剧本" {
		t.Fatalf("created=%#v err=%v", created, err)
	}
}
