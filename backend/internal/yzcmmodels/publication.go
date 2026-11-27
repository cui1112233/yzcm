package yzcmmodels

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"errors"
	"io"
	"slices"
	"strings"
)

// The digest covers every persisted model field and its provenance. Row order
// belongs to the adapter, so it does not change the identity of a snapshot.
func snapshotDigest(provenance string, models []Model) []byte {
	ordered := slices.Clone(models)
	slices.SortFunc(ordered, func(left, right Model) int {
		if order := strings.Compare(left.Kind, right.Kind); order != 0 {
			return order
		}
		return strings.Compare(left.ModelID, right.ModelID)
	})
	h := sha256.New()
	writeText := func(value string) {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(len(value)))
		_, _ = h.Write(size[:])
		_, _ = io.WriteString(h, value)
	}
	writeText("yzcm_model_catalog/v1")
	writeText(provenance)
	for _, model := range ordered {
		writeText(model.Kind)
		writeText(model.ModelID)
		writeText(model.DisplayName)
		var flags byte
		if model.Adapted {
			flags |= 1
		}
		if model.Allowed {
			flags |= 2
		}
		_, _ = h.Write([]byte{flags})
	}
	return h.Sum(nil)
}

func (s *Store) publish(ctx context.Context, run SyncRun, snapshot Snapshot) (SyncRun, error) {
	digest := snapshotDigest(run.Provenance, snapshot.Models)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return SyncRun{}, err
	}
	defer tx.Rollback()

	// All instances serialize comparisons and publication on the same stable row.
	var activeID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT sync_run_id FROM yzcm_model_catalog_head WHERE slot=1 FOR UPDATE`).Scan(&activeID); err != nil {
		return SyncRun{}, err
	}
	sourceAt := snapshot.SourceUpdatedAt.UTC()
	run.Status = "succeeded"
	if activeID.Valid {
		run.Status, run.Disposition, run.FailureCode, err = compareCatalogSnapshot(ctx, tx, activeID.Int64, sourceAt.UnixNano(), digest)
		if err != nil {
			return SyncRun{}, err
		}
	}
	if run.Status == "succeeded" {
		if err := insertCatalogModels(ctx, tx, run.ID, snapshot.Models); err != nil {
			return SyncRun{}, err
		}
	}

	finished := nowUTC()
	count := len(snapshot.Models)
	result, err := tx.ExecContext(ctx, `UPDATE yzcm_model_sync_runs
SET status=?, source_updated_at_us=?, source_updated_at_ns=?, snapshot_digest=?, finished_at_us=?, model_count=?,
    failure_code=NULLIF(?,''), disposition=NULLIF(?,'') WHERE id=? AND status='running'`,
		run.Status, sourceAt.UnixMicro(), sourceAt.UnixNano(), digest, finished.UnixMicro(), count, run.FailureCode, run.Disposition, run.ID)
	if err != nil {
		return SyncRun{}, err
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		return SyncRun{}, errors.New("YZCM catalog sync run is no longer pending")
	}
	if run.Status == "succeeded" {
		if _, err := tx.ExecContext(ctx, `UPDATE yzcm_model_catalog_head SET sync_run_id=? WHERE slot=1`, run.ID); err != nil {
			return SyncRun{}, err
		}
	}
	run.FinishedAt, run.SourceUpdatedAt, run.ModelCount = &finished, &sourceAt, &count
	return run, tx.Commit()
}

func compareCatalogSnapshot(ctx context.Context, tx *sql.Tx, activeID, sourceNS int64, digest []byte) (status, disposition, failureCode string, err error) {
	var activeNS int64
	var provenance string
	var currentDigest []byte
	var modelCount int
	if err := tx.QueryRowContext(ctx, `SELECT source_updated_at_ns,provenance,snapshot_digest,model_count
FROM yzcm_model_sync_runs WHERE id=? AND status='succeeded'`, activeID).Scan(&activeNS, &provenance, &currentDigest, &modelCount); err != nil {
		return "", "", "", err
	}
	if sourceNS < activeNS {
		return "ignored", "older_source_time", "", nil
	}
	// A missing digest identifies legacy precision. Any instant in the same
	// microsecond could precede the original. Do not replace differing content.
	if sourceNS > activeNS && (currentDigest != nil || sourceNS-activeNS >= 1000) {
		return "succeeded", "", "", nil
	}
	if currentDigest == nil {
		currentDigest, err = digestLegacyCatalog(ctx, tx, activeID, provenance, modelCount)
		if err != nil {
			return "", "", "", err
		}
	}
	if bytes.Equal(currentDigest, digest) {
		return "idempotent", "duplicate_snapshot", "", nil
	}
	return "failed", "", "source_time_conflict", nil
}

func digestLegacyCatalog(ctx context.Context, tx *sql.Tx, activeID int64, provenance string, count int) ([]byte, error) {
	if count < 0 || count > maxSnapshotModels {
		return nil, errors.New("invalid legacy YZCM catalog size")
	}
	rows, err := tx.QueryContext(ctx, `SELECT kind,model_id,display_name,adapted,allowed FROM yzcm_model_catalog WHERE sync_run_id=? LIMIT ?`, activeID, count+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	models := make([]Model, 0, count)
	for rows.Next() {
		var model Model
		if err := rows.Scan(&model.Kind, &model.ModelID, &model.DisplayName, &model.Adapted, &model.Allowed); err != nil {
			return nil, err
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(models) != count {
		return nil, errors.New("legacy YZCM catalog rows do not match its saved size")
	}
	return snapshotDigest(provenance, models), nil
}

func insertCatalogModels(ctx context.Context, tx *sql.Tx, id uint64, models []Model) error {
	insert, err := tx.PrepareContext(ctx, `INSERT INTO yzcm_model_catalog
(sync_run_id,kind,model_id,display_name,adapted,allowed,availability_reason) VALUES (?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insert.Close()
	for _, model := range models {
		if _, err := insert.ExecContext(ctx, id, model.Kind, model.ModelID, model.DisplayName,
			model.Adapted, model.Allowed, availabilityReason(model)); err != nil {
			return err
		}
	}
	return nil
}
