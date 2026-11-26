package screenplay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type assistantAssetFields struct {
	Name        *string `json:"name"`
	Identity    *string `json:"identity"`
	Description *string `json:"description"`
	Prompt      *string `json:"prompt"`
	ImagePrompt *string `json:"image_prompt"`
	Voice       *string `json:"voice"`
	Episodes    *string `json:"episodes"`
	Tag         *string `json:"tag"`
}
type assistantAssetEditArgs struct {
	assistantAssetFields
	AssetID       string                `json:"asset_id"`
	CharacterID   string                `json:"character_id"`
	SceneID       string                `json:"scene_id"`
	PropID        string                `json:"prop_id"`
	CharacterName string                `json:"character_name"`
	SceneName     string                `json:"scene_name"`
	PropName      string                `json:"prop_name"`
	Action        string                `json:"action"`
	NewData       *assistantAssetFields `json:"new_data"`
}

func IsAssistantAssetEditTool(name string) bool {
	return name == "modify_character" || name == "modify_scene" || name == "modify_prop"
}

// PrepareAssistantAssetEdit only prepares an existing, revision-protected asset
// proposal. Identity and every unchanged field come from this owner's store.
func PrepareAssistantAssetEdit(ctx context.Context, store Store, owner, project, name string, raw json.RawMessage) (AuthoringAction, string, error) {
	fail := func(reason string) (AuthoringAction, string, error) {
		return AuthoringAction{}, "", fmt.Errorf("%w: %s", ErrInvalidArgument, reason)
	}
	var kind AssetType
	var label, alias, targetName string
	switch name {
	case "modify_character":
		kind = AssetCharacter
		label = "人物"
	case "modify_scene":
		kind = AssetScene
		label = "场景"
	case "modify_prop":
		kind = AssetProp
		label = "道具"
	default:
		return fail("不支持的修改工具")
	}
	if len(raw) == 0 || len(raw) > 8192 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fail("修改参数为空或超过上限")
	}
	var args assistantAssetEditArgs
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&args) != nil {
		return fail("修改参数无效")
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return fail("修改参数必须是一个对象")
	}
	if args.Action != "" && args.Action != "update" {
		return fail("此入口只准备现有资产修改，不能新增或删除")
	}
	switch kind {
	case AssetCharacter:
		if args.SceneID != "" || args.PropID != "" || args.SceneName != "" || args.PropName != "" {
			return fail("人物工具不能指定其他资产类型")
		}
		alias = args.CharacterID
		targetName = args.CharacterName
	case AssetScene:
		if args.CharacterID != "" || args.PropID != "" || args.CharacterName != "" || args.PropName != "" {
			return fail("场景工具不能指定其他资产类型")
		}
		alias = args.SceneID
		targetName = args.SceneName
	case AssetProp:
		if args.CharacterID != "" || args.SceneID != "" || args.CharacterName != "" || args.SceneName != "" {
			return fail("道具工具不能指定其他资产类型")
		}
		alias = args.PropID
		targetName = args.PropName
	}
	id := args.AssetID
	if alias != "" {
		if id != "" && id != alias {
			return fail("目标编号冲突")
		}
		id = alias
	}
	fields := args.assistantAssetFields
	if id == "" && targetName == "" && fields.Name != nil {
		targetName = *fields.Name
		fields.Name = nil
	}
	targetName = strings.TrimSpace(targetName)
	if (id == "" && targetName == "") || len(id) > 256 || len(targetName) > 256 {
		return fail("请提供真实资产编号或完整名称")
	}
	if args.NewData != nil {
		pairs := []struct {
			dst **string
			src *string
		}{{&fields.Name, args.NewData.Name}, {&fields.Identity, args.NewData.Identity}, {&fields.Description, args.NewData.Description}, {&fields.Prompt, args.NewData.Prompt}, {&fields.ImagePrompt, args.NewData.ImagePrompt}, {&fields.Voice, args.NewData.Voice}, {&fields.Episodes, args.NewData.Episodes}, {&fields.Tag, args.NewData.Tag}}
		for _, pair := range pairs {
			if pair.src == nil {
				continue
			}
			if *pair.dst != nil && **pair.dst != *pair.src {
				return fail("同一字段的新内容冲突")
			}
			*pair.dst = pair.src
		}
	}
	if fields.ImagePrompt != nil {
		if fields.Prompt != nil && *fields.Prompt != *fields.ImagePrompt {
			return fail("画面提示词的新内容冲突")
		}
		fields.Prompt = fields.ImagePrompt
	}
	if fields.Voice != nil && kind != AssetCharacter {
		return fail("只有人物资产可以修改音色")
	}
	if _, e := store.GetProject(ctx, owner, project); e != nil {
		return AuthoringAction{}, "", e
	}
	assets, e := store.ListAssets(ctx, owner, project, kind)
	if e != nil {
		return AuthoringAction{}, "", e
	}
	var found *Asset
	for _, asset := range assets {
		if (id != "" && asset.ID != id) || (id == "" && asset.Name != targetName) {
			continue
		}
		if targetName != "" && asset.Name != targetName {
			return fail("编号和名称未指向同一资产")
		}
		if found != nil {
			return fail("存在同名资产，请使用真实编号")
		}
		copy := asset
		found = &copy
	}
	if found == nil {
		return AuthoringAction{}, "", fmt.Errorf("%w: 当前作品没有指定的%s", ErrNotFound, label)
	}
	desired := *found
	if found.Extra != nil {
		desired.Extra = map[string]string{}
		for k, v := range found.Extra {
			desired.Extra[k] = v
		}
	}
	changed := false
	for _, field := range []struct {
		dst   *string
		value *string
	}{{&desired.Name, fields.Name}, {&desired.Identity, fields.Identity}, {&desired.Description, fields.Description}, {&desired.Prompt, fields.Prompt}, {&desired.Voice, fields.Voice}, {&desired.Episodes, fields.Episodes}, {&desired.Tag, fields.Tag}} {
		if field.value != nil {
			value := strings.TrimSpace(*field.value)
			if value != *field.dst {
				changed = true
			}
			*field.dst = value
		}
	}
	if desired.Name == "" {
		return fail("资产名称不能为空")
	}
	for _, asset := range assets {
		if asset.ID != desired.ID && strings.EqualFold(strings.TrimSpace(asset.Name), desired.Name) {
			return fail("同类型资产已使用这个名称")
		}
	}
	if !changed {
		return fail("没有需要修改的字段")
	}
	action := AuthoringAction{Kind: "asset", TargetID: found.ID, Asset: &desired, ExpectedRevision: found.Revision}
	return action, fmt.Sprintf("修改%s「%s」", label, found.Name), nil
}
