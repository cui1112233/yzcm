package httpapi

import (
	"slices"
	"strings"
)

// A journal records the caller's explicit field assignments and removals.
// Channel paths address a stable channel ID, never a position in a list.
type settingsEdit struct {
	Path   []string `json:"path"`
	Value  any      `json:"value,omitempty"`
	Delete bool     `json:"delete,omitempty"`
}

type settingsEdits []settingsEdit

func fieldEdits(prefix []string, patch map[string]any) settingsEdits {
	var edits settingsEdits
	for field, value := range patch {
		path := append(slices.Clone(prefix), field)
		if nested, ok := value.(map[string]any); ok {
			edits = append(edits, fieldEdits(path, nested)...)
		} else {
			edits = append(edits, settingsEdit{Path: path, Value: value, Delete: value == nil})
		}
	}
	return edits
}

func applyFieldEdit(object map[string]any, path []string, edit settingsEdit) {
	for _, field := range path[:len(path)-1] {
		child, ok := object[field].(map[string]any)
		if !ok {
			if edit.Delete {
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
	} else {
		object[key] = edit.Value
	}
}

func (edits settingsEdits) apply(config map[string]any) {
	for _, edit := range edits {
		path := edit.Path
		if len(path) < 3 || path[0] != apiIntegrationsSettingKey || path[1] != "customChannels" {
			applyFieldEdit(config, path, edit)
			continue
		}
		integration := apiIntegrationStoredMap(config)
		channels := apiIntegrationChannels(integration)
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
			applyFieldEdit(row, path[3:], edit)
		}
		integration["customChannels"] = channels
		config[apiIntegrationsSettingKey] = integration
	}
}

func integrationChannelEdits(previous []any, submitted []map[string]any, normalized []any) settingsEdits {
	var edits settingsEdits
	present := make(map[string]bool)
	for i, input := range submitted {
		channel := normalized[i].(map[string]any)
		id := channel["id"].(string)
		present[id] = true
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
	for _, item := range previous {
		channel, _ := item.(map[string]any)
		if id, _ := channel["id"].(string); id != "" && !present[id] {
			edits = append(edits, settingsEdit{Path: []string{apiIntegrationsSettingKey, "customChannels", id}, Delete: true})
		}
	}
	return edits
}
