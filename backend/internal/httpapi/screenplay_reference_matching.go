package httpapi

import (
	"fmt"
	"strings"

	"qiantie/backend/internal/screenplay"
)

// Explicit IDs take precedence per type, including clears. Older ordinary
// video requests match whole names within the same project and asset type.
func screenplayReferenceTargets(project screenplay.Project, shot screenplay.StoryboardShot, assets []screenplay.Asset) ([]string, error) {
	explicitIDs := map[screenplay.AssetType][]string{}
	requested := map[screenplay.AssetType]map[string]bool{}
	for kind, field := range map[screenplay.AssetType]string{
		screenplay.AssetCharacter: shot.Characters,
		screenplay.AssetScene:     shot.Scene,
		screenplay.AssetProp:      shot.Props,
	} {
		ids, present, e := screenplay.ShotAssetBindingIDs(shot.ProtocolFields, kind, false)
		if e != nil {
			return nil, e
		}
		if present {
			explicitIDs[kind] = ids
			continue
		}
		requested[kind] = map[string]bool{}
		// Keep the separators accepted by storyboard splitAssetNames. Spaces
		// inside a name (for example Alice Smith) are not list separators.
		for _, part := range strings.FieldsFunc(field, func(r rune) bool {
			return strings.ContainsRune(",，、;；/|\n@", r)
		}) {
			name := strings.TrimSpace(part)
			if name != "" && name != "无" && name != "无道具" && name != "none" && name != "-" {
				requested[kind][name] = true
			}
		}
	}
	type assetName struct {
		kind screenplay.AssetType
		name string
	}
	counts := map[assetName]int{}
	ids := map[string]screenplay.AssetType{}
	for _, asset := range assets {
		if asset.ProjectID != project.ID || asset.OwnerUsername != project.OwnerUsername {
			continue
		}
		ids[asset.ID] = asset.Type
		counts[assetName{asset.Type, strings.TrimSpace(asset.Name)}]++
	}
	targets := []string{}
	if shot.ID != "" {
		targets = append(targets, shot.ID)
	}
	for _, kind := range []screenplay.AssetType{screenplay.AssetCharacter, screenplay.AssetScene, screenplay.AssetProp} {
		for _, id := range explicitIDs[kind] {
			if ids[id] != kind {
				return nil, fmt.Errorf("当前作品中的视频参考资产 %s 已失效，请重新绑定镜头", id)
			}
			targets = append(targets, id)
		}
	}
	for _, asset := range assets {
		name := strings.TrimSpace(asset.Name)
		if ids[asset.ID] == asset.Type && requested[asset.Type][name] && counts[assetName{asset.Type, name}] == 1 {
			targets = append(targets, asset.ID)
		}
	}
	return targets, nil
}
