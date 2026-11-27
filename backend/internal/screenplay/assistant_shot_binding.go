package screenplay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

type bindingAsset struct {
	ID       string    `json:"id"`
	Type     AssetType `json:"type"`
	Name     string    `json:"name"`
	Revision int64     `json:"revision"`
}
type AssistantShotBinding struct {
	// Only supplied types appear. An empty list is a confirmed clear.
	Selected map[AssetType][]string `json:"selected"`
	Assets   []bindingAsset         `json:"assets"`
}

func bindingFail(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidArgument, reason) }

func resolveBinding(values []string, assets []bindingAsset, kind AssetType, byName bool) ([]bindingAsset, error) {
	out := []bindingAsset{}
	seen := map[string]bool{}
	for _, value := range values {
		var found *bindingAsset
		for i := range assets {
			a := &assets[i]
			match := a.ID == value
			if byName {
				match = a.Name == value
			}
			if a.Type != kind || !match {
				continue
			}
			if found != nil {
				return nil, bindingFail("资产重名或编号有歧义，请核对")
			}
			found = a
		}
		if found == nil {
			return nil, bindingFail("当前作品中找不到对应类型的资产 " + value)
		}
		if !seen[found.ID] {
			out = append(out, *found)
			seen[found.ID] = true
		}
	}
	return out, nil
}

func bindingInput(f map[string]json.RawMessage, key string, scene bool) ([]string, error) {
	vals, e := bindingValues(f[key], scene)
	if e != nil || len(vals) > 200 {
		return nil, bindingFail("资产参数必须为完整名称或实际字符串编号，单类最多200项")
	}
	for _, v := range vals {
		if key == "scene_name" || strings.HasSuffix(key, "_names") {
			if utf8.RuneCountInString(v) > 191 {
				return nil, bindingFail("单个资产名称最多191个字符")
			}
		} else if len(v) > 512 {
			return nil, bindingFail("资产参数过长")
		}
	}
	return vals, nil
}
func previousBindings(sh StoryboardShot, kind AssetType, assets []bindingAsset, strict bool) ([]bindingAsset, error) {
	ids, present, e := ShotAssetBindingIDs(sh.ProtocolFields, kind, true)
	if e != nil {
		return nil, e
	}
	// A replacement can repair a deleted target. Record all surviving old
	// assets, including free-text names used by older ordinary video requests.
	var old []bindingAsset
	if present {
		for _, id := range ids {
			if strict {
				matches, e := resolveBinding([]string{id}, assets, kind, false)
				if e != nil {
					return nil, e
				}
				old = append(old, matches...)
				continue
			}
			for _, a := range assets {
				if a.Type == kind && a.ID == id {
					old = append(old, a)
				}
			}
		}
		_, explicit, _ := ShotAssetBindingIDs(sh.ProtocolFields, kind, false)
		if explicit {
			return old, nil
		}
	}
	for _, name := range splitAssetNames(*bindingShotField(&sh, kind)) {
		if name == "none" || name == "无道具" || name == "-" {
			continue
		}
		if strict {
			matches, e := resolveBinding([]string{name}, assets, kind, true)
			if e != nil {
				return nil, e
			}
			if present && !slices.Contains(old, matches[0]) {
				return nil, bindingFail("原镜头的资产名称和编号不一致，请明确替换")
			}
			if !present {
				old = append(old, matches...)
			}
		} else {
			for _, a := range assets {
				if a.Type == kind && a.Name == name && !slices.Contains(old, a) {
					old = append(old, a)
				}
			}
		}
	}
	return old, nil
}

func prepareAssistantShotBinding(ctx context.Context, store Store, o, p string, raw json.RawMessage) (AuthoringAction, string, error) {
	empty := AuthoringAction{}
	f, e := assistantShotObject(raw, "episode_no", "engine", "shot_id", "shot_number", "mode", "character_ids", "character_names", "scene_id", "scene_name", "prop_ids", "prop_names")
	if e != nil {
		return empty, "", e
	}
	var ep, n int
	var id string
	var engine StoryboardEngine
	mode := "merge"
	for key, dst := range map[string]any{"episode_no": &ep, "engine": &engine, "shot_number": &n, "shot_id": &id, "mode": &mode} {
		if f[key] != nil && json.Unmarshal(f[key], dst) != nil {
			return empty, "", bindingFail("镜头选择或模式类型无效")
		}
	}
	if (f["episode_no"] != nil && ep <= 0) || (f["shot_number"] != nil && n <= 0) || (f["shot_id"] != nil && (id == "" || len(id) > 256 || strings.TrimSpace(id) != id)) || (f["engine"] != nil && (engine == "" || len(engine) > 128)) || (id == "" && n == 0) || (mode != "replace" && mode != "merge") {
		return empty, "", bindingFail("请给出真实镜头和有效绑定模式")
	}
	b, sh, e := assistantShotTarget(ctx, store, o, p, ep, engine, id, n)
	if e != nil {
		return empty, "", e
	}
	rows, e := store.ListAssets(ctx, o, p, "")
	if e != nil {
		return empty, "", e
	}
	assets := []bindingAsset{}
	for _, a := range rows {
		if a.OwnerUsername == o && a.ProjectID == p {
			assets = append(assets, bindingAsset{a.ID, a.Type, a.Name, a.Revision})
		}
	}
	bind := &AssistantShotBinding{Selected: map[AssetType][]string{}}
	seen := map[string]bool{}
	for _, k := range bindingKinds {
		idKey := bindingIDKey(k)
		nameKey := map[AssetType]string{AssetCharacter: "character_names", AssetScene: "scene_name", AssetProp: "prop_names"}[k]
		if f[idKey] == nil && f[nameKey] == nil {
			continue
		}
		var next []bindingAsset
		for _, key := range []string{idKey, nameKey} {
			if f[key] == nil {
				continue
			}
			vs, e := bindingInput(f, key, k == AssetScene)
			if e != nil {
				return empty, "", e
			}
			resolved, e := resolveBinding(vs, assets, k, key == nameKey)
			if e != nil {
				return empty, "", e
			}
			if next != nil && !slices.Equal(next, resolved) {
				return empty, "", bindingFail("资产编号和完整名称不一致")
			}
			next = resolved
		}
		if mode == "merge" && len(next) == 0 {
			continue
		}
		old, e := previousBindings(*sh, k, assets, mode == "merge")
		if e != nil {
			return empty, "", e
		}
		if mode == "merge" {
			if k == AssetScene && len(old) > 0 && !slices.Equal(old, next) {
				return empty, "", bindingFail("镜头已有其他场景，请明确替换")
			}
			next = append(slices.Clone(old), next...)
		}
		unique := []string{}
		kindSeen := map[string]bool{}
		for _, a := range next {
			if !kindSeen[a.ID] {
				unique = append(unique, a.ID)
				kindSeen[a.ID] = true
			}
		}
		bind.Selected[k] = unique
		for _, list := range [][]bindingAsset{old, next} {
			for _, a := range list {
				if !seen[a.ID] {
					bind.Assets = append(bind.Assets, a)
					seen[a.ID] = true
				}
			}
		}
	}
	if len(bind.Selected) == 0 {
		return empty, "", bindingFail("请选择需要绑定的资产，清空已有绑定请使用replace")
	}
	edit := AssistantCreativeShotEdit{BoardID: b.ID, Episode: b.EpisodeNo, Engine: b.Engine, Before: *sh, Binding: bind}
	edit.After, edit.Changes, e = buildBindingShot(*sh, bind)
	if e != nil {
		return empty, "", e
	}
	a := AuthoringAction{Kind: "creative_shot", TargetID: sh.ID, ExpectedRevision: b.Revision, CreativeShot: &edit}
	if e = validateCreativeShotAction(a); e != nil {
		return empty, "", e
	}
	return a, fmt.Sprintf("绑定第%d集镜头%d资产", b.EpisodeNo, sh.Seq), nil
}

func bindingDisplay(label string, ids []string) string {
	if len(ids) == 0 {
		return label
	}
	return strings.TrimSpace(label + " [" + strings.Join(ids, ", ") + "]")
}
func buildBindingShot(before StoryboardShot, bind *AssistantShotBinding) (StoryboardShot, []AssistantShotChange, error) {
	after := before
	fields, e := shotProtocolObject(before.ProtocolFields)
	if e != nil {
		return after, nil, e
	}
	ns, e := bindingObject(fields["asset_bindings"])
	if e != nil {
		return after, nil, e
	}
	var changes []AssistantShotChange
	for _, kind := range bindingKinds {
		ids, ok := bind.Selected[kind]
		if !ok {
			continue
		}
		if ids == nil || len(ids) > 200 || kind == AssetScene && len(ids) > 1 {
			return after, nil, ErrInvalidArgument
		}
		selected, e := resolveBinding(ids, bind.Assets, kind, false)
		if e != nil || len(selected) != len(ids) {
			return after, nil, ErrInvalidArgument
		}
		names := []string{}
		for _, a := range selected {
			names = append(names, a.Name)
		}
		key := bindingIDKey(kind)
		if kind != AssetScene {
			delete(fields, key)
		}
		oldIDs, present, e := ShotAssetBindingIDs(before.ProtocolFields, kind, true)
		if e != nil {
			return after, nil, e
		}
		_, explicit, _ := ShotAssetBindingIDs(before.ProtocolFields, kind, false)
		dst := bindingShotField(&after, kind)
		old := *dst
		*dst = strings.Join(names, "、")
		// These are the lengths of the actual MySQL VARCHAR columns. Count the
		// separators as well, and validate only the types included in this edit.
		label, limit := "人物", 512
		if kind == AssetProp {
			label = "道具"
		} else if kind == AssetScene {
			label, limit = "场景", 191
		}
		if utf8.RuneCountInString(*dst) > limit {
			return after, nil, bindingFail(fmt.Sprintf("镜头%s名称合计超过%d个字符，请减少本次绑定", label, limit))
		}
		if kind == AssetScene {
			val := ""
			if len(ids) > 0 {
				val = ids[0]
			}
			ns[key], e = json.Marshal(val)
		} else {
			ns[key], e = json.Marshal(ids)
		}
		if e != nil {
			return after, nil, e
		}
		if old != *dst || !explicit || !slices.Equal(oldIDs, ids) {
			oldDisplay := old
			if present {
				oldDisplay = bindingDisplay(old, oldIDs)
			}
			changes = append(changes, AssistantShotChange{key, oldDisplay, bindingDisplay(*dst, ids)})
		}
	}
	if len(changes) == 0 {
		return after, nil, bindingFail("镜头资产绑定没有变化")
	}
	fields["asset_bindings"], e = json.Marshal(ns)
	if e != nil {
		return after, nil, e
	}
	after.ProtocolFields, e = json.Marshal(fields)
	return after, changes, e
}

func validateBindingEdit(d *AssistantCreativeShotEdit) error {
	bind := d.Binding
	if len(bind.Selected) == 0 || len(bind.Selected) > 3 {
		return ErrInvalidArgument
	}
	for k := range bind.Selected {
		if bindingIDKey(k) == "" {
			return ErrInvalidArgument
		}
	}
	seen := map[string]bool{}
	for _, a := range bind.Assets {
		if a.ID == "" || a.Name == "" || a.Revision < 1 || bindingIDKey(a.Type) == "" || seen[a.ID] {
			return ErrInvalidArgument
		}
		seen[a.ID] = true
	}
	after, changes, e := buildBindingShot(d.Before, bind)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(changes, d.Changes) || !bindingShotEqual(after, d.After) {
		return ErrInvalidArgument
	}
	return validateStoryboardShotInput(after)
}

func bindingShotEqual(left, right StoryboardShot) bool {
	semantic := func(shot StoryboardShot) (any, error) {
		raw, e := json.Marshal(shot)
		if e != nil {
			return nil, e
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		var v any
		e = decoder.Decode(&v)
		return v, e
	}
	a, e := semantic(left)
	if e != nil {
		return false
	}
	b, e := semantic(right)
	return e == nil && reflect.DeepEqual(a, b)
}
