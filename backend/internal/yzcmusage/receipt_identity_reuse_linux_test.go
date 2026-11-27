package yzcmusage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

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
			identity := before.encode()
			if version == "legacy" {
				identity = replaceReceiptFingerprintWithLegacy(t, first)
			}
			recreateReceiptMarkerOnReusedInode(t, filepath.Join(dir, receiptDirectoryIDFile))
			after, err := first.receiptBound.filesystemObjects()
			if err != nil || before.legacyEncoding() != after.legacyEncoding() || before.same(after) {
				t.Fatalf("fixture did not preserve the weak identity while changing the strong identity: %v", err)
			}
			t.Logf("marker birth %s -> %s, filesystem clock tick %s", before.marker.birth, after.marker.birth, after.marker.birthUncertainty)
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
