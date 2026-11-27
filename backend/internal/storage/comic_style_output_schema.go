package storage

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

func ComicStyleOutputMigrations() []Migration {
	return []Migration{{Version: 8800501, CallbackChecksum: "comic-style-scoped-pages-assets-collections-v1", Adopt: func(ctx context.Context, tx *sql.Tx) (bool, error) {
		err := ensureComicStyleOutputSchema(ctx, tx)
		return err == nil, err
	}, Reconcile: ensureComicStyleOutputSchema}}
}

// All table names are fixed application schema identifiers. DDL is resumed by
// inspecting the actual columns/indexes after an interrupted MySQL migration.
func ensureComicStyleOutputSchema(ctx context.Context, tx *sql.Tx) error {
	for _, table := range []string{"batch_factory_v12_comic_pages", "batch_factory_v12_comic_collections", "batch_factory_v12_book_asset_images"} {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME='style_id'`, table).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if _, err := tx.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN style_id VARCHAR(191) NOT NULL DEFAULT ''"); err != nil {
				return err
			}
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT INDEX_NAME,GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX SEPARATOR ',') FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='batch_factory_v12_comic_pages' AND NON_UNIQUE=0 GROUP BY INDEX_NAME`)
	if err != nil {
		return err
	}
	indexes := map[string]string{}
	for rows.Next() {
		var name, columns string
		if err := rows.Scan(&name, &columns); err != nil {
			rows.Close()
			return err
		}
		indexes[name] = columns
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	hasScoped := false
	for name, columns := range indexes {
		if columns == "owner_username,batch_id,book_id,video_id,style_id" {
			hasScoped = true
		}
		if columns == "owner_username,batch_id,book_id,video_id" {
			if !regexp.MustCompile(`^[A-Za-z0-9_]+$`).MatchString(name) {
				return fmt.Errorf("unsafe comic index identifier")
			}
			// Replace the old unique key in one ALTER so no write can occur without a
			// uniqueness guard, even though DDL commits independently of sql.Tx.
			_, err = tx.ExecContext(ctx, "ALTER TABLE batch_factory_v12_comic_pages DROP INDEX "+name+", ADD UNIQUE KEY uq_bfv12_comic_page_style (owner_username,batch_id,book_id,video_id,style_id)")
			if err != nil {
				return err
			}
			hasScoped = true
		}
	}
	if !hasScoped {
		return fmt.Errorf("comic page uniqueness does not match the expected scope; preserve existing schema")
	}
	_, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS batch_factory_v12_comic_style_outputs (
 owner_username VARCHAR(191) NOT NULL,batch_id VARCHAR(64) NOT NULL,book_id VARCHAR(64) NOT NULL,style_id VARCHAR(191) NOT NULL,
 style_name VARCHAR(255) NOT NULL DEFAULT '',snapshot_json JSON NULL,status VARCHAR(32) NOT NULL DEFAULT 'pending',stage VARCHAR(32) NOT NULL DEFAULT '',
 retry_count INT NOT NULL DEFAULT 0,next_retry_at DATETIME(6) NULL,collection_id VARCHAR(64) NULL,is_legacy BOOLEAN NOT NULL DEFAULT FALSE,
 created_at DATETIME(6) NOT NULL,updated_at DATETIME(6) NOT NULL,
 PRIMARY KEY(owner_username,batch_id,book_id,style_id),KEY idx_comic_style_due(status,next_retry_at)
 ) ENGINE=InnoDB`)
	if err != nil {
		return err
	}
	for _, entry := range []struct{ table, index string }{{"batch_factory_v12_comic_collections", "idx_comic_collections_style"}, {"batch_factory_v12_book_asset_images", "idx_asset_images_style"}} {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=?`, entry.table, entry.index).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			columns := "owner_username,batch_id,book_id,style_id"
			if strings.Contains(entry.table, "asset_images") {
				columns = "owner_username,asset_id,style_id"
			}
			if _, err := tx.ExecContext(ctx, "ALTER TABLE "+entry.table+" ADD KEY "+entry.index+" ("+columns+")"); err != nil {
				return err
			}
		}
	}
	return nil
}
