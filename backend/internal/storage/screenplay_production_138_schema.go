package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const screenplayProduction138ColumnQuery = `SELECT table_name,column_name,data_type,is_nullable,column_default FROM information_schema.columns WHERE table_schema=DATABASE() AND ((table_name='screenplay_batches' AND column_name='production_authorization') OR (table_name='screenplay_batch_episodes' AND column_name='storyboard_generation'))`

var screenplayProduction138Columns = []struct{ table, column string }{
	{"screenplay_batches", "production_authorization"},
	{"screenplay_batch_episodes", "storyboard_generation"},
}

func screenplayProduction138Migration() Migration {
	m := Migration{Version: 8800124, CallbackChecksum: "screenplay-production-138-authorization-wait-v1", SQL: []string{
		`ALTER TABLE screenplay_batches ADD COLUMN production_authorization JSON NULL`,
		`ALTER TABLE screenplay_batch_episodes ADD COLUMN storyboard_generation JSON NULL`,
	}}
	m.Adopt = func(ctx context.Context, tx *sql.Tx) (bool, error) {
		existing, err := inspectScreenplayProduction138Columns(ctx, tx)
		if err != nil {
			return false, err
		}
		// Inspect both columns before changing either one. MySQL DDL commits even if
		// the process stops before the migration ledger is written.
		for i, target := range screenplayProduction138Columns {
			if !existing[target.table+"."+target.column] {
				if _, err = tx.ExecContext(ctx, m.SQL[i]); err != nil {
					return false, err
				}
			}
		}
		return true, verifyScreenplayProduction138Columns(ctx, tx)
	}
	m.Reconcile = verifyScreenplayProduction138Columns
	return m
}
func inspectScreenplayProduction138Columns(ctx context.Context, tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.QueryContext(ctx, screenplayProduction138ColumnQuery)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	existing := map[string]bool{}
	for rows.Next() {
		var table, column, kind, nullable string
		var defaultValue sql.NullString
		if err = rows.Scan(&table, &column, &kind, &nullable, &defaultValue); err != nil {
			return nil, err
		}
		if !strings.EqualFold(kind, "json") || nullable != "YES" || defaultValue.Valid {
			return nil, fmt.Errorf("unexpected production column %s.%s: requires JSON NULL without a default", table, column)
		}
		existing[table+"."+column] = true
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return existing, nil
}
func verifyScreenplayProduction138Columns(ctx context.Context, tx *sql.Tx) error {
	existing, err := inspectScreenplayProduction138Columns(ctx, tx)
	if err != nil {
		return err
	}
	for _, target := range screenplayProduction138Columns {
		if !existing[target.table+"."+target.column] {
			return fmt.Errorf("missing production column %s.%s", target.table, target.column)
		}
	}
	return nil
}
