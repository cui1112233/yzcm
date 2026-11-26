package batchfactoryv11

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestComicStylesNormalizeLimitsAndLegacy(t *testing.T) {
	legacy := AIReasoningComicModule{Style: PresetSnapshot{ID: "old", Body: "original"}, GlobalReferenceImage: ComicGlobalReferenceImage{URL: "old-reference"}}
	styles, err := NormalizeComicStyleOutputs(legacy)
	if err != nil || len(styles) != 1 || styles[0].ID != "old" || styles[0].GlobalReferenceImage.URL != "old-reference" {
		t.Fatalf("legacy=%+v err=%v", styles, err)
	}
	module := AIReasoningComicModule{}
	for i := 0; i < 5; i++ {
		module.Styles = append(module.Styles, ComicStyleOutput{PresetSnapshot: PresetSnapshot{ID: fmt.Sprintf("s%d", i)}})
	}
	if got, err := NormalizeComicStyleOutputs(module); err != nil || len(got) != 5 {
		t.Fatalf("five styles: %v %v", got, err)
	}
	module.Styles = append(module.Styles, module.Styles[0])
	if _, err := NormalizeComicStyleOutputs(module); err == nil {
		t.Fatal("accepted six styles")
	}
	module.Styles = module.Styles[:2]
	module.Styles[1] = module.Styles[0]
	if _, err := NormalizeComicStyleOutputs(module); err == nil {
		t.Fatal("accepted duplicate IDs")
	}
	module.Styles = []ComicStyleOutput{{PresetSnapshot: PresetSnapshot{ID: ""}}}
	if _, err := NormalizeComicStyleOutputs(module); err == nil {
		t.Fatal("accepted empty style selection")
	}
}

func TestComicStylesResolveWithoutMutatingSharedSettings(t *testing.T) {
	raw := json.RawMessage(`{"comic":{"enabled":true,"presetId":"pages","body":"shared story","style":{"presetId":"old"},"styles":[{"presetId":"A","body":"ink","globalReferenceImage":{"url":"ink-ref","enabled":true}},{"presetId":"B","body":"realistic","globalReferenceImage":{"url":"photo-ref","enabled":true}}]},"video":{"presetId":"video-unchanged"}}`)
	original := SettingsPatch{"aiPromptConfig": raw, "textPageTarget": json.RawMessage(`150`)}
	got, err := ResolveComicStyleSettings(original, "B")
	if err != nil {
		t.Fatal(err)
	}
	comic := aiReasoningPromptConfig(got).Comic
	if comic.Style.ID != "B" || comic.Style.Body != "realistic" || comic.GlobalReferenceImage.URL != "photo-ref" || comic.Body != "shared story" {
		t.Fatalf("wrong selection: %+v", comic)
	}
	if string(original["aiPromptConfig"]) != string(raw) || aiReasoningPromptConfig(original).Comic.Style.ID != "old" {
		t.Fatal("mutated original settings")
	}
	if _, err := ResolveComicStyleSettings(original, "unknown"); err == nil {
		t.Fatal("accepted unselected style")
	}
	got["textPageTarget"][0] = '2'
	if string(original["textPageTarget"]) != "150" {
		t.Fatal("shallow copied settings")
	}
}
