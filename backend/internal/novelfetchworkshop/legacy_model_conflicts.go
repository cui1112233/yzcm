package novelfetchworkshop

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/apisecrets"
)

// LegacyConflictSource identifies one credential field at its original location.
// It is stored with the encrypted evidence and authenticated by the secret codec.
type LegacyConflictSource struct {
	Document     string
	Container    int
	Index        int
	Provider     string
	Video        bool
	Field        string
	Kind         string
	RowID        string
	ModelID      string
	ModelSource  [3]string
	TargetDigest string
}

type LegacyModelConflict struct {
	ID     string
	State  string
	Source LegacyConflictSource
}

type legacyConflictRecord struct {
	LegacyModelConflict
	fingerprint string
	ciphertext  sql.NullString
}

func ValidLegacyModelConflictID(value string) bool {
	if len(value) != 64 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func fileConflictDocument(path string) string {
	return "file:" + legacyEntryDigest(filepath.Clean(path))
}

func conflictSource(document string, entry legacySourceEntry, field string) LegacyConflictSource {
	return LegacyConflictSource{
		Document: document, Container: entry.Container, Index: entry.Index, Provider: entry.Provider, Video: entry.Video,
		Field: field, Kind: ModelConfigText(entry.model, "kind"), RowID: ModelConfigText(entry.model, "id"),
		ModelID: ModelConfigText(entry.model, "modelId"), ModelSource: ModelCredentialSource(entry.model),
		TargetDigest: legacyEntryDigest(ModelCredentialDestination(entry.model)),
	}
}

func (s *MySQLStore) archiveLegacyConflict(ctx context.Context, tx *sql.Tx, owner string, source LegacyConflictSource, key string) (string, error) {
	ciphertext, fingerprint, err := s.ModelSecrets.SealLegacyConflict(owner, source, key)
	if err != nil {
		return "", err
	}
	id := legacyEntryDigest([]any{source, fingerprint})
	encoded, err := json.Marshal(source)
	if err != nil {
		return "", err
	}
	// A replay never changes the first ciphertext or resets a human decision.
	if _, err := tx.ExecContext(ctx, `INSERT INTO novel_fetch_legacy_model_conflicts (owner_username,id,source_json,fingerprint,ciphertext)
VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE id=id`, owner, id, string(encoded), fingerprint, ciphertext); err != nil {
		return "", err
	}
	stored, err := readLegacyConflict(ctx, tx, owner, id, false)
	if err != nil {
		return "", err
	}
	if !s.legacyConflictMatches(owner, source, key, stored) {
		return "", fmt.Errorf("legacy credential recovery evidence did not verify")
	}
	return id, nil
}

func readLegacyConflict(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, owner, id string, lock bool) (legacyConflictRecord, error) {
	query := `SELECT source_json,state,fingerprint,ciphertext FROM novel_fetch_legacy_model_conflicts WHERE owner_username=? AND id=?`
	if lock {
		query += " FOR UPDATE"
	}
	var record legacyConflictRecord
	var source []byte
	err := q.QueryRowContext(ctx, query, owner, id).Scan(&source, &record.State, &record.fingerprint, &record.ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return record, ErrNotFound
	}
	if err != nil {
		return record, err
	}
	record.ID = id
	if err := json.Unmarshal(source, &record.Source); err != nil {
		return record, err
	}
	if legacyEntryDigest([]any{record.Source, record.fingerprint}) != id {
		return record, fmt.Errorf("legacy conflict identity did not verify")
	}
	return record, nil
}

func (s *MySQLStore) legacyConflictMatches(owner string, source LegacyConflictSource, key string, record legacyConflictRecord) bool {
	if record.Source != source || !record.ciphertext.Valid {
		return false
	}
	plain, err := s.ModelSecrets.OpenLegacyConflict(owner, record.Source, record.ciphertext.String, record.fingerprint)
	return err == nil && plain == key
}

func (s *MySQLStore) ListLegacyModelConflicts(ctx context.Context, owner, after string) ([]LegacyModelConflict, string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,source_json,state FROM novel_fetch_legacy_model_conflicts
WHERE owner_username=? AND id>? ORDER BY id LIMIT 101`, owner, after)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	records := make([]LegacyModelConflict, 0)
	for rows.Next() {
		var item LegacyModelConflict
		var source []byte
		if err := rows.Scan(&item.ID, &source, &item.State); err != nil {
			return nil, "", err
		}
		if err := json.Unmarshal(source, &item.Source); err != nil {
			return nil, "", err
		}
		records = append(records, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	if len(records) > 100 {
		return records[:100], records[99].ID, nil
	}
	return records, "", nil
}

func (s *MySQLStore) ReviewLegacyModelConflict(ctx context.Context, audit accountcenter.AuditEvent, id, action string) (string, error) {
	if !ValidLegacyModelConflictID(id) || audit.ImpersonationID != "" || s.ModelSecrets == nil {
		return "", accountcenter.ErrForbidden
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	owner := audit.SubjectUsername
	// Use the same lock order as import. Discard is available only after the
	// old file was scrubbed and acknowledged, so it cannot strand plaintext.
	var pending string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(JSON_UNQUOTE(JSON_EXTRACT(settings_json,'$.modelCatalogLegacyScrubPending')),'')
FROM novel_fetch_workshop_configs WHERE owner_username=? FOR UPDATE`, owner).Scan(&pending); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	record, err := readLegacyConflict(ctx, tx, owner, id, true)
	if err != nil {
		return "", err
	}
	var value string
	switch action {
	case "reveal":
		if !record.ciphertext.Valid {
			return "", ErrNotFound
		}
		value, err = s.ModelSecrets.OpenLegacyConflict(owner, record.Source, record.ciphertext.String, record.fingerprint)
	case "confirm":
		if !record.ciphertext.Valid {
			return "", ErrNotFound
		}
		if _, err := s.ModelSecrets.OpenLegacyConflict(owner, record.Source, record.ciphertext.String, record.fingerprint); err != nil {
			return "", err
		}
		_, err = tx.ExecContext(ctx, `UPDATE novel_fetch_legacy_model_conflicts
SET state='confirmed',reviewed_at=COALESCE(reviewed_at,CURRENT_TIMESTAMP(6)) WHERE owner_username=? AND id=?`, owner, id)
	case "discard":
		if pending != "" {
			return "", ErrConfigConflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE novel_fetch_legacy_model_conflicts
SET state='discarded',ciphertext=NULL,reviewed_at=CURRENT_TIMESTAMP(6) WHERE owner_username=? AND id=? AND state<>'discarded'`, owner, id)
	default:
		return "", accountcenter.ErrInvalid
	}
	if err != nil {
		return "", err
	}
	audit.Action = "legacy_key_" + action + ":" + id
	if err = accountcenter.AppendAuditInTransaction(ctx, tx, audit); err != nil {
		return "", err
	}
	if err = tx.Commit(); err != nil {
		return "", err
	}
	return value, nil
}

func legacySourceCredentials(source legacySourceEntry) (map[string]string, bool) {
	fields := []string{"credential", "apiKey"}
	keys := make(map[string]string)
	for _, field := range fields {
		if source.raw[field] == nil {
			continue
		}
		key, valid := source.raw[field].(string)
		if !valid || apisecrets.IsEncrypted(strings.TrimSpace(key)) {
			return nil, false
		}
		if strings.TrimSpace(key) != "" {
			keys[field] = key
		}
	}
	return keys, true
}

func legacySourceMatchesModel(source legacySourceEntry, target map[string]any) bool {
	keys, valid := legacySourceCredentials(source)
	if !valid {
		return false
	}
	if len(keys) == 0 {
		return verifyLegacyScrubTarget(source.model, target, source.RecoveryReason)
	}
	if !sameLegacyModelScope(source.model, target) || ModelConfigText(source.model, "modelId") != ModelConfigText(target, "modelId") ||
		target["credentialStatus"] == "needs_reentry" {
		return false
	}
	for _, key := range keys {
		if ModelConfigText(target, "credential") != key {
			return false
		}
	}
	return true
}

func (s *MySQLStore) verifyLegacySource(ctx context.Context, tx *sql.Tx, owner, document string, source legacySourceEntry, target map[string]any) bool {
	if len(source.ConflictIDs) == 0 {
		return legacySourceMatchesModel(source, target)
	}
	keys, valid := legacySourceCredentials(source)
	if !valid || len(keys) != len(source.ConflictIDs) {
		return false
	}
	for field, key := range keys {
		record, err := readLegacyConflict(ctx, tx, owner, source.ConflictIDs[field], false)
		if err != nil || !s.legacyConflictMatches(owner, conflictSource(document, source, field), key, record) {
			return false
		}
	}
	return true
}

func (s *MySQLStore) legacyConflictRecordsIntact(ctx context.Context, tx *sql.Tx, owner string, ids map[string]string) bool {
	for _, id := range ids {
		record, err := readLegacyConflict(ctx, tx, owner, id, false)
		if err != nil || !record.ciphertext.Valid {
			return false
		}
		if _, err := s.ModelSecrets.OpenLegacyConflict(owner, record.Source, record.ciphertext.String, record.fingerprint); err != nil {
			return false
		}
	}
	return true
}

func (s *MySQLStore) stageLegacyRecovery(ctx context.Context, tx *sql.Tx, owner string, updated map[string]any, migration *legacyConfigMigration) error {
	if len(migration.database) == 0 && len(migration.file) == 0 {
		return nil
	}
	verified := cloneMap(updated)
	if err := s.ModelSecrets.OpenModelConfig(owner, verified); err != nil {
		return err
	}
	rows := legacyCatalogRows(verified)
	targets := map[string]map[string]any{}
	for _, item := range rows {
		row, _ := item.(map[string]any)
		targets[ModelConfigText(row, "id")] = row
	}
	archive := func(document string, source legacySourceEntry, target map[string]any, fromDB bool) (map[string]string, error) {
		if legacySourceMatchesModel(source, target) {
			return nil, nil
		}
		keys, valid := legacySourceCredentials(source)
		if !valid && fromDB {
			return nil, fmt.Errorf("unverified legacy database credential; original data retained")
		}
		if len(keys) == 0 {
			return nil, nil
		}
		ids := make(map[string]string, len(keys))
		for _, field := range []string{"credential", "apiKey"} {
			key, exists := keys[field]
			if !exists {
				continue
			}
			id, err := s.archiveLegacyConflict(ctx, tx, owner, conflictSource(document, source, field), key)
			if err != nil {
				return nil, err
			}
			ids[field] = id
		}
		return ids, nil
	}
	for _, source := range migration.database {
		target, _ := findLegacyTarget(source.model, rows)
		if _, err := archive("db:novel_fetch_workshop_configs", source, target, true); err != nil {
			return err
		}
	}
	if len(migration.file) > 0 {
		plan := make([]legacyScrubEntry, 0, len(migration.file))
		for _, source := range migration.file {
			ids, err := archive(fileConflictDocument(migration.path), source, targets[source.TargetID], false)
			if err != nil {
				return err
			}
			source.ConflictIDs = ids
			plan = append(plan, source.legacyScrubEntry)
		}
		updated[legacyModelScrubEntriesKey] = plan
	}
	return nil
}

// Entry-v1 may have committed only part of its cleanup. Replanning can use
// its still-present sources when every locator and original digest match.
func legacySourcesMatchPreviousPlan(settings, document map[string]any, sources []legacySourceEntry) bool {
	var previous []legacyScrubEntry
	raw, _ := json.Marshal(settings[legacyModelScrubEntriesKey])
	if len(sources) == 0 || json.Unmarshal(raw, &previous) != nil {
		return false
	}
	sameSource := func(prior legacyScrubEntry, source legacySourceEntry) bool {
		return prior.Digest == source.Digest && prior.Container == source.Container && prior.Index == source.Index &&
			prior.Video == source.Video && prior.Provider == source.Provider
	}
	for _, source := range sources {
		if !slices.ContainsFunc(previous, func(prior legacyScrubEntry) bool { return sameSource(prior, source) }) {
			return false
		}
	}
	completed, _ := document[legacyModelScrubEntryDoneKey].(map[string]any)
	for _, prior := range previous {
		if !slices.ContainsFunc(sources, func(source legacySourceEntry) bool { return sameSource(prior, source) }) &&
			completed[legacyEntryDigest(prior)] != ModelConfigText(settings, modelLegacyScrubPendingKey) {
			return false
		}
	}
	return true
}
