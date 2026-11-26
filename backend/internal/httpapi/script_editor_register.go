package httpapi

import (
	"context"
	"net/http"

	"qiantie/backend/internal/batchfactoryv11"
)

type scriptEditorGenerator struct {
	provider batchfactoryv11.DirectorProvider
}

func (g scriptEditorGenerator) Generate(ctx context.Context, system, user, _ string) (string, error) {
	return g.provider.Complete(ctx, batchfactoryv11.TextCompletionRequest{SystemPrompt: system, UserPrompt: user, Temperature: 0.65, MaxTokens: 8192, DisableJSONResponse: true})
}

func registerScriptEditor(root *http.ServeMux, auth BridgeAuth, options RouterOptions, resolver func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error)) {
	RegisterScriptEditorRoutes(root, auth, options.ScriptEditor, resolver)
}
