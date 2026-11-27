package prompt

import (
	"fmt"
	"strings"
)

const scriptEditorGeneratePrompt = `你是中文竖屏短剧编剧。把用户提供的原文章节改编成可直接拍摄的分集剧本。
必须保留核心人物关系和因果；开场尽快出现冲突；用“集标题、场次、时间/内外景、人物动作、对白”的格式；不要解释创作过程，只输出完整剧本。`

const scriptEditorReviewPrompt = `你是短剧审稿编辑。检查剧本的开场钩子、冲突推进、人物动机、对白效率、集尾悬念和拍摄可执行性。输出明确的问题与可直接采用的修改建议，不虚构原文事实。`

const scriptEditorPolishPrompt = `你是短剧台词编辑。在不改变剧情事实和人物关系的前提下润色剧本，使对白口语化、动作可拍摄、节奏紧凑。只输出修改后的完整剧本。`

const scriptEditorViralOpeningPrompt = `你是短剧开场编辑。为现有剧本重写高吸引力开场，前几秒建立人物、危机和悬念，同时保持与后续剧情衔接。只输出重写后的完整剧本。`

func ScriptEditorPrompt(kind string) (string, error) {
	switch strings.TrimSpace(kind) {
	case "generate":
		return scriptEditorGeneratePrompt, nil
	case "review":
		return scriptEditorReviewPrompt, nil
	case "polish":
		return scriptEditorPolishPrompt, nil
	case "viral-opening":
		return scriptEditorViralOpeningPrompt, nil
	default:
		return "", fmt.Errorf("unknown script editor prompt: %s", kind)
	}
}
