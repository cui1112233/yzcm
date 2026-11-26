package yzcmusage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReceiptBindingMySQLDefaultStartupAndWholeParentReplacement(t *testing.T) {
	db, _ := receiptRecoveryMySQL(t)
	ctx := context.Background()
	root := t.TempDir()
	parent := filepath.Join(root, "member-system")
	dir, hidden := filepath.Join(parent, "usage-receipts"), filepath.Join(root, "hidden-member-system")
	store := Store{DB: db}
	first := &Tracker{Store: store, ReceiptDir: dir}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("fresh default path already exists: %v", err)
	}
	if err := first.InitializeReceiptStorage(ctx); err != nil {
		t.Fatal(err)
	}
	row, found, err := store.loadReceiptBinding(ctx)
	if err != nil || !found || row.PathDigest != receiptPathDigest(dir) || row.State != receiptBindingActive || row.Origin != receiptBindingNew {
		t.Fatalf("unexpected initial binding %+v found=%t err=%v", row, found, err)
	}
	calls := 0
	member := Request{Owner: "member", TeamID: "team-a", ActorUsername: "founder"}
	other := Request{Owner: "other", TeamID: "team-b", ActorUsername: "manager"}
	call := func(tracker *Tracker, identity Request, id string) (*http.Response, error) {
		return tracker.Wrap(transportFunc(func(*http.Request) (*http.Response, error) {
			calls++
			return fakeBilledResponse(id), nil
		})).RoundTrip(billingTestRequest(t, identity))
	}
	checkRow := func(id string) {
		t.Helper()
		var owner, team, actor string
		if err := db.QueryRow(`SELECT owner,team_id,actor_username FROM yzcm_usage_requests WHERE request_id=?`, id).Scan(&owner, &team, &actor); err != nil || owner != member.Owner || team != member.TeamID || actor != member.ActorUsername {
			t.Fatalf("request %s attribution=%q/%q/%q error=%v", id, owner, team, actor, err)
		}
	}
	checkCount := func(want int) {
		t.Helper()
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM yzcm_usage_requests`).Scan(&count); err != nil || count != want {
			t.Fatalf("usage rows=%d, want %d, error=%v", count, want, err)
		}
	}
	respond := func(tracker *Tracker, id string) {
		t.Helper()
		resp, err := call(tracker, member, id)
		if err != nil || resp == nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("first provider call failed: %+v %v", resp, err)
		}
		resp.Body.Close()
	}
	respond(first, "first-after-app-startup")
	if calls != 1 {
		t.Fatalf("first startup call count=%d", calls)
	}
	checkCount(1)
	checkRow("first-after-app-startup")
	first.Store.DB = nil // The running process already verified its immutable binding.
	const spooled = "claim-before-parent-loss"
	respond(first, spooled)
	checkCount(1)
	if files := receiptFiles(t, dir); len(files) != 1 {
		t.Fatalf("expected one offline claim, got %v", files)
	}
	if err := os.Rename(parent, hidden); err != nil {
		t.Fatal(err)
	}
	checkBlocked := func(tracker *Tracker) {
		t.Helper()
		before := calls
		if resp, err := call(tracker, other, spooled); resp != nil || !errors.Is(err, ErrReceiptPersistenceUnavailable) || calls != before {
			t.Fatalf("new process should block the paid call: calls=%d/%d err=%v", calls, before, err)
		}
		checkCount(1)
	}
	checkBlocked(&Tracker{Store: store, ReceiptDir: dir})
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Fatalf("active parent was recreated: %v", err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	checkBlocked(&Tracker{Store: store, ReceiptDir: dir})
	if _, err := os.Stat(filepath.Join(dir, receiptDirectoryIDFile)); !os.IsNotExist(err) {
		t.Fatalf("replacement was stamped: %v", err)
	}
	checkBlocked(&Tracker{Store: store, ReceiptDir: filepath.Join(root, "changed-config", "usage-receipts")})
	checkBlocked(&Tracker{Store: store, DBOnly: true})
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(hidden, parent); err != nil {
		t.Fatal(err)
	}
	checkBlocked(&Tracker{ReceiptDir: dir}) // A fresh process cannot verify authority with MySQL down.
	marker := filepath.Join(dir, receiptDirectoryIDFile)
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	checkBlocked(&Tracker{Store: store, ReceiptDir: dir})
	if err := os.WriteFile(marker, []byte(strings.Repeat("f", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	checkBlocked(&Tracker{Store: store, ReceiptDir: dir})
	if err := os.WriteFile(marker, []byte(row.StoreID), 0600); err != nil {
		t.Fatal(err)
	}
	restarted := &Tracker{Store: store, ReceiptDir: dir}
	t.Cleanup(restarted.closeReceiptScan)
	restarted.flushReceipts(ctx)
	checkCount(2)
	checkRow(spooled)
	if files := receiptFiles(t, dir); len(files) != 0 {
		t.Fatalf("replayed claim was not consumed: %v", files)
	}
}

func TestReceiptBindingMySQLConcurrentInitializationUsesOneID(t *testing.T) {
	db, _ := receiptRecoveryMySQL(t)
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "usage-receipts")
	store := Store{DB: db}
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			tracker := &Tracker{Store: store, ReceiptDir: dir}
			<-start
			results <- tracker.InitializeReceiptStorage(ctx)
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var count int
	var storeID, state string
	if err := db.QueryRow(`SELECT COUNT(*) FROM yzcm_usage_receipt_bindings`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("binding rows=%d error=%v", count, err)
	}
	if err := db.QueryRow(`SELECT store_id,state FROM yzcm_usage_receipt_bindings`).Scan(&storeID, &state); err != nil || state != receiptBindingActive {
		t.Fatalf("store ID=%q state=%q error=%v", storeID, state, err)
	}
	for _, path := range []string{filepath.Join(dir, receiptDirectoryIDFile), filepath.Join(filepath.Dir(dir), ".usage-receipts-receipt-id")} {
		if id, err := readReceiptDirectoryID(path); err != nil || id != storeID {
			t.Fatalf("marker=%q, want %q, error=%v", id, storeID, err)
		}
	}
}

func TestReceiptBindingMySQLResumesPendingAndPreservesLegacyFiles(t *testing.T) {
	db, _ := receiptRecoveryMySQL(t)
	store := Store{DB: db}
	ctx := context.Background()
	for phase, label := range []string{"after-db-reservation", "after-directory-create", "after-inner-marker", "after-both-markers", "after-filesystem-failure"} {
		t.Run(label, func(t *testing.T) {
			parent := t.TempDir()
			dir, anchor := filepath.Join(parent, "usage-receipts"), filepath.Join(parent, ".usage-receipts-receipt-id")
			id := fmt.Sprintf("%032x", phase+1)
			pending := receiptBindingRow{PathDigest: receiptPathDigest(dir), StoreID: id, State: receiptBindingPending, Origin: receiptBindingNew}
			if _, err := store.reserveReceiptBinding(ctx, pending); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_, _ = db.Exec(`DELETE FROM yzcm_usage_receipt_bindings WHERE binding_slot=? AND store_id=?`, receiptBindingSlot, id)
			})
			if phase >= 1 && phase <= 3 {
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if phase >= 2 && phase <= 3 {
				if err := publishReceiptFile(filepath.Join(dir, receiptDirectoryIDFile), ".fixture-*.tmp", []byte(id)); err != nil {
					t.Fatal(err)
				}
			}
			if phase == 3 {
				if err := publishReceiptFile(anchor, ".fixture-*.tmp", []byte(id)); err != nil {
					t.Fatal(err)
				}
			}
			tracker := &Tracker{Store: store, ReceiptDir: dir}
			if phase == 4 {
				if err := os.WriteFile(dir, []byte("fixture blocker"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := tracker.InitializeReceiptStorage(ctx); err == nil {
					t.Fatal("blocked filesystem unexpectedly activated")
				}
				if row, found, err := store.loadReceiptBinding(ctx); err != nil || !found || row != pending {
					t.Fatalf("failed filesystem initialization changed its reservation: %+v found=%t error=%v", row, found, err)
				}
				if err := os.Remove(dir); err != nil {
					t.Fatal(err)
				}
			}
			if err := tracker.InitializeReceiptStorage(ctx); err != nil {
				t.Fatal(err)
			}
			active, found, err := store.loadReceiptBinding(ctx)
			if err != nil || !found || active.StoreID != id || active.State != receiptBindingActive {
				t.Fatalf("resume changed reservation: %+v found=%t error=%v", active, found, err)
			}
			if err := store.finishReceiptBinding(ctx, pending, func(receiptBindingRow) error {
				return errors.New("stale pending reader attempted to touch active files")
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
	dir := t.TempDir()
	legacy := receipt{Request: Request{ID: "preexisting-file", Owner: "member", TeamID: "team-a", ActorUsername: "founder", Feature: "model"}, CredentialHash: Fingerprint("fixture-key"), Status: 200, At: time.Now().UTC()}
	if err := saveReceiptAt(dir, legacy); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, receiptName(legacy.Request.ID))
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tracker := &Tracker{Store: store, ReceiptDir: dir}
	t.Cleanup(tracker.closeReceiptScan)
	if err := tracker.InitializeReceiptStorage(ctx); err != nil {
		t.Fatal(err)
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
		t.Fatalf("legacy receipt changed during binding: %v", err)
	}
	tracker.flushReceipts(ctx)
	var owner string
	if err := db.QueryRow(`SELECT owner FROM yzcm_usage_requests WHERE request_id=?`, legacy.Request.ID).Scan(&owner); err != nil || owner != legacy.Request.Owner {
		t.Fatalf("legacy receipt was not recovered: owner=%q error=%v", owner, err)
	}
}
