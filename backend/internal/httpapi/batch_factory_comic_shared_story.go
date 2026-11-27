package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"qiantie/backend/internal/batchfactoryv11"
)

func comicSharedStoryboardKey(book batchfactoryv11.Book, settings batchfactoryv11.SettingsPatch) string {
	var config batchfactoryv11.AIReasoningPromptConfig
	_ = json.Unmarshal(settings["aiPromptConfig"], &config)
	var sources []string
	for _, video := range book.Videos {
		source := ""
		if video.ComicSourceSegment != nil {
			source = video.ComicSourceSegment.SourceText
		}
		sources = append(sources, video.ID+":"+source)
	}
	// Style changes do not alter shared story identity. Source/pagination/common
	// instructions do; image repair stays inside its selected page namespace.
	raw, _ := json.Marshal([]any{book.SourceText, sources, config.Comic.ID, config.Comic.Body, config.Comic.ImagePromptPrefix, config.Comic.ImagePromptSuffix, config.Comic.Constraints})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (c *BatchAutomationCoordinator) confirmComicSharedStory(ctx context.Context, owner, batchID string, book batchfactoryv11.Book, settings batchfactoryv11.SettingsPatch, preparedAll bool) (batchfactoryv11.Book, error) {
	if !hasMultipleComicStyles(settings) {
		return book, nil
	}
	book = c.refreshAutomationBook(ctx, owner, batchID, book)
	key := comicSharedStoryboardKey(book, settings)
	if firstMetadataString(book.SourceMetadata, "comicSharedStoryboardKey") == key {
		return book, nil
	}
	if !preparedAll {
		if c.stages == nil {
			return book, fmt.Errorf("请先重新分析共用漫画分镜，再生成多画风")
		}
		if _, err := c.stages.RunComicPromptExtraction(ctx, owner, batchID, book.ID); err != nil {
			return book, err
		}
		book = c.refreshAutomationBook(ctx, owner, batchID, book)
		key = comicSharedStoryboardKey(book, settings)
	}
	metadata := copyAnyMap(book.SourceMetadata)
	metadata["comicSharedStoryboardKey"] = key
	return c.updateAutomationBookMetadata(ctx, owner, batchID, book, batchfactoryv11.UpdateBookMetadataInput{Metadata: metadata})
}
