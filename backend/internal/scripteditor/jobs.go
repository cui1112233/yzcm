package scripteditor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"qiantie/backend/internal/prompt"
)

var ErrQueueEmpty = errors.New("script editor queue empty")

type JobQueue interface {
	Enqueue(context.Context, string) error
	Dequeue(context.Context, time.Duration) (string, error)
}

type ScriptGeneratorFunc func(context.Context, string, string, string) (string, error)

func (fn ScriptGeneratorFunc) Generate(ctx context.Context, system, user, model string) (string, error) {
	return fn(ctx, system, user, model)
}

type JobInput struct {
	Episodes   []int    `json:"episodes,omitempty"`
	ChapterIDs []string `json:"chapterIds,omitempty"`
	ScriptID   string   `json:"scriptId,omitempty"`
	ModelID    string   `json:"modelId"`
	Extra      string   `json:"extra,omitempty"`
	Kind       string   `json:"kind"`
}

type CreateGenerationJobInput struct {
	RequestKey string   `json:"requestKey"`
	Episodes   []int    `json:"episodes"`
	ChapterIDs []string `json:"chapterIds"`
	ModelID    string   `json:"modelId"`
	Extra      string   `json:"extra"`
}

type CreateReviewJobInput struct {
	RequestKey string `json:"requestKey"`
	ScriptID   string `json:"scriptId"`
	Kind       string `json:"kind"`
	ModelID    string `json:"modelId"`
	Extra      string `json:"extra"`
}

func (s *Service) CreateGenerationJob(ctx context.Context, owner, workID string, input CreateGenerationJobInput) (Job, error) {
	if s == nil || s.Store == nil || strings.TrimSpace(input.RequestKey) == "" || strings.TrimSpace(input.ModelID) == "" || len(input.Episodes) == 0 || len(input.ChapterIDs) == 0 {
		return Job{}, ErrValidation
	}
	detail, err := s.Store.GetWork(ctx, owner, workID)
	if err != nil {
		return Job{}, err
	}
	if existing, ok := s.jobByRequest(ctx, owner, workID, strings.TrimSpace(input.RequestKey)); ok {
		return existing, nil
	}
	available := map[string]bool{}
	for _, chapter := range detail.Chapters {
		available[chapter.ID] = true
	}
	for _, id := range input.ChapterIDs {
		if !available[id] {
			return Job{}, ErrValidation
		}
	}
	episodes := append([]int(nil), input.Episodes...)
	sort.Ints(episodes)
	for i, episode := range episodes {
		if episode < 1 || i > 0 && episodes[i-1] == episode {
			return Job{}, ErrValidation
		}
	}
	payload, _ := json.Marshal(JobInput{Episodes: episodes, ChapterIDs: input.ChapterIDs, ModelID: strings.TrimSpace(input.ModelID), Extra: strings.TrimSpace(input.Extra), Kind: "generation"})
	job := Job{ID: randomID("job"), OwnerUsername: owner, WorkID: workID, Type: "script-generation", Status: JobQueued, RequestKey: strings.TrimSpace(input.RequestKey), Input: payload, Result: json.RawMessage(`{}`), ProgressTotal: len(episodes)}
	job, err = s.Store.CreateJob(ctx, job)
	if err == nil && s.JobQueue != nil && job.Status == JobQueued {
		_ = s.JobQueue.Enqueue(ctx, job.ID)
	}
	return job, err
}

func (s *Service) CreateReviewJob(ctx context.Context, owner, workID string, input CreateReviewJobInput) (Job, error) {
	kind := strings.TrimSpace(input.Kind)
	if kind != "review" && kind != "polish" && kind != "viral-opening" {
		return Job{}, ErrValidation
	}
	if strings.TrimSpace(input.RequestKey) == "" || strings.TrimSpace(input.ModelID) == "" || strings.TrimSpace(input.ScriptID) == "" {
		return Job{}, ErrValidation
	}
	if existing, ok := s.jobByRequest(ctx, owner, workID, strings.TrimSpace(input.RequestKey)); ok {
		return existing, nil
	}
	scripts, err := s.Store.ListScripts(ctx, owner, workID)
	if err != nil {
		return Job{}, err
	}
	found := false
	for _, script := range scripts {
		if script.ID == input.ScriptID {
			found = true
			break
		}
	}
	if !found {
		return Job{}, ErrNotFound
	}
	payload, _ := json.Marshal(JobInput{ScriptID: input.ScriptID, ModelID: strings.TrimSpace(input.ModelID), Extra: strings.TrimSpace(input.Extra), Kind: kind})
	job := Job{ID: randomID("job"), OwnerUsername: owner, WorkID: workID, Type: kind, Status: JobQueued, RequestKey: strings.TrimSpace(input.RequestKey), Input: payload, Result: json.RawMessage(`{}`), ProgressTotal: 1}
	job, err = s.Store.CreateJob(ctx, job)
	if err == nil && s.JobQueue != nil && job.Status == JobQueued {
		_ = s.JobQueue.Enqueue(ctx, job.ID)
	}
	return job, err
}

func (s *Service) jobByRequest(ctx context.Context, owner, workID, requestKey string) (Job, bool) {
	rows, err := s.Store.ListJobs(ctx, owner, workID)
	if err != nil {
		return Job{}, false
	}
	for _, row := range rows {
		if row.RequestKey == requestKey {
			return row, true
		}
	}
	return Job{}, false
}

func (s *Service) RetryJob(ctx context.Context, owner, workID, id string) (Job, error) {
	job, err := s.Store.GetJob(ctx, owner, workID, id)
	if err != nil {
		return Job{}, err
	}
	if job.Status != JobFailed && job.Status != JobPartial && job.Status != JobCancelled {
		return Job{}, ErrValidation
	}
	var input JobInput
	var result JobResult
	if json.Unmarshal(job.Input, &input) != nil || json.Unmarshal(job.Result, &result) != nil {
		return Job{}, ErrValidation
	}
	if job.Type == "script-generation" && job.Status != JobCancelled {
		input.Episodes = nil
		for _, failed := range result.FailedEpisodes {
			input.Episodes = append(input.Episodes, failed.Episode)
		}
		if len(input.Episodes) == 0 {
			return Job{}, ErrValidation
		}
	}
	payload, _ := json.Marshal(input)
	total := len(input.Episodes)
	if total == 0 {
		total = 1
	}
	retry := Job{ID: randomID("job"), OwnerUsername: owner, WorkID: workID, Type: job.Type, Status: JobQueued, RequestKey: job.RequestKey + ":retry:" + randomID("r"), Input: payload, Result: json.RawMessage(`{}`), ProgressTotal: total}
	retry, err = s.Store.CreateJob(ctx, retry)
	if err == nil && s.JobQueue != nil {
		_ = s.JobQueue.Enqueue(ctx, retry.ID)
	}
	return retry, err
}

func (s *Service) CancelJob(ctx context.Context, owner, workID, id string) (Job, error) {
	job, err := s.Store.GetJob(ctx, owner, workID, id)
	if err != nil {
		return Job{}, err
	}
	if job.Status != JobQueued && job.Status != JobRunning {
		return Job{}, ErrValidation
	}
	job.Status, job.LeaseToken, job.LeaseUntil = JobCancelled, "", time.Time{}
	return s.Store.UpdateJob(ctx, job, job.Revision)
}

func (s *Service) ListJobs(ctx context.Context, owner, workID string) ([]Job, error) {
	return s.Store.ListJobs(ctx, owner, workID)
}

func (s *Service) RunJob(ctx context.Context, owner, id string) (Job, error) {
	if s == nil || s.Store == nil {
		return Job{}, ErrValidation
	}
	job, err := s.Store.ClaimJob(ctx, owner, id, randomID("lease"), time.Now().UTC().Add(15*time.Minute))
	if err != nil {
		return Job{}, err
	}
	var input JobInput
	var result JobResult
	if json.Unmarshal(job.Input, &input) != nil {
		return s.failJob(ctx, job, "INVALID_INPUT", "任务参数损坏")
	}
	_ = json.Unmarshal(job.Result, &result)
	if result.Calls == nil {
		result.Calls = map[string]JobCall{}
	}
	for _, call := range result.Calls {
		if call.State == JobCallMayHaveSent {
			return s.failJob(ctx, job, "RESULT_UNKNOWN", "模型请求可能已经发出，已停止自动重试，请人工确认")
		}
	}
	if s.GeneratorResolver == nil {
		return s.failJob(ctx, job, "MODEL_UNAVAILABLE", "文字模型不可用")
	}
	generator, err := s.GeneratorResolver(ctx, owner, input.ModelID)
	if err != nil || generator == nil {
		return s.failJob(ctx, job, "MODEL_UNAVAILABLE", "文字模型不可用")
	}
	if job.Type == "script-generation" {
		return s.runGenerationJob(ctx, job, input, result, generator)
	}
	return s.runReviewJob(ctx, job, input, result, generator)
}

func (s *Service) runGenerationJob(ctx context.Context, job Job, input JobInput, result JobResult, generator ScriptGenerator) (Job, error) {
	detail, err := s.Store.GetWork(ctx, job.OwnerUsername, job.WorkID)
	if err != nil {
		return Job{}, err
	}
	selected := map[string]bool{}
	for _, id := range input.ChapterIDs {
		selected[id] = true
	}
	var source strings.Builder
	for _, chapter := range detail.Chapters {
		if selected[chapter.ID] {
			fmt.Fprintf(&source, "\n## %s\n%s\n", chapter.Title, chapter.Content)
		}
	}
	system, _ := prompt.ScriptEditorPrompt("generate")
	for _, episode := range input.Episodes {
		current, err := s.Store.GetJob(ctx, job.OwnerUsername, job.WorkID, job.ID)
		if err != nil {
			return Job{}, err
		}
		if current.Status == JobCancelled {
			return current, nil
		}
		job = current
		user := fmt.Sprintf("作品：%s\n目标：第%d集\n额外要求：%s\n原文：%s", detail.Title, episode, input.Extra, source.String())
		key := fmt.Sprintf("episode-%d", episode)
		job, result, err = s.beforeModelCall(ctx, job, result, key, system, user, input.ModelID)
		if err != nil {
			return Job{}, err
		}
		content, callErr := generator.Generate(ctx, system, user, input.ModelID)
		if callErr != nil || strings.TrimSpace(content) == "" {
			result.Calls[key] = JobCall{Fingerprint: result.Calls[key].Fingerprint, State: JobCallFailed}
			message := "模型没有返回内容"
			if callErr != nil {
				message = callErr.Error()
			}
			result.FailedEpisodes = append(result.FailedEpisodes, JobEpisodeFailure{Episode: episode, Error: message})
			job, err = s.saveJobResult(ctx, job, result)
			if err != nil {
				return Job{}, err
			}
			continue
		}
		script, saveErr := s.Store.CreateScript(ctx, job.OwnerUsername, job.WorkID, CreateScriptInput{Episode: episode, Title: fmt.Sprintf("第%d集", episode), Content: content, SourceChapterIDs: input.ChapterIDs, GenerationSource: SourceAI, ModelID: input.ModelID, PromptID: "script-editor.generate", PromptRevision: 1})
		if saveErr != nil {
			return s.failJob(ctx, job, "RESULT_UNKNOWN", "模型已返回但剧本保存失败，请人工确认")
		}
		result.Calls[key] = JobCall{Fingerprint: result.Calls[key].Fingerprint, State: JobCallSucceeded}
		result.CompletedEpisodes = append(result.CompletedEpisodes, episode)
		result.ScriptIDs = append(result.ScriptIDs, script.ID)
		job.ProgressCurrent = len(result.CompletedEpisodes)
		job, err = s.saveJobResult(ctx, job, result)
		if err != nil {
			return Job{}, err
		}
	}
	job.Status = JobSucceeded
	if len(result.FailedEpisodes) > 0 {
		job.Status = JobPartial
		if len(result.CompletedEpisodes) == 0 {
			job.Status = JobFailed
		}
	}
	job.LeaseToken, job.LeaseUntil = "", time.Time{}
	return s.Store.UpdateJob(ctx, job, job.Revision)
}

func (s *Service) runReviewJob(ctx context.Context, job Job, input JobInput, result JobResult, generator ScriptGenerator) (Job, error) {
	scripts, err := s.Store.ListScripts(ctx, job.OwnerUsername, job.WorkID)
	if err != nil {
		return Job{}, err
	}
	var source Script
	for _, row := range scripts {
		if row.ID == input.ScriptID {
			source = row
			break
		}
	}
	if source.ID == "" {
		return s.failJob(ctx, job, "SCRIPT_NOT_FOUND", "剧本版本不存在")
	}
	system, _ := prompt.ScriptEditorPrompt(input.Kind)
	user := fmt.Sprintf("剧本：\n%s\n\n用户补充要求：%s", source.Content, input.Extra)
	job, result, err = s.beforeModelCall(ctx, job, result, "review", system, user, input.ModelID)
	if err != nil {
		return Job{}, err
	}
	output, err := generator.Generate(ctx, system, user, input.ModelID)
	if err != nil || strings.TrimSpace(output) == "" {
		return s.failJob(ctx, job, "MODEL_FAILED", "模型没有返回结果")
	}
	reviewRaw, _ := json.Marshal(map[string]string{"output": output})
	review, err := s.Store.CreateReview(ctx, Review{ID: randomID("review"), WorkID: job.WorkID, ScriptID: source.ID, OwnerUsername: job.OwnerUsername, Type: input.Kind, Result: reviewRaw, ModelID: input.ModelID, PromptID: "script-editor." + input.Kind})
	if err != nil {
		return s.failJob(ctx, job, "RESULT_UNKNOWN", "模型已返回但审稿结果保存失败，请人工确认")
	}
	result.Calls["review"] = JobCall{Fingerprint: result.Calls["review"].Fingerprint, State: JobCallSucceeded}
	result.Output, result.ReviewID = output, review.ID
	job.Result, _ = json.Marshal(result)
	job.ProgressCurrent, job.Status, job.LeaseToken, job.LeaseUntil = 1, JobSucceeded, "", time.Time{}
	return s.Store.UpdateJob(ctx, job, job.Revision)
}

func (s *Service) beforeModelCall(ctx context.Context, job Job, result JobResult, key, system, user, model string) (Job, JobResult, error) {
	digest := sha256.Sum256([]byte(system + "\x00" + user + "\x00" + model))
	result.Calls[key] = JobCall{Fingerprint: hex.EncodeToString(digest[:]), State: JobCallMayHaveSent}
	saved, err := s.saveJobResult(ctx, job, result)
	return saved, result, err
}

func (s *Service) saveJobResult(ctx context.Context, job Job, result JobResult) (Job, error) {
	job.Result, _ = json.Marshal(result)
	return s.Store.UpdateJob(ctx, job, job.Revision)
}
func (s *Service) failJob(ctx context.Context, job Job, code, message string) (Job, error) {
	job.Status, job.ErrorCode, job.ErrorMessage, job.LeaseToken, job.LeaseUntil = JobFailed, code, message, "", time.Time{}
	return s.Store.UpdateJob(ctx, job, job.Revision)
}

func (s *Service) ApplyJobResult(ctx context.Context, owner, workID, id string) (Script, error) {
	job, err := s.Store.GetJob(ctx, owner, workID, id)
	if err != nil {
		return Script{}, err
	}
	if job.Status != JobSucceeded || job.Type != "polish" && job.Type != "viral-opening" {
		return Script{}, ErrValidation
	}
	var input JobInput
	var result JobResult
	if json.Unmarshal(job.Input, &input) != nil || json.Unmarshal(job.Result, &result) != nil || strings.TrimSpace(result.Output) == "" {
		return Script{}, ErrValidation
	}
	scripts, err := s.Store.ListScripts(ctx, owner, workID)
	if err != nil {
		return Script{}, err
	}
	var source Script
	for _, row := range scripts {
		if row.ID == input.ScriptID {
			source = row
			break
		}
	}
	if source.ID == "" {
		return Script{}, ErrNotFound
	}
	return s.Store.CreateScript(ctx, owner, workID, CreateScriptInput{Episode: source.Episode, Title: source.Title, Content: result.Output, SourceChapterIDs: source.SourceChapterIDs, GenerationSource: SourceAI, ModelID: input.ModelID, PromptID: "script-editor." + job.Type, PromptRevision: 1})
}
