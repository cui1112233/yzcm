package novelfetchworkshop

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"qiantie/backend/internal/storage"
)

func migrationStateTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("QIANTIE_TEST_MYSQL84_DSN")
	if dsn == "" {
		t.Skip("requires disposable MySQL")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	name := fmt.Sprintf("legacy_state_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + name); err != nil {
			t.Error(err)
		}
	})
	cfg.DBName, cfg.ParseTime = name, true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestLegacyConfigMigrationStateRevisionCAS(t *testing.T) {
	db := migrationStateTestDB(t)
	ctx := context.Background()
	if _, err := db.Exec("INSERT INTO novel_fetch_legacy_config_migrations(owner_username,phase) VALUES('alice','awaiting_source')"); err != nil {
		t.Fatal(err)
	}
	store := NewMySQLStore(db)
	advance := func(revision uint64, phase LegacyConfigMigrationPhase) (bool, error) {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		ok, err := store.advanceLegacyConfigMigration(ctx, tx, "alice", revision, phase)
		if err == nil {
			err = tx.Commit()
		}
		return ok, err
	}
	if ok, err := advance(1, "complete"); ok || err == nil {
		t.Fatal("skipped phases")
	}
	if ok, err := advance(1, "reconciling"); err != nil || !ok {
		t.Fatal("valid transition failed", err)
	}
	if ok, err := advance(1, "finalizing"); err != nil || ok {
		t.Fatal("stale revision changed state", err)
	}
	state, found, err := store.LegacyConfigMigrationState(ctx, "alice")
	if err != nil || !found || state.Revision != 2 || state.Phase != "reconciling" {
		t.Fatal("unexpected state", err)
	}
}

type migrationFinalizerProbe struct {
	ModelConfigSecrets
	finalized bool
}

func (*migrationFinalizerProbe) OpenModelConfig(string, map[string]any) error { return nil }
func (p *migrationFinalizerProbe) FinalizeLegacySettings(string, string) error {
	p.finalized = true
	return nil
}

func TestLegacyConfigMigrationStateStaleFinalization(t *testing.T) {
	db := migrationStateTestDB(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		revision string
		plan     bool
	}{{"before-plan", "2", false}, {"missing-plan-at-same-config-revision", "3", false}, {"different-plan-same-digest", "3", true}} {
		t.Run(tc.name, func(t *testing.T) {
			current := map[string]any{ModelSecretVersionKey: ModelSecretVersion, ConfigRevisionKey: "3", "modelCatalog": []any{}, modelLegacyScrubPendingKey: "verified-plan", legacyModelMigrationVersionKey: legacyModelMigrationVersion, legacyModelScrubEntriesKey: []any{map[string]any{"container": 0, "index": 1, "digest": "original-entry"}}}
			raw, err := json.Marshal(current)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`INSERT INTO novel_fetch_workshop_configs(owner_username,settings_json,created_at,updated_at) VALUES(?,?,NOW(6),NOW(6))`, tc.name, raw); err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(`INSERT INTO novel_fetch_legacy_config_migrations(owner_username,phase,candidate_ciphertext,revision) VALUES(?,'finalizing','synthetic-sentinel',3)`, tc.name); err != nil {
				t.Fatal(err)
			}
			snap := cloneMap(current)
			snap[ConfigRevisionKey] = tc.revision
			if tc.plan {
				snap[legacyModelScrubEntriesKey] = []any{}
			} else {
				delete(snap, modelLegacyScrubPendingKey)
				delete(snap, legacyModelScrubEntriesKey)
			}
			store := NewMySQLStore(db)
			probe := &migrationFinalizerProbe{}
			store.ModelSecrets, store.LegacyConfigDir = probe, t.TempDir()
			_, err = store.openConfig(ctx, tc.name, snap)
			if !errors.Is(err, ErrConfigConflict) || probe.finalized {
				t.Errorf("outdated snapshot must be rejected before file finalization: finalized=%t error=%v", probe.finalized, err)
			}
			state, found, err := store.LegacyConfigMigrationState(ctx, tc.name)
			if err != nil || !found || state.Phase != LegacyFinalizing || state.Revision != 3 || state.CandidateEnvelope == "" {
				t.Fatal("pending state and evidence must survive stale reader")
			}
		})
	}
}

func TestLegacyConfigMigrationStateUpgradesKeylessGeneratedRows(t *testing.T) {
	raw := map[string]any{"model": "fixture-video", "createUrl": "https://test.invalid/create"}
	source := pendingVideoSource{legacyScrubEntry: legacyScrubEntry{Video: true, Provider: "personal_api"}, Raw: raw}
	old := cloneMap(source.model())
	old["tasksEndpoint"], old["resultEndpoint"] = "", ""
	file := map[string]any{"videoProviders": map[string]any{"personal_api": map[string]any{"model": "fixture-video", "createUrl": "https://test.invalid/create", "apiKey": "synthetic"}}}
	p := pendingLegacyVideos{Rows: []any{old}, Sources: []pendingVideoSource{source}}
	if !p.fileReady(file) {
		t.Error("old generated row blocked the verified original provider")
	}
	orphan := cloneMap(old)
	orphan["tasksEndpoint"] = "https://other.invalid/tasks"
	p.Rows = append(p.Rows, orphan)
	if p.fileReady(file) {
		t.Error("an orphan generated row still needs matching file evidence")
	}
}

func TestLegacyConfigMigrationStateRejectsMalformedTargetTypes(t *testing.T) {
	for _, field := range []string{"model", "createUrl", "tasksUrl", "resultUrl", "workflowId", "adapterKind", "provider"} {
		for _, side := range []string{"database", "file"} {
			t.Run(side+"-"+field, func(t *testing.T) {
				raw := map[string]any{"model": "fixture"}
				returning := cloneMap(raw)
				if field == "model" {
					delete(raw, field)
					delete(returning, field)
				}
				returning["apiKey"] = "synthetic"
				if side == "database" {
					raw[field] = 17
				} else {
					returning[field] = nil
				}
				p := pendingLegacyVideos{Sources: []pendingVideoSource{{legacyScrubEntry: legacyScrubEntry{Video: true, Provider: "personal_api"}, Raw: raw}}}
				if p.fileReady(map[string]any{"videoProviders": map[string]any{"personal_api": returning}}) {
					t.Fatal("malformed target accepted")
				}
			})
		}
	}
}

func TestLegacyConfigMigrationStateHistoricalProviderProjection(t *testing.T) {
	// Exact pre-1200007 provider shape before Normalize filled missing URLs.
	raw := map[string]any{"model": "fixture-video", "createUrl": "https://fixture.invalid/create"}
	source := pendingVideoSource{legacyScrubEntry: legacyScrubEntry{Video: true, Provider: "personal_api"}, Raw: raw}
	generated := map[string]any{"id": "legacy-video:personal_api", "kind": "video", "provider": "personal_api", "modelId": "fixture-video", "displayName": "fixture-video", "endpoint": "https://fixture.invalid/create", "tasksEndpoint": "", "resultEndpoint": "", "workflowId": "", "enabled": true, "credentialStatus": "needs_reentry", "credentialRecoveryReason": "legacy_missing"}
	returned := cloneMap(raw)
	returned["apiKey"] = "synthetic-restored"
	file := map[string]any{"videoProviders": map[string]any{"personal_api": returned}}
	t.Run("independent-disabled-row", func(t *testing.T) {
		other := cloneMap(generated)
		other["enabled"], other["displayName"] = false, "user-saved-video"
		for _, rows := range [][]any{{other, generated}, {generated, other}} {
			p := pendingLegacyVideos{Rows: rows, Sources: []pendingVideoSource{source}}
			orphan := p.orphanRows()
			if len(orphan) != 1 || !reflect.DeepEqual(orphan[0], other) || p.fileReady(file) {
				t.Fatal("independent same-ID row lost its own evidence requirement")
			}
		}
	})
	t.Run("one-source-one-generated-row", func(t *testing.T) {
		p := pendingLegacyVideos{Rows: []any{generated, cloneMap(generated)}, Sources: []pendingVideoSource{source}}
		if len(p.orphanRows()) != 1 || p.fileReady(file) {
			t.Fatal("a source claimed two historical rows")
		}
	})
	t.Run("keyed-row-with-original-adapter", func(t *testing.T) {
		original := cloneMap(raw)
		original["apiKey"], original["adapterKind"], original["catalogSource"] = "synthetic-key", "fixture-adapter", "fixture-catalog"
		original["tasksUrl"], original["resultUrl"] = "https://fixture.invalid/tasks", "https://fixture.invalid/result"
		keyed := cloneMap(generated)
		keyed["tasksEndpoint"], keyed["resultEndpoint"], keyed["credential"] = original["tasksUrl"], original["resultUrl"], original["apiKey"]
		delete(keyed, "credentialStatus")
		delete(keyed, "credentialRecoveryReason")
		saved := source
		saved.Raw = original
		p := pendingLegacyVideos{Rows: []any{keyed}, Sources: []pendingVideoSource{saved}}
		exactFile := map[string]any{"videoProviders": map[string]any{"personal_api": original}}
		if len(p.orphanRows()) != 0 || !p.fileReady(exactFile) {
			t.Error("genuine keyed historical row remained pending after original returned")
		}
		matches, _ := p.matchFile(exactFile)
		migration := legacyConfigMigration{releaseVideos: true, videoMatches: matches}
		active := migration.prepareVideoRows(nil, nil, exactFile)
		if len(active) != 1 {
			t.Fatal("a verified provider must restore exactly one row")
		}
		restored := active[0].(map[string]any)
		if restored["adapterKind"] != original["adapterKind"] || restored["catalogSource"] != original["catalogSource"] || restored["credential"] != original["apiKey"] {
			t.Fatal("restored row must use the original provider identity and credential")
		}
		for _, field := range []string{"adapterKind", "catalogSource"} {
			other := cloneMap(original)
			delete(other, field)
			if p.fileReady(map[string]any{"videoProviders": map[string]any{"personal_api": other}}) {
				t.Fatalf("file with missing %s accepted", field)
			}
		}
	})
}

func TestLegacyConfigMigrationStateIndependentIDUsesIdentityAndLocation(t *testing.T) {
	row := map[string]any{"kind": "video", "id": "same-id", "modelId": "fixture", "provider": "personal_api", "endpoint": "https://fixture.invalid", "workflowId": "fixture-flow", "displayName": "independent", "enabled": false}
	source := legacySourceEntry{legacyScrubEntry: legacyScrubEntry{Container: -1, Index: 7}, model: row}
	id := separateLegacyVideoID(source, nil)
	row["credential"], row["apiKey"], row["credentialRef"] = "synthetic-one", "synthetic-two", "synthetic-ref"
	if separateLegacyVideoID(source, nil) != id {
		t.Fatal("secret fields changed public model ID")
	}
	source.Index++
	if separateLegacyVideoID(source, nil) == id {
		t.Fatal("original sealed position omitted from new ID")
	}
	source.Index--
	occupied := map[string]bool{id: true}
	replacement := separateLegacyVideoID(source, occupied)
	if replacement == id || separateLegacyVideoID(source, occupied) != replacement {
		t.Fatal("existing ID collision was not handled deterministically")
	}
}

func (p pendingLegacyVideos) orphanRows() []map[string]any {
	var rows []map[string]any
	for _, i := range p.orphanIndices() {
		row, _ := p.Rows[i].(map[string]any)
		rows = append(rows, row)
	}
	return rows
}

func (p pendingLegacyVideos) fileReady(file map[string]any) bool {
	_, ok := p.matchFile(file)
	return ok
}
