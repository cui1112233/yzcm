package novelfetchworkshop

import "testing"

func TestLegacyModelKeysCheckStableIDAndScope(t *testing.T) {
	current := []any{
		map[string]any{"id": "a", "modelId": "same", "kind": "text", "baseUrl": "https://a.example.test/v1"},
		map[string]any{"id": "b", "modelId": "same", "kind": "text", "baseUrl": "https://b.example.test/v1"},
		map[string]any{"id": "c", "modelId": "same", "kind": "text", "baseUrl": "https://c.example.test/v1"},
	}
	legacy := []any{
		map[string]any{"id": "a", "modelId": "same", "kind": "text", "baseUrl": "https://a.example.test/v1", "credential": "fixture-a"},
		map[string]any{"id": "b", "modelId": "same", "kind": "text", "baseUrl": "https://b.example.test/v1", "credential": "fixture-b"},
		map[string]any{"id": "c", "modelId": "same", "kind": "text", "baseUrl": "https://a.example.test/v1", "credential": "fixture-c"},
	}
	mergeLegacyModelKeys(current, legacy)
	for i, want := range []string{"fixture-a", "fixture-b", ""} {
		model := current[i].(map[string]any)
		if ModelConfigText(model, "credential") != want || (want == "" && model["credentialStatus"] != "needs_reentry") {
			t.Fatalf("model %d got the wrong credential state", i)
		}
	}
}

func TestLegacyModelIDFallbackRequiresUniqueCandidate(t *testing.T) {
	model := map[string]any{"id": "current", "modelId": "active", "kind": "audio", "endpoint": "https://sound.example.test/v1"}
	first := map[string]any{"modelId": "active", "kind": "audio", "endpoint": "https://sound.example.test/v1", "credential": "fixture-first"}
	conflict := map[string]any{"modelId": "active", "kind": "audio", "endpoint": "https://sound.example.test/v1", "credential": "fixture-conflict"}
	wrongSource := map[string]any{"modelId": "active", "kind": "audio", "endpoint": "https://sound.example.test/v1", "provider": "other", "credential": "fixture-other"}
	if got := legacyModelCredentialCandidate(model, []any{wrongSource, first}); ModelConfigText(got, "credential") != "fixture-first" {
		t.Fatal("the only matching historical row was not selected")
	}
	if got := legacyModelCredentialCandidate(model, []any{first, conflict}); got != nil {
		t.Fatal("an ambiguous historical row was selected")
	}
}
