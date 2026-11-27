package httpapi

import (
	"qiantie/backend/internal/batchfactoryv11"
	"time"
)

func addComicStyleRuntimeSummary(summary map[string]any, outputs []batchfactoryv11.ComicStyleRuntimeOutput, videos []batchfactoryv11.Video) {
	ready, total, generating, failed, interrupted, collections, collectionPages, published := 0, 0, 0, 0, 0, 0, 0, 0
	allCollections := true
	var styles []map[string]any
	aggregate := comicReadiness{CanGenerateImages: true, CanBuildCollection: true, Limits: comicReadinessLimits{MaxPages: comicPageGenerationLimit}}
	for _, output := range outputs {
		readiness := buildComicReadiness(videos, output.Pages, comicPageGenerationLimit)
		activity := comicPageActivityCounts(output.Pages, time.Now())
		count, pages, uploaded := 0, 0, 0
		for _, c := range output.Collections {
			if len(c.Items) == 0 {
				continue
			}
			count++
			pages += len(c.Items)
			if output.Published[c.ID] {
				uploaded++
			}
		}
		ready += readiness.PrimaryReadyCount
		total += readiness.PagesTotal
		generating += activity.Generating
		failed += activity.Failed
		interrupted += activity.Interrupted
		collections += count
		collectionPages += pages
		published += uploaded
		allCollections = allCollections && count > 0
		styles = append(styles, map[string]any{"styleId": output.Style.ID, "styleName": output.Style.Name, "imageReadyCount": readiness.PrimaryReadyCount, "imageTotalCount": readiness.PagesTotal, "collectionCount": count, "publishedCollectionCount": uploaded, "readiness": readiness})
		aggregate.PagesTotal += readiness.PagesTotal
		aggregate.PrimaryReadyCount += readiness.PrimaryReadyCount
		aggregate.ImageReadyCount += readiness.ImageReadyCount
		aggregate.PromptReadyCount += readiness.PromptReadyCount
		aggregate.StaleCount += readiness.StaleCount
		aggregate.CanGenerateImages = aggregate.CanGenerateImages && readiness.CanGenerateImages
		aggregate.CanBuildCollection = aggregate.CanBuildCollection && readiness.CanBuildCollection
	}
	summary["comicStyleOutputs"] = styles
	summary["comicImageReadyCount"], summary["comicImageTotalCount"], summary["comicImageReady"] = ready, total, ready == total && total > 0
	summary["comicImageGeneratingCount"], summary["comicImageFailedCount"], summary["comicImageInterruptedCount"] = generating, failed, interrupted
	summary["comicCollectionCount"], summary["comicCollectionPageCount"], summary["comicPublishedCollectionCount"], summary["comicCollectionReady"] = collections, collectionPages, published, allCollections
	summary["comicReadiness"] = aggregate
}
