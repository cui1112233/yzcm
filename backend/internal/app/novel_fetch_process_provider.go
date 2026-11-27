package app

import (
	"context"
	"strings"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/httpapi"
)

// Explicit process selections use the shared channel/catalog entry point. An
// omitted selection retains the catalog's existing account-default behavior.
func resolveNovelFetchProcessTextProvider(ctx context.Context, accounts accountcenter.Store, owner, selection string,
	selected func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error),
	catalog func(context.Context, string, string) (*batchfactoryv11.OpenAICompatibleProvider, error),
) (batchfactoryv11.DirectorProvider, error) {
	selection = strings.TrimSpace(selection)
	if selection != "" {
		configOwner, err := httpapi.ResolveAPIConfigOwner(ctx, accounts, owner)
		if err != nil {
			return nil, err
		}
		provider, err := selected(ctx, configOwner, selection)
		if err != nil {
			return nil, err
		}
		if novelFetchTextProviderMissing(provider) {
			return nil, batchfactoryv11.NewConfigRequiredError("选择的文本模型不可用：" + selection)
		}
		return provider, nil
	}
	provider, err := catalog(ctx, owner, "")
	if err != nil {
		return nil, err
	}
	// Do not return a typed nil inside DirectorProvider to the caller's fallback.
	if provider == nil {
		return nil, nil
	}
	return provider, nil
}

func novelFetchTextProviderMissing(provider batchfactoryv11.DirectorProvider) bool {
	if provider == nil {
		return true
	}
	configured, ok := provider.(*batchfactoryv11.OpenAICompatibleProvider)
	return ok && configured == nil
}
