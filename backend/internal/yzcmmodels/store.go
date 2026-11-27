// Package yzcmmodels stores complete snapshots of the YZCM allowed catalog.
package yzcmmodels

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	provenanceYZCMRelay = "yzcm_relay"
	maxSnapshotModels   = 10000
	defaultLimit        = 30
	maxLimit            = 100
	maxOffset           = 10000
)

var (
	ErrInvalidQuery        = errors.New("invalid YZCM catalog query")
	ErrCatalogChanged      = errors.New("YZCM catalog snapshot changed")
	ErrSourceNotConfigured = errors.New("YZCM catalog sync source not configured")
	ErrSourceTimeConflict  = errors.New("YZCM catalog source time has conflicting snapshots")
)

type Model struct {
	ModelID     string `json:"modelId"`
	DisplayName string `json:"displayName"`
	Kind        string `json:"kind"`
	Adapted     bool   `json:"adapted"`
	Allowed     bool   `json:"allowed"`
}

type Snapshot struct {
	SourceUpdatedAt time.Time
	Models          []Model
	// Complete must be established by the source. A missing list is never an empty catalog.
	Complete bool
}

// Source is supplied by a verified server-side adapter. Searches accept only a Reader.
type Source interface {
	Fetch(context.Context) (Snapshot, error)
}

type Reader interface {
	Search(context.Context, Query) (Page, error)
}

type Query struct {
	Kind, Q       string
	Limit, Offset int
	SnapshotID    uint64
}

type CatalogRow struct {
	Model
	AvailabilityReason string    `json:"availabilityReason"`
	SourceUpdatedAt    time.Time `json:"sourceUpdatedAt"`
	Provenance         string    `json:"provenance"`
}

type SyncRun struct {
	ID              uint64     `json:"id,string"`
	Status          string     `json:"status"`
	Provenance      string     `json:"provenance"`
	StartedAt       time.Time  `json:"startedAt"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	SourceUpdatedAt *time.Time `json:"sourceUpdatedAt,omitempty"`
	ModelCount      *int       `json:"modelCount,omitempty"`
	FailureCode     string     `json:"failureCode,omitempty"`
	Disposition     string     `json:"disposition,omitempty"`
}

type Page struct {
	Status           string       `json:"status"`
	Reason           string       `json:"reason,omitempty"`
	CatalogUpdatedAt *time.Time   `json:"catalogUpdatedAt"`
	SourceUpdatedAt  *time.Time   `json:"sourceUpdatedAt,omitempty"`
	SnapshotID       uint64       `json:"snapshotId,string,omitempty"`
	Provenance       string       `json:"provenance,omitempty"`
	Stale            bool         `json:"stale"`
	LastSync         *SyncRun     `json:"lastSync,omitempty"`
	Models           []CatalogRow `json:"models"`
	Limit            int          `json:"limit"`
	Offset           int          `json:"offset"`
	HasMore          bool         `json:"hasMore"`
	NextOffset       *int         `json:"nextOffset,omitempty"`
}

type Store struct{ DB *sql.DB }

func validKind(kind string) bool {
	switch kind {
	case "text", "image", "video", "audio", "asr":
		return true
	default:
		return false
	}
}

func validText(value string, limit int) bool {
	return len(value) <= limit*utf8.UTFMax && utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit &&
		strings.IndexFunc(value, unicode.IsControl) == -1
}

func (query Query) Normalize() (Query, error) {
	query.Kind = strings.ToLower(strings.TrimSpace(query.Kind))
	query.Q = strings.TrimSpace(query.Q)
	if query.Limit == 0 {
		query.Limit = defaultLimit
	}
	if (query.Kind != "" && !validKind(query.Kind)) || !validText(query.Q, 191) ||
		query.Limit < 1 || query.Limit > maxLimit || query.Offset < 0 || query.Offset > maxOffset {
		return Query{}, ErrInvalidQuery
	}
	return query, nil
}

func availabilityReason(model Model) string {
	if !model.Adapted {
		return "not_adapted"
	}
	if !model.Allowed {
		return "not_allowed"
	}
	return "adapted_and_allowed"
}

func validateSnapshot(snapshot Snapshot, observedAt time.Time) error {
	sourceAt := snapshot.SourceUpdatedAt.UTC()
	if !snapshot.Complete || snapshot.Models == nil || len(snapshot.Models) > maxSnapshotModels ||
		sourceAt.IsZero() || sourceAt.Before(time.Unix(0, 0)) || !sourceAt.Equal(time.Unix(0, sourceAt.UnixNano())) || sourceAt.After(observedAt) {
		return errors.New("invalid YZCM catalog snapshot metadata")
	}
	type modelKey struct{ kind, id string }
	seen := make(map[modelKey]bool, len(snapshot.Models))
	for _, model := range snapshot.Models {
		key := modelKey{model.Kind, model.ModelID}
		if !validKind(model.Kind) || model.ModelID == "" || strings.TrimSpace(model.ModelID) != model.ModelID ||
			!validText(model.ModelID, 191) || strings.TrimSpace(model.DisplayName) == "" || !validText(model.DisplayName, 256) || seen[key] {
			return errors.New("invalid YZCM catalog snapshot records")
		}
		seen[key] = true
	}
	return nil
}

// Sync records each attempt. Publication compares the full source time against
// the locked catalog head and atomically advances that head with the model rows.
func (s *Store) Sync(ctx context.Context, source Source) (SyncRun, error) {
	if source == nil {
		return SyncRun{}, ErrSourceNotConfigured
	}
	if s == nil || s.DB == nil {
		return SyncRun{}, errors.New("YZCM catalog store unavailable")
	}
	run := SyncRun{Provenance: provenanceYZCMRelay, Status: "running", StartedAt: nowUTC()}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO yzcm_model_sync_runs (provenance, status, started_at_us) VALUES (?, 'running', ?)`,
		run.Provenance, run.StartedAt.UnixMicro())
	if err != nil {
		return SyncRun{}, fmt.Errorf("start YZCM catalog sync: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return SyncRun{}, fmt.Errorf("read YZCM catalog sync ID: %w", err)
	}
	run.ID = uint64(id)
	snapshot, err := source.Fetch(ctx)
	if err != nil {
		// Provider errors may contain tokens or HTTP headers. Keep only the class.
		return s.failSync(ctx, run, "source_error", errors.New("YZCM catalog source failed"))
	}
	if err := validateSnapshot(snapshot, time.Now().UTC()); err != nil {
		return s.failSync(ctx, run, "invalid_snapshot", err)
	}
	completed, err := s.publish(ctx, run, snapshot)
	if err != nil {
		return s.failSync(ctx, run, "persistence_error", errors.New("could not publish YZCM catalog snapshot"))
	}
	if completed.FailureCode == "source_time_conflict" {
		return completed, ErrSourceTimeConflict
	}
	return completed, nil
}

func nowUTC() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func (s *Store) failSync(ctx context.Context, run SyncRun, code string, cause error) (SyncRun, error) {
	// A cancelled source or a failed write still leaves a durable attempt record.
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	finished := nowUTC()
	result, err := s.DB.ExecContext(auditCtx, `UPDATE yzcm_model_sync_runs
SET status='failed', failure_code=?, finished_at_us=? WHERE id=? AND status='running'`, code, finished.UnixMicro(), run.ID)
	if err != nil {
		return run, errors.Join(cause, fmt.Errorf("record YZCM catalog failure: %w", err))
	}
	affected, err := result.RowsAffected()
	if err != nil || affected != 1 {
		// In an ambiguous commit, a successful run must not be relabelled as failed.
		return run, errors.Join(cause, errors.New("could not confirm YZCM catalog failure record"))
	}
	run.Status, run.FailureCode, run.FinishedAt = "failed", code, &finished
	return run, cause
}

const runColumns = `id, status, provenance, started_at_us, finished_at_us, source_updated_at_ns, model_count, failure_code, disposition`

func optionalTime(micros sql.NullInt64) *time.Time {
	if !micros.Valid {
		return nil
	}
	at := time.UnixMicro(micros.Int64).UTC()
	return &at
}

func readRun(row *sql.Row) (*SyncRun, error) {
	var run SyncRun
	var started int64
	var finished, sourceAt, count sql.NullInt64
	var reason, disposition sql.NullString
	err := row.Scan(&run.ID, &run.Status, &run.Provenance, &started, &finished, &sourceAt, &count, &reason, &disposition)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	run.StartedAt, run.FinishedAt = time.UnixMicro(started).UTC(), optionalTime(finished)
	run.FailureCode, run.Disposition = reason.String, disposition.String
	if sourceAt.Valid {
		at := time.Unix(0, sourceAt.Int64).UTC()
		run.SourceUpdatedAt = &at
	}
	if count.Valid {
		n := int(count.Int64)
		run.ModelCount = &n
	}
	return &run, nil
}

func (s *Store) Search(ctx context.Context, query Query) (Page, error) {
	query, err := query.Normalize()
	if err != nil {
		return Page{}, err
	}
	page := Page{Status: "unavailable", Reason: "no_saved_snapshot", Limit: query.Limit, Offset: query.Offset, Models: []CatalogRow{}}
	if s == nil || s.DB == nil {
		return Page{}, errors.New("YZCM catalog store unavailable")
	}
	// The metadata and rows must refer to the same snapshot while another sync publishes.
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return Page{}, err
	}
	defer tx.Rollback()
	page.LastSync, err = readRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM yzcm_model_sync_runs ORDER BY id DESC LIMIT 1`))
	if err != nil {
		return Page{}, err
	}
	active, err := readRun(tx.QueryRowContext(ctx, `SELECT `+runColumns+` FROM yzcm_model_sync_runs
WHERE id=(SELECT sync_run_id FROM yzcm_model_catalog_head WHERE slot=1) AND status='succeeded'`))
	if err != nil {
		return Page{}, err
	}
	if query.SnapshotID != 0 && (active == nil || active.ID != query.SnapshotID) {
		return Page{}, ErrCatalogChanged
	}
	if active == nil {
		return page, tx.Commit()
	}
	if active.SourceUpdatedAt == nil || active.FinishedAt == nil || active.ModelCount == nil {
		return Page{}, errors.New("invalid saved YZCM catalog metadata")
	}
	page.SnapshotID, page.SourceUpdatedAt, page.CatalogUpdatedAt, page.Provenance = active.ID, active.SourceUpdatedAt, active.FinishedAt, active.Provenance
	page.Stale = page.LastSync != nil && page.LastSync.Status == "failed" && page.LastSync.ID > active.ID
	page.Models, err = searchModels(ctx, tx, query, active)
	if err != nil {
		return Page{}, err
	}
	switch {
	case *active.ModelCount == 0:
		page.Status, page.Reason = "empty", "empty_catalog"
	case len(page.Models) == 0:
		page.Status, page.Reason = "empty", "no_matches"
	default:
		page.Status, page.Reason = "available", ""
	}
	if len(page.Models) > query.Limit {
		page.Models, page.HasMore = page.Models[:query.Limit], true
		if next := query.Offset + query.Limit; next <= maxOffset {
			page.NextOffset = &next
		}
	}
	return page, tx.Commit()
}

var likeLiteral = strings.NewReplacer("=", "==", "%", "=%", "_", "=_")

func searchModels(ctx context.Context, tx *sql.Tx, query Query, active *SyncRun) ([]CatalogRow, error) {
	statement := `SELECT model_id, display_name, kind, adapted, allowed, availability_reason FROM yzcm_model_catalog
WHERE sync_run_id=? AND adapted=TRUE AND allowed=TRUE`
	args := []any{active.ID}
	if query.Kind != "" {
		statement += ` AND kind=?`
		args = append(args, query.Kind)
	}
	if query.Q != "" {
		statement += ` AND (display_name LIKE ? ESCAPE '=' OR model_id COLLATE utf8mb4_0900_as_ci LIKE ? ESCAPE '=')`
		pattern := "%" + likeLiteral.Replace(query.Q) + "%"
		args = append(args, pattern, pattern)
	}
	statement += ` ORDER BY kind, display_name, model_id LIMIT ? OFFSET ?`
	rows, err := tx.QueryContext(ctx, statement, append(args, query.Limit+1, query.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]CatalogRow, 0, query.Limit+1)
	for rows.Next() {
		item := CatalogRow{SourceUpdatedAt: *active.SourceUpdatedAt, Provenance: active.Provenance}
		if err := rows.Scan(&item.ModelID, &item.DisplayName, &item.Kind, &item.Adapted, &item.Allowed, &item.AvailabilityReason); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
