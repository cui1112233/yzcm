package screenplay

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Names and actual asset IDs in saved shot fields are references. Prose prompts
// are not scanned: merely mentioning an asset is not an explicit binding.
func assistantAssetReference(asset Asset, characters, scene, props string, protocol json.RawMessage) bool {
	match := func(value string) bool {
		var list []string
		if json.Unmarshal([]byte(value), &list) == nil {
			for _, name := range list {
				if strings.TrimSpace(name) == asset.Name || strings.TrimSpace(name) == asset.ID {
					return true
				}
			}
			return false
		}
		for _, name := range strings.FieldsFunc(value, func(r rune) bool { return strings.ContainsRune(",，、;；/|\n@", r) }) {
			name = strings.TrimSpace(name)
			if name == asset.Name || name == asset.ID {
				return true
			}
		}
		return false
	}
	ids, _, bindingErr := ShotAssetBindingIDs(protocol, asset.Type, true)
	for _, id := range ids {
		if id == asset.ID {
			return true
		}
	}
	_, explicit, _ := ShotAssetBindingIDs(protocol, asset.Type, false)
	if !explicit || bindingErr != nil {
		switch asset.Type {
		case AssetCharacter:
			if match(characters) {
				return true
			}
		case AssetScene:
			if match(scene) {
				return true
			}
		case AssetProp:
			if match(props) {
				return true
			}
		}
	}
	var fields any
	if len(protocol) > 0 && json.Unmarshal(protocol, &fields) == nil {
		var walk func(any, bool) bool
		walk = func(v any, binding bool) bool {
			switch x := v.(type) {
			case string:
				return binding && x == asset.ID
			case []any:
				for _, value := range x {
					if walk(value, binding) {
						return true
					}
				}
			case map[string]any:
				for key, value := range x {
					k := strings.ToLower(key)
					explicit := binding || strings.Contains(k, "asset") || strings.Contains(k, "binding") || strings.Contains(k, "reference")
					if walk(value, explicit) {
						return true
					}
				}
			}
			return false
		}
		if obj, ok := fields.(map[string]any); ok {
			if bindingErr != nil && walk(obj[bindingIDKey(asset.Type)], true) {
				return true
			}
			if bindings, ok := obj["asset_bindings"].(map[string]any); ok {
				if bindingErr != nil && walk(bindings[bindingIDKey(asset.Type)], true) {
					return true
				}
				for _, k := range bindingKinds {
					delete(bindings, bindingIDKey(k))
				}
			}
		}
		if walk(fields, false) {
			return true
		}
	}
	return false
}
func assistantAssetReferencedError(asset Asset, shot string) error {
	return fmt.Errorf("%w: 资产「%s」仍被镜头%s引用，请先在镜头中解除引用后重新准备删除", ErrInvalidArgument, asset.Name, shot)
}
func checkAssistantAssetReferences(ctx context.Context, store Store, owner, project string, asset Asset) error {
	docs, err := store.ListDocuments(ctx, owner, project)
	if err != nil {
		return err
	}
	for _, doc := range docs {
		shots, err := store.ListShots(ctx, owner, project, doc.ID)
		if err != nil {
			return err
		}
		for _, shot := range shots {
			characters, _ := json.Marshal(shot.CharacterNames)
			props, _ := json.Marshal(shot.PropNames)
			if assistantAssetReference(asset, string(characters), shot.SceneName, string(props), nil) {
				return assistantAssetReferencedError(asset, shot.ID)
			}
		}
	}
	boards, err := store.ListStoryboards(ctx, owner, project, "")
	if err != nil {
		return err
	}
	for _, board := range boards {
		for _, shot := range board.Shots {
			if assistantAssetReference(asset, shot.Characters, shot.Scene, shot.Props, shot.ProtocolFields) {
				return assistantAssetReferencedError(asset, fmt.Sprintf("第%d集/%s/%d", board.EpisodeNo, board.Engine, shot.Seq))
			}
		}
	}
	return nil
}
func applyAssistantAssetMemory(ctx context.Context, store Store, owner, project string, a AuthoringAction) error {
	if a.Asset.OwnerUsername != "" && a.Asset.OwnerUsername != owner || a.Asset.ProjectID != "" && a.Asset.ProjectID != project {
		return ErrInvalidArgument
	}
	switch a.AssetOperation {
	case "add":
		_, err := store.CreateAsset(ctx, *a.Asset)
		return err
	case "delete":
		assets, err := store.ListAssets(ctx, owner, project, a.Asset.Type)
		if err != nil {
			return err
		}
		for _, current := range assets {
			if current.ID == a.TargetID {
				if current.Revision != a.ExpectedRevision {
					return ErrRevisionConflict
				}
				if err = checkAssistantAssetReferences(ctx, store, owner, project, current); err != nil {
					return err
				}
				return store.DeleteAsset(ctx, owner, project, a.TargetID)
			}
		}
		return ErrNotFound
	default:
		_, err := store.UpdateAsset(ctx, owner, project, a.TargetID, *a.Asset, a.ExpectedRevision)
		return err
	}
}
