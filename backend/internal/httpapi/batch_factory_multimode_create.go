package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/prompt"
)

type multiModeCreateRequest struct {
	batchfactoryv11.CreateMultiModeInput
	ManualInput *batchfactoryv11.ManualIntakeInput `json:"manualInput,omitempty"`
	IntakeID    string                             `json:"intakeId,omitempty"`
}

func registerMultiModeCreationRoutes(mux *http.ServeMux, store batchfactoryv11.Store, catalog *prompt.Catalog, coordinator *BatchAutomationCoordinator) {
	multi, ok := store.(batchfactoryv11.MultiModeCreationStore)
	if !ok || mux == nil {
		return
	}
	mux.HandleFunc("POST /api/batch-factory/v12/multi-mode-batches", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := bridgeOwner(r)
		if !ok {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		var request multiModeCreateRequest
		if !decodeJSON(w, r, &request) {
			return
		}
		input := request.CreateMultiModeInput
		if len(input.Books) == 0 && request.IntakeID != "" {
			intake, err := store.GetIntake(r.Context(), owner, request.IntakeID)
			if err != nil {
				writeStoreError(w, err)
				return
			}
			var source batchfactoryv11.NovelFetchIntakeInput
			if json.Unmarshal(intake.Payload, &source) != nil {
				writeStoreError(w, batchfactoryv11.ErrInvalid)
				return
			}
			input.Books = source.Books
		}
		if len(input.Books) == 0 && request.ManualInput != nil {
			var err error
			if len(request.ManualInput.Groups) > 0 {
				input.Books, err = batchfactoryv11.ParseGroupedManualBookLists(*request.ManualInput)
			} else {
				input.Books, err = batchfactoryv11.ParseManualBookList(*request.ManualInput)
			}
			if err != nil {
				writeStoreError(w, err)
				return
			}
		}
		for i := range input.Projects {
			project := &input.Projects[i]
			if project.PresetID != "" {
				if coordinator == nil || coordinator.presets == nil {
					writeStoreError(w, batchfactoryv11.ErrUnavailable)
					return
				}
				preset, err := coordinator.presets.Get(owner, project.PresetID)
				if err != nil || preset == nil {
					writeJSON(w, 400, map[string]string{"error": "自动化预设不存在或不属于当前账号"})
					return
				}
				if !multiModePresetCompatible(project.Mode, preset.Config) {
					writeJSON(w, 400, map[string]string{"error": "所选预设不适用于当前生产模式"})
					return
				}
			}
			if project.Settings == nil {
				project.Settings = batchfactoryv11.SettingsPatch{}
			}
			if project.Mode == "illustrated_text" {
				var config map[string]any
				_ = json.Unmarshal(project.Settings["aiPromptConfig"], &config)
				if config == nil {
					config = map[string]any{}
				}
				comic, _ := config["comic"].(map[string]any)
				if comic == nil {
					comic = map[string]any{}
				}
				comic["presetId"] = batchfactoryv11.ComicPageIllustratedTextPresetID
				comic["enabled"] = true
				config["comic"] = comic
				project.Settings["aiPromptConfig"], _ = json.Marshal(config)
			}
			raw, err := hydrateBatchFactoryPromptConfig(catalog, project.Settings["aiPromptConfig"])
			if err != nil {
				writeJSON(w, 400, map[string]string{"error": fmt.Sprintf("%s 配置：%v", project.Mode, err)})
				return
			}
			if len(raw) > 0 {
				project.Settings["aiPromptConfig"] = raw
			}
		}
		result, err := multi.CreateMultiModeBatches(r.Context(), owner, input)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		// Creation commits projects and durable source jobs only. Existing
		// automation workers read/classify each source after entering the workspace.
		writeJSON(w, http.StatusCreated, map[string]any{"groupId": result.GroupID, "batches": result.Batches, "projects": result.Projects})
	})
	mux.HandleFunc("GET /api/batch-factory/v12/multi-mode-batches/{groupId}", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := bridgeOwner(r)
		if !ok {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		result, err := multi.GetMultiModeCreation(r.Context(), owner, r.PathValue("groupId"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
	mux.HandleFunc("POST /api/batch-factory/v12/multi-mode-batches/{groupId}/resume", func(w http.ResponseWriter, r *http.Request) {
		owner, ok := bridgeOwner(r)
		if !ok {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		if coordinator == nil {
			writeStoreError(w, batchfactoryv11.ErrUnavailable)
			return
		}
		if err := coordinator.resolveMultiModeGroup(r.Context(), owner, r.PathValue("groupId")); err != nil {
			writeStoreError(w, err)
			return
		}
		result, err := multi.GetMultiModeCreation(r.Context(), owner, r.PathValue("groupId"))
		if err != nil {
			writeStoreError(w, err)
			return
		}
		writeJSON(w, 200, result)
	})
}

func multiModePresetCompatible(mode batchfactoryv11.CreationMode, config map[string]any) bool {
	selected, _ := config["productionMode"].(string)
	expected := string(mode)
	if expected == "illustrated_text" {
		expected = "comic"
	}
	return selected == "" || selected == expected
}
