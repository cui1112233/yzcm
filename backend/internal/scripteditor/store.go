package scripteditor

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"unicode"
)

type CreateWorkInput struct {
	Title          string
	Type           WorkType
	SourceFilename string
}

type WorkFilter struct {
	Type  WorkType
	Mark  MarkStatus
	Query string
	Sort  string
}

type UpdateWorkInput struct {
	Title *string
	Mark  *MarkStatus
}

type CreateChapterInput struct {
	Title   string
	Content string
	Order   int
}

type UpdateChapterInput struct {
	Title   *string
	Content *string
}

type CreateScriptInput struct {
	Episode          int
	Version          int
	Title            string
	Content          string
	SourceChapterIDs []string
	GenerationSource GenerationSource
	ModelID          string
	PromptID         string
	PromptRevision   int64
}

type UpdateScriptInput struct {
	Title   *string
	Content *string
}

type Store interface {
	CreateWork(context.Context, string, CreateWorkInput) (Work, error)
	ListWorks(context.Context, string, WorkFilter) ([]WorkSummary, error)
	GetWork(context.Context, string, string) (WorkDetail, error)
	UpdateWork(context.Context, string, string, UpdateWorkInput, int64) (Work, error)
	DeleteWork(context.Context, string, string, int64) error

	CreateChapter(context.Context, string, string, CreateChapterInput) (Chapter, error)
	UpdateChapter(context.Context, string, string, string, UpdateChapterInput, int64) (Chapter, error)
	DeleteChapter(context.Context, string, string, string, int64) error
	ReorderChapters(context.Context, string, string, []string) ([]Chapter, error)

	CreateScript(context.Context, string, string, CreateScriptInput) (Script, error)
	ListScripts(context.Context, string, string) ([]Script, error)
	UpdateScript(context.Context, string, string, string, UpdateScriptInput, int64) (Script, error)
	DeleteScript(context.Context, string, string, string, int64) error
	CopyScript(context.Context, string, string, string) (Script, error)
	PreferScript(context.Context, string, string, string, int64) (Script, error)
}

func randomID(prefix string) string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(raw[:])
}

func countWords(value string) int {
	count := 0
	for _, r := range value {
		if !unicode.IsSpace(r) {
			count++
		}
	}
	return count
}

func normalizedOwner(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrForbidden
	}
	return value, nil
}
