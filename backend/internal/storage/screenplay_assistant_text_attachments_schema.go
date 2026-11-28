package storage

// Kept separate from the frozen production migrations.
func ScreenplayAssistantTextAttachmentMigrations() []Migration {
	return []Migration{{Version: 8800125, CallbackChecksum: "screenplay-assistant-private-text-attachments-v1", SQL: []string{
		`CREATE TABLE IF NOT EXISTS screenplay_assistant_text_attachments (
 owner_username VARCHAR(191) NOT NULL, project_id VARCHAR(96) NOT NULL, attachment_id VARCHAR(96) NOT NULL,
 session_id VARCHAR(96) NOT NULL, uploader VARCHAR(191) NOT NULL, actual_uploader VARCHAR(191) NOT NULL,
 request_key_sha256 CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 object_key VARCHAR(255) NOT NULL, namespace_id CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 content_version BIGINT NOT NULL, state_revision BIGINT NOT NULL DEFAULT 1,
 body JSON NOT NULL, status VARCHAR(24) NOT NULL,
 work_token VARCHAR(96) NOT NULL DEFAULT '', work_deadline DATETIME(6) NULL,
 verified_at DATETIME(6) NULL, first_published_at DATETIME(6) NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6), updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(owner_username,project_id,attachment_id),
 UNIQUE KEY uq_assistant_attachment_request(uploader,actual_uploader,request_key_sha256),
 KEY idx_assistant_attachment_session(owner_username,project_id,session_id,status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`,
		`CREATE TABLE IF NOT EXISTS screenplay_assistant_text_attachment_messages (
 owner_username VARCHAR(191) NOT NULL, project_id VARCHAR(96) NOT NULL, session_id VARCHAR(96) NOT NULL,
 operation_id VARCHAR(96) NOT NULL, attachment_id VARCHAR(96) NOT NULL,
 content_version BIGINT NOT NULL, snapshot_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 effective_username VARCHAR(191) NOT NULL, actual_actor VARCHAR(191) NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 PRIMARY KEY(owner_username,project_id,session_id,operation_id,attachment_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`}}}
}
