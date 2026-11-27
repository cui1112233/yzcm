package screenplay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

var bindingKinds = []AssetType{AssetCharacter, AssetScene, AssetProp}

func bindingIDKey(kind AssetType) string {
	switch kind {
	case AssetCharacter:
		return "character_ids"
	case AssetScene:
		return "scene_id"
	case AssetProp:
		return "prop_ids"
	}
	return ""
}

func bindingObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	m := map[string]json.RawMessage{}
	if len(raw) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, fmt.Errorf("%w: 镜头绑定格式无效", ErrInvalidArgument)
	}
	return m, nil
}

func shotProtocolObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		raw = nil
	}
	return bindingObject(raw)
}

func bindingValues(raw json.RawMessage, single bool) ([]string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, ErrInvalidArgument
	}
	var values []string
	if single {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, ErrInvalidArgument
		}
		if value != "" {
			values = []string{value}
		}
	} else if err := json.Unmarshal(raw, &values); err != nil || values == nil {
		return nil, ErrInvalidArgument
	}
	for _, v := range values {
		if v == "" || strings.TrimSpace(v) != v {
			return nil, ErrInvalidArgument
		}
	}
	return values, nil
}

// ShotAssetBindingIDs distinguishes an explicit empty selection from a missing
// type. Old subject IDs are considered only when the caller requests them.
func ShotAssetBindingIDs(raw json.RawMessage, kind AssetType, legacy bool) ([]string, bool, error) {
	fields, e := shotProtocolObject(raw)
	if e != nil {
		return nil, false, e
	}
	key := bindingIDKey(kind)
	if ns, ok := fields["asset_bindings"]; ok {
		bindings, e := bindingObject(ns)
		if e != nil {
			return nil, true, e
		}
		if v, ok := bindings[key]; ok {
			ids, e := bindingValues(v, kind == AssetScene)
			return ids, true, e
		}
	}
	v := fields[key]
	if !legacy || len(v) == 0 || string(v) == "null" {
		return nil, false, nil
	}
	var raws []json.RawMessage
	if kind == AssetScene {
		raws = []json.RawMessage{v}
	} else if json.Unmarshal(v, &raws) != nil {
		return nil, true, ErrInvalidArgument
	}
	var ids []string
	for _, value := range raws {
		var id string
		if json.Unmarshal(value, &id) != nil {
			var n json.Number
			if json.Unmarshal(value, &n) != nil || n == "" {
				return nil, true, ErrInvalidArgument
			}
			id = n.String()
		}
		// A numeric scene_id identifies the source scene in imported storyboards.
		if kind == AssetScene {
			if _, e := strconv.ParseInt(id, 10, 64); e == nil || id == "" {
				continue
			}
		}
		ids = append(ids, id)
	}
	return ids, len(ids) > 0, nil
}

func bindingShotField(sh *StoryboardShot, k AssetType) *string {
	switch k {
	case AssetCharacter:
		return &sh.Characters
	case AssetScene:
		return &sh.Scene
	default:
		return &sh.Props
	}
}
