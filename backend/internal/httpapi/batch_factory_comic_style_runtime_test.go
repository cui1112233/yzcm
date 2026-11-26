package httpapi

import (
	"qiantie/backend/internal/batchfactoryv11"
	"testing"
)

func TestComicStyleRuntimeSummaryAggregatesEveryOutput(t *testing.T) {
	outputs := []batchfactoryv11.ComicStyleRuntimeOutput{{Style: batchfactoryv11.ComicStyleOutput{PresetSnapshot: batchfactoryv11.PresetSnapshot{ID: "A"}}, Pages: []batchfactoryv11.ComicPage{{VideoID: "v", Images: []batchfactoryv11.ComicPageImage{{IsPrimary: true}}}}}, {Style: batchfactoryv11.ComicStyleOutput{PresetSnapshot: batchfactoryv11.PresetSnapshot{ID: "B"}}}}
	summary := map[string]any{}
	addComicStyleRuntimeSummary(summary, outputs, []batchfactoryv11.Video{{ID: "v"}})
	if summary["comicImageReadyCount"] != 1 || summary["comicImageTotalCount"] != 2 || summary["comicImageReady"] != false {
		t.Fatalf("mixed style counts: %+v", summary)
	}
}
