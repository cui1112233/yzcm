package novelfetchworkshop

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type LegacyConfigMigrationPhase string

const (
	LegacyAwaitingSource LegacyConfigMigrationPhase = "awaiting_source"
	LegacyReconciling    LegacyConfigMigrationPhase = "reconciling"
	LegacyFinalizing     LegacyConfigMigrationPhase = "finalizing"
	LegacyComplete       LegacyConfigMigrationPhase = "complete"
)

type LegacyConfigMigrationState struct {
	Owner                                   string
	Phase                                   LegacyConfigMigrationPhase
	CandidateEnvelope, CandidateFingerprint string
	PatchEnvelope, PatchFingerprint         string
	Revision                                uint64
	CreatedAt, UpdatedAt                    time.Time
}

const migrationStateQuery = `SELECT owner_username,phase,COALESCE(candidate_ciphertext,''),COALESCE(candidate_fingerprint,''),
COALESCE(patch_ciphertext,''),COALESCE(patch_fingerprint,''),revision,created_at,updated_at
FROM novel_fetch_legacy_config_migrations WHERE owner_username=?`

func readMigrationState(row *sql.Row) (LegacyConfigMigrationState, bool, error) {
	var state LegacyConfigMigrationState
	err := row.Scan(&state.Owner, &state.Phase, &state.CandidateEnvelope, &state.CandidateFingerprint, &state.PatchEnvelope, &state.PatchFingerprint, &state.Revision, &state.CreatedAt, &state.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return state, false, nil
	}
	if err != nil {
		return state, false, err
	}
	switch state.Phase {
	case LegacyAwaitingSource, LegacyReconciling, LegacyFinalizing, LegacyComplete:
		return state, true, nil
	default:
		return state, false, fmt.Errorf("invalid legacy config migration phase %q", state.Phase)
	}
}

func (s *MySQLStore) LegacyConfigMigrationState(ctx context.Context, owner string) (LegacyConfigMigrationState, bool, error) {
	return readMigrationState(s.DB.QueryRowContext(ctx, migrationStateQuery, owner))
}

func (s *MySQLStore) HasPendingLegacyVideos(ctx context.Context, owner string) (bool, error) {
	var pending bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM novel_fetch_legacy_config_migrations WHERE owner_username=? AND phase!='complete')`, owner).Scan(&pending)
	return pending, err
}

func videoCandidatesBinding() LegacyConflictSource {
	return LegacyConflictSource{Document: "db:legacy_config_migrations", Field: "video_candidates"}
}

func (s *MySQLStore) beginLegacyConfigMigration(ctx context.Context, tx *sql.Tx, owner string, candidates pendingLegacyVideos) (LegacyConfigMigrationState, error) {
	raw, err := json.Marshal(candidates)
	if err != nil {
		return LegacyConfigMigrationState{}, err
	}
	ciphertext, fingerprint, err := s.ModelSecrets.SealLegacyConflict(owner, videoCandidatesBinding(), string(raw))
	if err != nil {
		return LegacyConfigMigrationState{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO novel_fetch_legacy_config_migrations(owner_username,phase,candidate_ciphertext,candidate_fingerprint)
VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE revision=revision`, owner, LegacyAwaitingSource, ciphertext, fingerprint)
	if err != nil {
		return LegacyConfigMigrationState{}, err
	}
	state, _, err := readMigrationState(tx.QueryRowContext(ctx, migrationStateQuery+" FOR UPDATE", owner))
	return state, err
}

func (s *MySQLStore) advanceLegacyConfigMigration(ctx context.Context, tx *sql.Tx, owner string, fromRevision uint64, phase LegacyConfigMigrationPhase) (bool, error) {
	state, found, err := readMigrationState(tx.QueryRowContext(ctx, migrationStateQuery+" FOR UPDATE", owner))
	if err != nil || !found || state.Revision != fromRevision {
		return false, err
	}
	allowed := map[LegacyConfigMigrationPhase]LegacyConfigMigrationPhase{LegacyAwaitingSource: LegacyReconciling, LegacyReconciling: LegacyFinalizing, LegacyFinalizing: LegacyComplete}
	if next, ok := allowed[state.Phase]; !ok || next != phase {
		return false, fmt.Errorf("invalid legacy config transition %s to %s", state.Phase, phase)
	}
	result, err := tx.ExecContext(ctx, `UPDATE novel_fetch_legacy_config_migrations SET phase=?,revision=revision+1,updated_at=CURRENT_TIMESTAMP(6),
candidate_ciphertext=IF(?='complete',NULL,candidate_ciphertext),candidate_fingerprint=IF(?='complete',NULL,candidate_fingerprint)
WHERE owner_username=? AND revision=?`, phase, phase, phase, owner, fromRevision)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *MySQLStore) moveLegacyMigration(ctx context.Context, tx *sql.Tx, state *LegacyConfigMigrationState, phase LegacyConfigMigrationPhase) error {
	ok, err := s.advanceLegacyConfigMigration(ctx, tx, state.Owner, state.Revision, phase)
	if err != nil {
		return err
	}
	if !ok {
		return ErrConfigConflict
	}
	state.Revision++
	state.Phase = phase
	return nil
}

// DiscoverLegacyConfigMigration also runs before a preference-only first write.
// It snapshots database candidates without reading or modifying the preference file.
func (s *MySQLStore) DiscoverLegacyConfigMigration(ctx context.Context, owner string) (LegacyConfigMigrationState, bool, error) {
	return s.discoverLegacyConfigMigration(ctx, owner, nil)
}

func (s *MySQLStore) discoverLegacyConfigMigration(ctx context.Context, owner string, snapshot map[string]any) (LegacyConfigMigrationState, bool, error) {
	state, found, err := s.LegacyConfigMigrationState(ctx, owner)
	if err != nil || found || s.LegacyConfigDir == "" {
		return state, found, err
	}
	// GetConfig already read this revision. Established top-level rows are
	// formal; only a legacy container or pending import needs a second, locked read.
	if snapshot != nil && snapshot[ModelSecretVersionKey] == ModelSecretVersion && snapshot[legacyVideoPendingKey] == nil &&
		snapshot[modelLegacyScrubPendingKey] == nil && !hasLegacyVideoProviders(snapshot) && !hasNestedLegacyModels(snapshot) {
		return state, false, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return state, false, err
	}
	defer tx.Rollback()
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=? FOR UPDATE`, owner).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return state, false, nil
	}
	if err != nil {
		return state, false, err
	}
	var settings map[string]any
	if err = json.Unmarshal(raw, &settings); err != nil {
		return state, false, err
	}
	if err := validateLegacyVideoDocument(settings); err != nil {
		return state, false, err
	}
	candidates, err := s.databaseVideoCandidates(owner, settings)
	if err != nil {
		return state, false, err
	}
	oldPlan := ModelConfigText(settings, modelLegacyScrubPendingKey) != "" && settings[legacyModelMigrationVersionKey] == legacyModelMigrationVersion
	if !candidates.any() && !oldPlan {
		return readMigrationState(tx.QueryRowContext(ctx, migrationStateQuery+" FOR UPDATE", owner))
	}
	if s.ModelSecrets == nil {
		return state, false, fmt.Errorf("legacy video candidates require model configuration encryption")
	}
	state, err = s.beginLegacyConfigMigration(ctx, tx, owner, candidates)
	if err != nil {
		return state, false, err
	}
	if oldPlan && !candidates.any() && state.Phase == LegacyAwaitingSource {
		if err = s.moveLegacyMigration(ctx, tx, &state, LegacyReconciling); err != nil {
			return state, false, err
		}
		if err = s.moveLegacyMigration(ctx, tx, &state, LegacyFinalizing); err != nil {
			return state, false, err
		}
	}
	return state, true, tx.Commit()
}
