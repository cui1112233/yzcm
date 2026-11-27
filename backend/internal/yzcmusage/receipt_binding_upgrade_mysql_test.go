//go:build linux || windows

package yzcmusage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReceiptBindingMySQLLegacyAlwaysRequiresAuditedRecovery(t *testing.T) {
	for _, scenario := range []struct {
		name, writerZone string
	}{
		{"same-timezone", "+00:00"},
		{"different-timezones", "+00:01"},
		{"coarse-activation-clock", "+00:00"},
		{"missing-identity", "+00:00"},
		{"missing-activation", "+00:00"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			first, dsn := startReceiptLegacyFixture(t, scenario.writerZone)
			ctx := context.Background()
			legacy := replaceReceiptFingerprintWithLegacy(t, first)
			switch scenario.name {
			case "coarse-activation-clock":
				if _, err := first.Store.DB.ExecContext(ctx, `UPDATE yzcm_usage_receipt_bindings SET activated_at=CAST(activated_at AS DATETIME(0)) WHERE binding_slot=?`, receiptBindingSlot); err != nil {
					t.Fatal(err)
				}
			case "missing-identity":
				if _, err := first.Store.DB.ExecContext(ctx, `UPDATE yzcm_usage_receipt_bindings SET filesystem_identity=NULL WHERE binding_slot=?`, receiptBindingSlot); err != nil {
					t.Fatal(err)
				}
				legacy = ""
			case "missing-activation":
				if _, err := first.Store.DB.ExecContext(ctx, `UPDATE yzcm_usage_receipt_bindings SET activated_at=NULL WHERE binding_slot=?`, receiptBindingSlot); err != nil {
					t.Fatal(err)
				}
			}
			reader, err := sql.Open("mysql", dsn)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reader.Close() })
			setReceiptFixtureZone(t, reader, "+00:00")
			if scenario.writerZone != "+00:00" {
				// Show that the actual two sessions give different meanings to the
				// same historical DATETIME. Production never interprets this value.
				const epoch = `SELECT CAST(UNIX_TIMESTAMP(activated_at)*1000000 AS SIGNED) FROM yzcm_usage_receipt_bindings WHERE binding_slot=?`
				var written, read int64
				if err := first.Store.DB.QueryRowContext(ctx, epoch, receiptBindingSlot).Scan(&written); err != nil {
					t.Fatal(err)
				}
				if err := reader.QueryRowContext(ctx, epoch, receiptBindingSlot).Scan(&read); err != nil || read-written != 60_000_000 {
					t.Fatalf("fixture must reinterpret legacy activation by sixty seconds: %d -> %d, %v", written, read, err)
				}
			}
			assertLegacyReceiptBlocked(t, Store{DB: reader}, first.ReceiptDir, legacy)
		})
	}
}

func TestReceiptBindingMySQLConcurrentLegacyReadersPreserveAuthority(t *testing.T) {
	first, _ := startReceiptLegacyFixture(t, "+00:00")
	legacy := replaceReceiptFingerprintWithLegacy(t, first)
	first.Store.DB.SetMaxOpenConns(0)
	for _, instance := range []string{"first", "second"} {
		t.Run(instance, func(t *testing.T) {
			t.Parallel()
			assertLegacyReceiptBlocked(t, first.Store, first.ReceiptDir, legacy)
		})
	}
}

func startReceiptLegacyFixture(t *testing.T, zone string) (*Tracker, string) {
	t.Helper()
	db, dsn := receiptRecoveryMySQL(t)
	setReceiptFixtureZone(t, db, zone)
	dir := filepath.Join(t.TempDir(), "usage-receipts")
	first := &Tracker{Store: Store{DB: db}, ReceiptDir: dir, receiptBindingSync: func(f *os.File) error {
		if f.Name() == filepath.Dir(dir) {
			// This valid historical fixture predates its database activation,
			// even at coarse file creation clock precision.
			time.Sleep(100 * time.Millisecond)
		}
		return f.Sync()
	}}
	if err := first.InitializeReceiptStorage(context.Background()); err != nil {
		t.Fatal(err)
	}
	return first, dsn
}

func setReceiptFixtureZone(t *testing.T, db *sql.DB, zone string) {
	t.Helper()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`SET SESSION time_zone = ?`, zone); err != nil {
		t.Fatal(err)
	}
}

func assertLegacyReceiptBlocked(t *testing.T, store Store, dir, legacy string) {
	t.Helper()
	ctx := context.Background()
	const activatedSQL = `SELECT CAST(activated_at AS CHAR) FROM yzcm_usage_receipt_bindings WHERE binding_slot=?`
	var initialActivation sql.NullString
	if err := store.DB.QueryRowContext(ctx, activatedSQL, receiptBindingSlot).Scan(&initialActivation); err != nil {
		t.Fatal(err)
	}
	calls := 0
	cold := &Tracker{Store: store, ReceiptDir: dir}
	response, err := cold.Wrap(transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return fakeBilledResponse("legacy-needs-recovery"), nil
	})).RoundTrip(billingTestRequest(t, Request{Owner: "member"}))
	if response != nil {
		response.Body.Close()
	}
	row, found, readErr := store.loadReceiptBinding(ctx)
	var rows int
	if countErr := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM yzcm_usage_requests`).Scan(&rows); countErr != nil {
		t.Fatal(countErr)
	}
	if response != nil || !errors.Is(err, errReceiptLegacyBindingUnverified) || calls != 0 || rows != 0 || readErr != nil || !found || row.FilesystemIdentity != legacy || row.State != receiptBindingActive {
		t.Fatalf("legacy binding must remain unchanged and require recovery: response=%v error=%v provider=%d ledger=%d found=%t row=%+v readErr=%v", response, err, calls, rows, found, row, readErr)
	}
	var activation sql.NullString
	if err := store.DB.QueryRowContext(ctx, activatedSQL, receiptBindingSlot).Scan(&activation); err != nil || activation != initialActivation {
		t.Fatalf("activation changed: %+v -> %+v, %v", initialActivation, activation, err)
	}
}

func replaceReceiptFingerprintWithLegacy(t *testing.T, tracker *Tracker) string {
	t.Helper()
	current := tracker.receiptBound.objects.encode()
	legacy := legacyReceiptFixtureIdentity(t, tracker.receiptBound)
	result, err := tracker.Store.DB.Exec(`UPDATE yzcm_usage_receipt_bindings SET filesystem_identity=? WHERE binding_slot=? AND filesystem_identity=?`, legacy, receiptBindingSlot, current)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		t.Fatalf("legacy fixture setup changed %d rows: %v", rows, err)
	}
	return legacy
}

func legacyReceiptFixtureIdentity(t *testing.T, b *receiptBinding) string {
	t.Helper()
	var legacy strings.Builder
	legacy.WriteString(receiptLegacyFixturePlatform)
	for _, path := range []string{filepath.Dir(b.dir), b.dir, filepath.Join(b.dir, receiptDirectoryIDFile), b.anchor} {
		volume, file := legacyReceiptFixtureFileID(t, path)
		fmt.Fprintf(&legacy, ":%016x%x", volume, file)
	}
	return legacy.String()
}
