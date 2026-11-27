package novelfetchworkshop

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
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
	Path          []string        `json:"path"`
	Value         json.RawMessage `json:"value,omitempty"`
	Delete        bool            `json:"delete,omitempty"`
	OnlyIfMissing bool            `json:"onlyIfMissing,omitempty"`
}

func settingPatchBinding() LegacyConflictSource {
	return LegacyConflictSource{Document: "db:legacy_config_migrations", Field: "setting_patch"}
}

func (edit LegacySettingEdit) Validate() error {
	path := edit.Path
	if len(path) == 0 || slices.Contains(path, "") || (edit.Delete && len(edit.Value) != 0) || (!edit.Delete && !json.Valid(edit.Value)) {
		return errors.New("invalid legacy setting edit")
	}
	if edit.OnlyIfMissing && edit.Delete {
		return errors.New("invalid snapshot edit")
	}
	if path[0] != "apiIntegrations" {
		return nil
	}
	if len(path) == 1 {
		return errors.New("whole integration section edit is unsafe")
	}
	if path[1] == "customChannels" {
		switch {
		case len(path) == 2:
			return errors.New("whole channel list edit is unsafe")
		case len(path) == 3 && !edit.Delete:
			return errors.New("whole channel replacement is unsafe")
		case len(path) > 3 && path[3] == "id":
			var id string
			if !channelIdentityEcho(edit) || edit.Delete || json.Unmarshal(edit.Value, &id) != nil || id != path[2] {
				return errors.New("channel identity cannot be reassigned")
			}
		}
	}
	return nil
}

func channelIdentityEcho(edit LegacySettingEdit) bool {
	p := edit.Path
	return len(p) == 4 && p[0] == "apiIntegrations" && p[1] == "customChannels" && p[3] == "id"
}

func historicalChannelDefault(path []string, value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		if strings.TrimSpace(v) == "" {
			return true
		}
		return len(path) == 1 && (path[0] == "apiFormat" && v == "openai_compatible" || path[0] == "model" && v == "gpt-4o-mini" || path[0] == "baseUrl" && v == "https://api.openai.com/v1")
	case float64:
		return v == 0
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	case bool:
		return len(path) == 1 && path[0] == "enabled" && v
	}
	return false
}

func (s *MySQLStore) LoadLegacySettingEdits(ctx context.Context, owner string) ([]LegacySettingEdit, uint64, bool, error) {
	state, found, err := s.LegacyConfigMigrationState(ctx, owner)
	if err != nil || !found || state.Phase == LegacyComplete {
		return nil, state.Revision, false, err
	}
	journal, err := s.openSettingJournal(owner, state)
	return journal.edits(), state.Revision, true, err
}

// Lock a row that exists independently of the workshop configuration. Both
// ordinary standard-file writes and finalization hold it until file I/O ends.
func (s *MySQLStore) beginLegacySettingFileWrite(ctx context.Context, owner string) (*sql.Tx, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO novel_fetch_legacy_setting_files(owner_username,fingerprint) VALUES(?,'')
 ON DUPLICATE KEY UPDATE fingerprint=fingerprint`, owner)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// WriteStandardSettingsIfReady checks the phase again after acquiring the
// file lock. A false result routes the request back to the active journal.
func (s *MySQLStore) WriteStandardSettingsIfReady(ctx context.Context, owner string, write func(completed bool) error) (bool, error) {
	tx, err := s.beginLegacySettingFileWrite(ctx, owner)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	state, found, err := readMigrationState(tx.QueryRowContext(ctx, migrationStateQuery+" FOR UPDATE", owner))
	if err != nil {
		return false, err
	}
	if found && state.Phase != LegacyComplete {
		return false, nil
	}
	if err := write(found); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func (s *MySQLStore) completeSettingJournal(ctx context.Context, tx *sql.Tx, state LegacyConfigMigrationState, journal settingJournal) (bool, error) {
	result, err := tx.ExecContext(ctx, `UPDATE novel_fetch_legacy_config_migrations SET phase='complete',revision=revision+1,
 candidate_ciphertext=NULL,candidate_fingerprint=NULL,patch_ciphertext=NULL,patch_fingerprint=NULL,updated_at=CURRENT_TIMESTAMP(6)
 WHERE owner_username=? AND phase='finalizing' AND revision=?`, state.Owner, state.Revision)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	// The CAS owns the migration row before we archive. An append that won
	// the race has already retained the old source in its own transaction.
	return true, s.retainOldDBPatch(ctx, tx, state, journal)
}

func (s *MySQLStore) CompleteLegacySettingEdits(ctx context.Context, owner string, revision uint64) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	state, found, err := readMigrationState(tx.QueryRowContext(ctx, migrationStateQuery+" FOR UPDATE", owner))
	if err != nil || !found || state.Phase != LegacyFinalizing || state.Revision != revision {
		return false, err
	}
	journal, err := s.openSettingJournal(owner, state)
	if err != nil {
		return false, err
	}
	ok, err := s.completeSettingJournal(ctx, tx, state, journal)
	if err != nil || !ok {
		return false, err
	}
	err = tx.Commit()
	return err == nil, err
}
