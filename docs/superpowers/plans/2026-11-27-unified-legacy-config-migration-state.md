# Unified Legacy Configuration Migration State Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace three inconsistent legacy-configuration guesses with one authoritative MySQL state machine and a field-level settings patch ledger, so delayed legacy files cannot activate the wrong video credential or discard acknowledged user settings.

**Architecture:** Migration `1200007` adds one owner-scoped state row. The row stores the migration phase, an account-bound encrypted snapshot of every database video candidate, an account-bound encrypted field-edit journal, and a monotonic revision. Video reconciliation and preference saves consult only this row; file contents are evidence, never migration state. Finalization writes the restored standard file first, then completes by revision compare-and-swap, so a crash or concurrent edit is retried without losing data.

**Tech Stack:** Go 1.26, MySQL 8.4, existing `novelfetchworkshop` MySQL store, existing model-secret AEAD bindings, existing React-independent HTTP tests.

## Global Constraints

- Work directly on Git branch `v88`; do not create a branch or worktree.
- Preserve all unrelated dirty files and commits; stage only files owned by the active task.
- Use migration version `1200007`; recheck it is unoccupied immediately before implementation.
- Use only synthetic credentials and disposable MySQL databases. Do not call external providers and do not operate shared port 4001.
- A database-only installation (`LegacyConfigDir == ""`) keeps its existing single-source migration behavior and must not wait for a file.
- A file-backed installation creates authoritative state from every legacy video catalog row, including rows with no credential field.
- File JSON booleans, file existence and model ID alone never complete migration.
- Legacy whole-group deferred snapshots never imply deletion. Empty/default fields from those snapshots preserve the restored original value.
- New setting requests record only fields explicitly present in the validated request; explicit deletion uses a tombstone.
- Internal state, ciphertext, fingerprints and revisions never appear in public or restricted configuration responses.
- Do not continue to personal-center API configuration frontend until both tasks have 0 Critical and 0 Important findings.

## File Structure

- Create `backend/internal/storage/legacy_config_migration_schema.go`: migration `1200007` table definition.
- Create `backend/internal/storage/legacy_config_migration_schema_test.go`: checksum, registration and MySQL 8.4 migration tests.
- Create `backend/internal/novelfetchworkshop/legacy_config_migration_state.go`: state model, encrypted candidate snapshot and compare-and-swap persistence.
- Modify `backend/internal/novelfetchworkshop/legacy_video_pending.go`: use the authoritative row; remove SQL/JSON inference.
- Modify `backend/internal/novelfetchworkshop/mysql_store.go`: begin, reconcile and finish migration through the state row.
- Modify `backend/internal/novelfetchworkshop/legacy_model_config.go`: collect all video candidates independent of credential presence.
- Modify `backend/internal/httpapi/user_config_patch.go`: use shared field-edit representation and conservative legacy conversion.
- Modify `backend/internal/httpapi/user_config_deferred.go`: replace the local deferred file as the active journal with MySQL append/load/CAS completion; retain one-time import compatibility.
- Modify the existing settings writers/readers only where required to pass explicit edits and propagate journal errors.
- Test in `backend/internal/httpapi/api_config_migration_completion_test.go` and focused `novelfetchworkshop`/storage test files.

---

### Task 1: Authoritative MySQL Video-Migration State

**Files:**
- Create: `backend/internal/storage/legacy_config_migration_schema.go`
- Create: `backend/internal/storage/legacy_config_migration_schema_test.go`
- Create: `backend/internal/novelfetchworkshop/legacy_config_migration_state.go`
- Modify: `backend/internal/storage/novel_fetch_workshop_schema.go`
- Modify: `backend/internal/novelfetchworkshop/legacy_video_pending.go`
- Modify: `backend/internal/novelfetchworkshop/legacy_model_config.go`
- Modify: `backend/internal/novelfetchworkshop/mysql_store.go`
- Test: `backend/internal/novelfetchworkshop/legacy_config_migration_state_test.go`
- Test: `backend/internal/httpapi/api_config_migration_completion_test.go`

**Interfaces:**
- Define phases `awaiting_source`, `reconciling`, `finalizing`, `complete` as `LegacyConfigMigrationPhase`.
- Define `LegacyConfigMigrationState` with `Owner`, `Phase`, `CandidateEnvelope`, `CandidateFingerprint`, `PatchEnvelope`, `PatchFingerprint`, `Revision`, `CreatedAt`, `UpdatedAt`.
- Produce methods:

```go
func (s *MySQLStore) LegacyConfigMigrationState(ctx context.Context, owner string) (LegacyConfigMigrationState, bool, error)
func (s *MySQLStore) HasPendingLegacyVideos(ctx context.Context, owner string) (bool, error)
func (s *MySQLStore) beginLegacyConfigMigration(ctx context.Context, tx *sql.Tx, owner string, candidates pendingLegacyVideos) (LegacyConfigMigrationState, error)
func (s *MySQLStore) advanceLegacyConfigMigration(ctx context.Context, tx *sql.Tx, owner string, fromRevision uint64, phase LegacyConfigMigrationPhase) (bool, error)
```

- `CandidateEnvelope` is produced with existing model-secret encryption bound to owner, document `db:legacy_config_migrations`, field `video_candidates`.
- `HasPendingLegacyVideos` reads only the table row and returns true for all phases except `complete`; it must not inspect arbitrary JSON paths.

- [ ] **Step 1: Write the failing schema and state tests**

Add tests asserting migration `1200007` creates:

```sql
CREATE TABLE IF NOT EXISTS novel_fetch_legacy_config_migrations (
  owner_username VARCHAR(191) NOT NULL,
  phase VARCHAR(32) NOT NULL,
  candidate_ciphertext LONGTEXT NULL,
  candidate_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
  patch_ciphertext LONGTEXT NULL,
  patch_fingerprint CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
  revision BIGINT UNSIGNED NOT NULL DEFAULT 1,
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (owner_username),
  KEY idx_legacy_config_migrations_phase (phase, updated_at)
) ENGINE=InnoDB
```

Assert checksum registration exactly once and idempotent MySQL 8.4 execution.

- [ ] **Step 2: Write failing behavior tests**

Add real MySQL tests for:

```go
func TestLegacyConfigMigrationStateCoversAllVideoCandidates(t *testing.T)
func TestLegacyConfigMigrationStateIsAccountAndPurposeBound(t *testing.T)
func TestLegacyConfigMigrationStateRevisionCAS(t *testing.T)
```

The first test must seed a top-level legacy `modelCatalog` video row with model/address but no `credential`, `apiKey` or `credentialRef`; with a normal preference file present before first read, it must enter `awaiting_source`, stay out of executable `modelCatalog`, and make `HasPendingLegacyVideos` true. Also cover top-level `videoProviders`, nested catalogs and a database-only store.

- [ ] **Step 3: Run RED and retain raw output**

Run from `backend`:

```powershell
go test ./internal/storage ./internal/novelfetchworkshop ./internal/httpapi -run 'TestLegacyConfigMigrationState|TestAPIConfigPolicyMySQLDeferredMigrationCompletion' -count=1 -v
```

Expected: schema/state symbols are missing and the no-credential first-read case completes incorrectly.

- [ ] **Step 4: Implement migration and state persistence**

Register `1200007`. Persist candidate rows and their original source positions in the encrypted envelope. Lock the owner row with `SELECT ... FOR UPDATE`; create once, increment `revision` on every state mutation, and reject invalid phase transitions.

- [ ] **Step 5: Make candidate discovery identical everywhere**

Create one helper used by initial discovery and retry:

```go
func collectLegacyVideoCandidates(document map[string]any) []legacySourceEntry
```

It includes every `kind == "video"` catalog row and every legacy `videoProviders` row, even when no credential field exists. Reconciliation still uses full provider/model/address/workflow/adapter identity and credential evidence. Remove decision-making authority from `LegacyModelDefaultsFileKey` and the old pending-JSON SQL predicate.

- [ ] **Step 6: Route migration phases through the authoritative row**

For file-backed stores, create `awaiting_source` before evaluating the file. A matching source advances to `reconciling`; conflict archiving and scrub-plan persistence occur before `finalizing`. Existing formal rows remain formal and are never placed in the candidate envelope. Database-only stores keep the existing direct migration behavior and do not create a waiting row.

- [ ] **Step 7: Run GREEN and regression tests**

Run the RED command plus:

```powershell
go test ./internal/httpapi -run '^(TestAPIConfigPolicy|TestCatalogVideo|Test.*Legacy.*Video|TestAPIConfigPolicyMySQLDeferredMigrationCompletion)' -count=1
go test ./internal/novelfetchworkshop -run 'Test.*(Legacy|ModelConfig|ConfigRevision|MigrationState)' -count=1
```

Expected: no-credential, wrong-model/address/workflow, direct restore, same-Key, existing formal row, two-source archive/recovery/audit and failure rollback all pass with zero skips.

- [ ] **Step 8: Commit Task 1**

Stage only Task 1 files and commit:

```text
refactor: centralize legacy config migration state
```

---

### Task 2: MySQL Field-Level Preference Patch Journal

**Files:**
- Modify: `backend/internal/novelfetchworkshop/legacy_config_migration_state.go`
- Modify: `backend/internal/httpapi/user_config_patch.go`
- Modify: `backend/internal/httpapi/user_config_deferred.go`
- Modify: `backend/internal/httpapi/user_config.go`
- Modify: `backend/internal/httpapi/api_integrations.go`
- Modify: `backend/internal/httpapi/storage_legacy.go`
- Modify: `backend/internal/httpapi/member_usage.go`
- Modify: `backend/internal/httpapi/novel_fetch_legacy.go`
- Test: `backend/internal/httpapi/api_config_migration_completion_test.go`

**Interfaces:**
- Move/share the journal representation as:

```go
type LegacySettingEdit struct {
    Path   []string        `json:"path"`
    Value  json.RawMessage `json:"value,omitempty"`
    Delete bool            `json:"delete,omitempty"`
}

func (s *MySQLStore) AppendLegacySettingEdits(ctx context.Context, owner string, edits []LegacySettingEdit) error
func (s *MySQLStore) LoadLegacySettingEdits(ctx context.Context, owner string) ([]LegacySettingEdit, uint64, bool, error)
func (s *MySQLStore) CompleteLegacySettingEdits(ctx context.Context, owner string, revision uint64) (bool, error)
```

- Appending locks the state row, decrypts the existing patch, compacts edits by exact path, encrypts the result with binding document `db:legacy_config_migrations`, field `setting_patch`, and increments revision.
- Completion is compare-and-swap: update to `complete` and clear patch fields only when `revision` still equals the revision applied to disk. A false return means a concurrent edit arrived; reload and replay.

- [ ] **Step 1: Write failing journal tests**

Add tests covering:

```go
func TestLegacySettingPatchJournalCompactsExplicitEdits(t *testing.T)
func TestLegacySettingPatchJournalCASPreservesConcurrentEdit(t *testing.T)
func TestLegacySettingPatchJournalRejectsCrossAccountAndPurpose(t *testing.T)
func TestLegacyDeferredSnapshotConversionIsConservative(t *testing.T)
```

The conservative conversion fixture must contain an old whole-channel snapshot whose generated `model`, `baseUrl`, `concurrency` and empty arrays were not explicitly submitted. Only non-empty proven assignments may become edits; empty/default values must not become tombstones.

- [ ] **Step 2: Write failing end-to-end preference tests**

Through authenticated routes, cover:

1. First preference save occurs before model-config first read while top-level legacy `modelCatalog` exists.
2. Save volume `42`, muted state, one pinned item, one explicit API value deletion and one channel rename.
3. Restore the actual legacy file containing unrelated channels, Key values and volume `60`.
4. Reconcile video conflicts, simulate a standard-file write interruption, retry from a cold store, and simulate a new concurrent edit between file write and CAS completion.
5. Assert final standard config preserves `42`, explicit deletion and rename, retains untouched original fields/channels, contains no internal envelope fields, and the MySQL state reaches `complete` only after the latest revision is applied.

- [ ] **Step 3: Run RED and retain raw output**

Run:

```powershell
go test ./internal/httpapi -run 'TestAPIConfigPolicyMySQLDeferredMigrationCompletion|TestLegacySettingPatch' -count=1 -v
```

Expected: pre-first-read save bypasses the journal, old snapshots erase original fields, or completion loses a concurrent edit.

- [ ] **Step 4: Implement MySQL patch append/load/CAS**

Replace active writes to `deferred-settings.json` with the table journal. Only validated request keys produce edits. Missing keys produce no edit. Explicit clear/delete produces `Delete:true`. Do not serialize default-filled response objects into the journal.

- [ ] **Step 5: Import old deferred files conservatively**

If `deferred-settings.json` exists, decrypt with its existing binding. For the new edit-array format, append directly. For an old map snapshot, convert only non-empty, non-default assignments whose presence proves intent; never infer deletion from `nil`, empty string, empty list, zero concurrency or generated defaults. Append to MySQL first, verify by readback, then remove the old file. Interruption leaves the file for an idempotent retry.

- [ ] **Step 6: Finalize with replay and revision CAS**

When video reconciliation reaches `finalizing`, load edits and revision, lock the owner configuration file, read/decrypt the restored standard file, apply edits idempotently, write and verify it, then call `CompleteLegacySettingEdits`. If CAS returns false, reload the newer journal and replay. Propagate decryption/read/write errors; do not report success or fall back to alternate credentials.

- [ ] **Step 7: Remove obsolete inference paths**

Delete active use of `LegacyModelDefaultsFileKey`, file-presence migration decisions, and the old JSON-path SQL predicate. Keep only the one-time deferred-file importer needed for already-created local files. Public views must strip `ciphertext`, `fingerprint`, revision and migration fields.

- [ ] **Step 8: Run GREEN, pure-tree build and regression**

Run from `backend`:

```powershell
go test ./internal/httpapi -run '^(TestAPIConfigPolicy|TestCatalogVideo|Test.*Legacy.*Video|Test.*UserConfig|Test.*APIIntegration|TestLegacySettingPatch|TestAPIConfigPolicyMySQLDeferredMigrationCompletion)' -count=1
go test ./internal/novelfetchworkshop ./internal/storage -run 'Test.*(Legacy|ModelConfig|ConfigRevision|MigrationState|SettingPatch)' -count=1
```

From a clean `git archive` of the final SHA, rebuild the frontend embed input and run:

```powershell
go build -buildvcs=false ./cmd/qiantie
```

Expected: all focused tests pass with zero skips; the Windows/amd64 service builds from the exact Git tree.

- [ ] **Step 9: Commit Task 2**

Stage only Task 2 files and commit:

```text
refactor: journal deferred settings in mysql
```

## Final Acceptance

- Both task reviews report 0 Critical and 0 Important.
- A final whole-plan review confirms the three former migration guesses are gone and there is one authoritative state reader.
- Stop and remove only the disposable MySQL containers created for this plan.
- Do not restart 4001 in this plan. The next personal-center API frontend milestone owns the combined build and visible runtime restart.
