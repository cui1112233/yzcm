package storage

func BatchMultiModeMigrations() []Migration {
	return []Migration{{Version: 8800502, SQL: []string{
		`CREATE TABLE IF NOT EXISTS batch_factory_v12_creation_groups (id VARCHAR(64) PRIMARY KEY,owner_username VARCHAR(191) NOT NULL,request_id VARCHAR(128) NOT NULL,input_digest CHAR(64) NOT NULL,created_at DATETIME(6) NOT NULL,UNIQUE KEY uq_multimode_request(owner_username,request_id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS batch_factory_v12_creation_projects (group_id VARCHAR(64) NOT NULL,owner_username VARCHAR(191) NOT NULL,mode VARCHAR(32) NOT NULL,batch_id VARCHAR(64) NOT NULL,preset_id VARCHAR(191) NOT NULL DEFAULT '',execution_mode VARCHAR(32) NOT NULL,ordinal INT NOT NULL,PRIMARY KEY(group_id,mode),UNIQUE KEY uq_multimode_batch(batch_id)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS batch_factory_v12_creation_sources (group_id VARCHAR(64) NOT NULL,source_key CHAR(64) NOT NULL,input_json JSON NOT NULL,snapshot_json JSON NULL,status VARCHAR(32) NOT NULL DEFAULT 'pending',lease_token VARCHAR(64) NOT NULL DEFAULT '',lease_until DATETIME(6) NULL,last_error TEXT NULL,PRIMARY KEY(group_id,source_key)) ENGINE=InnoDB`,
		`CREATE TABLE IF NOT EXISTS batch_factory_v12_creation_targets (group_id VARCHAR(64) NOT NULL,source_key CHAR(64) NOT NULL,batch_id VARCHAR(64) NOT NULL,book_id VARCHAR(64) NOT NULL,expected_revision BIGINT NOT NULL,status VARCHAR(32) NOT NULL DEFAULT 'pending',PRIMARY KEY(group_id,batch_id,book_id),KEY idx_multimode_source(group_id,source_key,status)) ENGINE=InnoDB`,
	}}}
}
