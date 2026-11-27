package httpapi

import (
	"errors"
	"net/http"

	"qiantie/backend/internal/batchfactoryv11"
)

// Runtime status reports only the model label and readiness for this subject.
func registerVideoProviderStatusRoute(mux *http.ServeMux, auth BridgeAuth, registry batchfactoryv11.VideoProviderRegistry, version string) {
	mux.Handle("GET /api/batch-factory/"+version+"/video-provider/status", auth.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner, ok := bridgeOwner(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		provider := batchfactoryv11.NormalizeVideoProviderForHTTP(r.URL.Query().Get("provider"))
		view := batchfactoryv11.VideoProviderConfigView{Provider: provider, Model: batchfactoryv11.DefaultVideoModelFor(provider)}
		if registry != nil {
			if current, err := registry.View(r.Context(), owner, provider); err == nil {
				view = current
			} else if !errors.Is(err, batchfactoryv11.ErrUnavailable) {
				writeStoreError(w, err)
				return
			}
		}
		writeJSON(w, http.StatusOK, view)
	})))
}
