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

// Opt-in only: the caller must supply a disposable, explicitly named schema.
// Body objects remain in memory; this never contacts a real TOS bucket/model.
func TestBodySafetyMySQLConditionalBody(t *testing.T) {
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
	if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
		t.Fatalf("isolated migrations: %v", err)
	}
	owner := fmt.Sprintf("conditional-body-%d", time.Now().UnixNano())
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		for _, table := range []string{"novel_fetch_workshop_bodies", "novel_fetch_workshop_documents", "novel_fetch_body_object_retirements"} {
			if _, err := db.ExecContext(cleanupCtx, `DELETE FROM `+table+` WHERE owner_username=?`, owner); err != nil {
				t.Errorf("test owner cleanup: %v", err)
			}
		}
	}()

	var mu sync.Mutex
	objects := map[string][]byte{}
	s := NewMySQLStore(db)
	s.ObjectBodies = &BodyObjectStore{Bucket: "conditional-in-memory", Put: func(_ context.Context, _, key string, value []byte) error {
		mu.Lock()
		defer mu.Unlock()
		if _, exists := objects[key]; exists {
			return errors.New("immutable object key reused")
		}
		objects[key] = append([]byte(nil), value...)
		return nil
	}, Get: func(_ context.Context, _, key string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		value, exists := objects[key]
		if !exists {
			return nil, errors.New("in-memory body object missing")
		}
		return append([]byte(nil), value...), nil
	}, Delete: func(_ context.Context, _, key string) error {
		mu.Lock()
		defer mu.Unlock()
		delete(objects, key)
		return nil
	}}
	body := func(book, version, text string) BodyRecord {
		return BodyRecord{BookID: book, BodyRef: BodyRef{VersionID: version, State: "ready"}, Content: text}
	}
	readBody := func(t *testing.T, book, version string) BodyRecord {
		t.Helper()
		value, err := s.GetBody(ctx, owner, book, version)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	readDocument := func(t *testing.T, book string) Document {
		t.Helper()
		doc, err := s.GetDocument(ctx, owner, book)
		if err != nil {
			t.Fatal(err)
		}
		return doc
	}
	unchanged := func(t *testing.T, want BodyRecord) {
		t.Helper()
		got := readBody(t, want.BookID, want.VersionID)
		if got.Content != want.Content || got.Revision != want.Revision || got.ContentHash != want.ContentHash || got.CharCount != want.CharCount {
			t.Fatalf("%s body/reference changed: want revision %d, got %d", want.VersionID, want.Revision, got.Revision)
		}
	}
	for _, scenario := range []string{"success", "concurrent_stop", "concurrent_body_edit"} {
		t.Run(scenario, func(t *testing.T) {
			book := "conditional-" + scenario
			if err := s.PutDocument(ctx, owner, Document{BookID: book, MaxTxt: 4000, Meta: map[string]any{"status": "completed", "keep": "yes"}}); err != nil {
				t.Fatal(err)
			}
			for version, text := range map[string]string{"original_raw": "完整备份正文必须保持不变", "original": "旧原文", "ai1": "已有AI成果"} {
				if _, err := s.PutBody(ctx, owner, body(book, version, text)); err != nil {
					t.Fatal(err)
				}
			}
			expected := readDocument(t, book)
			raw, ai := readBody(t, book, "original_raw"), readBody(t, book, "ai1")
			before := readBody(t, book, "original")
			switch scenario {
			case "concurrent_stop":
				latest := readDocument(t, book)
				latest.Meta["status"] = "stopped"
				latest.Meta["manualOnly"] = true
				if _, err := s.CompareAndSwapDocument(ctx, owner, latest); err != nil {
					t.Fatal(err)
				}
			case "concurrent_body_edit":
				if _, err := s.PutBody(ctx, owner, body(book, "original", "用户已经保存的新正文")); err != nil {
					t.Fatal(err)
				}
				before = readBody(t, book, "original")
			}
			ref, err := s.PutBodyIfDocumentUnchanged(ctx, owner, body(book, "original", "经过敏感处理的恢复正文"), expected)
			latest := readDocument(t, book)
			if scenario == "success" {
				current := readBody(t, book, "original")
				if err != nil || current.Content != "经过敏感处理的恢复正文" || ref.Revision != before.Revision+1 || latest.BodyRefs["original"].Revision != ref.Revision || latest.ProcessedChars != len([]rune(current.Content)) || latest.UpdatedAt == expected.UpdatedAt || latest.Meta["keep"] != "yes" {
					t.Fatalf("successful reference switch mismatch: ref=%+v err=%v", ref, err)
				}
			} else {
				if !errors.Is(err, ErrDocumentChanged) {
					t.Fatalf("stale restore must conflict: %v", err)
				}
				unchanged(t, before)
				if latest.BodyRefs["original"].Revision != before.Revision || latest.ProcessedChars != int(before.CharCount) {
					t.Fatal("failed restore changed document body summary")
				}
				if scenario == "concurrent_stop" {
					if latest.Meta["status"] != "stopped" || latest.Meta["manualOnly"] != true {
						t.Fatal("stale restore erased stop")
					}
					// A fresh timestamp still cannot authorize writing a stopped task.
					if _, err := s.PutBodyIfDocumentUnchanged(ctx, owner, body(book, "original", "不应写入"), latest); !errors.Is(err, ErrDocumentChanged) {
						t.Fatalf("stopped snapshot accepted: %v", err)
					}
					unchanged(t, before)
				}
			}
			unchanged(t, raw)
			unchanged(t, ai)
		})
	}
}
