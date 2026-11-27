package batchfactoryv11

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type CreationMode string

var ErrMultiModeSourcePending = errors.New("共享原文正在等待回填")

type ModeProjectInput struct {
	Mode          CreationMode  `json:"mode"`
	Settings      SettingsPatch `json:"settings"`
	PresetID      string        `json:"presetId,omitempty"`
	ExecutionMode string        `json:"executionMode"`
}
type CreateMultiModeInput struct {
	RequestID string             `json:"requestId"`
	Title     string             `json:"title"`
	Books     []CreateBookInput  `json:"books"`
	Projects  []ModeProjectInput `json:"projects"`
}
type MultiModeProject struct {
	Mode          CreationMode `json:"mode"`
	BatchID       string       `json:"batchId"`
	PresetID      string       `json:"presetId,omitempty"`
	ExecutionMode string       `json:"executionMode"`
}
type MultiModeCreationResult struct {
	GroupID  string             `json:"groupId"`
	Batches  []Batch            `json:"batches"`
	Projects []MultiModeProject `json:"projects"`
}
type MultiModeSourceSnapshot struct {
	Title       string         `json:"title"`
	OriginalRaw string         `json:"originalRaw"`
	Metadata    map[string]any `json:"metadata"`
}
type MultiModeSourceJob struct {
	GroupID    string                  `json:"groupId"`
	Key        string                  `json:"key"`
	Input      CreateBookInput         `json:"input"`
	Status     string                  `json:"status"`
	Token      string                  `json:"-"`
	LeaseUntil time.Time               `json:"-"`
	Snapshot   MultiModeSourceSnapshot `json:"snapshot"`
	Error      string                  `json:"error,omitempty"`
}
type MultiModeCreationStore interface {
	CreateMultiModeBatches(context.Context, string, CreateMultiModeInput) (MultiModeCreationResult, error)
	GetMultiModeCreation(context.Context, string, string) (MultiModeCreationResult, error)
	ListMultiModeSources(context.Context, string, string) ([]MultiModeSourceJob, error)
	ClaimMultiModeSource(context.Context, string, string, string) (MultiModeSourceJob, bool, error)
	CompleteMultiModeSource(context.Context, string, string, string, string, MultiModeSourceSnapshot, error) error
	DeliverMultiModeSources(context.Context, string, string) error
}

var creationModeNames = map[CreationMode]string{"video": "视频", "comic": "漫画", "illustrated_text": "文字插画", "text": "文本"}

func normalizeMultiModeInput(input CreateMultiModeInput) (CreateMultiModeInput, string, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Title = strings.TrimSpace(input.Title)
	if len(input.RequestID) < 1 || len(input.RequestID) > 128 || input.Title == "" || len(input.Books) == 0 || len(input.Books) > 500 || len(input.Projects) < 1 || len(input.Projects) > 4 {
		return input, "", fmt.Errorf("%w: 请填写书单并选择1～4种不同生产模式", ErrInvalid)
	}
	seen := map[CreationMode]bool{}
	for i, project := range input.Projects {
		if creationModeNames[project.Mode] == "" || seen[project.Mode] {
			return input, "", fmt.Errorf("%w: 生产模式无效或重复", ErrInvalid)
		}
		seen[project.Mode] = true
		if project.ExecutionMode == "" {
			project.ExecutionMode = "video_no_submit"
		}
		if project.ExecutionMode != "full_submit" && project.ExecutionMode != "video_no_submit" && project.ExecutionMode != "storyboard_only" {
			return input, "", fmt.Errorf("%w: 执行模式无效", ErrInvalid)
		}
		project.Settings = clonePatch(project.Settings)
		mode := string(project.Mode)
		if project.Mode == "illustrated_text" {
			mode = "comic"
		}
		project.Settings["productionMode"], _ = json.Marshal(mode)
		project.Settings["outputKind"], _ = json.Marshal(project.Mode)
		var config AIReasoningPromptConfig
		if len(project.Settings["aiPromptConfig"]) > 0 {
			if json.Unmarshal(project.Settings["aiPromptConfig"], &config) != nil {
				return input, "", ErrInvalid
			}
			if _, err := NormalizeComicStyleOutputs(config.Comic); err != nil {
				return input, "", err
			}
		}
		if project.Mode == "illustrated_text" {
			config.Comic.ID = ComicPageIllustratedTextPresetID
			config.Comic.Enabled = true
			project.Settings["aiPromptConfig"], _ = json.Marshal(config)
		}
		input.Projects[i] = project
	}
	// Never duplicate jobs, selected images, receipts or arbitrary runtime metadata.
	for i, book := range input.Books {
		book.Videos = nil
		book.SourceMetadata = multiModeSourceMetadata(book.SourceMetadata)
		if book.OriginalRaw == "" && book.SourceMetadata["rawSourceStatus"] != "unverified" && !isExcerptSource(book.SourceMetadata) {
			book.OriginalRaw = book.TxtText
		}

		input.Books[i] = book
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return input, "", ErrInvalid
	}
	sum := sha256.Sum256(raw)
	return input, hex.EncodeToString(sum[:]), nil
}
func multiModeSourceKey(book CreateBookInput) string {
	key := strings.TrimSpace(book.Platform) + "|" + strings.TrimSpace(sourceBookID(book))
	if strings.TrimSpace(sourceBookID(book)) == "" {
		key += "|" + book.OriginalRaw + "|" + book.Title
	}
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
func multiModeSourceMetadata(input map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"gender", "gender_name", "style", "styleName", "style_name", "category", "genre", "themes", "theme", "tags", "labels", "reason", "rating", "platformId", "platformName", "sourceLine", "materialId", "giantMaterialId", "qingyuMaterialId", "originalReadStrategy", "sourceKind", "customCopyId", "rawSourceOrigin", "rawSourceStatus", "sourceMode", "customCopyRecordId", "genderSource", "styleSource", "contentPending", "originalReadStage", "platformCode", "videoUrl", "videoDurationSeconds", "contentCaptureCharacters", "contentRangeLines", "comicAnalysisCharacters"} {
		if v, ok := input[key]; ok {
			raw, _ := json.Marshal(v)
			var copied any
			if json.Unmarshal(raw, &copied) == nil {
				out[key] = copied
			}
		}
	}
	return out
}
func multiModeBatchInput(input CreateMultiModeInput, project ModeProjectInput, groupID string) CreateBatchInput {
	books := make([]CreateBookInput, len(input.Books))
	for i, source := range input.Books {
		raw, _ := json.Marshal(source)
		_ = json.Unmarshal(raw, &books[i])
		if books[i].SourceMetadata == nil {
			books[i].SourceMetadata = map[string]any{}
		}
		applyMultiModeControls(books[i].SourceMetadata, project.Settings)
		books[i].SourceText = ""
		books[i].TxtText = ""
		books[i].OriginalRaw = ""
		books[i].SourceMetadata["multimodeGroupId"] = groupID
		books[i].SourceMetadata["multimodeSourceKey"] = multiModeSourceKey(source)
		books[i].SourceMetadata["multiModeSourceState"] = "pending"
	}
	settings := clonePatch(project.Settings)
	settings["multimodeGroupId"], _ = json.Marshal(groupID)
	return CreateBatchInput{PreserveBookRows: true, Title: input.Title + " · " + creationModeNames[project.Mode], Books: books, InitialBatchSettings: settings}
}
func newMultiModeSources(input CreateMultiModeInput, groupID string) []MultiModeSourceJob {
	var jobs []MultiModeSourceJob
	seen := map[string]bool{}
	for _, book := range input.Books {
		key := multiModeSourceKey(book)
		if seen[key] {
			continue
		}
		seen[key] = true
		jobs = append(jobs, MultiModeSourceJob{GroupID: groupID, Key: key, Input: book, Status: "pending"})
	}
	return jobs
}
func batchCreationBooks(input CreateBatchInput) []CreateBookInput {
	if input.PreserveBookRows {
		return input.Books
	}
	return uniqueBatchBooks(input.Books, map[string]bool{})
}

// Work is called only by an existing request/job. Claims are short durable
// leases, not a new background polling loop. Delivery can safely resume later.
func ResolveMultiModeSources(ctx context.Context, store MultiModeCreationStore, owner, groupID string, prepare func(context.Context, CreateBookInput) (MultiModeSourceSnapshot, error)) error {
	return resolveMultiModeSources(ctx, store, owner, groupID, "", prepare)
}

// ResolveMultiModeSource is used by an individual book's existing worker.
// Other books may still be reading or have failed; they must not block this book.
func ResolveMultiModeSource(ctx context.Context, store MultiModeCreationStore, owner, groupID, sourceKey string, prepare func(context.Context, CreateBookInput) (MultiModeSourceSnapshot, error)) error {
	if sourceKey == "" {
		return ErrInvalid
	}
	return resolveMultiModeSources(ctx, store, owner, groupID, sourceKey, prepare)
}

func resolveMultiModeSources(ctx context.Context, store MultiModeCreationStore, owner, groupID, sourceKey string, prepare func(context.Context, CreateBookInput) (MultiModeSourceSnapshot, error)) error {
	jobs, err := store.ListMultiModeSources(ctx, owner, groupID)
	if err != nil {
		return err
	}
	var failures []error
	found := sourceKey == ""
	for _, job := range jobs {
		if sourceKey != "" && job.Key != sourceKey {
			continue
		}
		found = true
		if job.Status == "ready" {
			continue
		}
		claimed, ok, claimErr := store.ClaimMultiModeSource(ctx, owner, groupID, job.Key)
		if claimErr != nil {
			return claimErr
		}
		if !ok {
			failures = append(failures, ErrMultiModeSourcePending)
			continue
		}
		snapshot, workErr := prepare(ctx, claimed.Input)
		saveCtx, cancelSave := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		saveErr := store.CompleteMultiModeSource(saveCtx, owner, groupID, job.Key, claimed.Token, snapshot, workErr)
		cancelSave()
		if saveErr != nil {
			return saveErr
		}
		if workErr != nil {
			failures = append(failures, workErr)
		}
	}
	if !found {
		return ErrNotFound
	}
	if err = store.DeliverMultiModeSources(ctx, owner, groupID); err != nil {
		return err
	}
	if len(failures) > 0 {
		return fmt.Errorf("%w: %w", ErrUnavailable, errors.Join(failures...))
	}
	return nil
}

var multiModeControlKeys = []string{"contentCaptureCharacters", "contentRangeLines", "comicAnalysisCharacters"}

func applyMultiModeControls(metadata map[string]any, settings SettingsPatch) {
	for _, key := range multiModeControlKeys {
		var value any
		if raw := settings[key]; len(raw) > 0 && json.Unmarshal(raw, &value) == nil {
			metadata[key] = value
		}
	}
}
func preserveMultiModeControls(metadata, current map[string]any) {
	for _, key := range multiModeControlKeys {
		if value, ok := current[key]; ok {
			metadata[key] = value
		}
	}
}
