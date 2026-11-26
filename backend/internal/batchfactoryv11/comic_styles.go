package batchfactoryv11

import (
	"encoding/json"
	"fmt"
	"strings"
)

const MaxComicOutputStyles = 5

type ComicStyleOutput struct {
	PresetSnapshot
	GlobalReferenceImage ComicGlobalReferenceImage `json:"globalReferenceImage"`
}

// NormalizeComicStyleOutputs preserves the saved order and per-style references.
// A nil list reads old single-style settings; an explicit empty list is invalid.
func NormalizeComicStyleOutputs(comic AIReasoningComicModule) ([]ComicStyleOutput, error) {
	styles := comic.Styles
	if styles == nil {
		if strings.TrimSpace(comic.Style.ID) == "" {
			return nil, nil
		}
		styles = []ComicStyleOutput{{PresetSnapshot: comic.Style, GlobalReferenceImage: comic.GlobalReferenceImage}}
	}
	if len(styles) < 1 || len(styles) > MaxComicOutputStyles {
		return nil, fmt.Errorf("%w: 漫画画风需选择1～5种", ErrInvalid)
	}
	out := append([]ComicStyleOutput(nil), styles...)
	seen := make(map[string]bool, len(out))
	for i := range out {
		out[i].ID = strings.TrimSpace(out[i].ID)
		if out[i].ID == "" || seen[out[i].ID] {
			return nil, fmt.Errorf("%w: 漫画画风不能为空或重复", ErrInvalid)
		}
		if out[i].Slot != "" && out[i].Slot != "batch.comic-style" {
			return nil, fmt.Errorf("%w: 所选预设不是漫画画风", ErrInvalid)
		}
		seen[out[i].ID] = true
	}
	return out, nil
}

// ResolveComicStyleSettings freezes a selection for a single operation without
// modifying the project's configuration or dropping other prompt modules.
func ResolveComicStyleSettings(base SettingsPatch, styleID string) (SettingsPatch, error) {
	module := aiReasoningPromptConfig(base).Comic
	styles, err := NormalizeComicStyleOutputs(module)
	if err != nil {
		return nil, err
	}
	var selected *ComicStyleOutput
	for i := range styles {
		if styles[i].ID == strings.TrimSpace(styleID) {
			selected = &styles[i]
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("%w: 当前书籍未选择该画风", ErrInvalid)
	}
	out := clonePatch(base)
	var config map[string]json.RawMessage
	if err := json.Unmarshal(out["aiPromptConfig"], &config); err != nil {
		return nil, err
	}
	var comic map[string]json.RawMessage
	if err := json.Unmarshal(config["comic"], &comic); err != nil {
		return nil, err
	}
	comic["style"], err = json.Marshal(selected.PresetSnapshot)
	if err != nil {
		return nil, err
	}
	comic["globalReferenceImage"], err = json.Marshal(selected.GlobalReferenceImage)
	if err != nil {
		return nil, err
	}
	config["comic"], err = json.Marshal(comic)
	if err != nil {
		return nil, err
	}
	out["aiPromptConfig"], err = json.Marshal(config)
	return out, err
}
