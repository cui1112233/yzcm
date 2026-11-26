package storage

func AccountCenterMigrations() []Migration {
	return []Migration{
		{Version: 8800403, SQL: accountCenterStatements(), CallbackChecksum: "v88-account-center-foundation-v1"},
		{Version: 8800404, CallbackChecksum: "v88-account-center-legacy-import-v1", SQL: []string{
			`CREATE TABLE IF NOT EXISTS account_center_imports (
  import_key VARCHAR(64) NOT NULL PRIMARY KEY,
  completed_at DATETIME(6) NULL
) ENGINE=InnoDB`,
		}},
	}
}

func accountCenterStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS account_center_profiles (
  username VARCHAR(32) NOT NULL PRIMARY KEY,
  display_name VARCHAR(191) NOT NULL,
  avatar_url VARCHAR(1024) NOT NULL DEFAULT '',
  role ENUM('founder', 'developer', 'manager', 'member') NOT NULL,
  archived_at DATETIME(6) NULL,
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  founder_slot TINYINT GENERATED ALWAYS AS (CASE WHEN role = 'founder' THEN 1 END) STORED,
  UNIQUE KEY uq_account_center_founder (founder_slot)
) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS account_center_teams (
  id VARCHAR(64) NOT NULL PRIMARY KEY,
  name VARCHAR(191) NOT NULL,
  manager_username VARCHAR(32) NOT NULL,
  created_by VARCHAR(32) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  archived_at DATETIME(6) NULL,
  KEY idx_account_center_teams_manager (manager_username, archived_at)
) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS account_center_memberships (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  team_id VARCHAR(64) NOT NULL,
  username VARCHAR(32) NOT NULL,
  joined_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  ended_at DATETIME(6) NULL,
  active_username VARCHAR(32) GENERATED ALWAYS AS (CASE WHEN ended_at IS NULL THEN username END) STORED,
  UNIQUE KEY uq_account_center_active_membership (active_username),
  KEY idx_account_center_memberships_user (username, ended_at, team_id),
  KEY idx_account_center_memberships_team (team_id, ended_at, username),
  FOREIGN KEY (team_id) REFERENCES account_center_teams(id)
) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS account_center_grants (
  id VARCHAR(64) NOT NULL PRIMARY KEY,
  username VARCHAR(32) NOT NULL,
  capability VARCHAR(64) NOT NULL,
  scope VARCHAR(64) NOT NULL,
  team_id VARCHAR(64) NULL,
  granted_by VARCHAR(32) NOT NULL,
  granted_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  revoked_at DATETIME(6) NULL,
  KEY idx_account_center_grants_user (username, revoked_at, granted_at)
) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS account_center_audit_events (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  actor_username VARCHAR(32) NOT NULL,
  subject_username VARCHAR(32) NOT NULL,
  team_id VARCHAR(64) NULL,
  impersonation_id VARCHAR(64) NULL,
  action VARCHAR(96) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  KEY idx_account_center_audit_team_time (team_id, created_at, id),
  KEY idx_account_center_audit_time (created_at, id)
) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS account_center_impersonations (
  id VARCHAR(64) NOT NULL PRIMARY KEY,
  token_hash BINARY(32) NOT NULL,
  actor_username VARCHAR(32) NOT NULL,
  subject_username VARCHAR(32) NOT NULL,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  expires_at DATETIME(6) NOT NULL,
  revoked_at DATETIME(6) NULL,
  UNIQUE KEY uq_account_center_impersonation_token (token_hash),
  KEY idx_account_center_impersonation_actor (actor_username, expires_at)
) ENGINE=InnoDB`,
	}
}
