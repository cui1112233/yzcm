package httpapi

import "net/http"

func registerScriptEditor(root *http.ServeMux, auth BridgeAuth, options RouterOptions) {
	RegisterScriptEditorRoutes(root, auth, options.ScriptEditor)
}
