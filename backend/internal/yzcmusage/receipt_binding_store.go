package yzcmusage

import (
	"context"
	"database/sql"
	"errors"
)

const receiptBindingSlot = "member-system"

const (
	receiptBindingPending = "pending"
	receiptBindingActive  = "active"
	receiptBindingNew     = "new"
	receiptBindingUpgrade = "upgrade"
	receiptBindingAdopt   = "adopt"
	receiptBindingDBOnly  = "db-only"
)

type receiptBindingRow struct {
	PathDigest, StoreID, State, Origin string
}

// ReceiptBindingStatement is registered as a separate app migration. It must
// not change the original usage ledger migration's checksum.
func ReceiptBindingStatement() string {
	return `CREATE TABLE IF NOT EXISTS yzcm_usage_receipt_bindings (
 binding_slot VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 path_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 store_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 state ENUM('pending','active') NOT NULL,
 origin ENUM('new','upgrade','adopt','db-only') NOT NULL,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 activated_at DATETIME(6) NULL
 ) ENGINE=InnoDB`
}

func (s Store) loadReceiptBinding(ctx context.Context) (receiptBindingRow, bool, error) {
	var row receiptBindingRow
	if s.DB == nil {
		return row, false, errReceiptDatabaseUnavailable
	}
	err := s.DB.QueryRowContext(ctx, `SELECT path_digest,store_id,state,origin FROM yzcm_usage_receipt_bindings WHERE binding_slot=?`, receiptBindingSlot).
		Scan(&row.PathDigest, &row.StoreID, &row.State, &row.Origin)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	return row, err == nil, err
}

func (s Store) reserveReceiptBinding(ctx context.Context, proposed receiptBindingRow) (receiptBindingRow, error) {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO yzcm_usage_receipt_bindings (binding_slot,path_digest,store_id,state,origin,activated_at)
VALUES (?,?,?,?,?,IF(?='active',CURRENT_TIMESTAMP(6),NULL)) ON DUPLICATE KEY UPDATE binding_slot=yzcm_usage_receipt_bindings.binding_slot`,
		receiptBindingSlot, proposed.PathDigest, proposed.StoreID, proposed.State, proposed.Origin, proposed.State)
	if err != nil {
		return receiptBindingRow{}, err
	}
	row, found, err := s.loadReceiptBinding(ctx)
	if err == nil && !found {
		err = errReceiptDirectoryChanged
	}
	return row, err
}

func (s Store) finishReceiptBinding(ctx context.Context, row receiptBindingRow, prepare func(receiptBindingRow) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Re-read the current state under the row lock before touching the files.
	// A stale pending reader must never recreate a now-active directory.
	var current receiptBindingRow
	err = tx.QueryRowContext(ctx, `SELECT path_digest,store_id,state,origin FROM yzcm_usage_receipt_bindings WHERE binding_slot=? FOR UPDATE`, receiptBindingSlot).
		Scan(&current.PathDigest, &current.StoreID, &current.State, &current.Origin)
	if err != nil {
		return err
	}
	if current.PathDigest != row.PathDigest || current.StoreID != row.StoreID || current.Origin != row.Origin {
		return errReceiptDirectoryChanged
	}
	switch current.State {
	case receiptBindingPending:
		if err := prepare(current); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE yzcm_usage_receipt_bindings SET state='active',activated_at=CURRENT_TIMESTAMP(6)
WHERE binding_slot=? AND state='pending'`, receiptBindingSlot); err != nil {
			return err
		}
	case receiptBindingActive:
	default:
		return errReceiptDirectoryChanged
	}
	return tx.Commit()
}
