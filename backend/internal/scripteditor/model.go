package scripteditor

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound   = errors.New("script editor item not found")
	ErrConflict   = errors.New("script editor revision conflict")
	ErrForbidden  = errors.New("script editor access denied")
	ErrValidation = errors.New("script editor validation failed")
)

type WorkType string

const (
	WorkNovel  WorkType = "novel"
	WorkScript WorkType = "script"
)

func (v WorkType) Valid() bool { return v == WorkNovel || v == WorkScript }

type MarkStatus string

const (
	MarkNone    MarkStatus = ""
	MarkKey     MarkStatus = "key"
	MarkPending MarkStatus = "pending"
	MarkDone    MarkStatus = "done"
)

func (v MarkStatus) Valid() bool {
	return v == MarkNone || v == MarkKey || v == MarkPending || v == MarkDone
}

type GenerationSource string

const (
	SourceManual GenerationSource = "manual"
	SourceImport GenerationSource = "import"
	SourceAI     GenerationSource = "ai"
	SourceCopy   GenerationSource = "copy"
)

type JobStatus string

const (
	JobQueued    JobStatus = "queued"
	JobRunning   JobStatus = "running"
	JobSucceeded JobStatus = "succeeded"
	JobPartial   JobStatus = "partial"
	JobFailed    JobStatus = "failed"
	JobCancelled JobStatus = "cancelled"
)

func (v JobStatus) Valid() bool {
	switch v {
	case JobQueued, JobRunning, JobSucceeded, JobPartial, JobFailed, JobCancelled:
		return true
	default:
		return false
	}
}

func NormalizeTitle(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > 120 {
		return "", ErrValidation
	}
	return value, nil
}

type Work struct {
	ID             string     `json:"id"`
	OwnerUsername  string     `json:"-"`
	Title          string     `json:"title"`
	Type           WorkType   `json:"type"`
	SourceFilename string     `json:"sourceFilename,omitempty"`
	Mark           MarkStatus `json:"markStatus,omitempty"`
	Revision       int64      `json:"revision"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	DeletedAt      *time.Time `json:"-"`
}

type WorkSummary struct {
	Work
	ChapterCount int `json:"chapterCount"`
	ScriptCount  int `json:"scriptCount"`
	WordCount    int `json:"wordCount"`
}

type WorkDetail struct {
	Work
	Chapters []Chapter `json:"chapters"`
	Scripts  []Script  `json:"scripts"`
}

type Chapter struct {
	ID                      string    `json:"id"`
	WorkID                  string    `json:"workId"`
	OwnerUsername           string    `json:"-"`
	Title                   string    `json:"title"`
	Order                   int       `json:"order"`
	Content                 string    `json:"content"`
	WordCount               int       `json:"wordCount"`
	Revision                int64     `json:"revision"`
	CreatedAt               time.Time `json:"createdAt"`
	UpdatedAt               time.Time `json:"updatedAt"`
	ReferencedByScriptCount int       `json:"referencedByScriptCount,omitempty"`
}

type Script struct {
	ID               string           `json:"id"`
	WorkID           string           `json:"workId"`
	OwnerUsername    string           `json:"-"`
	Episode          int              `json:"episode"`
	Version          int              `json:"version"`
	Title            string           `json:"title"`
	Content          string           `json:"content"`
	SourceChapterIDs []string         `json:"sourceChapterIds"`
	Preferred        bool             `json:"preferred"`
	GenerationSource GenerationSource `json:"generationSource"`
	ModelID          string           `json:"modelId,omitempty"`
	PromptID         string           `json:"promptId,omitempty"`
	PromptRevision   int64            `json:"promptRevision,omitempty"`
	Revision         int64            `json:"revision"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
}

type Review struct {
	ID            string          `json:"id"`
	WorkID        string          `json:"workId"`
	ScriptID      string          `json:"scriptId"`
	OwnerUsername string          `json:"-"`
	Type          string          `json:"type"`
	Result        json.RawMessage `json:"result"`
	ModelID       string          `json:"modelId,omitempty"`
	PromptID      string          `json:"promptId,omitempty"`
	CreatedAt     time.Time       `json:"createdAt"`
}

type Job struct {
	ID              string          `json:"id"`
	OwnerUsername   string          `json:"-"`
	WorkID          string          `json:"workId"`
	Type            string          `json:"type"`
	Status          JobStatus       `json:"status"`
	RequestKey      string          `json:"requestKey"`
	Input           json.RawMessage `json:"input"`
	Result          json.RawMessage `json:"result"`
	ProgressCurrent int             `json:"progressCurrent"`
	ProgressTotal   int             `json:"progressTotal"`
	ErrorCode       string          `json:"errorCode,omitempty"`
	ErrorMessage    string          `json:"errorMessage,omitempty"`
	LeaseToken      string          `json:"-"`
	LeaseUntil      time.Time       `json:"-"`
	Revision        int64           `json:"revision"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

type JobCallState string

const (
	JobCallMayHaveSent JobCallState = "may_have_sent"
	JobCallSucceeded   JobCallState = "succeeded"
	JobCallFailed      JobCallState = "failed"
)

type JobCall struct {
	Fingerprint string       `json:"fingerprint"`
	State       JobCallState `json:"state"`
}

type JobEpisodeFailure struct {
	Episode int    `json:"episode"`
	Error   string `json:"error"`
}

type JobResult struct {
	CompletedEpisodes []int               `json:"completedEpisodes,omitempty"`
	FailedEpisodes    []JobEpisodeFailure `json:"failedEpisodes,omitempty"`
	ScriptIDs         []string            `json:"scriptIds,omitempty"`
	ReviewID          string              `json:"reviewId,omitempty"`
	Output            string              `json:"output,omitempty"`
	Calls             map[string]JobCall  `json:"calls,omitempty"`
}

type Handoff struct {
	ID                  string          `json:"id"`
	OwnerUsername       string          `json:"-"`
	WorkID              string          `json:"workId"`
	RequestKey          string          `json:"requestKey"`
	ScreenplayProjectID string          `json:"screenplayProjectId"`
	Episodes            json.RawMessage `json:"episodes"`
	CreatedAt           time.Time       `json:"createdAt"`
}
