package httpapi

import (
	"context"
	"net/http"
	"qiantie/backend/internal/screenplay"
	"testing"
	"time"
)

func TestProduction138StartFreezesTrustedIdentityAndRejectsBodyIdentity(t *testing.T) {
	now := time.Date(2026, 11, 28, 0, 0, 0, 0, time.UTC)
	store := screenplay.NewMemoryStore()
	p, err := store.CreateProject(context.Background(), screenplay.Project{ID: "p138", OwnerUsername: "alice", PermissionMode: screenplay.PermissionBypass})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.SaveCreativeConfig(context.Background(), "alice", p.ID, screenplay.CreativeConfig{StoryboardPromptID: "luoshui_storyboard_138"}, p.Revision); err != nil {
		t.Fatal(err)
	}
	runtime := &screenplay.ProductionRuntime{Store: store, Repository: screenplay.NewMemoryProductionRepository(), RedisDo: func(context.Context, ...string) (any, error) { return "OK", nil }, Executor: func(string) screenplay.BatchExecutor {
		t.Fatal("start executed provider")
		return screenplay.BatchExecutor{}
	}}
	mux := http.NewServeMux()
	RegisterScreenplayProductionRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, runtime)
	path := "/api/comic-screenplays/projects/p138/production/start"
	bad := signedJSONRequest(t, mux, now, "alice", "POST", path, map[string]any{"rebuild": false, "actor": "forged"})
	if bad.Code != http.StatusBadRequest {
		t.Fatal("forged body accepted", bad.Code, bad.Body.String())
	}
	good := signedJSONRequest(t, mux, now, "alice", "POST", path, map[string]any{"rebuild": false})
	if good.Code != http.StatusOK {
		t.Fatal(good.Code, good.Body.String())
	}
	b, err := store.GetBatch(context.Background(), "alice", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	a := b.ProductionAuthorization
	if a == nil || a.Subject != "alice" || a.Actor != "alice" || a.RunID == "" {
		t.Fatal("trusted actor not frozen", a)
	}
	forged := *a
	forged.Actor = "forged"
	b.ProductionAuthorization = &forged
	if err = store.UpdateBatch(context.Background(), b, b.Revision); err != nil {
		t.Fatal(err)
	}
	b, _ = store.GetBatch(context.Background(), "alice", p.ID)
	if b.ProductionAuthorization.Actor != "alice" {
		t.Fatal("old update overwrote frozen actor")
	}
}
