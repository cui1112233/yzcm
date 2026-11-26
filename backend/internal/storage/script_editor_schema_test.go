package storage

import (
	"strings"
	"testing"
)

func TestScriptEditorMigrationContainsRequiredTables(t *testing.T) {
	migrations := ScriptEditorMigrations()
	if len(migrations) != 1 {
		t.Fatalf("migration count = %d, want 1", len(migrations))
	}
	if migrations[0].Version != 8800410 {
		t.Fatalf("version = %d, want 8800410", migrations[0].Version)
	}
	sql := strings.Join(migrations[0].SQL, "\n")
	for _, name := range []string{
		"script_editor_works",
		"script_editor_chapters",
		"script_editor_scripts",
		"script_editor_reviews",
		"script_editor_jobs",
		"script_editor_handoffs",
	} {
		if !strings.Contains(sql, name) {
			t.Fatalf("migration SQL missing %s", name)
		}
	}
}

func TestAppMigrationsIncludesScriptEditor(t *testing.T) {
	for _, migration := range AppMigrations() {
		if migration.Version == 8800410 {
			return
		}
	}
	t.Fatal("script editor migration missing from AppMigrations")
}
