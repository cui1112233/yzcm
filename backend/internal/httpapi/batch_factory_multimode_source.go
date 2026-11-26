package httpapi

import (
	"context"
	"fmt"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
	"strings"
)

func (c *BatchAutomationCoordinator) prepareMultiModeSource(ctx context.Context, owner string, input batchfactoryv11.CreateBookInput) (batchfactoryv11.MultiModeSourceSnapshot, error) {
	raw := input.OriginalRaw
	if strings.TrimSpace(raw) == "" && input.SourceMetadata["rawSourceStatus"] != "unverified" {
		raw = input.TxtText
	}

	info := novelfetchworkshop.BookInfo{}
	metadata := copyAnyMap(input.SourceMetadata)
	title := input.Title
	if strings.TrimSpace(raw) == "" && firstMetadataString(metadata, "originalReadStrategy") == "ocr_first" {
		return batchfactoryv11.MultiModeSourceSnapshot{}, batchfactoryv11.ErrMultiModeSourcePending
	}
	if strings.TrimSpace(raw) == "" && firstMetadataString(metadata, "sourceMode") == "custom_copy" {
		return batchfactoryv11.MultiModeSourceSnapshot{}, fmt.Errorf("自定义文案原文未验证，请重新选择原文")
	}
	if strings.TrimSpace(raw) == "" {
		book := batchfactoryv11.Book{BookID: input.BookID, Title: input.Title, Platform: input.Platform, SourceMetadata: metadata}
		result, _, _, err, _ := fetchBatchSourceWithGiantCandidate(ctx, c.fetcher, c.giantMaterialResolver, book, input.BookID, resolveSingleFetchPlatform("", book), 4000)
		if err != nil {
			return batchfactoryv11.MultiModeSourceSnapshot{}, err
		}
		raw, info = result.OriginalRaw, result.BookInfo
		title = fetchedBookSourceTitle(result, title)
	}
	if strings.TrimSpace(raw) == "" {
		return batchfactoryv11.MultiModeSourceSnapshot{}, fmt.Errorf("原文为空，请重试读取")
	}
	gender := validBatchGender(firstMetadataString(metadata, "gender", "gender_name"))
	style := validBatchStyle(firstMetadataString(metadata, "style", "styleName", "style_name"))
	if gender == "" || style == "" {
		metadata = finalizeFetchedBatchClassification(classifyFetchedBatchBook(ctx, owner, info, novelfetchworkshop.ProcessRawText(raw, 4000).ProcessedText, metadata, c.classification))
	}
	return batchfactoryv11.MultiModeSourceSnapshot{Title: title, OriginalRaw: raw, Metadata: metadata}, nil
}
func (c *BatchAutomationCoordinator) resolveMultiModeGroup(ctx context.Context, owner, groupID string) error {
	store, ok := c.store.(batchfactoryv11.MultiModeCreationStore)
	if !ok {
		return batchfactoryv11.ErrUnavailable
	}
	group, err := store.GetMultiModeCreation(ctx, owner, groupID)
	if err != nil {
		return err
	}
	return batchfactoryv11.ResolveMultiModeSources(ctx, store, owner, groupID, func(ctx context.Context, input batchfactoryv11.CreateBookInput) (batchfactoryv11.MultiModeSourceSnapshot, error) {
		// An OCR/direct-reader callback may have filled one child while the
		// other projects were waiting. Reuse that saved original for delivery.
		if strings.TrimSpace(input.OriginalRaw) == "" {
			for _, batch := range group.Batches {
				for _, book := range batch.Books {
					if book.BookID == input.BookID && book.Platform == input.Platform && hasVerifiedBatchOriginal(book) {
						input.OriginalRaw = book.TxtText
						input.SourceMetadata = book.SourceMetadata
						break
					}
				}
				if input.OriginalRaw != "" {
					break
				}
			}
		}
		return c.prepareMultiModeSource(ctx, owner, input)
	})
}
