package httpapi

import (
	"context"
	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
	"qiantie/backend/internal/yzcmusage"
	"testing"
)

func TestProduction138ProviderUsesFrozenSubjectTeamAndActualActor(t *testing.T) {
	center := newModelOwnerTestCenter()
	center.identities["founder"] = accountcenter.Identity{Username: "founder", Role: accountcenter.RoleFounder, Active: true}
	owners := []string{}
	calls := 0
	resolver := managedTextProviderResolver(func(ctx context.Context, configOwner, model string) (batchfactoryv11.DirectorProvider, error) {
		owners = append(owners, configOwner)
		return screenplayProviderFunc(func(c context.Context, _ batchfactoryv11.TextCompletionRequest) (string, error) {
			calls++
			team, actor := yzcmusage.AttributionForOwner(c, "member")
			if team != "model-team" || actor != "founder" || configOwner != "manager" {
				t.Fatalf("wrong provider/billing %s %s %s", configOwner, team, actor)
			}
			return "fixture", nil
		}), nil
	}, func(subject string) string {
		owner, _ := ResolveAPIConfigOwner(context.Background(), center, subject)
		return owner
	})
	g := screenplayProductionTextGenerator{accounts: center, generator: screenplayTextGenerator{owner: "member", resolver: resolver}}
	ctx := screenplay.WithProductionAuthorization(context.Background(), "member", "model-team", "founder")
	ctx = yzcmusage.WithAttribution(ctx, "member", "model-team", "founder")
	if _, err := g.Generate(ctx, screenplay.TextRequest{Model: "chosen", SingleAttempt: true}); err != nil {
		t.Fatal(err)
	}
	member := center.identities["member"]
	member.TeamID = "different-team"
	center.identities["member"] = member
	if _, err := g.Generate(ctx, screenplay.TextRequest{Model: "chosen"}); err == nil {
		t.Fatal("changed team dispatched")
	}
	if calls != 1 || len(owners) != 1 {
		t.Fatal("rejected identity resolved provider", owners, calls)
	}
}
