package httpapi

import (
	"context"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/yzcmusage"
)

// NovelCreationTextResolver rechecks active membership and the current manager
// binding before every actual external call. The supplied resolver honours an
// explicit model ID and never substitutes a different configured model.
func NovelCreationTextResolver(center accountcenter.Store, resolve AgentTextModelResolver) AgentTextModelResolver {
	return func(ctx context.Context, owner, modelID string) (batchfactoryv11.DirectorProvider, error) {
		effective, err := ResolveAPIConfigOwner(ctx, center, owner)
		if err != nil {
			return nil, err
		}
		p, err := resolve(ctx, effective, modelID)
		if err != nil || p == nil {
			return p, err
		}
		return novelScopedTextProvider{DirectorProvider: p, owner: effective, subject: owner}, nil
	}
}

type novelScopedTextProvider struct {
	batchfactoryv11.DirectorProvider
	owner   string
	subject string
}

func (p novelScopedTextProvider) TextModelMetadata() batchfactoryv11.TextModelMetadata {
	m := batchfactoryv11.TextModelMetadata{}
	if actual, ok := p.DirectorProvider.(batchfactoryv11.TextModelMetadataProvider); ok {
		m = actual.TextModelMetadata()
	}
	m.CatalogOwner = p.owner
	return m
}

// The worker reconstructs its context from a durable job. Keep the caller
// separately from the manager whose model settings supplied the credentials.
func (p novelScopedTextProvider) Complete(ctx context.Context, input batchfactoryv11.TextCompletionRequest) (string, error) {
	ctx = yzcmusage.WithIdentity(ctx, p.subject, "novel-creation")
	return p.DirectorProvider.Complete(ctx, input)
}
