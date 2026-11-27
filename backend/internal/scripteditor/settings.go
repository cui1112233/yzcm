package scripteditor

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type WorkSettings struct {
	ModelID           string            `json:"modelId"`
	VisualModelID     string            `json:"visualModelId"`
	ScriptStyle       string            `json:"scriptStyle"`
	VisualStyle       string            `json:"visualStyle"`
	Ratio             string            `json:"ratio"`
	Concurrency       int               `json:"concurrency"`
	AutosaveSeconds   int               `json:"autosaveSeconds"`
	StoryboardDensity string            `json:"storyboardDensity"`
	Revision          int64             `json:"revision"`
	Sources           map[string]string `json:"sources,omitempty"`
	UpdatedAt         time.Time         `json:"updatedAt,omitempty"`
}

type SettingsStore interface {
	GetWorkSettings(context.Context, string, string) (WorkSettings, error)
	SaveWorkSettings(context.Context, string, string, WorkSettings, int64) (WorkSettings, error)
}

func DefaultWorkSettings() WorkSettings {
	return WorkSettings{ScriptStyle: "短剧", VisualStyle: "电影感", Ratio: "9:16", Concurrency: 2, AutosaveSeconds: 3, StoryboardDensity: "标准"}
}

func ResolveSettings(system, account, work WorkSettings) WorkSettings {
	result := system
	result.Sources = map[string]string{}
	setString := func(key string, accountValue, workValue string, target *string) {
		result.Sources[key] = "system"
		if strings.TrimSpace(accountValue) != "" {
			*target = accountValue
			result.Sources[key] = "account"
		}
		if strings.TrimSpace(workValue) != "" {
			*target = workValue
			result.Sources[key] = "work"
		}
	}
	setInt := func(key string, accountValue, workValue int, target *int) {
		result.Sources[key] = "system"
		if accountValue > 0 {
			*target = accountValue
			result.Sources[key] = "account"
		}
		if workValue > 0 {
			*target = workValue
			result.Sources[key] = "work"
		}
	}
	setString("modelId", account.ModelID, work.ModelID, &result.ModelID)
	setString("visualModelId", account.VisualModelID, work.VisualModelID, &result.VisualModelID)
	setString("scriptStyle", account.ScriptStyle, work.ScriptStyle, &result.ScriptStyle)
	setString("visualStyle", account.VisualStyle, work.VisualStyle, &result.VisualStyle)
	setString("ratio", account.Ratio, work.Ratio, &result.Ratio)
	setString("storyboardDensity", account.StoryboardDensity, work.StoryboardDensity, &result.StoryboardDensity)
	setInt("concurrency", account.Concurrency, work.Concurrency, &result.Concurrency)
	setInt("autosaveSeconds", account.AutosaveSeconds, work.AutosaveSeconds, &result.AutosaveSeconds)
	result.Revision, result.UpdatedAt = work.Revision, work.UpdatedAt
	return result
}

func (s *Service) Settings(ctx context.Context, owner, workID string) (WorkSettings, error) {
	if _, err := s.Store.GetWork(ctx, owner, workID); err != nil {
		return WorkSettings{}, err
	}
	store, ok := s.Store.(SettingsStore)
	if !ok {
		return WorkSettings{}, ErrValidation
	}
	row, err := store.GetWorkSettings(ctx, owner, workID)
	if err == ErrNotFound {
		return ResolveSettings(DefaultWorkSettings(), WorkSettings{}, WorkSettings{}), nil
	}
	if err != nil {
		return WorkSettings{}, err
	}
	return ResolveSettings(DefaultWorkSettings(), WorkSettings{}, row), nil
}
func (s *Service) SaveSettings(ctx context.Context, owner, workID string, input WorkSettings, expected int64) (WorkSettings, error) {
	if _, err := s.Store.GetWork(ctx, owner, workID); err != nil {
		return WorkSettings{}, err
	}
	if input.Concurrency < 0 || input.Concurrency > 20 || input.AutosaveSeconds < 0 || input.AutosaveSeconds > 300 {
		return WorkSettings{}, ErrValidation
	}
	store, ok := s.Store.(SettingsStore)
	if !ok {
		return WorkSettings{}, ErrValidation
	}
	input.Sources = nil
	saved, err := store.SaveWorkSettings(ctx, owner, workID, input, expected)
	if err != nil {
		return WorkSettings{}, err
	}
	return ResolveSettings(DefaultWorkSettings(), WorkSettings{}, saved), nil
}
func settingsJSON(row WorkSettings) ([]byte, error) {
	row.Revision = 0
	row.Sources = nil
	row.UpdatedAt = time.Time{}
	return json.Marshal(row)
}
func decodeSettingsJSON(raw []byte) (WorkSettings, error) {
	var row WorkSettings
	err := json.Unmarshal(raw, &row)
	return row, err
}
