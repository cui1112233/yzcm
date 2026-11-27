package httpapi

import (
	"context"
	"errors"
	"qiantie/backend/internal/novelfetchworkshop"
	"testing"
)

type preferenceCatalogReadProbe struct {
	*novelfetchworkshop.MemoryStore
	strictCalls int
	owner       string
	readErr     error
}

func (s *preferenceCatalogReadProbe) GetConfig(context.Context, string) (map[string]any, error) {
	s.strictCalls++
	return nil, errors.New("legacy cleanup pending")
}
func (s *preferenceCatalogReadProbe) ReadRuntimeConfig(_ context.Context, owner string) (map[string]any, error) {
	s.owner = owner
	if s.readErr != nil {
		return nil, s.readErr
	}
	return map[string]any{"modelCatalogVersion": 7, "modelCatalog": []any{map[string]any{"id": "text", "kind": "text", "enabled": true, "credentialStatus": "needs_reentry"}, map[string]any{"id": "video", "kind": "video", "enabled": false}}}, nil
}

func TestUserConfigCatalogReadDoesNotRunLegacyCleanup(t *testing.T) {
	s := &preferenceCatalogReadProbe{MemoryStore: novelfetchworkshop.NewMemoryStore()}
	config := map[string]any{"language": "zh-CN", "apiConfigAccess": map[string]any{"canManageModels": true}}
	if err := mergeManagedModelCatalog(context.Background(), config, s, "founder"); err != nil {
		t.Fatal(err)
	}
	rows := modelCatalog(config)
	if s.strictCalls != 0 || s.owner != "founder" || len(rows) != 2 || rows[0]["credentialStatus"] != "needs_reentry" || rows[1]["enabled"] != false {
		t.Fatal("formal catalog disappeared or pending model protection changed")
	}
	if config["language"] != "zh-CN" || mapOrEmpty(config["apiConfigAccess"])["canManageModels"] != true {
		t.Fatal("catalog read changed preferences or role information")
	}
}

func TestUserConfigCatalogReadKeepsStorageError(t *testing.T) {
	cause := errors.New("runtime catalog database unavailable")
	s := &preferenceCatalogReadProbe{MemoryStore: novelfetchworkshop.NewMemoryStore(), readErr: cause}
	config := map[string]any{"language": "zh-CN"}
	if err := mergeManagedModelCatalog(context.Background(), config, s, "founder"); !errors.Is(err, cause) {
		t.Fatalf("storage error hidden or replaced: %v", err)
	}
	if s.strictCalls != 0 || config["modelCatalog"] != nil {
		t.Fatal("failed read invoked cleanup or fabricated empty catalog")
	}
}
