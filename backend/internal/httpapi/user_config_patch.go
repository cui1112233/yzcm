package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"qiantie/backend/internal/novelfetchworkshop"
)

// A journal records the caller's explicit field assignments and removals.
// Channel paths address a stable channel ID, never a position in a list.
type settingsEdit = novelfetchworkshop.LegacySettingEdit

type settingsEdits []settingsEdit

func fieldEdits(prefix []string, patch map[string]any) settingsEdits {
	var edits settingsEdits
	for field, value := range patch {
		path := append(slices.Clone(prefix), field)
		if nested, ok := value.(map[string]any); ok {
			edits = append(edits, fieldEdits(path, nested)...)
		} else {
			edit := settingsEdit{Path: path, Delete: value == nil}
			if value != nil {
				edit.Value, _ = json.Marshal(value)
			}
			edits = append(edits, edit)
		}
	}
	return edits
}

func applyFieldEdit(object map[string]any, path []string, value any, edit settingsEdit) {
	for _, field := range path[:len(path)-1] {
		prior, present := object[field]
		child, ok := prior.(map[string]any)
		if !ok {
			if edit.Delete || edit.OnlyIfMissing && present {
				return
			}
			child = map[string]any{}
			object[field] = child
		}
		object = child
	}
	key := path[len(path)-1]
	if edit.Delete {
		delete(object, key)
	} else if existing, present := object[key]; present && edit.OnlyIfMissing {
		if slices.Equal(edit.Path, []string{apiIntegrationsSettingKey, "pinned"}) {
			original, ok := existing.([]any)
			incoming, _ := value.([]any)
			if ok && len(original) > 0 {
				for _, v := range incoming {
					id, ok := v.(string)
					if ok && !slices.ContainsFunc(original, func(x any) bool { return x == id }) {
						original = append(original, id)
					}
				}
				object[key] = original
			}
		}
	} else {
		object[key] = value
	}
}

func (edits settingsEdits) apply(config map[string]any) error {
	for _, edit := range edits {
		if err := edit.Validate(); err != nil {
			return err
		}
		var value any
		if !edit.Delete {
			if err := json.Unmarshal(edit.Value, &value); err != nil {
				return err
			}
		}
		path := edit.Path
		if len(path) < 3 || path[0] != apiIntegrationsSettingKey || path[1] != "customChannels" {
			applyFieldEdit(config, path, value, edit)
			continue
		}
		integration, valid := config[apiIntegrationsSettingKey].(map[string]any)
		if !valid {
			if _, present := config[apiIntegrationsSettingKey]; present && edit.OnlyIfMissing {
				continue
			}
			if edit.Delete {
				continue
			}
			integration = map[string]any{}
		}
		channels, ok := integration["customChannels"].([]any)
		if _, present := integration["customChannels"]; present && !ok {
			if edit.OnlyIfMissing {
				continue
			}
			return errors.New("invalid restored channel list")
		}
		index := slices.IndexFunc(channels, func(v any) bool { row, _ := v.(map[string]any); return row["id"] == path[2] })
		if len(path) == 3 {
			if index >= 0 && edit.Delete {
				channels = slices.Delete(channels, index, index+1)
			}
		} else {
			var row map[string]any
			if index >= 0 {
				row = channels[index].(map[string]any)
			} else {
				if edit.Delete {
					continue
				}
				row = map[string]any{"id": path[2]}
				channels = append(channels, row)
			}
			applyFieldEdit(row, path[3:], value, edit)
		}
		integration["customChannels"] = channels
		config[apiIntegrationsSettingKey] = integration
	}
	return nil
}

func integrationChannelEdits(submitted []map[string]any, normalized []any, deleted []string) settingsEdits {
	var edits settingsEdits
	for i, input := range submitted {
		channel := normalized[i].(map[string]any)
		id := channel["id"].(string)
		path := []string{apiIntegrationsSettingKey, "customChannels", id}
		changes := map[string]any{}
		for key, raw := range input {
			if key == "id" || key == "clearApiKey" || key == "apiKey" {
				continue
			}
			if key == "params" || key == "capabilities" {
				fields, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				if len(fields) == 0 {
					changes[key] = nil
					continue
				}
			}
			if v, ok := channel[key]; ok {
				changes[key] = v
			}
		}
		if clear, _ := input["clearApiKey"].(bool); clear {
			changes["apiKey"] = nil
		} else if key, _ := input["apiKey"].(string); strings.TrimSpace(key) != "" && channel["apiKey"] != nil {
			changes["apiKey"] = channel["apiKey"]
		}
		edits = append(edits, fieldEdits(path, changes)...)
	}
	for _, id := range deleted {
		edits = append(edits, settingsEdit{Path: []string{apiIntegrationsSettingKey, "customChannels", id}, Delete: true})
	}
	return edits
}

func submittedIDs(value any, max int) ([]string, error) {
	list, ok := value.([]any)
	if !ok || max > 0 && len(list) > max {
		return nil, errors.New("选项列表无效")
	}
	ids := make([]string, 0, len(list))
	seen := map[string]bool{}
	for _, item := range list {
		id, ok := item.(string)
		id = strings.TrimSpace(id)
		if !ok || id == "" || len(id) > 64 {
			return nil, errors.New("选项标识无效")
		}
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return ids, nil
}

func integrationRequestEdits(config, input map[string]any) (settingsEdits, error) {
	explicit := map[string]any{}
	if raw, ok := input["values"]; ok {
		fields, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("接口值无效")
		}
		values := map[string]any{}
		for key, value := range fields {
			secret, known := apiIntegrationFields[key]
			if !known {
				continue
			}
			if value != nil {
				coerced, ok := coerceIntegrationValue(key, value)
				if !ok {
					return nil, fmt.Errorf("字段内容无效：%s", key)
				}
				value = coerced
				if secret && value == "" {
					value = nil
				}
			}
			values[key] = value
		}
		explicit["values"] = values
	}
	if pins, ok := input["pinned"]; ok {
		ids, err := submittedIDs(pins, 0)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if _, ok := apiIntegrationFields[id]; !ok {
				return nil, fmt.Errorf("未知置顶项：%s", id)
			}
		}
		explicit["pinned"] = ids
	}
	if raw, ok := input["channelSelection"]; ok {
		groups, ok := raw.(map[string]any)
		if !ok {
			return nil, errors.New("渠道选择无效")
		}
		selections := map[string]any{}
		for group, value := range groups {
			if !apiIntegrationChannelGroups[group] {
				return nil, errors.New("未知渠道分组")
			}
			var ids []string
			switch v := value.(type) {
			case string:
				var err error
				ids, err = submittedIDs([]any{v}, 1)
				if err != nil {
					return nil, err
				}
			case map[string]any:
				if len(v) != 1 {
					return nil, errors.New("渠道选择包含未知字段")
				}
				var err error
				ids, err = submittedIDs(v["enabled"], 20)
				if err != nil {
					return nil, err
				}
			default:
				return nil, errors.New("渠道选择无效")
			}
			selections[group] = map[string]any{"enabled": ids}
		}
		explicit["channelSelection"] = selections
	}
	edits := fieldEdits([]string{apiIntegrationsSettingKey}, explicit)
	var deleted []string
	if v, ok := input["deletedChannelIds"]; ok {
		var err error
		deleted, err = submittedIDs(v, 0)
		if err != nil {
			return nil, err
		}
	}
	var submitted []map[string]any
	var normalized []any
	if raw, ok := input["customChannels"]; ok {
		list, ok := raw.([]any)
		if !ok {
			return nil, errors.New("自定义渠道无效")
		}
		existing := map[string]map[string]any{}
		for _, item := range apiIntegrationChannels(apiIntegrationStoredMap(config)) {
			row := item.(map[string]any)
			id, _ := row["id"].(string)
			existing[id] = row
		}
		seen := map[string]bool{}
		for _, item := range list {
			row, ok := item.(map[string]any)
			if !ok {
				return nil, errors.New("自定义渠道无效")
			}
			id, _ := row["id"].(string)
			id = strings.TrimSpace(id)
			validation := row
			if _, hasName := row["name"]; !hasName && id != "" {
				validation = cloneConfigMap(row)
				validation["name"] = id
			}
			channel, ok := sanitizeIntegrationChannel(validation, existing[id])
			if !ok {
				return nil, errors.New("自定义渠道无效")
			}
			id = channel["id"].(string)
			if seen[id] || slices.Contains(deleted, id) {
				return nil, errors.New("渠道标识重复")
			}
			seen[id] = true
			submitted = append(submitted, row)
			normalized = append(normalized, channel)
		}
	}
	return append(edits, integrationChannelEdits(submitted, normalized, deleted)...), nil
}
