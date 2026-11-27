package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/novelfetchworkshop"
)

type heldLegacyRuntimeStore struct {
	*novelfetchworkshop.MemoryStore
	strictReads                   int
	runtimeOwners, workflowOwners []string
}

func (s *heldLegacyRuntimeStore) GetConfig(context.Context, string) (map[string]any, error) {
	s.strictReads++
	return nil, errors.New("legacy credentials still require verified recovery")
}
func (s *heldLegacyRuntimeStore) GetWorkflowConfig(_ context.Context, owner string) (map[string]any, error) {
	s.workflowOwners = append(s.workflowOwners, owner)
	return map[string]any{"prefsOwner": owner}, nil
}
func (s *heldLegacyRuntimeStore) ReadRuntimeConfig(_ context.Context, owner string) (map[string]any, error) {
	s.runtimeOwners = append(s.runtimeOwners, owner)
	return map[string]any{"prefsOwner": owner, "modelCatalog": []any{map[string]any{"id": owner + "-text", "kind": "text", "enabled": true}}}, nil
}

func TestRuntimeCatalogReadDoesNotFinalizeHeldLegacyCredentials(t *testing.T) {
	for _, subject := range []string{"manager", "member"} {
		s := &heldLegacyRuntimeStore{MemoryStore: novelfetchworkshop.NewMemoryStore()}
		wrapped := runtimeModelCatalogStore(s, func(string) string { return "manager" })
		config, err := wrapped.GetConfig(context.Background(), subject)
		if err != nil || config["prefsOwner"] != subject {
			t.Fatalf("workflow blocked by legacy cleanup: %v", err)
		}
		rows := modelCatalog(config)
		if len(rows) != 1 || rows[0]["id"] != "manager-text" || s.strictReads != 0 || len(s.runtimeOwners) != 1 || s.runtimeOwners[0] != "manager" {
			t.Fatal("runtime read bypassed formal owner or invoked strict cleanup")
		}
	}
}

type runtimeCatalogAccounts struct{ accountcenter.Store }

func (*runtimeCatalogAccounts) ResolveIdentity(_ context.Context, owner string) (accountcenter.Identity, error) {
	if owner == "member" {
		return accountcenter.Identity{Username: owner, Role: accountcenter.RoleMember, TeamID: "team", Active: true}, nil
	}
	return accountcenter.Identity{Username: owner, Role: accountcenter.RoleManager, Active: true}, nil
}
func (*runtimeCatalogAccounts) ListTeams(context.Context, accountcenter.TeamFilter) ([]accountcenter.TeamSummary, error) {
	return []accountcenter.TeamSummary{{Team: accountcenter.Team{ID: "team", ManagerUsername: "manager"}}}, nil
}

func TestFormalCompatibilityCatalogUsesReadOnlyResolvedOwner(t *testing.T) {
	s := &heldLegacyRuntimeStore{MemoryStore: novelfetchworkshop.NewMemoryStore()}
	request := httptest.NewRequest("GET", "/config", nil)
	request = request.WithContext(context.WithValue(request.Context(), apiConfigCompatKey{}, BridgeAuth{AccountCenterStore: &runtimeCatalogAccounts{}}))
	rows := formalCompatibilityCatalog(request, s, "member", map[string]any{"modelCatalog": []any{map[string]any{"id": "personal"}}})
	models, ok := rows.([]any)
	if !ok || len(models) != 1 || models[0].(map[string]any)["id"] != "manager-text" || s.strictReads != 0 {
		t.Fatal("compatibility catalog invoked cleanup or returned personal keys")
	}
}

type runtimeCatalogSecrets struct {
	novelfetchworkshop.ModelConfigSecrets
}

func (*runtimeCatalogSecrets) OpenModelConfig(string, map[string]any) error { return nil }

func TestAgentVideoResolverCannotUseHeldRuntimeVideoOrFallback(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := novelfetchworkshop.NewMySQLStore(db)
	store.ModelSecrets = &runtimeCatalogSecrets{}
	raw := `{"modelCatalogSecretVersion":"bound-v3","modelCatalog":[{"id":"held-video","kind":"video","modelId":"held-video-model","credential":"test-video","enabled":true},{"id":"text","kind":"text","credential":"test-text","enabled":true}]}`
	resolver := agentConfiguredModelResolver(runtimeModelCatalogStore(store, func(owner string) string { return owner }))
	for _, id := range []string{"held-video", "held-video-model", ""} {
		mock.ExpectQuery("SELECT c.settings_json,EXISTS").WithArgs("manager", "manager").WillReturnRows(sqlmock.NewRows([]string{"settings_json", "pending"}).AddRow(raw, true))
		model, err := resolver(context.Background(), "manager", id, "video")
		if err == nil || model != nil {
			t.Fatalf("held video resolved or fell back: id=%q", id)
		}
	}
	mock.ExpectQuery("SELECT c.settings_json,EXISTS").WithArgs("manager", "manager").WillReturnRows(sqlmock.NewRows([]string{"settings_json", "pending"}).AddRow(raw, true))
	if model, err := resolver(context.Background(), "manager", "text", "text"); err != nil || model["id"] != "text" {
		t.Fatal("verified text blocked with pending video", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
