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
	if got := ChecksumFor(migrations[0]); got != "de5293ca7f66778f84661a47f4087bcb542be27c3601e1ef8b9a238e6e3cabf4" {
		t.Fatalf("migration 8800413 checksum changed: %s", got)
	}
	counts := map[int]int{}
	for _, migration := range AppMigrations() {
		counts[migration.Version]++
	}
	if counts[8800413] != 1 || counts[8800414] != 1 || YZCMModelCatalogPrecisionMigration().Version != 8800414 {
		t.Fatalf("catalog migrations registered %d/%d times", counts[8800413], counts[8800414])
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
	if err := RunMigrations(context.Background(), db, append(YZCMModelCatalogMigrations(), YZCMModelCatalogPrecisionMigration())); err != nil {
		t.Fatal(err)
	}
	return &yzcmmodels.Store{DB: db}
}

func TestYZCMModelCatalogPrecisionMigrationMySQLResumeAndLegacy(t *testing.T) {
	for _, phase := range []string{"fresh", "alter committed", "head table committed", "backfill committed", "head seeded"} {
		t.Run(phase, func(t *testing.T) {
			db := energyMigrationTestDatabase(t)
			ctx := context.Background()
			if err := RunMigrations(ctx, db, YZCMModelCatalogMigrations()); err != nil {
				t.Fatal(err)
			}
			legacyAt := time.Date(2026, 10, 1, 5, 4, 37, 871000, time.UTC)
			exec := func(statement string, args ...any) sql.Result {
				t.Helper()
				result, err := db.ExecContext(ctx, statement, args...)
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			exec(`INSERT INTO yzcm_model_sync_runs (id,provenance,status,started_at_us,finished_at_us,source_updated_at_us,model_count)
VALUES (1,'yzcm_relay','succeeded',?,?,?,1),(2,'yzcm_relay','succeeded',?,?,?,1)`, legacyAt.UnixMicro(), legacyAt.UnixMicro(), legacyAt.UnixMicro(),
				legacyAt.UnixMicro(), legacyAt.UnixMicro(), legacyAt.UnixMicro())
			exec(`INSERT INTO yzcm_model_catalog (sync_run_id,kind,model_id,display_name,adapted,allowed,availability_reason)
VALUES (1,'text','legacy-shadow','旧候选',TRUE,TRUE,'adapted_and_allowed'), (2,'text','legacy-active','当前目录',TRUE,TRUE,'adapted_and_allowed')`)
			migration := YZCMModelCatalogPrecisionMigration()
			steps := map[string]int{"fresh": 0, "alter committed": 1, "head table committed": 2, "backfill committed": 3, "head seeded": 4}[phase]
			for _, statement := range migration.SQL[:steps] {
				exec(statement)
			}
			for range 2 {
				if err := RunMigrations(ctx, db, []Migration{migration}); err != nil {
					t.Fatal(err)
				}
			}
			var checksum string
			if err := db.QueryRowContext(ctx, `SELECT checksum FROM schema_migrations WHERE version=8800414`).Scan(&checksum); err != nil || checksum != ChecksumFor(migration) {
				t.Fatalf("invalid resumed migration checksum: %q %v", checksum, err)
			}
			var backfilled int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM yzcm_model_sync_runs WHERE source_updated_at_ns=? AND snapshot_digest IS NULL`, legacyAt.UnixMicro()*1000).Scan(&backfilled); err != nil || backfilled != 2 {
				t.Fatalf("legacy timestamp backfill: %d %v", backfilled, err)
			}
			store := &yzcmmodels.Store{DB: db}
			assertLegacy := func() {
				t.Helper()
				page, err := store.Search(ctx, yzcmmodels.Query{})
				if err != nil || page.SnapshotID != 2 || page.SourceUpdatedAt == nil || !page.SourceUpdatedAt.Equal(legacyAt) ||
					len(page.Models) != 1 || page.Models[0].ModelID != "legacy-active" {
					t.Fatalf("legacy directory changed: %+v %v", page, err)
				}
			}
			assertLegacy()
			candidate := yzcmmodels.Snapshot{Complete: true, SourceUpdatedAt: legacyAt.Add(900 * time.Nanosecond), Models: []yzcmmodels.Model{
				{ModelID: "legacy-active", DisplayName: "当前目录", Kind: "text", Adapted: true, Allowed: true},
			}}
			source := yzcmTestSource(func(context.Context) (yzcmmodels.Snapshot, error) { return candidate, nil })
			if run, err := store.Sync(ctx, source); err != nil || run.Status != "idempotent" {
				t.Fatalf("same legacy payload was not idempotent: %+v %v", run, err)
			}
			candidate.Models[0].Allowed = false
			if run, err := store.Sync(ctx, source); err == nil || run.FailureCode != "source_time_conflict" {
				t.Fatalf("uncertain legacy microsecond overrode saved data: %+v %v", run, err)
			}
			assertLegacy()
			candidate.SourceUpdatedAt = legacyAt.Add(time.Microsecond)
			accepted := mustSyncYZCMCatalog(t, store, candidate)
			page, err := store.Search(ctx, yzcmmodels.Query{})
			if err != nil || page.SnapshotID != accepted.ID || len(page.Models) != 0 {
				t.Fatalf("provably newer source could not remove old selection: %+v %v", page, err)
			}
		})
	}
}

func mustSyncYZCMCatalog(t *testing.T, store *yzcmmodels.Store, snapshot yzcmmodels.Snapshot) yzcmmodels.SyncRun {
	t.Helper()
	run, err := store.Sync(context.Background(), yzcmTestSource(func(context.Context) (yzcmmodels.Snapshot, error) { return snapshot, nil }))
	if err != nil || run.Status != "succeeded" {
		t.Fatalf("sync: %+v %v", run, err)
	}
	return run
}

func TestYZCMModelCatalogMySQLNanosecondOrderingAndTies(t *testing.T) {
	base := time.Date(2026, 10, 1, 5, 4, 37, 0, time.UTC)
	olderAt, newerAt := base.Add(100*time.Nanosecond), base.Add(900*time.Nanosecond)
	model := func(id string) yzcmmodels.Model {
		return yzcmmodels.Model{ModelID: id, DisplayName: id, Kind: "text", Adapted: true, Allowed: true}
	}
	older := yzcmmodels.Snapshot{Complete: true, SourceUpdatedAt: olderAt, Models: []yzcmmodels.Model{model("older")}}
	newer := yzcmmodels.Snapshot{Complete: true, SourceUpdatedAt: newerAt, Models: []yzcmmodels.Model{model("newer"), model("second")}}
	sync := func(t *testing.T, store *yzcmmodels.Store, snapshot yzcmmodels.Snapshot, status, failure string) yzcmmodels.SyncRun {
		t.Helper()
		run, err := store.Sync(context.Background(), yzcmTestSource(func(context.Context) (yzcmmodels.Snapshot, error) { return snapshot, nil }))
		if run.Status != status || run.FailureCode != failure || (failure != "") != (err != nil) || run.SourceUpdatedAt == nil || !run.SourceUpdatedAt.Equal(snapshot.SourceUpdatedAt) {
			t.Fatalf("want %s/%s at %s, got %+v %v", status, failure, snapshot.SourceUpdatedAt, run, err)
		}
		return run
	}
	assertNewer := func(t *testing.T, store *yzcmmodels.Store, id uint64) {
		t.Helper()
		fresh := &yzcmmodels.Store{DB: store.DB}
		page, err := fresh.Search(context.Background(), yzcmmodels.Query{})
		if err != nil || page.SnapshotID != id || len(page.Models) != 2 || page.Models[0].ModelID != "newer" ||
			!page.SourceUpdatedAt.Equal(newerAt) || !page.Models[0].SourceUpdatedAt.Equal(newerAt) {
			t.Fatalf("newest exact snapshot was lost: %+v %v", page, err)
		}
	}
	t.Run("new arrives first", func(t *testing.T) {
		store := yzcmCatalogTestStore(t)
		first := sync(t, store, newer, "succeeded", "")
		ignored := sync(t, store, older, "ignored", "")
		var stamp int64
		var reason string
		if err := store.DB.QueryRow(`SELECT source_updated_at_ns,disposition FROM yzcm_model_sync_runs WHERE id=?`, ignored.ID).Scan(&stamp, &reason); err != nil || stamp != olderAt.UnixNano() || reason != "older_source_time" {
			t.Fatalf("ignored run lost precise source time or reason: %d %q %v", stamp, reason, err)
		}
		assertNewer(t, store, first.ID)
	})
	t.Run("old arrives first", func(t *testing.T) {
		store := yzcmCatalogTestStore(t)
		sync(t, store, older, "succeeded", "")
		second := sync(t, store, newer, "succeeded", "")
		assertNewer(t, store, second.ID)
	})
	t.Run("equal source time", func(t *testing.T) {
		store := yzcmCatalogTestStore(t)
		first := sync(t, store, newer, "succeeded", "")
		same := newer
		same.Models = []yzcmmodels.Model{newer.Models[1], newer.Models[0]}
		repeated := sync(t, store, same, "idempotent", "")
		changed := newer
		changed.Models = append([]yzcmmodels.Model(nil), newer.Models...)
		changed.Models[0].Allowed = false
		conflicting := sync(t, store, changed, "failed", "source_time_conflict")
		var duplicateRows int
		if err := store.DB.QueryRow(`SELECT COUNT(*) FROM yzcm_model_catalog WHERE sync_run_id IN (?,?)`, repeated.ID, conflicting.ID).Scan(&duplicateRows); err != nil || duplicateRows != 0 {
			t.Fatalf("repeated or conflicting run wrote catalog rows: %d %v", duplicateRows, err)
		}
		assertNewer(t, store, first.ID)
	})
	t.Run("concurrent equal source time", func(t *testing.T) {
		store := yzcmCatalogTestStore(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ready, release := make(chan struct{}, 2), make(chan struct{})
		type attempt struct {
			run yzcmmodels.SyncRun
			err error
		}
		results := make(chan attempt, 2)
		for _, id := range []string{"same-instant-a", "same-instant-b"} {
			go func(id string) {
				other := &yzcmmodels.Store{DB: store.DB}
				run, err := other.Sync(ctx, yzcmTestSource(func(ctx context.Context) (yzcmmodels.Snapshot, error) {
					ready <- struct{}{}
					select {
					case <-release:
						return yzcmmodels.Snapshot{Complete: true, SourceUpdatedAt: newerAt, Models: []yzcmmodels.Model{model(id)}}, nil
					case <-ctx.Done():
						return yzcmmodels.Snapshot{}, ctx.Err()
					}
				}))
				results <- attempt{run, err}
			}(id)
		}
		for range 2 {
			select {
			case <-ready:
			case got := <-results:
				t.Fatalf("sync ended before reaching concurrent source: %+v %v", got.run, got.err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		close(release)
		var winner uint64
		conflicts := 0
		for range 2 {
			select {
			case result := <-results:
				if result.err == nil && result.run.Status == "succeeded" {
					winner = result.run.ID
				} else if errors.Is(result.err, yzcmmodels.ErrSourceTimeConflict) && result.run.Status == "failed" {
					conflicts++
				} else {
					t.Fatalf("unexpected concurrent outcome: %+v %v", result.run, result.err)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		page, err := store.Search(ctx, yzcmmodels.Query{})
		if err != nil || winner == 0 || conflicts != 1 || page.SnapshotID != winner || len(page.Models) != 1 {
			t.Fatalf("concurrent tie was not serialized: winner=%d conflicts=%d page=%+v error=%v", winner, conflicts, page, err)
		}
	})
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
	olderAt, newerAt := tick.Add(100*time.Nanosecond), tick.Add(900*time.Nanosecond)
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
			return snapshot("older", olderAt), nil
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
	latest := mustSyncYZCMCatalog(t, store, snapshot("newer", newerAt))
	close(releaseSlow)
	if err := <-slowDone; err != nil {
		t.Fatal(err)
	}
	// An older upstream snapshot is ignored even if its run started later.
	if run, err := store.Sync(ctx, yzcmTestSource(func(context.Context) (yzcmmodels.Snapshot, error) {
		return snapshot("late-obsolete", olderAt), nil
	})); err != nil || run.Status != "ignored" || run.Disposition != "older_source_time" {
		t.Fatalf("older publication: %+v %v", run, err)
	}
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
