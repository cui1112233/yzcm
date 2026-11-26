package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"qiantie/backend/internal/batchfactoryv11"
	"strings"
	"sync"
	"time"
)

type comicStyleFrozenSettingsKey struct{}
type comicStyleRun struct {
	Style        batchfactoryv11.ComicStyleOutput `json:"style"`
	Settings     batchfactoryv11.SettingsPatch    `json:"settings"`
	Status       string                           `json:"status"`
	Stage        string                           `json:"stage"`
	Error        string                           `json:"error,omitempty"`
	CollectionID string                           `json:"collectionId,omitempty"`
	PageCount    int                              `json:"pageCount"`
	Attempts     int                              `json:"attempts"`
	Published    bool                             `json:"published"`
	CarouselID   int64                            `json:"carouselId,omitempty"`
	UpdatedAt    time.Time                        `json:"updatedAt"`
}
type comicStyleRunResult struct {
	Total       int
	Completed   int
	Published   int
	Pages       int
	Collections []batchfactoryv11.ComicCollection
}

func configuredComicStyleOutputs(settings batchfactoryv11.SettingsPatch) ([]batchfactoryv11.ComicStyleOutput, error) {
	var config batchfactoryv11.AIReasoningPromptConfig
	if raw := settings["aiPromptConfig"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &config); err != nil {
			return nil, err
		}
	}
	return batchfactoryv11.NormalizeComicStyleOutputs(config.Comic)
}
func hasMultipleComicStyles(settings batchfactoryv11.SettingsPatch) bool {
	list, err := configuredComicStyleOutputs(settings)
	return err == nil && len(list) > 1
}
func hasFrozenComicStyles(entry map[string]any) bool {
	return automationStringValue(entry["comicStyleRuns"], "") != ""
}

// Runs are stored inside the existing MySQL-backed automation job, alongside
// its durable retry state. Persist each collection identity before publishing.
// This avoids a second scheduler or an independent source/classification pass.
func (c *BatchAutomationCoordinator) runComicStyleOutputs(ctx context.Context, owner, batchID string, book batchfactoryv11.Book, settings batchfactoryv11.SettingsPatch, entry map[string]any, persist func(), upload, deferred bool) (comicStyleRunResult, error) {
	var runs []comicStyleRun
	if raw := automationStringValue(entry["comicStyleRuns"], ""); raw != "" {
		if err := json.Unmarshal([]byte(raw), &runs); err != nil {
			return comicStyleRunResult{}, fmt.Errorf("画风任务快照损坏：%w", err)
		}
	} else {
		styles, err := configuredComicStyleOutputs(settings)
		if err != nil {
			return comicStyleRunResult{}, err
		}
		if len(styles) < 1 {
			return comicStyleRunResult{}, fmt.Errorf("请选择漫画画风")
		}
		if legacy, ok := c.store.(batchfactoryv11.ComicLegacyStyleStore); ok {
			var config batchfactoryv11.AIReasoningPromptConfig
			_ = json.Unmarshal(settings["aiPromptConfig"], &config)
			if config.Comic.Style.ID != "" {
				if err := legacy.AdoptLegacyComicStyle(ctx, owner, batchID, book.ID, batchfactoryv11.ComicStyleOutput{PresetSnapshot: config.Comic.Style, GlobalReferenceImage: config.Comic.GlobalReferenceImage}); err != nil {
					return comicStyleRunResult{}, err
				}
			}
		}
		for _, style := range styles {
			frozen, err := batchfactoryv11.ResolveComicStyleSettings(settings, style.ID)
			if err != nil {
				return comicStyleRunResult{}, err
			}
			runs = append(runs, comicStyleRun{Style: style, Settings: frozen, Status: "pending"})
		}
	}
	if len(runs) < 1 || len(runs) > batchfactoryv11.MaxComicOutputStyles {
		return comicStyleRunResult{}, fmt.Errorf("画风任务数量无效")
	}
	var mu sync.Mutex
	save := func() {
		raw, _ := json.Marshal(runs)
		entry["comicStyleRuns"] = string(raw)
		entry["updatedAt"] = c.now()
		persist()
	}
	save()
	result := comicStyleRunResult{Total: len(runs)}
	pages, ok := c.store.(batchfactoryv11.ComicPageStore)
	if !ok {
		return result, batchfactoryv11.ErrUnavailable
	}
	var failures []error
	for i := range runs {
		run := &runs[i]
		if err := ctx.Err(); err != nil {
			return result, err
		}
		scoped, err := batchfactoryv11.ResolveComicStyleContext(ctx, pages, owner, batchID, book.ID, run.Style.ID)
		if err != nil {
			return result, err
		}
		scoped = context.WithValue(scoped, comicStyleFrozenSettingsKey{}, run.Settings)
		var collection batchfactoryv11.ComicCollection
		if run.Published {
			result.Completed++
			result.Published++
			result.Pages += run.PageCount
			result.Collections = append(result.Collections, batchfactoryv11.ComicCollection{ID: run.CollectionID, StyleID: run.Style.ID})
			continue
		}
		if run.CollectionID != "" {
			collection, err = c.latestComicCollection(scoped, owner, batchID, book.ID, run.CollectionID)
			if err != nil {
				failures = append(failures, fmt.Errorf("%s：读取已保存合集失败：%w", run.Style.Name, err))
				continue
			}
		}
		// A confirmed upload is never repeated by a continuation job.
		if run.CollectionID == "" {
			run.Status, run.Stage, run.Error = "running", "comic_images", ""
			run.Attempts++
			run.UpdatedAt = c.now()
			save()
			generationCtx, lease, cancel := comicGenerationLeaseContext(scoped, comicGenerationBudget(comicGenerationPageCount(book), 4), comicGenerationHardCap)
			generationCtx = withComicGenerationHeartbeat(generationCtx, func() { lease.touch(comicGenerationIdleExtend) })
			generationCtx = withComicGenerationProgress(generationCtx, func(stage, message string) {
				mu.Lock()
				defer mu.Unlock()
				entry["message"] = fmt.Sprintf("画风 %d/%d · %s：%s", i+1, len(runs), run.Style.Name, message)
				save()
			})
			var release func()
			if c.comicRunSlots != nil {
				group := ""
				if c.comicGenerationGroup != nil {
					group, _ = c.comicGenerationGroup(generationCtx, owner, run.Settings)
				}
				release, err = c.comicRunSlots.acquireGroup(generationCtx, group, func(position int) {
					lease.touch(comicGenerationIdleExtend)
					mu.Lock()
					defer mu.Unlock()
					entry["message"] = fmt.Sprintf("画风 %s 等待出图槽位，前面还有 %d 本", run.Style.Name, position)
					save()
				})
			}
			if err == nil {
				if c.comicGenerate == nil {
					err = fmt.Errorf("漫画图片生成服务未配置")
				} else {
					collection, err = c.comicGenerate(generationCtx, owner, batchID, book.ID)
				}
			}
			if release != nil {
				release()
			}
			cancel()
			if err == nil {
				collection, err = c.confirmComicCollection(scoped, owner, batchID, book.ID, collection)
			}
			if err != nil {
				run.Status, run.Error = "failed", err.Error()
				save()
				failures = append(failures, fmt.Errorf("%s：%w", run.Style.Name, err))
				continue
			}
			run.CollectionID, run.PageCount, run.Status, run.Stage = collection.ID, len(collection.Items), "ready", "comic_collection"
			save()
		}
		collection.StyleID = run.Style.ID
		result.Collections = append(result.Collections, collection)
		result.Pages += run.PageCount
		if upload && !deferred {
			run.Stage = "comic_publish"
			save()
			publishCtx, cancel := context.WithTimeout(scoped, comicPublishStageTimeout)
			var record N8ComicPublishRecord
			if c.comicPublish == nil {
				err = fmt.Errorf("漫画发布服务未配置")
			} else {
				record, err = c.comicPublish(publishCtx, owner, batchID, book.ID, collection, run.Settings)
			}
			cancel()
			if err == nil && record.Status != "published" {
				err = errN8ComicCarouselIncomplete
			}
			if err != nil {
				run.Status, run.Error = "failed", err.Error()
				save()
				failures = append(failures, fmt.Errorf("%s：%w", run.Style.Name, err))
				continue
			}
			run.Published, run.CarouselID, run.Stage = true, record.CarouselID, "published"
			result.Published++
		}
		run.Status, run.Error = "completed", ""
		result.Completed++
		save()
	}
	entry["comicStyleTotal"], entry["comicStyleCompleted"], entry["comicStylePublished"], entry["comicPageCount"] = result.Total, result.Completed, result.Published, result.Pages
	save()
	if len(failures) > 0 {
		return result, fmt.Errorf("%d/%d 种画风已完成：%w", result.Completed, result.Total, errors.Join(failures...))
	}
	return result, nil
}

func comicStyleCollectionTitle(ctx context.Context, title string) string {
	settings, _ := ctx.Value(comicStyleFrozenSettingsKey{}).(batchfactoryv11.SettingsPatch)
	if len(settings) == 0 {
		return title
	}
	outputs, _ := configuredComicStyleOutputs(settings)
	if len(outputs) < 2 {
		return title
	}
	selected := batchfactoryv11.ComicStyleOutputID(ctx)
	for _, style := range outputs {
		if style.ID == selected {
			label := strings.TrimSpace(style.Name)
			if label == "" {
				label = style.ID
			}
			r := []rune(label)
			if len(r) > 12 {
				label = string(r[:12])
			}
			t := []rune(title)
			if len(t) > 30 {
				title = string(t[:30])
			}
			return title + "-" + label
		}
	}
	return title
}
