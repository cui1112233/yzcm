package novelfetchworkshop

import "strings"

// ModelCredentialTarget records the configured destinations used by model
// adapters. Most use Endpoint; legacy text and quota adapters also use BaseURL.
type ModelCredentialTarget struct {
	Endpoint, BaseURL, TasksEndpoint, ResultEndpoint string
}

func ModelCredentialDestination(model map[string]any) ModelCredentialTarget {
	target := ModelCredentialTarget{
		Endpoint: ModelConfigText(model, "endpoint"), BaseURL: ModelConfigText(model, "baseUrl"),
		TasksEndpoint: ModelConfigText(model, "tasksEndpoint"), ResultEndpoint: ModelConfigText(model, "resultEndpoint"),
	}
	if target.Endpoint == "" {
		target.Endpoint = target.BaseURL
	}
	return target
}

func ModelConfigText(model map[string]any, key string) string {
	value, _ := model[key].(string)
	return strings.TrimSpace(value)
}

func ModelCredentialSource(model map[string]any) [3]string {
	return [3]string{ModelConfigText(model, "catalogSource"), ModelConfigText(model, "provider"), ModelConfigText(model, "adapterKind")}
}
