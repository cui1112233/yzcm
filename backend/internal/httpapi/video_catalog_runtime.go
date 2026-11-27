package httpapi

import (
	"context"
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
	var empty batchfactoryv11.VideoProviderConfig
	settings, err := currentVideoRuntimeSettings(ctx, r.accounts, r.catalog, subject)
	if err != nil {
		return empty, batchfactoryv11.ErrUnavailable
	}
	provider = batchfactoryv11.NormalizeVideoProviderForHTTP(provider)
	selected = strings.TrimSpace(selected)
	if variant := batchfactoryv11.FanKeModelFromSelection(selected); variant != "" {
		selected = variant
	}
	rows := modelCatalog(settings)
	candidates := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if row["kind"] != "video" {
			continue
		}
		id, upstream := modelString(row, "id"), modelString(row, "modelId")
		if selected != "" && id == selected {
			candidates = []map[string]any{row}
			break
		}
		if selected == "" && videoCatalogProvider(row) == provider || selected != "" && upstream == selected {
			candidates = append(candidates, row)
		}
	}
	if len(candidates) != 1 {
		return empty, batchfactoryv11.ErrUnavailable
	}
	row := candidates[0]
	// A relay selection is never submitted to a different vendor using its
	// saved key. The batch adapters currently implement the native providers.
	if isManagedYZCMModel(row) || videoCatalogProvider(row) != provider || !managedModelAvailable(row) ||
		row["credentialStatus"] == "needs_reentry" {
		return empty, batchfactoryv11.ErrUnavailable
	}
	target := novelfetchworkshop.ModelCredentialDestination(row)
	return batchfactoryv11.NormalizeVideoProviderConfig(batchfactoryv11.VideoProviderConfig{
		ConfigID: modelString(row, "id"), Provider: provider, Model: modelString(row, "modelId"), APIKey: shuihuoModelCredential(row),
		CreateURL: target.Endpoint, TasksURL: target.TasksEndpoint, ResultURL: target.ResultEndpoint, WorkflowID: modelString(row, "workflowId"),
	})
}

func currentVideoRuntimeSettings(ctx context.Context, accounts accountcenter.Store, catalog novelfetchworkshop.Store, subject string) (map[string]any, error) {
	owner, err := ResolveAPIConfigOwner(ctx, accounts, subject)
	if err != nil || catalog == nil {
		return nil, batchfactoryv11.ErrUnavailable
	}
	authority, err := accounts.ResolveIdentity(ctx, owner)
	if err != nil {
		return nil, err
	}
	policyCtx := context.WithValue(ctx, accountCenterIdentityKey{}, accountcenter.RequestIdentity{Actor: authority, Subject: authority})
	access := apiConfigPolicy(policyCtx, accounts)
	if !access.allowed {
		return nil, batchfactoryv11.ErrUnavailable
	}
	config, err := catalog.GetConfig(ctx, owner)
	if err != nil {
		return nil, err
	}
	if authority.Role == accountcenter.RoleDeveloper && !access.custom {
		models := []map[string]any{}
		for _, row := range modelCatalog(config) {
			if row["kind"] == "video" && isManagedYZCMModel(row) {
				models = append(models, row)
			}
		}
		config["modelCatalog"] = models
	}
	return config, nil
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
	return batchfactoryv11.VideoProviderForModel(modelString(row, "modelId"), batchfactoryv11.VideoProviderForModel(modelString(row, "id"), modelString(row, "provider")))
}
