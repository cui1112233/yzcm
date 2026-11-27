package novelfetchworkshop

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"qiantie/backend/internal/storage"
)

// This test can only touch an explicitly supplied disposable schema. TOS is
// entirely in memory; no real provider, bucket, or task is contacted.
func TestBodySafetyMySQLStorage(t *testing.T) {
	dsn := os.Getenv("QIANTIE_BODY_SAFETY_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated MySQL DSN not configured")
	}
	config, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal("invalid isolated MySQL DSN")
	}
	if !strings.HasPrefix(config.DBName, "nfw_body_safety_") {
		t.Fatal("refusing non-test database")
	}
	config.ParseTime = true
	config.Loc = time.UTC
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal("open isolated MySQL failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	for i := 0; i < 2; i++ {
		if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
			t.Fatalf("migration pass %d: %v", i, err)
		}
	}
	owner := fmt.Sprintf("body-safety-%d", time.Now().UnixNano())
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, table := range []string{"novel_fetch_workshop_bodies", "novel_fetch_workshop_documents", "novel_fetch_body_object_retirements"} {
			if _, err := db.ExecContext(cleanupCtx, `DELETE FROM `+table+` WHERE owner_username IN (?,?)`, owner, owner+"-other"); err != nil {
				t.Errorf("test owner cleanup: %v", err)
			}
		}
	}()
	var mu sync.Mutex
	objects := map[string]string{}
	deleted := map[string]bool{}
	s := NewMySQLStore(db)
	s.ObjectBodies = &BodyObjectStore{Bucket: "in-memory-test", Put: func(_ context.Context, _, key string, b []byte) error {
		mu.Lock()
		defer mu.Unlock()
		if _, exists := objects[key]; exists {
			return fmt.Errorf("key was reused")
		}
		objects[key] = string(b)
		return nil
	}, Get: func(_ context.Context, _, key string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		body, exists := objects[key]
		if !exists {
			return nil, fmt.Errorf("active object missing")
		}
		return []byte(body), nil
	}, Delete: func(_ context.Context, _, key string) error {
		mu.Lock()
		defer mu.Unlock()
		deleted[key] = true
		delete(objects, key)
		return nil
	}}
	body := func(book, text string) BodyRecord {
		return BodyRecord{BookID: book, BodyRef: BodyRef{VersionID: "ai1"}, Content: text}
	}
	if _, err := s.PutBody(ctx, owner, body("existing", "old")); err != nil {
		t.Fatal(err)
	}
	t.Run("body-edit-and-stale-document-consistency", func(t *testing.T) {
		stale := Document{BookID: "metadata", Original: "old", ProcessedChars: 3, Meta: map[string]any{"keep": "yes"}}
		if err := s.PutDocument(ctx, owner, stale); err != nil {
			t.Fatal(err)
		}
		rawRef, err := s.PutBody(ctx, owner, BodyRecord{BookID: "metadata", BodyRef: BodyRef{VersionID: "original_raw"}, Content: "完整原始正文不得截断"})
		if err != nil {
			t.Fatal(err)
		}
		ref, err := s.PutBody(ctx, owner, BodyRecord{BookID: "metadata", BodyRef: BodyRef{VersionID: "original"}, Content: "现在正文已经修改为更长内容"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.PutDocument(ctx, owner, stale); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetDocument(ctx, owner, "metadata")
		if err != nil {
			t.Fatal(err)
		}
		if got.ProcessedChars != 13 || got.BodyRefs["original"].CharCount != 13 || got.BodyRefs["original"].Revision != ref.Revision || got.BodyRefs["original"].ContentHash != ref.ContentHash || got.Original != "" || got.BodyRefs["original_raw"].ContentHash != rawRef.ContentHash {
			t.Fatalf("stale body/task summary: %+v", got)
		}
		raw, err := ReadBodyText(ctx, s, owner, got, "original_raw")
		if err != nil || raw != "完整原始正文不得截断" {
			t.Fatal("full raw body changed")
		}
	})
	t.Run("deleted-body-is-not-resurrected-by-old-document", func(t *testing.T) {
		if err := s.PutDocument(ctx, owner, Document{BookID: "deleted-stale", Original: "legacy original to migrate"}); err != nil {
			t.Fatal(err)
		}
		old, err := s.GetDocument(ctx, owner, "deleted-stale")
		if err != nil {
			t.Fatal(err)
		}
		if old.BodyRefs["original"].Revision == 0 {
			t.Fatal("legacy document did not acquire actual body reference")
		}
		// A public hydrated snapshot carries both the old text and its body ref.
		old.Original = "legacy original to migrate"
		old.ProcessedText = old.Original
		if removed, err := s.DeleteBody(ctx, owner, "deleted-stale", "original"); err != nil || !removed {
			t.Fatal(removed, err)
		}
		afterDelete, readErr := s.GetDocument(ctx, owner, "deleted-stale")
		if readErr != nil || afterDelete.ProcessedChars != 0 || afterDelete.Original != "" {
			t.Fatalf("deleted body retained active summary: %+v %v", afterDelete, readErr)
		}
		if _, err := s.CompareAndSwapDocument(ctx, owner, old); !errors.Is(err, ErrDocumentChanged) {
			t.Fatalf("CAS ignored body deletion: %v", err)
		}
		if err := s.PutDocument(ctx, owner, old); err != nil {
			t.Fatal(err)
		}
		latest, err := s.GetDocument(ctx, owner, "deleted-stale")
		if err != nil {
			t.Fatal(err)
		}
		if _, exists := latest.BodyRefs["original"]; exists || latest.Original != "" || latest.ProcessedText != "" {
			t.Fatalf("stale snapshot revived deleted body: %+v", latest)
		}
		if _, err := s.GetBody(ctx, owner, "deleted-stale", "original"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("legacy migration resurrected deleted body row: %v", err)
		}
	})
	t.Run("concurrent-existing-revisions", func(t *testing.T) {
		start := make(chan struct{})
		type result struct {
			ref  BodyRef
			err  error
			text string
		}
		results := make(chan result, 2)
		for _, text := range []string{"first concurrent body", "second concurrent body"} {
			go func(text string) {
				<-start
				ref, err := s.PutBody(ctx, owner, body("existing", text))
				results <- result{ref, err, text}
			}(text)
		}
		close(start)
		seen := map[uint64]bool{}
		for i := 0; i < 2; i++ {
			r := <-results
			if r.err != nil {
				t.Fatal(r.err)
			}
			_, hash, _, _ := encodeBody(r.text)
			if r.ref.ContentHash != hash || r.ref.Revision < 2 || r.ref.Revision > 3 {
				t.Fatalf("wrong exact result: %+v", r.ref)
			}
			seen[r.ref.Revision] = true
		}
		if len(seen) != 2 {
			t.Fatal("concurrent saves returned the same revision")
		}
		latest, err := s.GetBody(ctx, owner, "existing", "ai1")
		if err != nil || latest.Revision != 3 {
			t.Fatalf("latest=%+v err=%v", latest, err)
		}
	})
	t.Run("first-insert-race-retains-every-upload", func(t *testing.T) {
		start := make(chan struct{})
		results := make(chan error, 2)
		for i := 0; i < 2; i++ {
			go func(i int) {
				<-start
				_, err := s.PutBody(ctx, owner, body("new-race", fmt.Sprintf("new %d", i)))
				results <- err
			}(i)
		}
		close(start)
		success := 0
		for i := 0; i < 2; i++ {
			if <-results == nil {
				success++
			}
		}
		if success == 0 {
			t.Fatal("neither first insert succeeded")
		}
		if _, err := s.GetBody(ctx, owner, "new-race", "ai1"); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		if len(deleted) != 0 {
			t.Error("first-insert conflict deleted an upload")
		}
		mu.Unlock()
	})
	t.Run("save-cleanup-and-reap-interleaving", func(t *testing.T) {
		other := owner + "-other"
		if _, err := s.PutBody(ctx, other, body("other-account", "keep other owner")); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 5; i++ {
			book := fmt.Sprintf("cleanup-%d", i)
			if _, err := s.PutBody(ctx, owner, body(book, "old cleanup body")); err != nil {
				t.Fatal(err)
			}
			released := time.Now().UTC().Add(-10 * 24 * time.Hour)
			if _, err := s.MarkBodyReleasable(ctx, owner, book, "ai1", released, 1); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			saveResult := make(chan error, 1)
			cleanResult := make(chan error, 1)
			go func() { <-start; _, err := s.PutBody(ctx, owner, body(book, "new saved body")); saveResult <- err }()
			go func() {
				<-start
				_, err := s.CleanupBodies(ctx, owner, BodyCleanupRequest{Now: time.Now().UTC(), Reason: BodyCleanupReasonExpired, Limit: 100})
				cleanResult <- err
			}()
			close(start)
			saveErr, cleanErr := <-saveResult, <-cleanResult
			// Deadlocks are safe explicit failures. No uncertain write may delete an object.
			if saveErr != nil {
				var remaining int
				if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM novel_fetch_workshop_bodies WHERE owner_username=? AND book_id=?`, owner, book).Scan(&remaining); err != nil {
					t.Fatal(err)
				}
				if remaining > 0 {
					if _, err := s.GetBody(ctx, owner, book, "ai1"); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				got, err := s.GetBody(ctx, owner, book, "ai1")
				if err != nil || got.Content != "new saved body" {
					t.Fatalf("new saved body lost: %v", err)
				}
			}
			if cleanErr != nil && !strings.Contains(cleanErr.Error(), "Deadlock") {
				t.Fatal(cleanErr)
			}
		}
		// Before one hour no immutable object can be removed.
		if n, err := s.ReapRetiredBodyObjects(ctx, time.Now().UTC(), 500); err != nil || n != 0 {
			t.Fatalf("grace violated: %d %v", n, err)
		}
		if n, err := s.ReapRetiredBodyObjects(ctx, time.Now().UTC().Add(2*time.Hour), 500); err != nil || n == 0 {
			t.Fatalf("reap failed: %d %v", n, err)
		}
		for _, pair := range [][2]string{{owner, "existing"}, {owner, "new-race"}, {other, "other-account"}} {
			if _, err := s.GetBody(ctx, pair[0], pair[1], "ai1"); err != nil {
				t.Fatal(err)
			}
		}
	})
	t.Run("legacy-held-and-restart-retry", func(t *testing.T) {
		legacy := BodyObjectKey(owner, "legacy", "ai1")
		mu.Lock()
		objects[legacy] = "legacy body"
		mu.Unlock()
		now := time.Now().UTC()
		_, err := db.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_bodies(owner_username,book_id,version_id,revision,content_encoding,content_blob,content_hash,char_count,state,created_at,updated_at,last_needed_at) VALUES(?,?,?,1,'tos',?,'hash',11,'ready',?,?,?)`, owner, "legacy", "ai1", []byte(legacy), now, now, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.DeleteBody(ctx, owner, "legacy", "ai1"); err != nil {
			t.Fatal(err)
		}
		var state string
		if err := db.QueryRowContext(ctx, `SELECT state FROM novel_fetch_body_object_retirements WHERE object_key=?`, legacy).Scan(&state); err != nil || state != "held" {
			t.Fatalf("legacy state=%s err=%v", state, err)
		}
		if _, err := s.PutBody(ctx, owner, body("retry", "retry body")); err != nil {
			t.Fatal(err)
		}
		var key []byte
		if err := db.QueryRowContext(ctx, `SELECT content_blob FROM novel_fetch_workshop_bodies WHERE owner_username=? AND book_id='retry'`, owner).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if _, err := s.DeleteBody(ctx, owner, "retry", "ai1"); err != nil {
			t.Fatal(err)
		}
		oldDelete := s.ObjectBodies.Delete
		s.ObjectBodies.Delete = func(context.Context, string, string) error { return fmt.Errorf("injected delete failure") }
		if _, err := s.ReapRetiredBodyObjects(ctx, now.Add(2*time.Hour), 500); err == nil {
			t.Fatal("expected persisted delete failure")
		}
		var attempts int
		if err := db.QueryRowContext(ctx, `SELECT attempts,state FROM novel_fetch_body_object_retirements WHERE object_key=?`, string(key)).Scan(&attempts, &state); err != nil || attempts != 1 || state != "pending" {
			t.Fatalf("failure not persisted: %d %s %v", attempts, state, err)
		}
		restarted := NewMySQLStore(db)
		restarted.ObjectBodies = &BodyObjectStore{Bucket: "in-memory-test", Delete: oldDelete}
		if n, err := restarted.ReapRetiredBodyObjects(ctx, now.Add(4*time.Hour), 500); err != nil || n != 1 {
			t.Fatalf("restart retry failed: %d %v", n, err)
		}
		mu.Lock()
		defer mu.Unlock()
		if objects[legacy] != "legacy body" || deleted[legacy] {
			t.Fatal("legacy body removed")
		}
		if !deleted[string(key)] {
			t.Fatal("exact retry key was not removed")
		}
	})
}
