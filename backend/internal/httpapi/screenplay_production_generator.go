package httpapi

import (
	"context"
	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/screenplay"
	"qiantie/backend/internal/yzcmusage"
)

// A background call retains the authenticated subject and actual actor. A team
// change or deactivated identity stops before resolving a paid provider.
type screenplayProductionTextGenerator struct {
	generator screenplayTextGenerator
	accounts  accountcenter.Store
}

func (g screenplayProductionTextGenerator) Generate(ctx context.Context, req screenplay.TextRequest) (screenplay.TextResponse, error) {
	if _, frozen := screenplay.ProductionAuthorizationFromContext(ctx); frozen && g.accounts != nil {
		team, actor := yzcmusage.AttributionForOwner(ctx, g.generator.owner)
		if actor == "" {
			return screenplay.TextResponse{}, screenplay.ErrAccessDenied
		}
		subject, err := g.accounts.ResolveIdentity(ctx, g.generator.owner)
		if err != nil || !subject.Active || subject.ArchivedAt != nil || subject.TeamID != team {
			return screenplay.TextResponse{}, screenplay.ErrAccessDenied
		}
		actual, err := g.accounts.ResolveIdentity(ctx, actor)
		if err != nil || !actual.Active || actual.ArchivedAt != nil {
			return screenplay.TextResponse{}, screenplay.ErrAccessDenied
		}
		if (actual.Role == accountcenter.RoleMember || actual.Role == accountcenter.RoleManager) && actual.TeamID != team {
			return screenplay.TextResponse{}, screenplay.ErrAccessDenied
		}
		configOwner, err := ResolveAPIConfigOwner(ctx, g.accounts, g.generator.owner)
		if err != nil {
			return screenplay.TextResponse{}, screenplay.ErrAccessDenied
		}
		if actual.Username != subject.Username && !(accountcenter.Policy{}).Can(actual, accountcenter.ActionImpersonate, accountcenter.PolicyTarget{Identity: subject, InManagedTeam: subject.TeamID != "" && configOwner == actual.Username}) {
			return screenplay.TextResponse{}, screenplay.ErrAccessDenied
		}
	}
	return g.generator.Generate(ctx, req)
}
