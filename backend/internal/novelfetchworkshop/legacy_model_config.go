package novelfetchworkshop

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"qiantie/backend/internal/apisecrets"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/safepath"
)

const modelLegacyScrubPendingKey = "modelCatalogLegacyScrubPending"
const modelLegacyScrubDoneKey = "modelCatalogLegacyScrubDone"
const legacyVideoMigratedKey = "videoProviderLegacyMigrated"
const legacyModelMigrationVersionKey = "modelCatalogLegacyMigrationVersion"
const legacyModelScrubEntriesKey = "modelCatalogLegacyScrubEntries"
const legacyModelScrubEntryDoneKey = "modelCatalogLegacyScrubEntryDone"
const legacyModelMigrationVersion = "entry-v2"

func hasLegacyVideoProviders(document map[string]any) bool {
	for _, container := range legacyCatalogContainers(document) {
		if _, exists := container["videoProviders"]; exists {
			return true
		}
	}
	return false
}

func hasNestedLegacyModels(document map[string]any) bool {
	for _, container := range legacyCatalogContainers(document)[1:] {
		if _, exists := container["modelCatalog"]; exists {
			return true
		}
	}
	return false
}

func legacyVideoProviderRows(container map[string]any) []any {
	providers, _ := container["videoProviders"].(map[string]any)
	names := make([]string, 0, len(providers))
	for name := range providers {
		names = append(names, name)
	}
	sort.Strings(names)
	rows := make([]any, 0, len(names))
	for _, name := range names {
		fields, _ := providers[name].(map[string]any)
		provider := batchfactoryv11.NormalizeVideoProviderForHTTP(name)
		candidate := batchfactoryv11.VideoProviderConfig{Provider: provider, Model: ModelConfigText(fields, "model"),
			APIKey: ModelConfigText(fields, "apiKey"), CreateURL: ModelConfigText(fields, "createUrl"),
			TasksURL: ModelConfigText(fields, "tasksUrl"), ResultURL: ModelConfigText(fields, "resultUrl"), WorkflowID: ModelConfigText(fields, "workflowId")}
		row, err := batchfactoryv11.NormalizeVideoProviderConfig(candidate)
		model := map[string]any{"id": "legacy-video:" + provider, "kind": "video", "modelId": row.Model,
			"displayName": row.Model, "provider": provider, "endpoint": row.CreateURL, "tasksEndpoint": row.TasksURL,
			"resultEndpoint": row.ResultURL, "workflowId": row.WorkflowID, "credential": row.APIKey, "enabled": true}
		for _, field := range []string{"adapterKind", "catalogSource"} {
			if value, ok := fields[field]; ok {
				model[field] = value
			}
		}
		if err != nil {
			markLegacyKeyReentry(model, "legacy_video_configuration_unusable")
		}
		rows = append(rows, model)
	}
	return rows
}

func legacyCatalogContainers(document map[string]any) []map[string]any {
	nested, _ := document["settings"].(map[string]any)
	app, _ := document["appConfig"].(map[string]any)
	nestedApp, _ := nested["appConfig"].(map[string]any)
	return []map[string]any{document, nested, app, nestedApp}
}

func legacyCatalogRows(document map[string]any) []any {
	var rows []any
	for _, container := range legacyCatalogContainers(document) {
		catalog, _ := container["modelCatalog"].([]any)
		catalog = append(append([]any(nil), catalog...), legacyVideoProviderRows(container)...)
		for _, item := range catalog {
			duplicate := false
			for _, previous := range rows {
				if reflect.DeepEqual(item, previous) {
					duplicate = true
					break
				}
			}
			if !duplicate {
				rows = append(rows, item)
			}
		}
	}
	return rows
}

func preserveBoundLegacyRows(settings map[string]any) ([]any, error) {
	raw := settings["modelCatalog"]
	formal, valid := raw.([]any)
	if raw != nil && !valid {
		return nil, fmt.Errorf("bound model catalog is not a list")
	}
	used := make(map[string]bool, len(formal))
	for _, item := range formal {
		row, _ := item.(map[string]any)
		id := ModelConfigText(row, "id")
		if id == "" || used[id] {
			return nil, fmt.Errorf("bound model row identity could not be verified")
		}
		if ModelConfigText(row, "kind") == "video" {
			for _, field := range []string{"credential", "apiKey"} {
				value, ok := row[field].(string)
				if row[field] != nil && (!ok || value != "" && !strings.HasPrefix(value, apisecrets.BoundPrefix)) {
					return nil, fmt.Errorf("bound video credential could not be verified")
				}
			}
		}
		used[id] = true
	}
	old := cloneMap(settings)
	delete(old, "modelCatalog")
	rows := append([]any(nil), formal...)
	for _, item := range legacyCatalogRows(old) {
		if row, ok := item.(map[string]any); ok {
			if _, conflicts := findLegacyTarget(row, formal); !conflicts {
				rows = append(rows, row)
			}
		}
	}
	return rows, nil
}

func legacyCatalogSecretDigest(document map[string]any) string {
	hasVideoData := false
	for _, container := range legacyCatalogContainers(document) {
		if raw := container["videoProviders"]; raw != nil {
			rows, ok := raw.(map[string]any)
			hasVideoData = hasVideoData || !ok || len(rows) != 0
		}
	}
	hasSecret := hasVideoData
	for _, item := range legacyCatalogRows(document) {
		model, _ := item.(map[string]any)
		for _, field := range []string{"credential", "apiKey", "credentialRef"} {
			if _, present := model[field]; present {
				hasSecret = true
			}
		}
	}
	if !hasSecret {
		return ""
	}
	if !hasVideoData {
		rows, _ := json.Marshal(legacyCatalogRows(document))
		sum := sha256.Sum256(rows)
		return hex.EncodeToString(sum[:])
	}
	parts := []any{legacyCatalogRows(document)}
	for _, container := range legacyCatalogContainers(document) {
		parts = append(parts, container["videoProviders"])
	}
	rows, _ := json.Marshal(parts)
	sum := sha256.Sum256(rows)
	return hex.EncodeToString(sum[:])
}

func priorLegacyModelDigest(document map[string]any) string {
	copy := cloneMap(document)
	for _, container := range legacyCatalogContainers(copy) {
		delete(container, "videoProviders")
	}
	return legacyCatalogSecretDigest(copy)
}

func (s *MySQLStore) legacyModelFilePath(owner string) (string, error) {
	if s.LegacyConfigDir == "" {
		return "", nil
	}
	return safepath.UserPath(s.LegacyConfigDir, owner, "api-config.json")
}

func readLegacyModelDocument(path string) (map[string]any, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	for _, container := range legacyCatalogContainers(document) {
		if raw := container["modelCatalog"]; raw != nil {
			if _, ok := raw.([]any); !ok {
				return nil, fmt.Errorf("legacy model catalog is not a list")
			}
		}
	}
	return document, nil
}

type legacyScrubEntry struct {
	Container      int               `json:"container"`
	Index          int               `json:"index"`
	Video          bool              `json:"video,omitempty"`
	Provider       string            `json:"provider,omitempty"`
	Digest         string            `json:"digest"`
	TargetID       string            `json:"targetId,omitempty"`
	RecoveryReason string            `json:"recoveryReason,omitempty"`
	ConflictIDs    map[string]string `json:"conflictIds,omitempty"`
}

type legacySourceEntry struct {
	legacyScrubEntry
	raw, model map[string]any
}

type legacyConfigMigration struct {
	path                        string
	state                       LegacyConfigMigrationState
	releaseVideos, hasFileVideo bool
	file, database              []legacySourceEntry
	videoMatches                []legacyVideoMatch
	fileModelIDs                map[legacyVideoPosition]string
}

func legacyEntryDigest(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func hasLegacyKey(row map[string]any) bool {
	for _, field := range []string{"credential", "apiKey", "credentialRef"} {
		if _, ok := row[field]; ok {
			return true
		}
	}
	return false
}

func findLegacyTarget(row map[string]any, rows []any) (map[string]any, bool) {
	var match map[string]any
	id := ModelConfigText(row, "id")
	modelID := ModelConfigText(row, "modelId")
	for _, item := range rows {
		candidate, _ := item.(map[string]any)
		candidateID := ModelConfigText(candidate, "id")
		sameScope := sameLegacyModelScope(row, candidate) && modelID != "" && modelID == ModelConfigText(candidate, "modelId")
		sameID := id != "" && id == candidateID
		if !sameID && (!sameScope || id != "" && candidateID != "") {
			continue
		}
		if !sameScope || match != nil {
			return nil, true
		}
		match = candidate
	}
	return match, match != nil
}

func prepareLegacyModelEntries(rows []any, legacy map[string]any, sources []legacySourceEntry, preserveBoundRows bool) ([]any, []legacySourceEntry) {
	preserved := map[string]bool{}
	if preserveBoundRows {
		for _, item := range rows {
			row, _ := item.(map[string]any)
			preserved[ModelConfigText(row, "id")] = true
		}
	}
	fileRows := legacyCatalogRows(legacy)
	var imports []any
	for _, item := range fileRows {
		if row, ok := item.(map[string]any); ok {
			if _, found := findLegacyTarget(row, rows); !found {
				incoming := cloneMap(row)
				delete(incoming, "credentialStatus")
				delete(incoming, "credentialRecoveryReason")
				if ModelConfigText(incoming, "credentialRef") != "" &&
					ModelConfigText(incoming, "credential") == "" && ModelConfigText(incoming, "apiKey") == "" {
					markLegacyKeyReentry(incoming, "legacy_reference_requires_key")
				}
				imports = append(imports, incoming)
			}
		}
	}
	rows = normalizeLegacyModelRows(append(rows, imports...))
	for i := range sources {
		entry := &sources[i]
		if target, _ := findLegacyTarget(entry.model, rows); target != nil {
			entry.TargetID = ModelConfigText(target, "id")
			if !preserved[entry.TargetID] {
				entry.RecoveryReason = ModelConfigText(target, "credentialRecoveryReason")
			}
		}
	}
	return rows, sources
}

func collectLegacyVideoCandidates(document map[string]any) []legacySourceEntry {
	return collectLegacyEntries(document, true)
}

func collectLegacySources(document map[string]any) []legacySourceEntry {
	return collectLegacyEntries(document, false)
}

func collectLegacyEntries(document map[string]any, videosOnly bool) []legacySourceEntry {
	var entries []legacySourceEntry
	for i, container := range legacyCatalogContainers(document) {
		catalog, _ := container["modelCatalog"].([]any)
		for index, item := range catalog {
			if row, ok := item.(map[string]any); ok && ((videosOnly && ModelConfigText(row, "kind") == "video") || (!videosOnly && hasLegacyKey(row))) {
				source := cloneMap(row)
				entries = append(entries, legacySourceEntry{legacyScrubEntry: legacyScrubEntry{Container: i, Index: index, Digest: legacyEntryDigest(row)}, raw: source, model: source})
			}
		}
		providers, _ := container["videoProviders"].(map[string]any)
		names := make([]string, 0, len(providers))
		for name := range providers {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			source := legacyVideoProviderRows(map[string]any{"videoProviders": map[string]any{name: providers[name]}})[0].(map[string]any)
			raw, _ := providers[name].(map[string]any)
			entries = append(entries, legacySourceEntry{legacyScrubEntry: legacyScrubEntry{Container: i, Video: true, Provider: name, Digest: legacyEntryDigest(providers[name])},
				raw: cloneMap(raw), model: source})
		}
	}
	return entries
}

// Reconcile all legacy origins before a staged video row can become an
// executable model. The migration still retains each original source for the
// independent encrypted archive and cleanup checks.
func reconcileLegacyVideoCredentials(rows, boundRows []any, sourceGroups ...[]legacySourceEntry) {
	type videoChoice struct {
		row         map[string]any
		key         string
		fingerprint [32]byte
		blocked     bool
	}
	bound := make(map[string]bool, len(boundRows))
	for _, item := range boundRows {
		row, _ := item.(map[string]any)
		bound[ModelConfigText(row, "id")] = true
	}
	var choices []*videoChoice
	for _, item := range rows {
		row, _ := item.(map[string]any)
		if ModelConfigText(row, "kind") == "video" {
			choices = append(choices, &videoChoice{row: row})
		}
	}
	for _, sources := range sourceGroups {
		for _, source := range sources {
			if ModelConfigText(source.model, "kind") != "video" {
				continue
			}
			id := source.TargetID
			if id == "" {
				id = ModelConfigText(source.model, "id")
			}
			var matches []*videoChoice
			for _, choice := range choices {
				if id != "" && id == ModelConfigText(choice.row, "id") || id == "" && sameLegacyVideoTarget(source.model, choice.row) {
					matches = append(matches, choice)
				}
			}
			keys, valid := legacySourceCredentials(source)
			for _, choice := range matches {
				if len(matches) != 1 || !valid || !sameLegacyVideoTarget(source.model, choice.row) ||
					len(keys) != 0 && source.model["credentialStatus"] == "needs_reentry" {
					choice.blocked = true
					continue
				}
				for _, key := range keys {
					fingerprint := sha256.Sum256([]byte(key))
					if choice.key == "" {
						choice.key, choice.fingerprint = key, fingerprint
					} else if choice.fingerprint != fingerprint || choice.key != key {
						choice.blocked = true
					}
				}
			}
		}
	}
	for _, choice := range choices {
		row := choice.row
		// Existing bound rows keep both their ciphertext and deliberate clears.
		if bound[ModelConfigText(row, "id")] {
			continue
		}
		if choice.blocked {
			markLegacyKeyReentry(row, "legacy_video_source_conflict")
		} else if choice.key != "" && ModelConfigText(row, "provider") != batchfactoryv11.VideoProviderDoubaoLocal {
			row["credential"] = choice.key
			delete(row, "apiKey")
			delete(row, "credentialRef")
			delete(row, "credentialStatus")
			delete(row, "credentialRecoveryReason")
		}
	}
}

func sameLegacyVideoTarget(source, target map[string]any) bool {
	modelID := ModelConfigText(source, "modelId")
	return modelID != "" && modelID == ModelConfigText(target, "modelId") &&
		sameLegacyModelScope(source, target) && ModelConfigText(source, "workflowId") == ModelConfigText(target, "workflowId")
}

func verifyLegacyScrubTarget(source, target map[string]any, recoveryReason string) bool {
	if !sameLegacyModelScope(source, target) || ModelConfigText(source, "modelId") != ModelConfigText(target, "modelId") {
		return false
	}
	if target["credentialStatus"] == "needs_reentry" {
		return recoveryReason != "" && ModelConfigText(target, "credentialRecoveryReason") == recoveryReason &&
			ModelConfigText(target, "credential") == "" && ModelConfigText(target, "apiKey") == "" &&
			ModelConfigText(source, "credential") == "" && ModelConfigText(source, "apiKey") == ""
	}
	credential := ModelConfigText(target, "credential")
	if credential == "" {
		return !hasLegacyCredentialValue(source)
	}
	for _, field := range []string{"credential", "apiKey"} {
		value, ok := source[field].(string)
		if source[field] != nil && !ok || strings.TrimSpace(value) != "" && strings.TrimSpace(value) != credential {
			return false
		}
	}
	return ModelConfigText(source, "credential") == credential || ModelConfigText(source, "apiKey") == credential
}

func hasLegacyCredentialValue(row map[string]any) bool {
	for _, field := range []string{"credential", "apiKey", "credentialRef"} {
		raw := row[field]
		if value, ok := raw.(string); raw != nil && (!ok || strings.TrimSpace(value) != "") {
			return true
		}
	}
	return false
}

func (s *MySQLStore) applyLegacyScrubEntry(ctx context.Context, tx *sql.Tx, owner, document string, containers []map[string]any, entry legacyScrubEntry, targets map[string]map[string]any) bool {
	if entry.Container < 0 || entry.Container >= len(containers) || entry.TargetID == "" && len(entry.ConflictIDs) == 0 {
		return false
	}
	container := containers[entry.Container]
	if entry.Video {
		providers, _ := container["videoProviders"].(map[string]any)
		value, present := providers[entry.Provider]
		if !present || legacyEntryDigest(value) != entry.Digest {
			return false
		}
		source := legacyVideoProviderRows(map[string]any{"videoProviders": map[string]any{entry.Provider: value}})[0].(map[string]any)
		fields, ok := value.(map[string]any)
		if !ok {
			return false
		}
		if !s.verifyLegacySource(ctx, tx, owner, document, legacySourceEntry{legacyScrubEntry: entry, raw: fields, model: source}, targets[entry.TargetID]) {
			return false
		}
		delete(providers, entry.Provider)
		if len(providers) == 0 {
			delete(container, "videoProviders")
		}
		return true
	}
	catalog, _ := container["modelCatalog"].([]any)
	if entry.Index < 0 || entry.Index >= len(catalog) {
		return false
	}
	row, _ := catalog[entry.Index].(map[string]any)
	if legacyEntryDigest(row) != entry.Digest || !s.verifyLegacySource(ctx, tx, owner, document, legacySourceEntry{legacyScrubEntry: entry, raw: row, model: row}, targets[entry.TargetID]) {
		return false
	}
	for _, key := range []string{"credential", "apiKey", "credentialRef"} {
		delete(row, key)
	}
	row["credentialStatus"] = "imported_or_needs_reentry"
	return true
}

// Hold the owner config and migration row locks across file work. A reader
// may have loaded its settings before a different reader committed a new
// scrub plan. Validate both snapshots before using targets or touching files.
func (s *MySQLStore) finishLegacyMigration(ctx context.Context, owner string, settings map[string]any, state LegacyConfigMigrationState) error {
	if s.LegacyConfigLocker != nil {
		s.LegacyConfigLocker.Lock()
		defer s.LegacyConfigLocker.Unlock()
	}
	path, err := s.legacyModelFilePath(owner)
	if err != nil {
		return err
	}
	if path == "" {
		return fmt.Errorf("legacy config directory unavailable during finalization")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=? FOR UPDATE`, owner).Scan(&raw); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var current map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &current); err != nil {
			return err
		}
	}
	locked, found, err := readMigrationState(tx.QueryRowContext(ctx, migrationStateQuery, owner))
	if err != nil {
		return err
	}
	if found != (state.Owner != "") || locked.Phase != state.Phase || configRevision(current) != configRevision(settings) ||
		!reflect.DeepEqual(current[modelLegacyScrubPendingKey], settings[modelLegacyScrubPendingKey]) || !reflect.DeepEqual(current[legacyModelScrubEntriesKey], settings[legacyModelScrubEntriesKey]) {
		return ErrConfigConflict
	}
	expected := ModelConfigText(current, modelLegacyScrubPendingKey)
	if expected == "" && current[legacyModelScrubEntriesKey] != nil {
		return ErrConfigConflict
	}
	if found && locked.Phase != LegacyFinalizing && locked.Phase != LegacyComplete {
		return ErrConfigConflict
	}
	if expected != "" {
		if err := s.scrubLegacyModelFile(ctx, tx, owner, path, settings); err != nil {
			return err
		}
	}
	if expected != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE novel_fetch_workshop_configs SET settings_json=JSON_REMOVE(settings_json,'$.modelCatalogLegacyScrubPending','$.modelCatalogLegacyScrubEntries') WHERE owner_username=?`, owner); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return s.importAndCompleteLegacySettings(ctx, owner, path, locked.Phase)
}

// Ordinary reads only check for an old journal left by a previous release.
func (s *MySQLStore) resumeArchivedSettingFile(ctx context.Context, owner string, phase LegacyConfigMigrationPhase) error {
	if s.LegacyConfigDir == "" {
		return nil
	}
	if s.LegacyConfigLocker != nil {
		s.LegacyConfigLocker.Lock()
		defer s.LegacyConfigLocker.Unlock()
	}
	path, err := s.legacyModelFilePath(owner)
	if err != nil {
		return err
	}
	return s.importAndCompleteLegacySettings(ctx, owner, path, phase)
}

func (s *MySQLStore) importAndCompleteLegacySettings(ctx context.Context, owner, path string, phase LegacyConfigMigrationPhase) error {
	imported := false
	if importer, ok := s.ModelSecrets.(interface {
		ImportDeferredSettings(context.Context, string, string, *MySQLStore) (bool, error)
	}); ok {
		var err error
		imported, err = importer.ImportDeferredSettings(ctx, owner, path, s)
		if err != nil {
			return err
		}
	}
	if phase != LegacyFinalizing && (!imported || phase == LegacyAwaitingSource || phase == LegacyReconciling) {
		return nil
	}
	// Serialize competing standard-file writers. Appends only lock the
	// migration row and therefore remain possible between writing and CAS.
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// The per-account writer row exists even for an imported settings file
	// whose owner has never opened the novel workshop. An append only locks the
	// separate migration row, allowing its revision to change while we write.
	if _, err := tx.ExecContext(ctx, `INSERT INTO novel_fetch_legacy_setting_files(owner_username,fingerprint) VALUES(?,'')
 ON DUPLICATE KEY UPDATE fingerprint=fingerprint`, owner); err != nil {
		return err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=? FOR UPDATE`, owner).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if len(raw) > 0 {
		var latest map[string]any
		if err := json.Unmarshal(raw, &latest); err != nil {
			return err
		}
		if ModelConfigText(latest, modelLegacyScrubPendingKey) != "" || latest[legacyModelScrubEntriesKey] != nil {
			return ErrConfigConflict
		}
	}
	for {
		latest, found, err := readMigrationState(tx.QueryRowContext(ctx, migrationStateQuery, owner))
		if err != nil {
			return err
		}
		if !found || latest.Phase == LegacyComplete {
			break
		}
		if latest.Phase != LegacyFinalizing {
			return ErrConfigConflict
		}
		edits, err := s.openSettingEdits(owner, latest)
		if err != nil {
			return err
		}
		writer, ok := s.ModelSecrets.(interface {
			ApplyLegacySettingEdits(string, string, []LegacySettingEdit) error
		})
		if !ok {
			return errors.New("no standard settings writer available")
		}
		if err := writer.ApplyLegacySettingEdits(owner, path, edits); err != nil {
			return err
		}

		complete, err := completeLegacySettingEdits(ctx, tx, owner, latest.Revision)
		if err != nil {
			return err
		}
		if complete {
			break
		}
	}
	return tx.Commit()
}

func (s *MySQLStore) scrubLegacyModelFile(ctx context.Context, tx *sql.Tx, owner, path string, settings map[string]any) error {
	document, err := readLegacyModelDocument(path)
	if err != nil {
		return err
	}
	if document == nil {
		return fmt.Errorf("legacy model file is unavailable during credential cleanup")
	}
	expected := ModelConfigText(settings, modelLegacyScrubPendingKey)
	encoded, _ := json.Marshal(settings[legacyModelScrubEntriesKey])
	var entries []legacyScrubEntry
	if err := json.Unmarshal(encoded, &entries); err != nil {
		return err
	}
	targets := map[string]map[string]any{}
	for _, value := range legacyCatalogRows(settings) {
		row, _ := value.(map[string]any)
		targets[ModelConfigText(row, "id")] = row
	}
	completed, _ := document[legacyModelScrubEntryDoneKey].(map[string]any)
	if completed == nil {
		completed = map[string]any{}
	}
	changed, unresolved := false, false
	containers := legacyCatalogContainers(document)
	for _, entry := range entries {
		receipt := legacyEntryDigest(entry)
		if completed[receipt] == expected {
			if !s.legacyConflictRecordsIntact(ctx, tx, owner, entry.ConflictIDs) {
				unresolved = true
			}
			continue
		}
		if !s.applyLegacyScrubEntry(ctx, tx, owner, fileConflictDocument(path), containers, entry, targets) {
			unresolved = true
			continue
		}
		completed[receipt] = expected
		changed = true
	}
	if changed {
		document[legacyModelScrubEntryDoneKey] = completed
		replace := s.ReplaceLegacyConfig
		if replace == nil {
			replace = replaceLegacyModelDocument
		}
		if err := replace(path, document); err != nil {
			return err
		}
	}
	if unresolved || legacyCatalogSecretDigest(document) != "" {
		return fmt.Errorf("legacy credentials still require verified recovery; original entries retained")
	}
	if len(entries) == 0 && ModelConfigText(document, modelLegacyScrubDoneKey) != expected {
		return fmt.Errorf("legacy credential cleanup has no matching completion record")
	}
	return nil
}

func replaceLegacyModelDocument(path string, document map[string]any) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".api-config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(document); err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err != nil || closeErr != nil {
		return errors.Join(err, closeErr)
	}
	return os.Rename(temporary.Name(), path)
}

// ResumeLegacyModelCleanup retries previously committed file cleanup after a restart.
func (s *MySQLStore) ResumeLegacyModelCleanup(ctx context.Context) error {
	// Include accounts that predate the migration table. GetConfig discovers
	// their state with the same decoder used by preference saves.
	rows, err := s.DB.QueryContext(ctx, `SELECT BINARY owner_username FROM novel_fetch_workshop_configs
 UNION SELECT BINARY owner_username FROM novel_fetch_legacy_config_migrations WHERE phase!='complete'`)
	if err != nil {
		return err
	}
	var pending []string
	for rows.Next() {
		var owner string
		if err := rows.Scan(&owner); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, owner)
	}
	err = rows.Err()
	rows.Close()
	for _, owner := range pending {
		if _, e := s.GetConfig(ctx, owner); e != nil {
			err = errors.Join(err, fmt.Errorf("clean legacy model configuration: %w", e))
		}
	}
	return err
}

func sameLegacyModelScope(left, right map[string]any) bool {
	target := ModelCredentialDestination(left)
	kind := ModelConfigText(left, "kind")
	local := kind == "video" && ModelConfigText(left, "provider") == batchfactoryv11.VideoProviderDoubaoLocal &&
		ModelConfigText(right, "provider") == batchfactoryv11.VideoProviderDoubaoLocal
	return kind != "" && (target.Endpoint != "" || local) && kind == ModelConfigText(right, "kind") &&
		ModelCredentialSource(left) == ModelCredentialSource(right) && target == ModelCredentialDestination(right)
}

func legacyModelCredentialCandidate(current map[string]any, legacy []any) map[string]any {
	indices, compatible := legacyModelCredentialCandidates(current, legacy)
	if !compatible || len(indices) != 1 {
		return nil
	}
	result, _ := legacy[indices[0]].(map[string]any)
	return result
}

func legacyModelCredentialCandidates(current map[string]any, legacy []any) ([]int, bool) {
	id := ModelConfigText(current, "id")
	modelID := ModelConfigText(current, "modelId")
	var matches []int
	compatible := true
	if id != "" {
		for i, item := range legacy {
			candidate, _ := item.(map[string]any)
			if ModelConfigText(candidate, "id") != id {
				continue
			}
			matches = append(matches, i)
			compatible = compatible && sameLegacyModelScope(current, candidate) && modelID != "" && modelID == ModelConfigText(candidate, "modelId")
		}
		if len(matches) != 0 {
			return matches, compatible
		}
	}
	for i, item := range legacy {
		candidate, _ := item.(map[string]any)
		candidateID := ModelConfigText(candidate, "id")
		if !sameLegacyModelScope(current, candidate) || modelID == "" || modelID != ModelConfigText(candidate, "modelId") {
			continue
		}
		matches = append(matches, i)
		compatible = compatible && (id == "" || candidateID == "")
	}
	return matches, compatible
}

func mergeLegacyModelKeys(current, legacy []any) {
	type claim struct {
		model      map[string]any
		candidates []int
		compatible bool
	}
	claims := make([]claim, 0, len(current))
	claimed := make([]int, len(legacy))
	for _, item := range current {
		model, ok := item.(map[string]any)
		if !ok {
			continue
		}
		candidates, compatible := legacyModelCredentialCandidates(model, legacy)
		for _, candidate := range candidates {
			claimed[candidate]++
		}
		claims = append(claims, claim{model, candidates, compatible})
	}
	for _, claim := range claims {
		model := claim.model
		if len(claim.candidates) == 0 {
			if ModelConfigText(model, "credential") == "" && ModelConfigText(model, "apiKey") == "" {
				markLegacyKeyReentry(model, "legacy_missing")
			}
			continue
		}
		if !claim.compatible || len(claim.candidates) != 1 || claimed[claim.candidates[0]] != 1 {
			markLegacyKeyReentry(model, "legacy_scope_or_mapping_conflict")
			continue
		}
		match, _ := legacy[claim.candidates[0]].(map[string]any)
		keys := map[string]struct{}{}
		for _, row := range []map[string]any{model, match} {
			for _, key := range []string{"credential", "apiKey"} {
				if value := ModelConfigText(row, key); value != "" {
					keys[value] = struct{}{}
				}
			}
		}
		if len(keys) != 1 {
			markLegacyKeyReentry(model, "legacy_credential_conflict")
			continue
		}
		for _, key := range []string{"credential", "apiKey"} {
			if value := ModelConfigText(match, key); value != "" {
				model[key] = value
			}
		}
	}
}

func markLegacyKeyReentry(model map[string]any, reason string) {
	delete(model, "credential")
	delete(model, "apiKey")
	delete(model, "credentialRef")
	model["credentialStatus"] = "needs_reentry"
	model["credentialRecoveryReason"] = reason
}

func legacyModelRowID(model map[string]any) string {
	if id := ModelConfigText(model, "id"); id != "" {
		return id
	}
	raw, _ := json.Marshal([]any{ModelConfigText(model, "kind"), ModelConfigText(model, "modelId"), ModelCredentialSource(model), ModelCredentialDestination(model)})
	sum := sha256.Sum256(raw)
	return "legacy:" + hex.EncodeToString(sum[:16])
}

func normalizeLegacyModelRows(rows []any) []any {
	catalog := make([]any, 0, len(rows))
	byID := make(map[string]map[string]any, len(rows))
	for _, item := range rows {
		model, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id := legacyModelRowID(model)
		if ModelConfigText(model, "id") == "" {
			model["id"] = id
		}
		if prior := byID[id]; prior != nil {
			markLegacyKeyReentry(prior, "legacy_duplicate_row")
			continue
		}
		byID[id] = model
		catalog = append(catalog, model)
	}
	return catalog
}
