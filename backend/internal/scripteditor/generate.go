package scripteditor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"qiantie/backend/internal/prompt"
)

type ScriptGenerator interface {
	Generate(context.Context, string, string, string) (string, error)
}

type GenerateScriptInput struct {
	Episode    int      `json:"episode"`
	ChapterIDs []string `json:"chapterIds"`
	ModelID    string   `json:"modelId"`
	Extra      string   `json:"extra"`
}

func (s *Service) GenerateScript(ctx context.Context, owner, workID string, input GenerateScriptInput, generator ScriptGenerator) (Script, error) {
	if s == nil || s.Store == nil || generator == nil || input.Episode < 1 || strings.TrimSpace(input.ModelID) == "" {
		return Script{}, ErrValidation
	}
	detail, err := s.Store.GetWork(ctx, owner, workID)
	if err != nil {
		return Script{}, err
	}
	selected := map[string]bool{}
	for _, id := range input.ChapterIDs {
		selected[strings.TrimSpace(id)] = true
	}
	chapters := append([]Chapter(nil), detail.Chapters...)
	sort.Slice(chapters, func(i, j int) bool { return chapters[i].Order < chapters[j].Order })
	var source strings.Builder
	for _, chapter := range chapters {
		if len(selected) > 0 && !selected[chapter.ID] {
			continue
		}
		fmt.Fprintf(&source, "\n## %s\n%s\n", chapter.Title, chapter.Content)
	}
	if strings.TrimSpace(source.String()) == "" {
		return Script{}, ErrValidation
	}
	system, _ := prompt.ScriptEditorPrompt("generate")
	user := fmt.Sprintf("作品：%s\n目标：第%d集\n额外要求：%s\n原文：%s", detail.Title, input.Episode, strings.TrimSpace(input.Extra), source.String())
	content, err := generator.Generate(ctx, system, user, input.ModelID)
	if err != nil {
		return Script{}, err
	}
	if strings.TrimSpace(content) == "" {
		return Script{}, errors.New("模型没有返回剧本内容")
	}
	return s.Store.CreateScript(ctx, owner, workID, CreateScriptInput{Episode: input.Episode, Title: fmt.Sprintf("第%d集", input.Episode), Content: content, SourceChapterIDs: input.ChapterIDs, GenerationSource: SourceAI, ModelID: input.ModelID, PromptID: "script-editor.generate", PromptRevision: 1})
}
