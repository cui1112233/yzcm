package storage

func screenplaySessionAttemptsMigration() Migration {
	return Migration{Version: 8800123, SQL: []string{`CREATE TABLE IF NOT EXISTS screenplay_session_usage_attempts (
 attempt_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 project_owner VARCHAR(191) NOT NULL, project_id VARCHAR(64) NOT NULL, session_id VARCHAR(64) NOT NULL,
 request_token VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 stage ENUM('initial','read_reply') NOT NULL,
 subject VARCHAR(32) NOT NULL, team_id VARCHAR(64) NOT NULL, actor VARCHAR(32) NOT NULL,
 credential_hash CHAR(64) CHARACTER SET ascii NOT NULL,
 state ENUM('reserved','unknown','captured') NOT NULL DEFAULT 'reserved',
 provider_id VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 UNIQUE KEY session_attempt_stage(request_token,stage),
 UNIQUE KEY session_attempt_provider(provider_id)
 ) ENGINE=InnoDB`}}
}
