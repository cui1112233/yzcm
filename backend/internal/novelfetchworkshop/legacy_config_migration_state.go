package novelfetchworkshop

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
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
	state, found, err := s.LegacyConfigMigrationState(ctx, owner)
	return found && state.Phase != LegacyComplete, err
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
	allowed := map[LegacyConfigMigrationPhase]LegacyConfigMigrationPhase{LegacyAwaitingSource: LegacyReconciling, LegacyReconciling: LegacyFinalizing}
	if next, ok := allowed[state.Phase]; !ok || next != phase {
		return false, fmt.Errorf("invalid legacy config transition %s to %s", state.Phase, phase)
	}
	result, err := tx.ExecContext(ctx, `UPDATE novel_fetch_legacy_config_migrations SET phase=?,revision=revision+1,updated_at=CURRENT_TIMESTAMP(6)
WHERE owner_username=? AND revision=?`, phase, owner, fromRevision)
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

type LegacySettingEdit struct {
	Path   []string        `json:"path"`
	Value  json.RawMessage `json:"value,omitempty"`
	Delete bool            `json:"delete,omitempty"`
}

func settingPatchBinding() LegacyConflictSource {
	return LegacyConflictSource{Document: "db:legacy_config_migrations", Field: "setting_patch"}
}

func compactSettingEdits(edits []LegacySettingEdit) ([]LegacySettingEdit, error) {
	// Keep the order of the last assignments: a channel deletion may be
	// followed by recreation, or a group deletion by a child assignment.
	seen := map[string]bool{}
	result := make([]LegacySettingEdit, 0, len(edits))
	for i := len(edits) - 1; i >= 0; i-- {
		edit := edits[i]
		if len(edit.Path) == 0 || slices.Contains(edit.Path, "") || (edit.Delete && len(edit.Value) != 0) || (!edit.Delete && !json.Valid(edit.Value)) {
			return nil, errors.New("invalid legacy setting edit")
		}
		key, err := json.Marshal(edit.Path)
		if err != nil {
			return nil, err
		}
		if !seen[string(key)] {
			result = append(result, edit)
			seen[string(key)] = true
		}
	}
	slices.Reverse(result)
	return result, nil
}

func (s *MySQLStore) openSettingEdits(owner string, state LegacyConfigMigrationState) ([]LegacySettingEdit, error) {
	if state.PatchEnvelope == "" && state.PatchFingerprint == "" {
		return nil, nil
	}
	if s.ModelSecrets == nil {
		return nil, errors.New("legacy setting patch requires encryption")
	}
	plain, err := s.ModelSecrets.OpenLegacyConflict(owner, settingPatchBinding(), state.PatchEnvelope, state.PatchFingerprint)
	if err != nil {
		return nil, err
	}
	var edits []LegacySettingEdit
	if err := json.Unmarshal([]byte(plain), &edits); err != nil {
		return nil, err
	}
	return compactSettingEdits(edits)
}

func (s *MySQLStore) LoadLegacySettingEdits(ctx context.Context, owner string) ([]LegacySettingEdit, uint64, bool, error) {
	state, found, err := s.LegacyConfigMigrationState(ctx, owner)
	if err != nil || !found || state.Phase == LegacyComplete {
		return nil, state.Revision, false, err
	}
	edits, err := s.openSettingEdits(owner, state)
	return edits, state.Revision, true, err
}
func (s *MySQLStore) AppendLegacySettingEdits(ctx context.Context, owner string, edits []LegacySettingEdit) error {
	return s.mergeLegacySettingEdits(ctx, owner, edits, false)
}

// Import puts older assignments before existing edits, including when the
// caller retries after committing the import but before retiring its file.
func (s *MySQLStore) ImportLegacySettingEdits(ctx context.Context, owner string, edits []LegacySettingEdit) error {
	return s.mergeLegacySettingEdits(ctx, owner, edits, true)
}
func (s *MySQLStore) mergeLegacySettingEdits(ctx context.Context, owner string, edits []LegacySettingEdit, importing bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if importing {
		_, err = tx.ExecContext(ctx, `INSERT INTO novel_fetch_legacy_config_migrations(owner_username,phase) VALUES(?,'finalizing')
ON DUPLICATE KEY UPDATE revision=revision+IF(phase='complete',1,0),phase=IF(phase='complete','finalizing',phase)`, owner)
		if err != nil {
			return err
		}
	}
	state, found, err := readMigrationState(tx.QueryRowContext(ctx, migrationStateQuery+" FOR UPDATE", owner))
	if err != nil {
		return err
	}
	if !found || state.Phase == LegacyComplete {
		return ErrConfigConflict
	}
	previous, err := s.openSettingEdits(owner, state)
	if err != nil {
		return err
	}
	combined := append(slices.Clone(previous), edits...)
	if importing {
		combined = append(slices.Clone(edits), previous...)
	}
	combined, err = compactSettingEdits(combined)
	if err != nil {
		return err
	}
	if len(edits) == 0 || importing && reflect.DeepEqual(combined, previous) {
		return tx.Commit()
	}
	if s.ModelSecrets == nil {
		return errors.New("legacy setting patch requires encryption")
	}
	raw, err := json.Marshal(combined)
	if err != nil {
		return err
	}
	ciphertext, fingerprint, err := s.ModelSecrets.SealLegacyConflict(owner, settingPatchBinding(), string(raw))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE novel_fetch_legacy_config_migrations SET patch_ciphertext=?,patch_fingerprint=?,revision=revision+1,updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=?`, ciphertext, fingerprint, owner)
	if err != nil {
		return err
	}
	if importing {
		stored, _, err := readMigrationState(tx.QueryRowContext(ctx, migrationStateQuery, owner))
		if err != nil {
			return err
		}
		verified, err := s.openSettingEdits(owner, stored)
		if err != nil {
			return err
		}
		verifiedRaw, err := json.Marshal(verified)
		if err != nil {
			return err
		}
		if !bytes.Equal(verifiedRaw, raw) {
			return errors.New("imported setting patch failed verification")
		}
	}
	return tx.Commit()
}

type settingPatchExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func completeLegacySettingEdits(ctx context.Context, db settingPatchExecutor, owner string, revision uint64) (bool, error) {
	result, err := db.ExecContext(ctx, `UPDATE novel_fetch_legacy_config_migrations SET phase='complete',revision=revision+1,
 candidate_ciphertext=NULL,candidate_fingerprint=NULL,patch_ciphertext=NULL,patch_fingerprint=NULL,updated_at=CURRENT_TIMESTAMP(6)
 WHERE owner_username=? AND phase='finalizing' AND revision=?`, owner, revision)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
func (s *MySQLStore) CompleteLegacySettingEdits(ctx context.Context, owner string, revision uint64) (bool, error) {
	return completeLegacySettingEdits(ctx, s.DB, owner, revision)
}
