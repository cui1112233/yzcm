package storage

import (
	"strings"
	"testing"
)

func TestNovelFetchWorkshopSchemaIsOwnerScopedAndJSONBacked(t *testing.T) {
	joined := strings.Join(NovelFetchWorkshopStatements(), "\n")
	for _, expected := range []string{
		"novel_fetch_workshop_documents",
		"PRIMARY KEY (owner_username, book_id)",
		"document_json JSON NOT NULL",
		"novel_fetch_workshop_configs",
		"settings_json JSON NOT NULL",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("schema missing %q", expected)
		}
	}
	migrations := NovelFetchWorkshopMigrations()
	wantVersions := []int{1200001, 1200002, 1200003, 1200004, 1200005}
	if len(migrations) != len(wantVersions) {
		t.Fatalf("unexpected migrations: %#v", migrations)
	}
	for index, want := range wantVersions {
		if migrations[index].Version != want {
			t.Fatalf("migration %d version = %d, want %d", index, migrations[index].Version, want)
		}
	}
	joinedRuns := strings.Join(NovelFetchWorkshopRunAuditStatements(), "\n")
	for _, expected := range []string{"novel_fetch_workshop_runs", "text_model_id", "model_id", "model_display_name", "idx_nfw_runs_owner_book_updated"} {
		if !strings.Contains(joinedRuns, expected) {
			t.Fatalf("run audit schema missing %q", expected)
		}
	}
}

func TestBodyObjectRetirementMigrationIsDurableAndBounded(t *testing.T) {
	migrations := NovelFetchWorkshopMigrations()
	if len(migrations) < 5 {
		t.Fatal("missing body retirement migration")
	}
	sql := strings.Join(migrations[4].SQL, "\n")
	for _, want := range []string{"CREATE TABLE IF NOT EXISTS novel_fetch_body_object_retirements", "PRIMARY KEY (object_key)", "owner_username", "book_id", "version_id", "registered_at", "not_before", "state", "attempts", "last_error", "idx_nfw_retirements_due"} {
		if !strings.Contains(sql, want) {
			t.Fatalf("retirement schema missing %s", want)
		}
	}
}
