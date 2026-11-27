package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// MySQL DDL commits implicitly. An interrupted migration may therefore have
// applied some columns without reaching its ledger insert. Recover only after
// verifying the exact existing shape; never swallow a duplicate-column error.
func adoptDurationColumns(table string, statements []string) func(context.Context, *sql.Tx) (bool, error) {
	return func(ctx context.Context, tx *sql.Tx) (bool, error) {
		rows, err := tx.QueryContext(ctx, `SELECT column_name,column_type,is_nullable,column_default FROM information_schema.columns WHERE table_schema=DATABASE() AND table_name=? AND column_name IN ('target_duration_seconds','requested_duration_seconds','actual_duration_seconds')`, table)
		if err != nil {
			return false, err
		}
		existing := map[string]bool{}
		for rows.Next() {
			var name, kind, nullable string
			var defaultValue sql.NullString
			if err := rows.Scan(&name, &kind, &nullable, &defaultValue); err != nil {
				rows.Close()
				return false, err
			}
			value, parseErr := strconv.ParseFloat(defaultValue.String, 64)
			if !strings.EqualFold(kind, "decimal(10,3)") || nullable != "NO" || !defaultValue.Valid || parseErr != nil || value != 0 {
				rows.Close()
				return false, fmt.Errorf("existing duration column %s.%s does not match decimal(10,3) NOT NULL DEFAULT 0", table, name)
			}
			existing[strings.ToLower(name)] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return false, err
		}
		if err := rows.Close(); err != nil {
			return false, err
		}
		if len(existing) == 0 {
			return false, nil
		}
		for i, name := range []string{"target_duration_seconds", "requested_duration_seconds", "actual_duration_seconds"} {
			if existing[name] {
				continue
			}
			if _, err := tx.ExecContext(ctx, statements[i]); err != nil {
				return false, err
			}
		}
		return true, nil
	}
}
