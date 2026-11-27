package yzcmusage

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReceiptBindingMySQLConcurrentLegacyPromotion(t *testing.T) {
	db, _ := receiptRecoveryMySQL(t)
	store := Store{DB: db}
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "usage-receipts")
	first := &Tracker{Store: store, ReceiptDir: dir, receiptBindingSync: func(f *os.File) error {
		if f.Name() == filepath.Dir(dir) {
			// Ensure the true marker creation and activation are in distinct
			// filesystem clock ticks for this verifiable legacy fixture.
			time.Sleep(50 * time.Millisecond)
		}
		return f.Sync()
	}}
	if err := first.InitializeReceiptStorage(ctx); err != nil {
		t.Fatal(err)
	}
	initial, found, err := store.loadReceiptBinding(ctx)
	if err != nil || !found || !strings.HasPrefix(initial.FilesystemIdentity, receiptIdentityV2Prefix) || len(initial.FilesystemIdentity) != 68 {
		t.Fatalf("invalid current fingerprint: row=%+v found=%t error=%v", initial, found, err)
	}
	legacy := replaceReceiptFingerprintWithLegacy(t, first)
	stale := initial
	stale.FilesystemIdentity = legacy
	var activated string
	if err := db.QueryRowContext(ctx, `SELECT CAST(activated_at AS CHAR) FROM yzcm_usage_receipt_bindings WHERE binding_slot=?`, receiptBindingSlot).Scan(&activated); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	outcomes := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			cold := &Tracker{Store: store, ReceiptDir: dir}
			outcomes <- cold.InitializeReceiptStorage(ctx)
		}()
	}
	close(start)
	for range 2 {
		if err := <-outcomes; err != nil {
			t.Fatal(err)
		}
	}
	if err := store.upgradeReceiptBinding(ctx, stale, legacy, initial.FilesystemIdentity, first.receiptBound.objects); err != nil {
		t.Fatalf("stale reader must recognize the exact committed promotion: %v", err)
	}
	updated, found, err := store.loadReceiptBinding(ctx)
	if err != nil || !found || updated != initial {
		t.Fatalf("promotion changed the slot authority: row=%+v want=%+v found=%t error=%v", updated, initial, found, err)
	}
	var finalActivated string
	if err := db.QueryRowContext(ctx, `SELECT CAST(activated_at AS CHAR) FROM yzcm_usage_receipt_bindings WHERE binding_slot=?`, receiptBindingSlot).Scan(&finalActivated); err != nil || finalActivated != activated {
		t.Fatalf("promotion changed activation time: before=%q after=%q error=%v", activated, finalActivated, err)
	}
	latest := &Tracker{Store: store, ReceiptDir: dir}
	if err := latest.InitializeReceiptStorage(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestReceiptBindingMySQLLegacyPromotionRequiresStrictTimeEvidence(t *testing.T) {
	for _, missing := range []string{"identity", "activation", "ordering", "timestamp-resolution", "clock"} {
		t.Run(missing, func(t *testing.T) {
			db, _ := receiptRecoveryMySQL(t)
			store := Store{DB: db}
			ctx := context.Background()
			dir := filepath.Join(t.TempDir(), "usage-receipts")
			first := &Tracker{Store: store, ReceiptDir: dir}
			if err := first.InitializeReceiptStorage(ctx); err != nil {
				t.Fatal(err)
			}
			legacy := replaceReceiptFingerprintWithLegacy(t, first)
			switch missing {
			case "identity":
				if _, err := db.ExecContext(ctx, `UPDATE yzcm_usage_receipt_bindings SET filesystem_identity=NULL WHERE binding_slot=?`, receiptBindingSlot); err != nil {
					t.Fatal(err)
				}
				legacy = ""
			case "activation":
				if _, err := db.ExecContext(ctx, `UPDATE yzcm_usage_receipt_bindings SET activated_at=NULL WHERE binding_slot=?`, receiptBindingSlot); err != nil {
					t.Fatal(err)
				}
			case "ordering", "timestamp-resolution":
				marker := first.receiptBound.objects.marker
				if marker.birth.IsZero() || marker.birthUncertainty <= 0 {
					t.Fatal("test marker does not expose a creation timestamp")
				}
				birth := marker.birth
				if missing == "timestamp-resolution" {
					birth = birth.Add(marker.birthUncertainty / 2)
				}
				if _, err := db.ExecContext(ctx, `UPDATE yzcm_usage_receipt_bindings SET activated_at=TIMESTAMPADD(MICROSECOND, ?, '1970-01-01 00:00:00') WHERE binding_slot=?`, birth.UnixMicro(), receiptBindingSlot); err != nil {
					t.Fatal(err)
				}
			case "clock":
				db.SetMaxOpenConns(1)
				if _, err := db.ExecContext(ctx, `SET timestamp = 1680000000`); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			cold := &Tracker{Store: store, ReceiptDir: dir}
			response, err := cold.Wrap(transportFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return fakeBilledResponse("unexpected-legacy-promotion"), nil
			})).RoundTrip(billingTestRequest(t, Request{Owner: "member"}))
			if response != nil || !errors.Is(err, errReceiptLegacyBindingUnverified) || calls != 0 {
				t.Fatalf("inconclusive evidence permitted a provider call: response=%v error=%v calls=%d", response, err, calls)
			}
			row, found, err := store.loadReceiptBinding(ctx)
			if err != nil || !found || row.FilesystemIdentity != legacy {
				t.Fatalf("unverified legacy binding changed: row=%+v found=%t error=%v", row, found, err)
			}
		})
	}
}

func replaceReceiptFingerprintWithLegacy(t *testing.T, tracker *Tracker) string {
	t.Helper()
	objects := tracker.receiptBound.objects
	legacy := objects.legacyEncoding()
	if legacy == "" {
		t.Fatal("fixture cannot represent a legacy identity")
	}
	result, err := tracker.Store.DB.Exec(`UPDATE yzcm_usage_receipt_bindings SET filesystem_identity=? WHERE binding_slot=? AND filesystem_identity=?`, legacy, receiptBindingSlot, objects.encode())
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := result.RowsAffected(); err != nil || rows != 1 {
		t.Fatalf("legacy fixture setup changed %d rows: %v", rows, err)
	}
	return legacy
}
