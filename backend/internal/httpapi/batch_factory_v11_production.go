package httpapi

import (
	"net/http"
	"strings"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/yzcmmodels"
)

type productionSubmitInput struct {
	RequestID     string `json:"requestId"`
	Provider      string `json:"provider,omitempty"`
	CompilationID string `json:"compilationId,omitempty"`
}

func registerProductionRoutes(mux *http.ServeMux, service *batchfactoryv11.ProductionService, accounts accountcenter.Store, store novelfetchworkshop.Store, saved yzcmmodels.Reader, catalogStores ...novelfetchworkshop.Store) {
	registerProductionStatusRoute(mux, service, "v11")
	mux.HandleFunc("PUT /api/batch-factory/v11/video-provider/config", func(w http.ResponseWriter, r *http.Request) {
		writeVideoConfigAlias(w, r, accounts, store, saved)
	})
	mux.HandleFunc("POST /api/batch-factory/v11/batches/{batchId}/books/{bookId}/production", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := bridgeOwner(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		var input productionSubmitInput
		if !decodeJSON(w, r, &input) {
			return
		}
		if strings.TrimSpace(input.RequestID) == "" {
			writeStoreError(w, batchfactoryv11.ErrInvalid)
			return
		}
		if service != nil && service.ProviderRegistry != nil {
			if syncErr := syncShuihuoConfiguredVideoModels(r.Context(), service.Store, service.ProviderRegistry, owner, catalogStores...); syncErr != nil {
				writeStoreError(w, syncErr)
				return
			}
		}
		job, err := service.SubmitBookProductionWithOptions(r.Context(), owner, r.PathValue("batchId"), r.PathValue("bookId"), input.RequestID, input.Provider, batchfactoryv11.ProductionOptions{CompilationID: input.CompilationID})
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"job": job})
	})
	mux.HandleFunc("POST /api/batch-factory/v11/batches/{batchId}/production", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := bridgeOwner(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		var input productionSubmitInput
		if !decodeJSON(w, r, &input) {
			return
		}
		if strings.TrimSpace(input.RequestID) == "" {
			writeStoreError(w, batchfactoryv11.ErrInvalid)
			return
		}
		if service != nil && service.ProviderRegistry != nil {
			if syncErr := syncShuihuoConfiguredVideoModels(r.Context(), service.Store, service.ProviderRegistry, owner, catalogStores...); syncErr != nil {
				writeStoreError(w, syncErr)
				return
			}
		}
		status, err := service.SubmitBatchProductionWithProvider(r.Context(), owner, r.PathValue("batchId"), input.RequestID, input.Provider)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, status)
	})
	mux.HandleFunc("POST /api/batch-factory/v11/batches/{batchId}/production/cancel", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := bridgeOwner(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if service == nil {
			writeStoreError(w, batchfactoryv11.ErrUnavailable)
			return
		}
		var input productionSubmitInput
		if !decodeJSON(w, r, &input) {
			return
		}
		if strings.TrimSpace(input.RequestID) == "" {
			writeStoreError(w, batchfactoryv11.ErrInvalid)
			return
		}
		result, err := service.CancelBatch(r.Context(), owner, r.PathValue("batchId"), input.RequestID)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	})
	mux.HandleFunc("DELETE /api/batch-factory/v11/batches/{batchId}/books/{bookId}/videos/{videoId}/tasks/{taskId}", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := bridgeOwner(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if service == nil {
			writeStoreError(w, batchfactoryv11.ErrUnavailable)
			return
		}
		if err := service.RemoveBookProductionCandidate(r.Context(), owner, r.PathValue("batchId"), r.PathValue("bookId"), r.PathValue("videoId"), r.PathValue("taskId")); err != nil {
			writeStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	/* status is registered by registerProductionStatusRoute above */
}

func registerProductionStatusRoute(mux *http.ServeMux, service *batchfactoryv11.ProductionService, version string) {
	mux.HandleFunc("GET /api/batch-factory/"+version+"/batches/{batchId}/status", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := bridgeOwner(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		status, err := service.GetBatchStatus(r.Context(), owner, r.PathValue("batchId"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	})
}
