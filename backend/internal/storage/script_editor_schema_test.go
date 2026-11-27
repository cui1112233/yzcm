package storage

import (
	"strings"
	"testing"
)

func TestScriptEditorMigrationContainsRequiredTables(t *testing.T) {
	migrations := ScriptEditorMigrations()
	if len(migrations) != 2 {
		t.Fatalf("migration count = %d, want 2", len(migrations))
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

func TestScriptEditorStudioBindingMigration(t *testing.T) {
	migrations := ScriptEditorMigrations()
	latest := migrations[len(migrations)-1]
	if latest.Version != 8800412 {
		t.Fatalf("version = %d, want 8800412", latest.Version)
	}
	sql := strings.Join(latest.SQL, "\n")
	for _, table := range []string{"script_editor_work_bindings", "script_editor_source_links", "script_editor_asset_links", "script_editor_job_links"} {
		if !strings.Contains(sql, "CREATE TABLE IF NOT EXISTS "+table) {
			t.Fatalf("binding migration SQL missing %s", table)
		}
	}
	for _, column := range []string{"owner_username", "work_id", "source_revision", "sync_state", "conflict_state"} {
		if !strings.Contains(sql, column) {
			t.Fatalf("binding migration SQL missing %s", column)
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
