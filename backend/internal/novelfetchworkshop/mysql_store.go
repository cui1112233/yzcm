package novelfetchworkshop

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
)

type MySQLStore struct {
	DB                 *sql.DB
	LegacyConfigDir    string
	ObjectBodies       *BodyObjectStore
	ModelSecrets       ModelConfigSecrets
	LegacyConfigLocker sync.Locker
	// ReplaceLegacyConfig can supply an atomic model-file replacement for
	// installations with an alternative file writer.
	ReplaceLegacyConfig func(string, map[string]any) error
}

func NewMySQLStore(db *sql.DB) *MySQLStore { return &MySQLStore{DB: db} }

const ModelSecretVersionKey = "modelCatalogSecretVersion"
const ModelSecretVersion = "bound-v3"

type ModelConfigSecrets interface {
	SealModelConfig(owner string, next, previous map[string]any) error
	OpenModelConfig(owner string, settings map[string]any) error
	SealLegacyConflict(owner string, source LegacyConflictSource, key string) (ciphertext, fingerprint string, err error)
	OpenLegacyConflict(owner string, source LegacyConflictSource, ciphertext, fingerprint string) (string, error)
}

func (s *MySQLStore) GetDocument(ctx context.Context, owner, bookID string) (Document, error) {
	var raw []byte
	var updated time.Time
	err := s.DB.QueryRowContext(ctx, `SELECT document_json, updated_at FROM novel_fetch_workshop_documents WHERE owner_username=? AND book_id=?`, owner, bookID).Scan(&raw, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	var document Document
	if err := json.Unmarshal(raw, &document); err != nil {
		return Document{}, fmt.Errorf("decode novel fetch document: %w", err)
	}
	document = normalizeDocument(document)
	document.BookID = bookID
	if hasLegacyDocumentBodies(document) {
		if err := s.MigrateLegacyDocumentBodies(ctx, owner, bookID); err != nil {
			return Document{}, err
		}
		return s.GetDocument(ctx, owner, bookID)
	}
	document.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	return document, nil
}

func (s *MySQLStore) PutDocument(ctx context.Context, owner string, document Document) error {
	document = normalizeDocument(document)
	if document.BookID == "" {
		return errors.New("book id is required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	document, err = prepareDocumentWrite(ctx, tx, owner, document)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(document)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_documents(owner_username,book_id,document_json,created_at,updated_at) VALUES(?,?,CAST(? AS CHAR CHARACTER SET utf8mb4),CURRENT_TIMESTAMP(6),CURRENT_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE document_json=VALUES(document_json), updated_at=GREATEST(CURRENT_TIMESTAMP(6), TIMESTAMPADD(MICROSECOND,1,updated_at))`, owner, document.BookID, string(raw))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *MySQLStore) CompareAndSwapDocument(ctx context.Context, owner string, document Document) (string, error) {
	document = normalizeDocument(document)
	if document.BookID == "" {
		return "", errors.New("book id is required")
	}
	var previous time.Time
	var err error
	if document.UpdatedAt != "" {
		previous, err = time.Parse(time.RFC3339Nano, document.UpdatedAt)
		if err != nil {
			return "", err
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	document, err = prepareDocumentWrite(ctx, tx, owner, document)
	if err != nil {
		return "", err
	}
	next := nextDocumentWriteTime(previous)
	document.UpdatedAt = next.Format(time.RFC3339Nano)
	raw, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	if previous.IsZero() {
		_, err = tx.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_documents(owner_username,book_id,document_json,created_at,updated_at) VALUES(?,?,CAST(? AS CHAR CHARACTER SET utf8mb4),?,?)`, owner, document.BookID, string(raw), next, next)
		var duplicate *mysql.MySQLError
		if errors.As(err, &duplicate) && duplicate.Number == 1062 {
			return "", ErrDocumentChanged
		}
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE novel_fetch_workshop_documents SET document_json=CAST(? AS CHAR CHARACTER SET utf8mb4), updated_at=? WHERE owner_username=? AND book_id=? AND updated_at=?`, string(raw), next, owner, document.BookID, previous.UTC())
		if err == nil {
			var n int64
			n, err = result.RowsAffected()
			if err == nil && n == 0 {
				return "", ErrDocumentChanged
			}
		}
	}
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return document.UpdatedAt, nil
}

func (s *MySQLStore) ListDocuments(ctx context.Context, owner string) ([]Document, error) {
	// 两步查询：document_json 存整本小说正文，直接参与 ORDER BY 的 filesort
	// 会撑爆 sort_buffer_size 并返回 MySQL ERROR 1038。先只对 book_id 排序，
	// 再按 id 分批读取正文，顺序保持 updated_at DESC, book_id ASC。
	idRows, err := s.DB.QueryContext(ctx, `SELECT book_id FROM novel_fetch_workshop_documents WHERE owner_username=? ORDER BY updated_at DESC, book_id ASC`, owner)
	if err != nil {
		return nil, err
	}
	bookIDs := []string{}
	for idRows.Next() {
		var bookID string
		if err := idRows.Scan(&bookID); err != nil {
			idRows.Close()
			return nil, err
		}
		bookIDs = append(bookIDs, bookID)
	}
	if err := idRows.Err(); err != nil {
		idRows.Close()
		return nil, err
	}
	idRows.Close()

	result := []Document{}
	const pageSize = 200
	for start := 0; start < len(bookIDs); start += pageSize {
		end := start + pageSize
		if end > len(bookIDs) {
			end = len(bookIDs)
		}
		page := bookIDs[start:end]
		placeholders := make([]string, 0, len(page))
		args := make([]any, 0, len(page)+1)
		args = append(args, owner)
		for _, bookID := range page {
			placeholders = append(placeholders, "?")
			args = append(args, bookID)
		}
		rows, err := s.DB.QueryContext(ctx, `SELECT book_id, document_json, updated_at FROM novel_fetch_workshop_documents WHERE owner_username=? AND book_id IN (`+strings.Join(placeholders, ",")+`)`, args...)
		if err != nil {
			return nil, err
		}
		type documentRow struct {
			raw     []byte
			updated time.Time
		}
		entries := make(map[string]documentRow, len(page))
		for rows.Next() {
			var bookID string
			var raw []byte
			var updated time.Time
			if err := rows.Scan(&bookID, &raw, &updated); err != nil {
				rows.Close()
				return nil, err
			}
			entries[bookID] = documentRow{raw: raw, updated: updated}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		for _, bookID := range page {
			entry, ok := entries[bookID]
			if !ok {
				continue
			}
			var document Document
			if err := json.Unmarshal(entry.raw, &document); err != nil {
				return nil, fmt.Errorf("decode novel fetch document %s: %w", bookID, err)
			}
			document = normalizeDocument(document)
			document.BookID = bookID
			if hasLegacyDocumentBodies(document) {
				document = lightweightLegacyDocument(document, legacyBodyRecords(document))
			}
			document.UpdatedAt = entry.updated.UTC().Format(time.RFC3339Nano)
			result = append(result, document)
		}
	}
	return result, nil
}

func (s *MySQLStore) DeleteDocuments(ctx context.Context, owner string, ids []string) (DeleteResult, error) {
	result := DeleteResult{Requested: len(ids), Results: make([]DeleteRecord, 0, len(ids))}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			result.Results = append(result.Results, DeleteRecord{BookID: rawID, Deleted: false})
			continue
		}
		// 删除历史文档时保留正文，但必须转入可回收状态；否则这些正文会永久
		// 停留在 ready，既不会被容量统计回收，也不会被定时清理命中。
		releasedAt := time.Now().UTC()
		expiresAt := releasedAt.Add(DefaultBodyRetentionDays * 24 * time.Hour)
		if _, err := tx.ExecContext(ctx, `UPDATE novel_fetch_workshop_bodies SET state='releasable', releasable_at=?, expires_at=?, updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=? AND book_id=? AND state<>'releasable'`, releasedAt, expiresAt, owner, id); err != nil {
			return result, err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM novel_fetch_workshop_documents WHERE owner_username=? AND book_id=?`, owner, id)
		if err != nil {
			return result, err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return result, err
		}
		deleted := rows > 0
		if deleted {
			result.Deleted++
		}
		result.Results = append(result.Results, DeleteRecord{BookID: id, Deleted: deleted})
	}
	if err := tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}

func (s *MySQLStore) GetConfig(ctx context.Context, owner string) (map[string]any, error) {
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=?`, owner).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	var settings map[string]any
	if len(raw) != 0 {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return nil, fmt.Errorf("decode novel fetch config: %w", err)
		}
	}
	if settings == nil {
		settings = map[string]any{}
	}
	settings[ConfigRevisionKey] = configRevision(settings)
	if s.ModelSecrets != nil {
		return s.openConfig(ctx, owner, settings)
	}
	if hasLegacyVideoProviders(settings) || settings[legacyVideoPendingKey] != nil || (s.LegacyConfigDir != "" && len(collectLegacyVideoCandidates(settings)) > 0) {
		return nil, fmt.Errorf("legacy video credentials require model configuration encryption")
	}
	return EnsureWorkshopDefaults(settings), nil
}

// GetWorkflowConfig returns personal workflow preferences without loading a
// member's historical model keys when runtime uses the current team catalog.
func (s *MySQLStore) GetWorkflowConfig(ctx context.Context, owner string) (map[string]any, error) {
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT JSON_REMOVE(settings_json,
 '$.modelCatalog', '$.settings.modelCatalog', '$.appConfig.modelCatalog', '$.settings.appConfig.modelCatalog', '$.modelCatalogSecretVersion', '$.modelCatalogLegacyScrubPending',
 '$.modelCatalogLegacyScrubEntries', '$.modelCatalogLegacyMigrationVersion', '$.modelCatalogLegacyVideoPending',
 '$.videoProviders', '$.settings.videoProviders', '$.appConfig.videoProviders', '$.settings.appConfig.videoProviders')
FROM novel_fetch_workshop_configs WHERE owner_username=?`, owner).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return EnsureWorkshopDefaults(nil), nil
	}
	if err != nil {
		return nil, err
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if hasLegacyVideoProviders(settings) || ModelConfigText(settings, modelLegacyScrubPendingKey) != "" {
		return nil, ErrConfigConflict
	}
	return EnsureWorkshopDefaults(settings), nil
}

// ReadBoundModelConfig compares a read-only form echo without triggering an
// import or rewriting storage on a forbidden request.
func (s *MySQLStore) ReadBoundModelConfig(ctx context.Context, owner string) (map[string]any, error) {
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=?`, owner).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, err
	}
	if _, exists := settings["modelCatalog"]; !exists {
		return map[string]any{}, nil
	}
	if s.ModelSecrets == nil || settings[ModelSecretVersionKey] != ModelSecretVersion {
		return nil, ErrConfigConflict
	}
	delete(settings, legacyVideoPendingKey)
	return settings, s.ModelSecrets.OpenModelConfig(owner, settings)
}

func (s *MySQLStore) openConfig(ctx context.Context, owner string, settings map[string]any) (map[string]any, error) {
	currentVersion := settings[ModelSecretVersionKey] == ModelSecretVersion
	state, found, err := s.discoverLegacyConfigMigration(ctx, owner, settings)
	if err != nil {
		return nil, err
	}
	if !found && s.LegacyConfigDir == "" && settings[legacyVideoPendingKey] != nil {
		return nil, fmt.Errorf("staged legacy videos require their original file directory")
	}
	waiting := found && (state.Phase == LegacyAwaitingSource || state.Phase == LegacyReconciling)
	checkFile := s.LegacyConfigDir != "" && (waiting || (!found && settings[legacyModelMigrationVersionKey] != legacyModelMigrationVersion))
	needsDBMigration := !currentVersion || settings[legacyVideoPendingKey] != nil || hasLegacyVideoProviders(settings) || hasNestedLegacyModels(settings)
	if needsDBMigration || checkFile {
		path, err := s.legacyModelFilePath(owner)
		if err != nil {
			return nil, err
		}
		legacy, err := readLegacyModelDocument(path)
		if err != nil {
			return nil, err
		}
		var candidates pendingLegacyVideos
		if waiting {
			candidates, err = s.migrationCandidates(state)
			if err != nil {
				return nil, err
			}
			if !candidates.fileReady(legacy) {
				legacy = nil
			}
		} else if !found && len(legacyCatalogRows(legacy)) == 0 && ModelConfigText(settings, modelLegacyScrubPendingKey) == "" {
			legacy = nil
		}
		if legacy == nil && ModelConfigText(settings, modelLegacyScrubPendingKey) != "" {
			return nil, fmt.Errorf("legacy model file is unavailable during credential cleanup")
		}
		fileChecked := legacy != nil
		rows := legacyCatalogRows(settings)
		hasCatalog := hasLegacyVideoProviders(settings)
		for _, container := range legacyCatalogContainers(settings) {
			if _, exists := container["modelCatalog"]; exists {
				hasCatalog = true
				break
			}
		}
		if (needsDBMigration || fileChecked) && (currentVersion || hasCatalog || len(rows) > 0 || len(legacy) > 0 || settings[legacyVideoMigratedKey] == true) {
			previouslyBound := hasBoundCatalog(settings)
			var boundRows []any
			if previouslyBound {
				boundRows, _ = settings["modelCatalog"].([]any)
				rows, err = preserveBoundLegacyRows(settings)
				if err != nil {
					return nil, err
				}
			}
			migration := &legacyConfigMigration{path: path, state: state, releaseVideos: fileChecked, hasFileVideo: len(collectLegacyVideoCandidates(legacy)) > 0}
			for _, source := range collectLegacySources(settings) {
				if (!waiting || ModelConfigText(source.model, "kind") != "video") && (!previouslyBound || source.Video || source.Container != 0) {
					migration.database = append(migration.database, source)
				}
			}
			if !previouslyBound && len(rows) > 0 {
				mergeLegacyModelKeys(rows, legacyCatalogRows(legacy))
			}
			if waiting {
				rows = candidates.prepareRows(rows, boundRows, migration)
			}
			delete(settings, legacyVideoPendingKey)
			rows, migration.file = prepareLegacyModelEntries(rows, legacy, collectLegacySources(legacy), previouslyBound)
			reconcileLegacyVideoCredentials(rows, boundRows, migration.database, migration.file)
			digest := legacyCatalogSecretDigest(legacy)
			settings["modelCatalog"] = rows
			if digest != "" {
				pending := ModelConfigText(settings, modelLegacyScrubPendingKey)
				if pending != "" && pending != digest && pending != priorLegacyModelDigest(legacy) &&
					!legacySourcesMatchPreviousPlan(settings, legacy, migration.file) {
					return nil, fmt.Errorf("legacy model file changed before credential cleanup")
				}
				settings[modelLegacyScrubPendingKey] = digest
			}
			settings[legacyVideoMigratedKey] = true
			if fileChecked {
				settings[legacyModelMigrationVersionKey] = legacyModelMigrationVersion
			}
			if err := s.putConfig(ctx, owner, settings, migration); err != nil {
				return nil, err
			}
			return s.GetConfig(ctx, owner)
		}
	}
	delete(settings, legacyVideoPendingKey)
	if err := s.ModelSecrets.OpenModelConfig(owner, settings); err != nil {
		return nil, err
	}
	if ModelConfigText(settings, modelLegacyScrubPendingKey) != "" {
		if err := s.scrubLegacyModelFile(ctx, owner, settings, state); err != nil {
			return nil, fmt.Errorf("clean legacy model configuration: %w", err)
		}
		delete(settings, modelLegacyScrubPendingKey)
		delete(settings, legacyModelScrubEntriesKey)
	} else if (found && state.Phase == LegacyFinalizing) || (settings[legacyModelMigrationVersionKey] == legacyModelMigrationVersion && !checkFile) {
		path, err := s.legacyModelFilePath(owner)
		if err != nil {
			return nil, err
		}
		if s.LegacyConfigLocker != nil {
			s.LegacyConfigLocker.Lock()
			defer s.LegacyConfigLocker.Unlock()
		}
		if err := s.finalizeLegacySettings(owner, path); err != nil {
			return nil, err
		}
		if state.Phase == LegacyFinalizing {
			if path == "" {
				return nil, fmt.Errorf("legacy config directory unavailable during finalization")
			}
			if err := s.completeLegacyMigration(ctx, owner, "", state); err != nil {
				return nil, err
			}
		}
	}
	return EnsureWorkshopDefaults(settings), nil
}

func (s *MySQLStore) PutConfig(ctx context.Context, owner string, settings map[string]any) error {
	return s.putConfig(ctx, owner, settings, nil)
}

func (s *MySQLStore) putConfig(ctx context.Context, owner string, settings map[string]any, migration *legacyConfigMigration) error {
	if settings == nil {
		settings = map[string]any{}
	}
	if s.ModelSecrets == nil && hasLegacyVideoProviders(settings) {
		return fmt.Errorf("legacy video credentials require model configuration encryption")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Create an empty row only inside this transaction so even the first two
	// writers serialize on the same MySQL record.
	if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?,'{}',CURRENT_TIMESTAMP(6),CURRENT_TIMESTAMP(6))`, owner); err != nil {
		return err
	}
	var previousRaw []byte
	if err := tx.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=? FOR UPDATE`, owner).Scan(&previousRaw); err != nil {
		return err
	}
	var previous map[string]any
	if err := json.Unmarshal(previousRaw, &previous); err != nil {
		return err
	}
	next, err := checkedConfigRevision(previous, settings)
	if err != nil {
		return err
	}
	updated := cloneMap(settings)
	if migration == nil {
		delete(updated, legacyVideoPendingKey)
		if pending, exists := previous[legacyVideoPendingKey]; exists {
			if s.ModelSecrets == nil {
				return ErrConfigConflict
			}
			updated[legacyVideoPendingKey] = pending
		}
	}
	if s.ModelSecrets != nil {
		if migration == nil {
			unverifiedVideos := s.LegacyConfigDir != "" && !hasBoundCatalog(previous) && len(collectLegacyVideoCandidates(previous)) > 0
			if unverifiedVideos || hasLegacyVideoProviders(previous) || hasNestedLegacyModels(previous) || ModelConfigText(previous, modelLegacyScrubPendingKey) != "" {
				return ErrConfigConflict
			}
			for _, key := range []string{modelLegacyScrubPendingKey, legacyModelScrubEntriesKey, legacyModelMigrationVersionKey} {
				delete(updated, key)
			}
		}
		if pending := ModelConfigText(previous, modelLegacyScrubPendingKey); pending != "" && (previous[legacyModelMigrationVersionKey] == legacyModelMigrationVersion || ModelConfigText(updated, modelLegacyScrubPendingKey) == "") {
			updated[modelLegacyScrubPendingKey] = pending
			if entries, ok := previous[legacyModelScrubEntriesKey]; ok {
				updated[legacyModelScrubEntriesKey] = entries
			}
		}
		if previous[legacyModelMigrationVersionKey] == legacyModelMigrationVersion {
			updated[legacyModelMigrationVersionKey] = legacyModelMigrationVersion
		}
		for _, nested := range legacyCatalogContainers(updated)[1:] {
			delete(nested, "modelCatalog")
			delete(nested, "modelCatalogVersion")
		}
		for _, container := range legacyCatalogContainers(updated) {
			delete(container, "videoProviders")
		}
		updated[legacyVideoMigratedKey] = true
		delete(updated, ModelSecretVersionKey)
		if _, present := updated["modelCatalog"]; present {
			if err := s.ModelSecrets.SealModelConfig(owner, updated, previous); err != nil {
				return err
			}
			updated[ModelSecretVersionKey] = ModelSecretVersion
		} else if previous[ModelSecretVersionKey] == ModelSecretVersion {
			updated["modelCatalog"] = []any{}
			updated[ModelSecretVersionKey] = ModelSecretVersion
		}
		if migration != nil {
			state := migration.state
			if state.Owner == "" && migration.releaseVideos && migration.hasFileVideo {
				state, err = s.beginLegacyConfigMigration(ctx, tx, owner, pendingLegacyVideos{})
				if err != nil {
					return err
				}
			}
			if migration.releaseVideos && state.Phase == LegacyAwaitingSource {
				if err := s.moveLegacyMigration(ctx, tx, &state, LegacyReconciling); err != nil {
					return err
				}
			}
			if err := s.stageLegacyRecovery(ctx, tx, owner, updated, migration); err != nil {
				return err
			}
			if migration.releaseVideos && state.Phase == LegacyReconciling {
				if err := s.moveLegacyMigration(ctx, tx, &state, LegacyFinalizing); err != nil {
					return err
				}
			}
		}
	}
	updated[ConfigRevisionKey] = next
	raw, err := json.Marshal(updated)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE novel_fetch_workshop_configs SET settings_json=CAST(? AS CHAR CHARACTER SET utf8mb4), updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=?`, string(raw), owner); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	settings[ConfigRevisionKey] = next
	return nil
}

func (s *MySQLStore) ListConfigOwners(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT owner_username FROM novel_fetch_workshop_configs ORDER BY owner_username ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owners := []string{}
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			return nil, err
		}
		owners = append(owners, owner)
	}
	return owners, rows.Err()
}

func (s *MySQLStore) PutRun(ctx context.Context, owner string, record RunRecord) error {
	record = normalizeRunRecord(record)
	// TextModelID 允许为空：空值表示未显式选择模型，运行时按账号默认模型解析。
	if record.RunID == "" || record.BookID == "" || record.Stage == "" || record.Status == "" || record.ModelID == "" {
		return errors.New("run audit fields are required")
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_runs(run_id,owner_username,book_id,stage,status,attempts,text_model_id,model_id,model_display_name,request_id,error_message,started_at,finished_at,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE book_id=VALUES(book_id),stage=VALUES(stage),status=VALUES(status),attempts=VALUES(attempts),text_model_id=VALUES(text_model_id),model_id=VALUES(model_id),model_display_name=VALUES(model_display_name),request_id=VALUES(request_id),error_message=VALUES(error_message),started_at=VALUES(started_at),finished_at=VALUES(finished_at),updated_at=CURRENT_TIMESTAMP(6)`, record.RunID, owner, record.BookID, record.Stage, record.Status, record.Attempts, record.TextModelID, record.ModelID, record.ModelDisplayName, nullableRunString(record.RequestID), nullableRunString(record.ErrorMessage), nullableTime(record.StartedAt), nullableTime(record.FinishedAt), time.Now().UTC())
	return err
}

func (s *MySQLStore) ListRuns(ctx context.Context, owner, bookID string) ([]RunRecord, error) {
	query := `SELECT run_id,book_id,stage,status,attempts,text_model_id,model_id,model_display_name,COALESCE(request_id,''),COALESCE(error_message,''),started_at,finished_at,updated_at FROM novel_fetch_workshop_runs WHERE owner_username=?`
	args := []any{owner}
	if bookID != "" {
		query += ` AND book_id=?`
		args = append(args, bookID)
	}
	query += ` ORDER BY updated_at DESC, run_id ASC`
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []RunRecord{}
	for rows.Next() {
		var record RunRecord
		var startedAt, finishedAt sql.NullTime
		var updatedAt time.Time
		if err := rows.Scan(&record.RunID, &record.BookID, &record.Stage, &record.Status, &record.Attempts, &record.TextModelID, &record.ModelID, &record.ModelDisplayName, &record.RequestID, &record.ErrorMessage, &startedAt, &finishedAt, &updatedAt); err != nil {
			return nil, err
		}
		if startedAt.Valid {
			record.StartedAt = startedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		if finishedAt.Valid {
			record.FinishedAt = finishedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		record.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		result = append(result, record)
	}
	return result, rows.Err()
}

func nullableTime(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return parsed.UTC()
}

func nullableRunString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func (s *MySQLStore) PutBody(ctx context.Context, owner string, body BodyRecord) (BodyRef, error) {
	bookID, versionID := strings.TrimSpace(body.BookID), strings.TrimSpace(body.VersionID)
	if bookID == "" {
		return BodyRef{}, errors.New("book id is required")
	}
	if versionID == "" {
		return BodyRef{}, errors.New("version id is required")
	}
	state := strings.TrimSpace(body.State)
	if state == "" {
		state = "ready"
	}
	blob, hash, charCount, err := encodeBody(body.Content)
	if err != nil {
		return BodyRef{}, err
	}
	encoding := "gzip"
	if s.ObjectBodies != nil {
		key, err := newBodyObjectKey(owner, bookID, versionID)
		if err != nil {
			return BodyRef{}, err
		}
		if err := s.ObjectBodies.PutContent(ctx, key, body.Content); err != nil {
			return BodyRef{}, fmt.Errorf("store novel fetch body in TOS: %w", err)
		}
		encoding, blob = "tos", []byte(key)
	}
	ref, err := s.switchBodyReference(ctx, owner, bookID, versionID, encoding, blob, hash, charCount, state)
	if err != nil && encoding == "tos" {
		// An ambiguous commit must never trigger object deletion.
		log.Printf("novel fetch body reference not confirmed; uploaded object retained id=%x", sha256.Sum256(blob))
	}
	return ref, err
}

func (s *MySQLStore) GetBody(ctx context.Context, owner, bookID, versionID string) (BodyRecord, error) {
	bookID = strings.TrimSpace(bookID)
	versionID = strings.TrimSpace(versionID)
	var ref BodyRef
	ref.VersionID = versionID
	var encoding string
	var blob []byte
	var updated time.Time
	err := s.DB.QueryRowContext(ctx, `SELECT revision, content_encoding, content_blob, content_hash, char_count, state, updated_at FROM novel_fetch_workshop_bodies WHERE owner_username=? AND book_id=? AND version_id=?`, owner, bookID, versionID).Scan(&ref.Revision, &encoding, &blob, &ref.ContentHash, &ref.CharCount, &ref.State, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return BodyRecord{}, ErrNotFound
	}
	if err != nil {
		return BodyRecord{}, err
	}
	var content string
	if encoding == "tos" {
		if s.ObjectBodies == nil {
			return BodyRecord{}, errors.New("novel fetch TOS body store is not configured")
		}
		content, err = s.ObjectBodies.GetContent(ctx, string(blob))
	} else {
		content, err = decodeBody(blob, encoding)
	}
	if err != nil {
		return BodyRecord{}, fmt.Errorf("decode novel fetch body: %w", err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE novel_fetch_workshop_bodies SET last_needed_at=CURRENT_TIMESTAMP(6) WHERE owner_username=? AND book_id=? AND version_id=?`, owner, bookID, versionID); err != nil {
		return BodyRecord{}, err
	}
	ref.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	return BodyRecord{BodyRef: ref, BookID: bookID, Content: content}, nil
}

func (s *MySQLStore) ListBodyRefs(ctx context.Context, owner, bookID string) ([]BodyRef, error) {
	bookID = strings.TrimSpace(bookID)
	rows, err := s.DB.QueryContext(ctx, `SELECT version_id, revision, content_hash, char_count, state, updated_at FROM novel_fetch_workshop_bodies WHERE owner_username=? AND book_id=? ORDER BY version_id ASC`, owner, bookID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	refs := []BodyRef{}
	for rows.Next() {
		var ref BodyRef
		var updated time.Time
		if err := rows.Scan(&ref.VersionID, &ref.Revision, &ref.ContentHash, &ref.CharCount, &ref.State, &updated); err != nil {
			return nil, err
		}
		ref.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
		refs = append(refs, ref)
	}
	return refs, rows.Err()
}

func (s *MySQLStore) DeleteBody(ctx context.Context, owner, bookID, versionID string) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	candidate, err := lockBody(ctx, tx, owner, strings.TrimSpace(bookID), strings.TrimSpace(versionID))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := removeBody(ctx, tx, candidate, time.Now().UTC()); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
