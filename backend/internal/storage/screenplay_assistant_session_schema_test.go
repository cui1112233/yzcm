package storage

import (
	"strings"
	"testing"
)

func TestAssistantSessionMigration(t *testing.T) {
	var registered int
	for _, m := range AppMigrations() {
		if m.Version != 8800121 {
			continue
		}
		registered++
		if len(m.SQL) != 1 || !strings.HasPrefix(m.SQL[0], "CREATE TABLE IF NOT EXISTS screenplay_assistant_sessions (") {
			t.Fatal("expected one additive session table")
		}
	}
	if registered != 1 {
		t.Fatalf("expected one migration, got %d", registered)
	}
}
