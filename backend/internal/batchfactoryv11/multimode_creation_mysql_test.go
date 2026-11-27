//go:build mysql84

package batchfactoryv11_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	bf "qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/storage"
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestMultiModeMySQLConcurrentCreationAndSourceRecovery(t *testing.T) {
	dsn := os.Getenv("QIANTIE_TEST_COMIC_STYLES_DSN")
	if dsn == "" {
		t.Skip("requires disposable fixture DB")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	template := os.Getenv("QIANTIE_TEST_COMIC_STYLES_TEMPLATE_DB")
	if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(template) {
		t.Fatal("unsafe template identifier")
	}
	rows, err := db.QueryContext(ctx, `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_NAME LIKE 'batch_factory_v12_%'`, template)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var table string
		_ = rows.Scan(&table)
		if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(table) {
			t.Fatal("unsafe table")
		}
		tables = append(tables, table)
	}
	rows.Close()
	for _, table := range tables {
		if _, err = db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS "+table+" LIKE "+template+"."+table); err != nil {
			t.Fatal(err)
		}
	}
	if err = storage.RunMigrations(ctx, db, storage.BatchMultiModeMigrations()); err != nil {
		t.Fatal(err)
	}
	input := bf.CreateMultiModeInput{RequestID: fmt.Sprintf("mysql-%d", time.Now().UnixNano()), Title: "four modes", Books: []bf.CreateBookInput{{BookID: "738579", Title: "one"}, {BookID: "738580", Title: "two"}}}
	for _, mode := range []bf.CreationMode{"video", "comic", "illustrated_text", "text"} {
		input.Projects = append(input.Projects, bf.ModeProjectInput{Mode: mode, Settings: bf.SettingsPatch{"captureCharacters": json.RawMessage(`4000`)}})
	}
	var wg sync.WaitGroup
	results := make([]bf.MultiModeCreationResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = bf.NewMySQLStore(db).CreateMultiModeBatches(ctx, "alice", input)
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if results[0].GroupID != results[1].GroupID || len(results[0].Batches) != 4 {
		t.Fatal("duplicate group from concurrent create")
	}
	s := bf.NewMySQLStore(db)
	group := results[0]
	jobs, err := s.ListMultiModeSources(ctx, "alice", group.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	// Persist a ready result but stop before delivery, then rebuild the store.
	claim, ok, err := s.ClaimMultiModeSource(ctx, "alice", group.GroupID, jobs[0].Key)
	if err != nil || !ok {
		t.Fatal(err)
	}
	snapshot := bf.MultiModeSourceSnapshot{Title: claim.Input.Title, OriginalRaw: "full-" + claim.Input.BookID, Metadata: map[string]any{"gender": "女频", "style": "现代言情"}}
	if err = s.CompleteMultiModeSource(ctx, "alice", group.GroupID, claim.Key, claim.Token, snapshot, nil); err != nil {
		t.Fatal(err)
	}
	a := group.Batches[0]
	if _, err = s.UpdateBookMetadata(ctx, "alice", a.ID, a.Books[0].ID, bf.UpdateBookMetadataInput{ExpectedRevision: 1, Metadata: map[string]any{"gender": "男频"}}); err != nil {
		t.Fatal(err)
	}
	s = bf.NewMySQLStore(db)
	calls := 0
	work := func(_ context.Context, in bf.CreateBookInput) (bf.MultiModeSourceSnapshot, error) {
		calls++
		return bf.MultiModeSourceSnapshot{Title: in.Title, OriginalRaw: "full-" + in.BookID, Metadata: map[string]any{"gender": "女频", "style": "现代言情"}}, nil
	}
	if err = bf.ResolveMultiModeSources(ctx, s, "alice", group.GroupID, work); err != nil {
		t.Fatal(err)
	}
	if err = bf.ResolveMultiModeSources(ctx, s, "alice", group.GroupID, work); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("refetched saved source: %d", calls)
	}
	reread, err := s.GetMultiModeCreation(ctx, "alice", group.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range reread.Batches {
		if len(b.Books) != 2 || b.Books[0].TxtText != "full-738579" {
			t.Fatalf("bad delivery %d %+v", i, b.Books)
		}
		want := "女频"
		if i == 0 {
			want = "男频"
		}
		if b.Books[0].SourceMetadata["gender"] != want {
			t.Fatalf("manual classification lost: %v", b.Books[0].SourceMetadata)
		}
	}
	if _, err = s.GetMultiModeCreation(ctx, "bob", group.GroupID); err != bf.ErrNotFound {
		t.Fatalf("foreign read %v", err)
	}
	input.Title = "changed"
	if _, err = s.CreateMultiModeBatches(ctx, "alice", input); err != bf.ErrConflict {
		t.Fatalf("changed request accepted %v", err)
	}
	if err = storage.RunMigrations(ctx, db, storage.BatchMultiModeMigrations()); err != nil {
		t.Fatal(err)
	}
}
