# Personal Center API Configuration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve the real API configuration page while making manager presets YZCM-only, per-model Key based, and securely role-aware.

**Architecture:** Keep the existing platform preset and capability group UI. Add a server-owned YZCM allowed catalog and capability response; the frontend selection modal only chooses displayed models, while each selected card continues to edit its own Key on the main page.

**Tech Stack:** Go 1.26, MySQL 8, existing user config secret encryption, React 18, Ant Design 5.

## Global Constraints

- Managers see only YZCM models in “自定义预设”.
- Managers never receive or submit addresses, API formats, advanced developer fields, or “添加自定义模型”.
- Each selected model has an independent inline Key field; changing one Key never overwrites another.
- Founder and authorized developer retain the current full custom-model workflow.
- Members do not see API configuration and never receive complete Keys.
- Current platform presets, capability groups, autosave, quota refresh, and auxiliary configuration groups remain available according to role.

---

### Task 1: Persist and serve the allowed YZCM model catalog

**Files:**
- Create: `backend/internal/storage/yzcm_model_catalog_schema.go`
- Create: `backend/internal/storage/yzcm_model_catalog_schema_test.go`
- Create: `backend/internal/yzcmmodels/store.go`
- Create: `backend/internal/yzcmmodels/store_test.go`
- Modify: `backend/internal/storage/local_executor_schema.go`
- Modify: `backend/internal/httpapi/model_catalog.go`
- Modify: `backend/internal/httpapi/model_catalog_test.go`
- Modify: `backend/internal/httpapi/api_config_policy.go`
- Modify: `backend/internal/httpapi/api_config_policy_test.go`

**Interfaces:**
- Migration version `8800406` creates `yzcm_model_catalog` and `yzcm_model_sync_runs`.
- Produces `GET /api/config/models/yzcm?kind=text&q=...` with saved catalog rows only; no upstream request occurs for each search.

- [ ] **Step 1: Write failing tests** for capability filtering, name/model-ID search, last-good catalog retention after sync failure, and adapted/allowed gating.
- [ ] **Step 2: Run `go test ./internal/storage ./internal/yzcmmodels ./internal/httpapi -run YZCMModel -count=1`; verify failure.**
- [ ] **Step 3: Implement migration, store, and read endpoint.** Return model ID, display name, capability, availability reason and catalog timestamp; never return provider admin credentials.
- [ ] **Step 4: Run focused tests; expect PASS.**
- [ ] **Step 5: Commit.**

```powershell
git add backend/internal/storage/yzcm_model_catalog_schema.go backend/internal/storage/yzcm_model_catalog_schema_test.go backend/internal/storage/local_executor_schema.go backend/internal/yzcmmodels/store.go backend/internal/yzcmmodels/store_test.go backend/internal/httpapi/model_catalog.go backend/internal/httpapi/model_catalog_test.go
git commit -m "feat(models): add saved YZCM model catalog"
```

### Task 2: Enforce role-aware configuration contracts in Go

**Files:**
- Create: `backend/internal/httpapi/api_config_policy.go`
- Create: `backend/internal/httpapi/api_config_policy_test.go`
- Modify: `backend/internal/httpapi/model_catalog.go`
- Modify: `backend/internal/httpapi/config_secrets.go`
- Modify: `backend/internal/httpapi/router.go`

**Interfaces:**
- Produces capability flags `canConfigureAPI`, `canAddCustomModel`, `canEditAddress`, `catalogSource`.
- Manager writes accept only selected YZCM model IDs and per-model Key changes; forbidden fields return 403 rather than being silently ignored.

- [ ] **Step 1: Write table-driven HTTP tests** for founder, authorized developer, unauthorized developer, manager and member reads/writes, including forged `baseURL` and `apiFormat` manager payloads.
- [ ] **Step 2: Run `go test ./internal/httpapi -run APIConfigPolicy -count=1`; verify failure.**
- [ ] **Step 3: Implement policy projection and write validation.** Preserve encrypted-at-rest handling through `configSecretProvider`; redact every role’s list response and reveal one Key only through the existing protected field-retrieval action.
- [ ] **Step 4: Run focused tests plus `go test ./internal/httpapi -run 'ConfigSecret|ModelCatalog' -count=1`; expect PASS.**
- [ ] **Step 5: Commit.**

```powershell
git add backend/internal/httpapi/api_config_policy.go backend/internal/httpapi/api_config_policy_test.go backend/internal/httpapi/model_catalog.go backend/internal/httpapi/config_secrets.go backend/internal/httpapi/router.go
git commit -m "feat(models): enforce role-aware API configuration"
```

### Task 3: Adapt the existing page and modal without replacing them

**Files:**
- Modify: `backend/internal/httpapi/model_catalog.go`
- Modify: `backend/internal/httpapi/model_catalog_test.go`
- Modify: `frontend/src/user/pages/ApiConfigPage.jsx`
- Modify: `frontend/src/user/pages/ApiIntegrationSettings.jsx`
- Modify: `frontend/src/user/pages/ApiPresetSelectionModal.jsx`
- Modify: `frontend/src/user/pages/ApiCustomModelModal.jsx`
- Modify: `frontend/src/user/pages/apiConfigCatalogMerge.js`
- Modify: `frontend/src/shared/api/modelCatalog.js`
- Modify: `frontend/src/shared/api/modelCatalog.test.js`
- Create: `frontend/src/user/pages/api-config-role-policy.js`
- Create: `frontend/src/user/pages/api-config-role-policy.test.js`
- Modify: `frontend/src/user/pages/ApiConfigPage.source.test.js`
- Modify: `frontend/src/user/pages/apiPresetSelection.test.js`

**Interfaces:**
- `projectApiConfigCapabilities(center)` controls rendering only; server remains authoritative.
- Manager modal searches the YZCM catalog, selects on the left, removes on the right, and returns selected IDs. Inline cards own Key editing/autosave.
- Restricted-role selection is the JSON boolean `selectedForConfigPage` on an existing managed YZCM row; absence means `true`. Deselecting hides the card but never deletes the row, clears the Key, or changes runtime availability. Restricted capability actions expose `update_model_selection` and do not expose `remove_model`; full accounts retain `remove_model`.

- [ ] **Step 1: Add failing tests** asserting manager-only YZCM results, no external providers, no address/add button, modal hidden until click, and one independent inline Key field per selected model.
- [ ] **Step 1a: Add failing Go tests** proving restricted-role selection can hide and re-show a YZCM row without changing its credential or runtime availability; forged address/provider fields and restricted DELETE remain forbidden.
- [ ] **Step 2: Run the targeted Node tests and verify failure.**

Run: `node --test src/user/pages/api-config-role-policy.test.js src/user/pages/apiPresetSelection.test.js src/user/pages/ApiConfigPage.source.test.js` from `frontend`

- [ ] **Step 3: Implement the selection contract and minimal UI changes** around the existing page. Do not remove platform presets, autosave delay, quota refresh, text/video/image/audio/asr groups, or auxiliary settings. The actual UI owner is `ApiIntegrationSettings.jsx`; the shared YZCM search client belongs in `modelCatalog.js`.
- [ ] **Step 4: Run targeted tests and `npm run build`; expect PASS.**
- [ ] **Step 5: Commit.**

```powershell
git add frontend/src/user/pages/ApiConfigPage.jsx frontend/src/user/pages/ApiPresetSelectionModal.jsx frontend/src/user/pages/ApiCustomModelModal.jsx frontend/src/user/pages/apiConfigCatalogMerge.js frontend/src/user/pages/api-config-role-policy.js frontend/src/user/pages/api-config-role-policy.test.js frontend/src/user/pages/ApiConfigPage.source.test.js frontend/src/user/pages/apiPresetSelection.test.js
git commit -m "feat(models): make manager presets YZCM-only"
```
