package httpapi

import (
	"context"
	"fmt"
	"strings"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
)

// CatalogVideoProviderRegistry resolves credentials from current account and
// model records for every use. It never retains provider keys.
type CatalogVideoProviderRegistry struct {
	accounts accountcenter.Store
	catalog  novelfetchworkshop.Store
}

func NewCatalogVideoProviderRegistry(accounts accountcenter.Store, catalog novelfetchworkshop.Store) *CatalogVideoProviderRegistry {
	return &CatalogVideoProviderRegistry{accounts: accounts, catalog: catalog}
}

func (*CatalogVideoProviderRegistry) Put(context.Context, string, batchfactoryv11.VideoProviderConfig) error {
	return batchfactoryv11.ErrUnavailable
}

func (r *CatalogVideoProviderRegistry) Resolve(ctx context.Context, subject, provider string) (batchfactoryv11.VideoProviderConfig, error) {
	return r.ResolveModel(ctx, subject, provider, "")
}

func (r *CatalogVideoProviderRegistry) ResolveModel(ctx context.Context, subject, provider, selected string) (batchfactoryv11.VideoProviderConfig, error) {
	return r.resolveVideoModel(ctx, subject, provider, selected, false)
}

func (r *CatalogVideoProviderRegistry) ResolveExactVideoModel(ctx context.Context, subject, id string) (batchfactoryv11.VideoProviderConfig, error) {
	return r.resolveVideoModel(ctx, subject, "", id, true)
}

func (r *CatalogVideoProviderRegistry) resolveVideoModel(ctx context.Context, subject, provider, selected string, exact bool) (batchfactoryv11.VideoProviderConfig, error) {
	var empty batchfactoryv11.VideoProviderConfig
	owner, settings, err := resolveVideoRuntimeSettings(ctx, r.accounts, r.catalog, subject)
	if err != nil {
		return empty, batchfactoryv11.ErrUnavailable
	}
	selected = strings.TrimSpace(selected)
	if exact && selected == "" {
		return empty, batchfactoryv11.ErrVideoModelNotFound
	}
	var upstreamSelection = selected
	if variant := batchfactoryv11.FanKeModelFromSelection(selected); !exact && variant != "" {
		upstreamSelection = variant
	}
	if !exact && selected == "yd2-mini-video" {
		upstreamSelection = batchfactoryv11.DefaultPersonalVideoModel
	}
	var providerFilter string
	if provider != "" {
		providerFilter = batchfactoryv11.NormalizeVideoProviderForHTTP(provider)
	}
	var matchesID, matchesFallback []map[string]any
	rows := modelCatalog(settings)
	for _, row := range rows {
		id, upstream := modelString(row, "id"), modelString(row, "modelId")
		if selected != "" && id == selected {
			matchesID = append(matchesID, row)
		}
		if row["kind"] != "video" {
			continue
		}
		if !exact && (selected == "" && videoCatalogProvider(row) == providerFilter || selected != "" && upstream == upstreamSelection) {
			matchesFallback = append(matchesFallback, row)
		}
	}
	candidates := matchesID
	if len(candidates) == 0 && !exact {
		candidates = matchesFallback
	}
	if len(candidates) == 0 {
		return empty, fmt.Errorf("%w: %w，请重新选择视频模型", batchfactoryv11.ErrUnavailable, batchfactoryv11.ErrVideoModelNotFound)
	}
	if len(candidates) != 1 {
		return empty, fmt.Errorf("%w: 有多条视频模型配置符合旧选择，请重新选择", batchfactoryv11.ErrUnavailable)
	}
	row := candidates[0]
	// A relay selection is never submitted to a different vendor using its
	// saved key. The batch adapters currently implement the native providers.
	provider = videoCatalogProvider(row)
	if row["kind"] != "video" || videoExecutionUnavailableReason(row) != "" || providerFilter != "" && provider != providerFilter {
		return empty, fmt.Errorf("%w: 所选视频模型的执行器暂不可用", batchfactoryv11.ErrUnavailable)
	}
	if !managedModelAvailable(row) || row["credentialStatus"] == "needs_reentry" {
		return empty, batchfactoryv11.ErrUnavailable
	}
	target := novelfetchworkshop.ModelCredentialDestination(row)
	return batchfactoryv11.NormalizeVideoProviderConfig(batchfactoryv11.VideoProviderConfig{
		ConfigID: modelString(row, "id"), ConfigOwner: owner, Provider: provider, Model: modelString(row, "modelId"), APIKey: shuihuoModelCredential(row),
		CreateURL: target.Endpoint, TasksURL: target.TasksEndpoint, ResultURL: target.ResultEndpoint, WorkflowID: modelString(row, "workflowId"),
	})
}

func currentVideoRuntimeSettings(ctx context.Context, accounts accountcenter.Store, catalog novelfetchworkshop.Store, subject string) (map[string]any, error) {
	_, config, err := resolveVideoRuntimeSettings(ctx, accounts, catalog, subject)
	return config, err
}

func resolveVideoRuntimeSettings(ctx context.Context, accounts accountcenter.Store, catalog novelfetchworkshop.Store, subject string) (string, map[string]any, error) {
	owner, err := ResolveAPIConfigOwner(ctx, accounts, subject)
	if err != nil || catalog == nil {
		return "", nil, batchfactoryv11.ErrUnavailable
	}
	authority, err := accounts.ResolveIdentity(ctx, owner)
	if err != nil {
		return "", nil, err
	}
	policyCtx := context.WithValue(ctx, accountCenterIdentityKey{}, accountcenter.RequestIdentity{Actor: authority, Subject: authority})
	access := apiConfigPolicy(policyCtx, accounts)
	if !access.allowed {
		return "", nil, batchfactoryv11.ErrUnavailable
	}
	config, err := catalog.GetConfig(ctx, owner)
	if err != nil {
		return "", nil, err
	}
	if authority.Role == accountcenter.RoleDeveloper && !access.custom {
		models := []map[string]any{}
		for _, row := range modelCatalog(config) {
			if row["kind"] == "video" && (isManagedYZCMModel(row) || videoCatalogProvider(row) == batchfactoryv11.VideoProviderDoubaoLocal) {
				models = append(models, row)
			}
		}
		config["modelCatalog"] = models
	}
	return owner, config, nil
}

func (r *CatalogVideoProviderRegistry) View(ctx context.Context, owner, provider string) (batchfactoryv11.VideoProviderConfigView, error) {
	config, err := r.Resolve(ctx, owner, provider)
	if err != nil {
		return batchfactoryv11.VideoProviderConfigView{Provider: batchfactoryv11.NormalizeVideoProviderForHTTP(provider)}, err
	}
	configured := false
	switch config.Provider {
	case batchfactoryv11.VideoProviderPersonalAPI:
		adapter := batchfactoryv11.YadiVideoAdapter{CreateURL: config.CreateURL, TasksURL: config.TasksURL, ResultURL: config.ResultURL, APIKey: config.APIKey, Model: config.Model}
		configured = adapter.Validate() == nil
	case batchfactoryv11.VideoProviderAutoDLH3:
		adapter := batchfactoryv11.AutoDLH3VideoAdapter{CreateURL: config.CreateURL, TasksURL: config.TasksURL, APIKey: config.APIKey, Model: config.Model, WorkflowID: config.WorkflowID}
		configured = adapter.Validate() == nil
	case batchfactoryv11.VideoProviderYFAISeedance:
		adapter := batchfactoryv11.YFAISeedanceAdapter{BaseURL: config.CreateURL, APIKey: config.APIKey, Model: config.Model}
		configured = adapter.Validate() == nil
	}
	return batchfactoryv11.VideoProviderConfigView{Provider: config.Provider, Model: config.Model, Configured: configured}, nil
}

func videoCatalogProvider(row map[string]any) string {
	if modelString(row, "adapterKind") == "local_executor_video" || modelString(row, "provider") == "local_executor_video" {
		return batchfactoryv11.VideoProviderDoubaoLocal
	}
	return batchfactoryv11.VideoProviderForModel(modelString(row, "modelId"), batchfactoryv11.VideoProviderForModel(modelString(row, "id"), modelString(row, "provider")))
}

func videoExecutionUnavailableReason(row map[string]any) string {
	if row["kind"] != "video" {
		return ""
	}
	const unavailable = "execution_adapter_unavailable"
	if row["catalogSource"] == managedYZCMSource || row["provider"] == "yzcm" {
		return unavailable
	}
	switch videoCatalogProvider(row) {
	case batchfactoryv11.VideoProviderPersonalAPI, batchfactoryv11.VideoProviderAutoDLH3, batchfactoryv11.VideoProviderYFAISeedance, batchfactoryv11.VideoProviderDoubaoLocal:
		return ""
	default:
		return unavailable
	}
}
