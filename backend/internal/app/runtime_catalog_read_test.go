package app

import (
	"context"
	"errors"
	"testing"

	"qiantie/backend/internal/novelfetchworkshop"
)

type heldAppRuntimeCatalog struct {
	*novelfetchworkshop.MemoryStore
	strictReads int
	owner       string
}

func (s *heldAppRuntimeCatalog) GetConfig(context.Context, string) (map[string]any, error) {
	s.strictReads++
	return nil, errors.New("legacy cleanup held")
}
func (s *heldAppRuntimeCatalog) ReadRuntimeConfig(_ context.Context, owner string) (map[string]any, error) {
	s.owner = owner
	return map[string]any{"modelCatalog": []any{map[string]any{"id": "text", "kind": "text", "modelId": "fixture-text", "baseUrl": "https://model.invalid/v1", "credential": "fixture-key", "enabled": true}}}, nil
}
func TestLoadSubjectModelConfigReadsVerifiedRuntimeWithoutCleanup(t *testing.T) {
	s := &heldAppRuntimeCatalog{MemoryStore: novelfetchworkshop.NewMemoryStore()}
	settings, err := loadSubjectModelConfig(context.Background(), &asrTeamFixture{manager: "manager"}, s, "member")
	if err != nil || s.strictReads != 0 || s.owner != "manager" {
		t.Fatalf("runtime resolver used legacy cleanup or wrong owner: %v", err)
	}
	provider, err := resolveNovelFetchTextProvider(settings, "text")
	if err != nil || provider == nil {
		t.Fatal("verified runtime text model unavailable", err)
	}
}
