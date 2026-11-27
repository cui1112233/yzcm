package yzcmusage

import (
	"context"
	"database/sql"
	"errors"
	"time"
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
	FilesystemIdentity                 string
	SyncParentLevels                   int
}

const receiptBindingColumns = "path_digest,store_id,state,origin,COALESCE(filesystem_identity,''),sync_parent_levels"

var errReceiptLegacyBindingUnverified = errors.New("legacy usage receipt binding cannot be verified; audited recovery is required")

// ReceiptBindingStatement creates the current shape for standalone ledger tests.
// Application migrations freeze their own DDL and checksums.
func ReceiptBindingStatement() string {
	return `CREATE TABLE IF NOT EXISTS yzcm_usage_receipt_bindings (
 binding_slot VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
 path_digest CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 store_id CHAR(32) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
 state ENUM('pending','active') NOT NULL,
 origin ENUM('new','upgrade','adopt','db-only') NOT NULL,
 filesystem_identity VARCHAR(224) CHARACTER SET ascii COLLATE ascii_bin NULL,
 sync_parent_levels SMALLINT UNSIGNED NOT NULL DEFAULT 1,
 created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
 activated_at DATETIME(6) NULL
 ) ENGINE=InnoDB`
}

func (s Store) loadReceiptBinding(ctx context.Context) (receiptBindingRow, bool, error) {
	var row receiptBindingRow
	if s.DB == nil {
		return row, false, errReceiptDatabaseUnavailable
	}
	err := s.DB.QueryRowContext(ctx, `SELECT `+receiptBindingColumns+` FROM yzcm_usage_receipt_bindings WHERE binding_slot=?`, receiptBindingSlot).
		Scan(&row.PathDigest, &row.StoreID, &row.State, &row.Origin, &row.FilesystemIdentity, &row.SyncParentLevels)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	return row, err == nil, err
}

func (s Store) reserveReceiptBinding(ctx context.Context, proposed receiptBindingRow) (receiptBindingRow, error) {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO yzcm_usage_receipt_bindings (binding_slot,path_digest,store_id,state,origin,sync_parent_levels,activated_at)
VALUES (?,?,?,?,?,?,IF(?='active',CURRENT_TIMESTAMP(6),NULL)) ON DUPLICATE KEY UPDATE binding_slot=yzcm_usage_receipt_bindings.binding_slot`,
		receiptBindingSlot, proposed.PathDigest, proposed.StoreID, proposed.State, proposed.Origin, proposed.SyncParentLevels, proposed.State)
	if err != nil {
		return receiptBindingRow{}, err
	}
	row, found, err := s.loadReceiptBinding(ctx)
	if err == nil && !found {
		err = errReceiptDirectoryChanged
	}
	return row, err
}

func (s Store) finishReceiptBinding(ctx context.Context, row receiptBindingRow, prepare func(receiptBindingRow) (string, error)) (string, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	// Re-read the current state under the row lock before touching the files.
	// A stale pending reader must never recreate a now-active directory.
	var current receiptBindingRow
	err = tx.QueryRowContext(ctx, `SELECT `+receiptBindingColumns+` FROM yzcm_usage_receipt_bindings WHERE binding_slot=? FOR UPDATE`, receiptBindingSlot).
		Scan(&current.PathDigest, &current.StoreID, &current.State, &current.Origin, &current.FilesystemIdentity, &current.SyncParentLevels)
	if err != nil {
		return "", err
	}
	if current.PathDigest != row.PathDigest || current.StoreID != row.StoreID || current.Origin != row.Origin || current.SyncParentLevels != row.SyncParentLevels {
		return "", errReceiptDirectoryChanged
	}
	switch current.State {
	case receiptBindingPending:
		if current.FilesystemIdentity != "" {
			return "", errReceiptDirectoryChanged
		}
		current.FilesystemIdentity, err = prepare(current)
		if err != nil {
			return "", err
		}
		if current.FilesystemIdentity == "" {
			return "", errReceiptStableIdentityUnavailable
		}
		if _, err := tx.ExecContext(ctx, `UPDATE yzcm_usage_receipt_bindings SET filesystem_identity=?,state='active',activated_at=CURRENT_TIMESTAMP(6)
WHERE binding_slot=? AND state='pending'`, current.FilesystemIdentity, receiptBindingSlot); err != nil {
			return "", err
		}
	case receiptBindingActive:
	default:
		return "", errReceiptDirectoryChanged
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return current.FilesystemIdentity, nil
}

func (s Store) upgradeReceiptBinding(ctx context.Context, row receiptBindingRow, legacy, fingerprint string, objects receiptFilesystemObjects) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current receiptBindingRow
	var activated, databaseNow sql.NullInt64
	localBefore := time.Now()
	err = tx.QueryRowContext(ctx, `SELECT `+receiptBindingColumns+`,
CAST(UNIX_TIMESTAMP(activated_at)*1000000 AS SIGNED), CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(6))*1000000 AS SIGNED)
FROM yzcm_usage_receipt_bindings WHERE binding_slot=? FOR UPDATE`, receiptBindingSlot).
		Scan(&current.PathDigest, &current.StoreID, &current.State, &current.Origin, &current.FilesystemIdentity, &current.SyncParentLevels, &activated, &databaseNow)
	localAfter := time.Now()
	if err != nil {
		return err
	}
	if current.State != receiptBindingActive || current.PathDigest != row.PathDigest || current.StoreID != row.StoreID ||
		current.Origin != row.Origin || current.SyncParentLevels != row.SyncParentLevels {
		return errReceiptDirectoryChanged
	}
	if current.FilesystemIdentity == fingerprint {
		// Another process already upgraded this exact set of objects.
		return tx.Commit()
	}
	if legacy == "" || current.FilesystemIdentity != legacy {
		return errReceiptDirectoryChanged
	}
	// The database clock sample must be inside the local round trip. Even then,
	// a positive database clock offset up to the observed bound is possible.
	if !activated.Valid || !databaseNow.Valid || activated.Int64 <= 0 || databaseNow.Int64 < activated.Int64 ||
		databaseNow.Int64 < localBefore.UnixMicro() || databaseNow.Int64 > localAfter.UnixMicro() {
		return errReceiptLegacyBindingUnverified
	}
	localActivationLowerBound := activated.Int64 - (databaseNow.Int64 - localBefore.UnixMicro())
	for _, id := range []receiptObjectID{objects.parent, objects.directory, objects.marker, objects.anchor} {
		// Creation can occur anywhere in the reported filesystem clock tick.
		// Compare its latest plausible time at the precision of DATETIME(6).
		if id.birth.IsZero() || id.birthUncertainty <= 0 || id.birth.Add(id.birthUncertainty).UnixMicro() >= localActivationLowerBound {
			return errReceiptLegacyBindingUnverified
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE yzcm_usage_receipt_bindings SET filesystem_identity=?
WHERE binding_slot=? AND state='active' AND filesystem_identity=?`, fingerprint, receiptBindingSlot, legacy)
	if err != nil {
		return err
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		return errors.Join(errReceiptDirectoryChanged, err)
	}
	return tx.Commit()
}
