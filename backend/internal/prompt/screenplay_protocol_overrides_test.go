package prompt

import (
	"strings"
	"testing"
)

func TestStoryboard139ExplicitRefreshPinsActualBodyAndLeavesOtherFrozenContracts(t *testing.T) {
	current, err := ResolveCreativePrompt("139", "storyboard")
	if err != nil {
		t.Fatal(err)
	}
	old := current
	old.Supported = false
	old.ParserVersion = "storyboard_image_voice_v1"
	refreshed, ok := RefreshStoryboard139ForExplicitSelection(old)
	if !ok || !refreshed.Supported || refreshed.Body != old.Body || refreshed.Digest != old.Digest || refreshed.ParserVersion != storyboard139OptionalImageParser {
		t.Fatal(refreshed, ok)
	}
	for _, change := range []func(*CreativePromptSnapshot){func(p *CreativePromptSnapshot) { p.Body += "\n" }, func(p *CreativePromptSnapshot) { p.Digest = "changed" }, func(p *CreativePromptSnapshot) { p.SourceID = 138 }, func(p *CreativePromptSnapshot) { p.ID = "other" }} {
		bad := old
		change(&bad)
		got, ok := RefreshStoryboard139ForExplicitSelection(bad)
		if ok || got.Supported {
			t.Fatal("unreviewed frozen body upgraded", ok, got.ID)
		}
	}
	required := strings.Join(current.RequiredFields, ",")
	if strings.Contains(required, "image_prompt") || !strings.Contains(required, "source_elements") || !strings.Contains(required, "video_desc") || !strings.Contains(required, "voice_actor") {
		t.Fatal(required)
	}
	other, _ := ResolveCreativePrompt("138", "storyboard")
	if !Storyboard138SupportsBatch(other) {
		t.Fatal("138 source continuation adapter missing")
	}
	if _, ok := RefreshStoryboard139ForExplicitSelection(other); ok {
		t.Fatal("138 refreshed")
	}
}
func TestStoryboard138ExplicitRefreshRequiresExactFrozenBody(t *testing.T) {
	p, e := ResolveCreativePrompt("138", "storyboard")
	if e != nil {
		t.Fatal(e)
	}
	p.Supported = false
	p.ParserVersion = "storyboard_image_voice_v1"
	updated, ok := RefreshStoryboard138ForExplicitSelection(p)
	if !ok || !Storyboard138SupportsBatch(updated) || updated.Body != p.Body {
		t.Fatal("exact frozen adapter not refreshed")
	}
	p.Body += "\n"
	if _, ok := RefreshStoryboard138ForExplicitSelection(p); ok {
		t.Fatal("modified body enabled")
	}
}
