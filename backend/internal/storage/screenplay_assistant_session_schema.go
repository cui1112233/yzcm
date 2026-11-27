package storage

func screenplayAssistantSessionMigration() Migration {
	return Migration{Version: 8800121, SQL: []string{`CREATE TABLE IF NOT EXISTS screenplay_assistant_sessions (
        owner_username VARCHAR(191) NOT NULL,
        project_id VARCHAR(64) NOT NULL,
        session_id VARCHAR(64) NOT NULL,
        parent_session_id VARCHAR(64) NULL,
        creator_username VARCHAR(191) NOT NULL,
        visibility ENUM('private','project') NOT NULL DEFAULT 'private',
        mode ENUM('confirm','readonly') NOT NULL DEFAULT 'confirm',
        title VARCHAR(191) NOT NULL,
        revision BIGINT UNSIGNED NOT NULL DEFAULT 1,
        archived_at DATETIME(6) NULL,
        request_token VARCHAR(64) NULL,
        request_deadline DATETIME(6) NULL,
        request_actor VARCHAR(191) NULL,
        request_status ENUM('running','unknown') NULL,
        body JSON NOT NULL,
        created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
        updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
        PRIMARY KEY(owner_username,project_id,session_id),
        KEY assistant_session_list(owner_username,project_id,archived_at,updated_at),
        KEY assistant_session_parent(owner_username,project_id,parent_session_id)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`}}
}
