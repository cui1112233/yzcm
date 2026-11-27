package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"qiantie/backend/internal/prompt"
	"testing"
)

func TestComicStylesHydrateUsesCatalogAndRejectsInvalidSelections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")
	fixture := `[{"id":"batch-comic-style-01","module":"batch-factory","name":"A","kind":"base","version":1,"revision":1,"status":"published","body":"style A from catalog","protocolLock":{"format":"style","slot":"batch.comic-style"}},{"id":"batch-comic-style-02","module":"batch-factory","name":"B","kind":"base","version":1,"revision":1,"status":"published","body":"style B from catalog","protocolLock":{"format":"style","slot":"batch.comic-style"}}]`
	if err := os.WriteFile(path, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	catalog := prompt.NewCatalog(path)
	raw := json.RawMessage(`{"comic":{"enabled":true,"styles":[{"presetId":"batch-comic-style-01","body":"untrusted"},{"presetId":"batch-comic-style-02","body":"untrusted"}]}}`)
	got, err := hydrateBatchFactoryPromptConfig(catalog, raw)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err = json.Unmarshal(got, &config); err != nil {
		t.Fatal(err)
	}
	selected := config["comic"].(map[string]any)["styles"].([]any)
	for _, v := range selected {
		s := v.(map[string]any)
		if s["body"] == "untrusted" || s["body"] == "" || s["presetSlot"] != "batch.comic-style" {
			t.Fatal("did not hydrate style from system catalog")
		}
	}
	for _, bad := range []string{
		`{"comic":{"styles":[{"presetId":"unknown-style"}]}}`,
		`{"comic":{"styles":[{"presetId":"batch-comic-style-01"},{"presetId":"batch-comic-style-01"}]}}`,
		`{"comic":{"styles":[{}]}}`,
		`{"comic":{"styles":"wrong-type"}}`,
	} {
		if _, err := hydrateBatchFactoryPromptConfig(catalog, json.RawMessage(bad)); err == nil {
			t.Fatalf("accepted bad styles: %s", bad)
		}
	}
}
