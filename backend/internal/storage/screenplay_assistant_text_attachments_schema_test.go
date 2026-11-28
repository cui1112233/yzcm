package storage

import (
	"strings"
	"testing"
)

func TestAssistantTextAttachmentMigration(t *testing.T) {
	m := ScreenplayAssistantTextAttachmentMigrations()
	if len(m) != 1 || m[0].Version != 8800125 || len(m[0].SQL) != 2 {
		t.Fatal(m)
	}
	for _, ddl := range m[0].SQL {
		if !strings.Contains(ddl, "DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin") {
			t.Fatal("scope identifiers need binary collation")
		}
	}
	joined := strings.Join(m[0].SQL, "\n")
	for _, s := range []string{"screenplay_assistant_text_attachments", "screenplay_assistant_text_attachment_messages", "request_key_sha256", "actual_uploader", "object_key", "namespace_id", "content_version", "state_revision", "operation_id", "UNIQUE KEY"} {
		if !strings.Contains(joined, s) {
			t.Fatal("missing", s)
		}
	}
	for _, s := range []string{"secret_access", "access_key_id", "request_key VARCHAR"} {
		if strings.Contains(joined, s) {
			t.Fatal("secret stored")
		}
	}
}
