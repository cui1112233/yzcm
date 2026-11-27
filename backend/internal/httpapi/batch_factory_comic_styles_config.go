package httpapi

import (
	"fmt"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/prompt"
	"strings"
)

func hydrateComicOutputStyles(catalog *prompt.Catalog, comic map[string]any) error {
	raw, exists := comic["styles"]
	if !exists {
		return nil
	}
	values, ok := raw.([]any)
	if !ok || len(values) < 1 || len(values) > batchfactoryv11.MaxComicOutputStyles {
		return fmt.Errorf("漫画画风需选择1～5种")
	}
	if catalog == nil {
		return fmt.Errorf("画风目录暂不可用，请稍后重试")
	}
	seen := make(map[string]bool, len(values))
	for i, v := range values {
		selection, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("第%d种画风格式不正确", i+1)
		}
		id := strings.TrimSpace(stringValue(selection["presetId"]))
		if id == "" || seen[id] {
			return fmt.Errorf("漫画画风不能为空或重复")
		}
		seen[id] = true
		if err := freezePromptSelection(catalog, selection, "batch-factory", comicStyleSlot, ""); err != nil {
			return fmt.Errorf("第%d种画风：%w", i+1, err)
		}
		if ref, ok := selection["globalReferenceImage"].(map[string]any); ok {
			if scene, ok := ref["scene"].(map[string]any); ok {
				if err := freezePromptSelection(catalog, scene, "batch-factory", comicReferenceSceneSlot, ""); err != nil {
					return fmt.Errorf("画风参考图：%w", err)
				}
			}
		}
	}
	return nil
}
