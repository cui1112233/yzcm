package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"qiantie/backend/internal/screenplay"
)

func assistantReadScopedTool(ctx context.Context, store screenplay.Store, owner, project, name string, args json.RawMessage) (screenplay.AssistantToolRun, map[string]any, error) {
	if name != "storyboard_failures" {
		return screenplay.ReadAssistantTool(ctx, store, owner, project, name, args)
	}
	if len(args) > 0 {
		var object map[string]json.RawMessage
		if len(args) > 8192 || json.Unmarshal(args, &object) != nil || object == nil || len(object) > 0 {
			return screenplay.AssistantToolRun{}, nil, fmt.Errorf("%w: 此工具不接受参数", screenplay.ErrInvalidArgument)
		}
	}
	run, data := assistantReadStoryboardFailures(ctx, store, owner, project)
	return run, data, nil
}
