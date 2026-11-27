package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func screenplayAssistantSessionUpgrade() Migration {
	return Migration{Version: 8800122, CallbackChecksum: "screenplay-assistant-request-history-deadline-virtual-parent-v1", Adopt: func(ctx context.Context, tx *sql.Tx) (bool, error) {
		var kind, nullable string
		err := tx.QueryRowContext(ctx, "SELECT column_type,is_nullable FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name='screenplay_assistant_sessions' AND column_name='request_deadline'").Scan(&kind, &nullable)
		addDeadline := errors.Is(err, sql.ErrNoRows)
		if err != nil && !addDeadline {
			return false, err
		}
		if !addDeadline && (kind != "datetime(6)" || nullable != "YES") {
			return false, fmt.Errorf("incompatible assistant session deadline %s nullable %s", kind, nullable)
		}
		var fk int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.table_constraints WHERE constraint_schema=DATABASE() AND table_name='screenplay_assistant_sessions' AND constraint_name='fk_assistant_session_parent' AND constraint_type='FOREIGN KEY'").Scan(&fk); err != nil {
			return false, err
		}
		if addDeadline {
			if _, err = tx.ExecContext(ctx, "ALTER TABLE screenplay_assistant_sessions ADD COLUMN request_deadline DATETIME(6) NULL"); err != nil {
				return false, err
			}
		}
		if fk != 0 {
			if _, err = tx.ExecContext(ctx, "ALTER TABLE screenplay_assistant_sessions DROP FOREIGN KEY fk_assistant_session_parent"); err != nil {
				return false, err
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE screenplay_assistant_sessions SET request_status='unknown',revision=revision+1 WHERE request_status='running' AND request_deadline IS NULL"); err != nil {
			return false, err
		}
		_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS screenplay_assistant_request_history (
    owner_username VARCHAR(191) NOT NULL,
    project_id VARCHAR(64) NOT NULL,
    session_id VARCHAR(64) NOT NULL,
    request_token VARCHAR(64) NOT NULL,
    actor VARCHAR(191) NOT NULL,
    outcome ENUM('unknown','completed') NOT NULL,
    release_reason VARCHAR(32) NOT NULL,
    provider_receipt VARCHAR(191) NULL,
    recorded_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY(owner_username, project_id, session_id, request_token)
   ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin`)
		return err == nil, err
	}}
}
