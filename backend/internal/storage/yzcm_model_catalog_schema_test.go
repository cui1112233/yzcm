package storage

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/yzcmmodels"
)

func TestYZCMModelCatalogMigrationRegistered(t *testing.T) {
	migrations := YZCMModelCatalogMigrations()
	if len(migrations) != 1 || migrations[0].Version != 8800413 {
		t.Fatalf("unexpected YZCM catalog migration: %+v", migrations)
	}
	count := 0
	for _, migration := range AppMigrations() {
		if migration.Version == 8800413 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("migration 8800413 registered %d times", count)
	}
}

func TestYZCMModelCatalogMigrationMySQLResumesAfterFirstDDL(t *testing.T) {
	db := energyMigrationTestDatabase(t)
	ctx := context.Background()
	migrations := YZCMModelCatalogMigrations()
	if _, err := db.ExecContext(ctx, migrations[0].SQL[0]); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := RunMigrations(ctx, db, migrations); err != nil {
			t.Fatal(err)
		}
	}
	var checksum string
	if err := db.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version=8800413`).Scan(&checksum); err != nil || checksum != ChecksumFor(migrations[0]) {
		t.Fatalf("invalid migration ledger: %q %v", checksum, err)
	}
	_, err := db.ExecContext(ctx, `SELECT model_id FROM yzcm_model_catalog`)
	if err != nil {
		t.Fatal(err)
	}
}

type yzcmTestSource func(context.Context) (yzcmmodels.Snapshot, error)

func (f yzcmTestSource) Fetch(ctx context.Context) (yzcmmodels.Snapshot, error) { return f(ctx) }

func yzcmCatalogTestStore(t *testing.T) *yzcmmodels.Store {
	t.Helper()
	db := energyMigrationTestDatabase(t)
	if err := RunMigrations(context.Background(), db, YZCMModelCatalogMigrations()); err != nil {
		t.Fatal(err)
	}
	return &yzcmmodels.Store{DB: db}
}

func mustSyncYZCMCatalog(t *testing.T, store *yzcmmodels.Store, snapshot yzcmmodels.Snapshot) yzcmmodels.SyncRun {
	t.Helper()
	run, err := store.Sync(context.Background(), yzcmTestSource(func(context.Context) (yzcmmodels.Snapshot, error) { return snapshot, nil }))
	if err != nil || run.Status != "succeeded" {
		t.Fatalf("sync: %+v %v", run, err)
	}
	return run
}

func TestYZCMModelCatalogMySQLSearchAndLastGood(t *testing.T) {
	store := yzcmCatalogTestStore(t)
	ctx := context.Background()
	search := func(query yzcmmodels.Query) yzcmmodels.Page {
		t.Helper()
		page, err := store.Search(ctx, query)
		if err != nil {
			t.Fatal(err)
		}
		return page
	}
	if empty := search(yzcmmodels.Query{}); empty.Status != "unavailable" || empty.Reason != "no_saved_snapshot" || empty.Models == nil || empty.CatalogUpdatedAt != nil {
		t.Fatalf("initial catalog: %+v", empty)
	}
	now := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
	model := func(id, name, kind string, adapted, allowed bool) yzcmmodels.Model {
		return yzcmmodels.Model{ModelID: id, DisplayName: name, Kind: kind, Adapted: adapted, Allowed: allowed}
	}
	snapshot := yzcmmodels.Snapshot{Complete: true, SourceUpdatedAt: now, Models: []yzcmmodels.Model{
		model("qa-text-2", "中文模型", "text", true, true), model("qa-text-1", "中文模型", "text", true, true),
		model("qa_image%", "画面模型", "image", true, true), model("qa-image-other", "图像", "image", true, true),
		model("hidden-allowed", "未适配", "text", false, true), model("hidden-adapted", "未准入", "text", true, false),
	}}
	run := mustSyncYZCMCatalog(t, store, snapshot)
	first := search(yzcmmodels.Query{Kind: "text", Q: "中文", Limit: 1})
	if first.Status != "available" || first.SnapshotID != run.ID || first.Provenance != "yzcm_relay" || !first.SourceUpdatedAt.Equal(now) ||
		first.CatalogUpdatedAt == nil || first.NextOffset == nil || *first.NextOffset != 1 || !first.HasMore || len(first.Models) != 1 ||
		first.Models[0].ModelID != "qa-text-1" || first.Models[0].AvailabilityReason != "adapted_and_allowed" ||
		!first.Models[0].SourceUpdatedAt.Equal(now) || first.Models[0].Provenance != "yzcm_relay" {
		t.Fatalf("wrong first page: %+v", first)
	}
	second := search(yzcmmodels.Query{Kind: "text", Q: "中文", Limit: 1, Offset: 1, SnapshotID: first.SnapshotID})
	if len(second.Models) != 1 || second.Models[0].ModelID != "qa-text-2" || second.HasMore {
		t.Fatalf("unstable second page: %+v", second)
	}
	if literal := search(yzcmmodels.Query{Kind: "image", Q: "IMAGE%"}); len(literal.Models) != 1 || literal.Models[0].ModelID != "qa_image%" {
		t.Fatalf("nonliteral search or model id mismatch: %+v", literal)
	}
	if denied := search(yzcmmodels.Query{Q: "hidden"}); denied.Status != "empty" || denied.Reason != "no_matches" || len(denied.Models) != 0 {
		t.Fatalf("disabled row leaked: %+v", denied)
	}
	var disabled int
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM yzcm_model_catalog WHERE availability_reason IN ('not_adapted','not_allowed')`).Scan(&disabled); err != nil || disabled != 2 {
		t.Fatalf("stored disabled state: %d %v", disabled, err)
	}
	secret := "fixture-provider-administrator-must-not-leak"
	for _, scenario := range []struct {
		name, code string
		source     yzcmTestSource
	}{
		{"provider error", "source_error", func(context.Context) (yzcmmodels.Snapshot, error) { return yzcmmodels.Snapshot{}, errors.New(secret) }},
		{"partial", "invalid_snapshot", func(context.Context) (yzcmmodels.Snapshot, error) { s := snapshot; s.Complete = false; return s, nil }},
		{"missing rows", "invalid_snapshot", func(context.Context) (yzcmmodels.Snapshot, error) { s := snapshot; s.Models = nil; return s, nil }},
		{"future source time", "invalid_snapshot", func(context.Context) (yzcmmodels.Snapshot, error) {
			s := snapshot
			s.SourceUpdatedAt = time.Now().Add(24 * time.Hour)
			return s, nil
		}},
		{"duplicate", "invalid_snapshot", func(context.Context) (yzcmmodels.Snapshot, error) {
			s := snapshot
			s.Models = []yzcmmodels.Model{snapshot.Models[0], snapshot.Models[0]}
			return s, nil
		}},
		{"bad kind", "invalid_snapshot", func(context.Context) (yzcmmodels.Snapshot, error) {
			s := snapshot
			s.Models = []yzcmmodels.Model{model("bad", "invalid kind", "arbitrary", true, true)}
			return s, nil
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			failed, err := store.Sync(ctx, scenario.source)
			if err == nil || strings.Contains(err.Error(), secret) || failed.Status != "failed" || failed.FailureCode != scenario.code {
				t.Fatalf("incorrect sync failure: %+v %v", failed, err)
			}
			page := search(yzcmmodels.Query{Kind: "text", Q: "中文", Limit: 1})
			if !reflect.DeepEqual(page.Models, first.Models) || !page.Stale || page.SnapshotID != run.ID || page.LastSync == nil || page.LastSync.FailureCode != scenario.code {
				t.Fatalf("lost last-good snapshot or failure state: %+v", page)
			}
		})
	}
	nextRun := mustSyncYZCMCatalog(t, store, yzcmmodels.Snapshot{Complete: true, Models: []yzcmmodels.Model{}, SourceUpdatedAt: now.Add(time.Second)})
	if empty := search(yzcmmodels.Query{}); empty.Status != "empty" || empty.Reason != "empty_catalog" || empty.SnapshotID != nextRun.ID || empty.Stale {
		t.Fatalf("explicit complete empty snapshot: %+v", empty)
	}
	if _, err := store.Search(ctx, yzcmmodels.Query{SnapshotID: first.SnapshotID}); !errors.Is(err, yzcmmodels.ErrCatalogChanged) {
		t.Fatalf("changed snapshot pagination: %v", err)
	}
}

func TestYZCMModelCatalogMySQLConcurrentSyncAndRollback(t *testing.T) {
	store := yzcmCatalogTestStore(t)
	ctx := context.Background()
	tick := time.Now().Add(-time.Minute).UTC().Truncate(time.Microsecond)
	snapshot := func(id string, at time.Time) yzcmmodels.Snapshot {
		return yzcmmodels.Snapshot{Complete: true, SourceUpdatedAt: at, Models: []yzcmmodels.Model{{ModelID: id, DisplayName: id, Kind: "text", Allowed: true, Adapted: true}}}
	}
	prior := mustSyncYZCMCatalog(t, store, snapshot("baseline", tick.Add(-time.Second)))
	slowStarted, releaseSlow := make(chan struct{}), make(chan struct{})
	slowDone := make(chan error, 1)
	t.Cleanup(func() {
		select {
		case <-releaseSlow:
		default:
			close(releaseSlow)
		}
	})
	otherStore := &yzcmmodels.Store{DB: store.DB}
	go func() {
		_, err := otherStore.Sync(ctx, yzcmTestSource(func(context.Context) (yzcmmodels.Snapshot, error) {
			close(slowStarted)
			<-releaseSlow
			return snapshot("older", tick), nil
		}))
		slowDone <- err
	}()
	select {
	case <-slowStarted:
	case err := <-slowDone:
		t.Fatalf("older sync did not start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("older sync did not reach the source fixture")
	}
	if page, err := store.Search(ctx, yzcmmodels.Query{}); err != nil || page.SnapshotID != prior.ID || page.LastSync == nil || page.LastSync.Status != "running" {
		t.Fatalf("a running sync replaced last-good: %+v %v", page, err)
	}
	latest := mustSyncYZCMCatalog(t, store, snapshot("newer", tick.Add(time.Second)))
	close(releaseSlow)
	if err := <-slowDone; err != nil {
		t.Fatal(err)
	}
	// An upstream timestamp older than the last-good remains stale even if its run started later.
	_ = mustSyncYZCMCatalog(t, store, snapshot("late-obsolete", tick))
	assertLatest := func() {
		t.Helper()
		page, err := store.Search(ctx, yzcmmodels.Query{})
		if err != nil || page.SnapshotID != latest.ID || len(page.Models) != 1 || page.Models[0].ModelID != "newer" {
			t.Fatalf("a stale or partial run replaced the winning snapshot: %+v %v", page, err)
		}
	}
	assertLatest()
	if _, err := store.DB.ExecContext(ctx, `CREATE TRIGGER fail_qa_catalog BEFORE INSERT ON yzcm_model_catalog FOR EACH ROW
BEGIN IF NEW.model_id = 'fail-row' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture rejects this row'; END IF; END`); err != nil {
		t.Fatal(err)
	}
	partial := snapshot("must-rollback", tick.Add(2*time.Second))
	partial.Models = append(partial.Models, snapshot("fail-row", tick).Models[0])
	failed, err := store.Sync(ctx, yzcmTestSource(func(context.Context) (yzcmmodels.Snapshot, error) { return partial, nil }))
	if err == nil || failed.FailureCode != "persistence_error" {
		t.Fatalf("transaction failure: %+v %v", failed, err)
	}
	var rows int
	var state string
	var finished sql.NullInt64
	if err := store.DB.QueryRowContext(ctx, `SELECT status,finished_at_us FROM yzcm_model_sync_runs WHERE id=?`, failed.ID).Scan(&state, &finished); err != nil || state != "failed" || !finished.Valid {
		t.Fatalf("failed run state not recorded: %q %v", state, err)
	}
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM yzcm_model_catalog WHERE sync_run_id=?`, failed.ID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("partial rows survived a failed publication: %d %v", rows, err)
	}
	assertLatest()
}
