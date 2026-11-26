package prompt

import (
	"strings"
	"testing"
)

func TestScriptEditorPromptBodiesStayOnServer(t *testing.T) {
	for _, kind := range []string{"generate", "review", "polish", "viral-opening"} {
		body, err := ScriptEditorPrompt(kind)
		if err != nil || !strings.Contains(body, "剧本") {
			t.Fatalf("kind=%s body=%q err=%v", kind, body, err)
		}
	}
	if _, err := ScriptEditorPrompt("unknown"); err == nil {
		t.Fatal("unknown prompt must fail")
	}
}
