package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/yzcmmodels"
)

func writeVideoConfigAlias(w http.ResponseWriter, r *http.Request, accounts accountcenter.Store, store novelfetchworkshop.Store, saved yzcmmodels.Reader) {
	access := apiConfigPolicy(r.Context(), accounts)
	if !access.authorize(w, true, false) {
		return
	}
	if store == nil {
		writeAccountCenterForbidden(w)
		return
	}
	var input map[string]any
	if !decodeJSON(w, r, &input) {
		return
	}
	var row map[string]any
	var err error
	existingOnly := false
	if !access.custom && allowedInputFields(input, "model", "apiKey") {
		input = map[string]any{"modelId": input["model"], "credential": input["apiKey"]}
	}
	if allowedInputFields(input, "modelId", "credential") {
		row, err = newRestrictedYZCMModel(r.Context(), saved, map[string]any{"kind": "video", "modelId": input["modelId"], "credential": input["credential"]})
		if errors.Is(err, accountcenter.ErrForbidden) {
			existingOnly = true
			row, err = existingVideoAliasRow(r.Context(), store, access.identity.Subject.Username, input)
		}
	} else if access.custom && allowedInputFields(input, "provider", "model", "apiKey", "createUrl", "tasksUrl", "resultUrl", "workflowId") {
		key, _, keyErr := modelCredentialPatch(map[string]any{"credential": firstText(input["apiKey"])})
		if keyErr != nil {
			writeAPIConfigError(w, keyErr)
			return
		}
		config, normalizeErr := batchfactoryv11.NormalizeVideoProviderConfig(batchfactoryv11.VideoProviderConfig{
			Provider: firstText(input["provider"]), Model: firstText(input["model"]), APIKey: key,
			CreateURL: firstText(input["createUrl"]), TasksURL: firstText(input["tasksUrl"]),
			ResultURL: firstText(input["resultUrl"]), WorkflowID: firstText(input["workflowId"]),
		})
		if normalizeErr != nil {
			writeAPIConfigError(w, accountcenter.ErrInvalid)
			return
		}
		row = map[string]any{"id": "legacy-video:" + config.Provider, "kind": "video", "provider": config.Provider, "modelId": config.Model,
			"displayName": config.Model, "credential": config.APIKey, "endpoint": config.CreateURL, "tasksEndpoint": config.TasksURL,
			"resultEndpoint": config.ResultURL, "workflowId": config.WorkflowID, "enabled": true}
	} else {
		writeAccountCenterForbidden(w)
		return
	}
	if err != nil {
		writeAPIConfigError(w, err)
		return
	}
	if modelString(row, "provider") != batchfactoryv11.VideoProviderDoubaoLocal && strings.TrimSpace(shuihuoModelCredential(row)) == "" {
		writeAPIConfigError(w, accountcenter.ErrInvalid)
		return
	}
	owner := access.identity.Subject.Username
	settings, err := store.GetConfig(r.Context(), owner)
	if err != nil {
		writeAuthError(w, http.StatusServiceUnavailable, "模型配置暂不可用")
		return
	}
	rows := modelCatalog(settings)
	found := false
	for i, current := range rows {
		if current["id"] == row["id"] {
			rows[i] = row
			found = true
			break
		}
	}
	if !found {
		if existingOnly {
			writeAccountCenterForbidden(w)
			return
		}
		rows = append(rows, row)
	}
	settings["modelCatalog"] = rows
	if err := store.PutConfig(r.Context(), owner, settings); err != nil {
		writeAuthError(w, http.StatusServiceUnavailable, "模型配置保存失败")
		return
	}
	view := map[string]any{"configured": true, "model": modelString(row, "modelId"), "id": modelString(row, "id")}
	if access.custom {
		view["provider"] = modelString(row, "provider")
	}
	if isManagedYZCMModel(row) {
		view["runtimeAvailable"] = false
		view["runtimeReason"] = videoExecutionUnavailableReason(row)
	}
	writeJSON(w, http.StatusOK, view)
}

func existingVideoAliasRow(ctx context.Context, store novelfetchworkshop.Store, owner string, input map[string]any) (map[string]any, error) {
	key, _, err := modelCredentialPatch(input)
	modelID := modelString(input, "modelId")
	if err != nil || key == "" || modelID == "" {
		return nil, accountcenter.ErrForbidden
	}
	read := store.GetConfig
	if bound, ok := store.(interface {
		ReadBoundModelConfig(context.Context, string) (map[string]any, error)
	}); ok {
		read = bound.ReadBoundModelConfig
	}
	config, err := read(ctx, owner)
	if err != nil {
		return nil, accountcenter.ErrForbidden
	}
	for _, row := range modelCatalog(config) {
		if row["id"] == "yzcm:video:"+modelID && row["kind"] == "video" && row["modelId"] == modelID && isManagedYZCMModel(row) {
			row["credential"] = key
			delete(row, "apiKey")
			delete(row, "credentialStatus")
			delete(row, "credentialRecoveryReason")
			return row, nil
		}
	}
	return nil, accountcenter.ErrForbidden
}
