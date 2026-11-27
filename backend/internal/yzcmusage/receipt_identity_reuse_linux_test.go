package yzcmusage

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const receiptLegacyFixturePlatform = "unix1"

func legacyReceiptFixtureFileID(t *testing.T, path string) (uint64, [16]byte) {
	t.Helper()
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		t.Fatal(err)
	}
	var file [16]byte
	binary.BigEndian.PutUint64(file[8:], uint64(stat.Ino))
	return uint64(stat.Dev), file
}

func TestReceiptIdentityRejectsRecreatedMarkerWithReusedInode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "usage-receipts")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	tracker := &Tracker{ReceiptDir: dir}
	cacheReceiptTestBinding(t, tracker)
	bound := *tracker.receiptBound
	recreateReceiptMarkerOnReusedInode(t, filepath.Join(dir, receiptDirectoryIDFile))
	if err := bound.verifyVisible(); !errors.Is(err, errReceiptDirectoryChanged) {
		t.Fatalf("a replacement marker with the original inode must invalidate the binding, got %v", err)
	}
}

func TestReceiptBindingMySQLReusedInodeStopsBeforeProvider(t *testing.T) {
	for _, version := range []string{"legacy", "current"} {
		t.Run(version, func(t *testing.T) {
			db, _ := receiptRecoveryMySQL(t)
			store := Store{DB: db}
			dir := filepath.Join(t.TempDir(), "usage-receipts")
			first := &Tracker{Store: store, ReceiptDir: dir}
			ctx := context.Background()
			if err := first.InitializeReceiptStorage(ctx); err != nil {
				t.Fatal(err)
			}
			before := first.receiptBound.objects
			legacyBefore := legacyReceiptFixtureIdentity(t, first.receiptBound)
			identity := before.encode()
			if version == "legacy" {
				identity = replaceReceiptFingerprintWithLegacy(t, first)
			}
			recreateReceiptMarkerOnReusedInode(t, filepath.Join(dir, receiptDirectoryIDFile))
			after, err := first.receiptBound.filesystemObjects()
			if err != nil || legacyBefore != legacyReceiptFixtureIdentity(t, first.receiptBound) || before.same(after) {
				t.Fatalf("fixture did not preserve the weak identity while changing the strong identity: %v", err)
			}
			calls := 0
			cold := &Tracker{Store: store, ReceiptDir: dir}
			response, err := cold.Wrap(transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return fakeBilledResponse("replacement-fixture"), nil
			})).RoundTrip(billingTestRequest(t, Request{Owner: "member"}))
			if response != nil || !errors.Is(err, ErrReceiptPersistenceUnavailable) || calls != 0 {
				t.Fatalf("replacement permitted a paid request: response=%v error=%v calls=%d", response, err, calls)
			}
			if version == "legacy" && !errors.Is(err, errReceiptLegacyBindingUnverified) {
				t.Fatalf("legacy matching inode requires audited recovery, got %v", err)
			}
			row, found, err := store.loadReceiptBinding(ctx)
			if err != nil || !found || row.FilesystemIdentity != identity {
				t.Fatalf("replacement changed authoritative identity: row=%+v found=%t error=%v", row, found, err)
			}
			var rows int
			if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM yzcm_usage_requests`).Scan(&rows); err != nil || rows != 0 {
				t.Fatalf("replacement created %d ledger rows: %v", rows, err)
			}
		})
	}
}

func TestReceiptBindingMySQLLegacyReusedInodeAcrossTimezones(t *testing.T) {
	first, dsn := startReceiptLegacyFixture(t, "+00:01")
	legacy := replaceReceiptFingerprintWithLegacy(t, first)
	recreateReceiptMarkerOnReusedInode(t, filepath.Join(first.ReceiptDir, receiptDirectoryIDFile))
	objects, err := first.receiptBound.filesystemObjects()
	if err != nil || legacyReceiptFixtureIdentity(t, first.receiptBound) != legacy || first.receiptBound.objects.same(objects) {
		t.Fatalf("fixture must preserve the weak identity while changing the strong identity: %v", err)
	}
	reader, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	setReceiptFixtureZone(t, reader, "+00:00")
	// Wait past even the incorrectly reinterpreted activation, so a future-date
	// guard alone cannot satisfy this test. Both clocks run normally.
	var remainingMicros int64
	if err := reader.QueryRow(`SELECT TIMESTAMPDIFF(MICROSECOND, CURRENT_TIMESTAMP(6), activated_at) FROM yzcm_usage_receipt_bindings WHERE binding_slot=?`, receiptBindingSlot).Scan(&remainingMicros); err != nil {
		t.Fatal(err)
	}
	if remainingMicros > 0 {
		time.Sleep(time.Duration(remainingMicros)*time.Microsecond + 20*time.Millisecond)
	}
	assertLegacyReceiptBlocked(t, Store{DB: reader}, first.ReceiptDir, legacy)
}

func TestReceiptIdentityRejectsUnsupportedFilesystem(t *testing.T) {
	if _, err := readReceiptObject("/proc", true, ""); !errors.Is(err, errReceiptStableIdentityUnavailable) {
		t.Fatalf("procfs must report an explicit stable identity error, got %v", err)
	}
}

func recreateReceiptMarkerOnReusedInode(t *testing.T, path string) {
	t.Helper()
	original, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	marker, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Keep unsuccessful replacements allocated so the filesystem cannot keep
	// reissuing the first substitute inode while the original is available.
	otherInodes := t.TempDir()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2048; attempt++ {
		if err := os.WriteFile(path, marker, 0600); err != nil {
			t.Fatal(err)
		}
		replacement, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if os.SameFile(original, replacement) {
			t.Logf("Linux reused the removed marker inode after %d creations", attempt)
			return
		}
		if err := os.Rename(path, filepath.Join(otherInodes, fmt.Sprint(attempt))); err != nil {
			t.Fatal(err)
		}
	}
	if os.Getenv("QIANTIE_TEST_REQUIRE_INODE_REUSE") == "1" {
		t.Fatal("fixture filesystem did not reuse the marker inode")
	}
	t.Skip("fixture filesystem did not reuse the marker inode")
}
