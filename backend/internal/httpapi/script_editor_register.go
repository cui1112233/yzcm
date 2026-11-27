package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/scripteditor"
)

type scriptEditorGenerator struct {
	provider batchfactoryv11.DirectorProvider
}

func (g scriptEditorGenerator) Generate(ctx context.Context, system, user, _ string) (string, error) {
	return g.provider.Complete(ctx, batchfactoryv11.TextCompletionRequest{SystemPrompt: system, UserPrompt: user, Temperature: 0.65, MaxTokens: 8192, DisableJSONResponse: true})
}

func registerScriptEditor(root *http.ServeMux, auth BridgeAuth, options RouterOptions, resolver func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error)) {
	if options.ScriptEditor != nil && resolver != nil {
		options.ScriptEditor.GeneratorResolver = func(ctx context.Context, owner, modelID string) (scripteditor.ScriptGenerator, error) {
			provider, err := resolver(ctx, owner, modelID)
			if err != nil || provider == nil {
				if err == nil {
					err = fmt.Errorf("text model unavailable")
				}
				return nil, err
			}
			return scriptEditorGenerator{provider: provider}, nil
		}
	}
	RegisterScriptEditorRoutes(root, auth, options.ScriptEditor, resolver)
}
