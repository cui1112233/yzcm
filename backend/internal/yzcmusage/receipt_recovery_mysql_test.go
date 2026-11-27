package yzcmusage

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func TestReceiptMySQLExplicitDBOnlyFallback(t *testing.T) {
	db, dsn := receiptRecoveryMySQL(t)
	blocked := filepath.Join(t.TempDir(), "file-in-place-of-directory")
	if err := os.WriteFile(blocked, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(blocked, "receipts")
	tracker := &Tracker{Store: Store{DB: db}, DBOnly: true}
	calls := 0
	base := transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return fakeBilledResponse("db-fallback"), nil
	})
	for _, identity := range []Request{
		{Owner: "member", TeamID: "team-a", ActorUsername: "founder"},
		{Owner: "other", TeamID: "team-b", ActorUsername: "manager"},
	} {
		response, err := tracker.Wrap(base).RoundTrip(billingTestRequest(t, identity))
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 || string(body) != "provider result" {
			t.Fatalf("provider response changed: status=%d body=%q err=%v", response.StatusCode, body, err)
		}
	}
	var count int
	var owner, team, actor string
	if err := db.QueryRow(`SELECT COUNT(*) FROM yzcm_usage_requests WHERE request_id='db-fallback'`).Scan(&count); err != nil || count != 1 || calls != 2 {
		t.Fatalf("provider calls=%d ledger rows=%d err=%v", calls, count, err)
	}
	if err := db.QueryRow(`SELECT owner,team_id,actor_username FROM yzcm_usage_requests WHERE request_id='db-fallback'`).Scan(&owner, &team, &actor); err != nil || owner != "member" || team != "team-a" || actor != "founder" {
		t.Fatalf("ledger attribution=%q/%q/%q err=%v", owner, team, actor, err)
	}
	if _, err := os.Lstat(dir); err == nil {
		t.Fatal("a recovery directory unexpectedly exists")
	}

	// The successful storage call needs no filesystem. If both destinations
	// are already down, reject before calling the paid provider.
	brokenDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err = brokenDB.Close(); err != nil {
		t.Fatal(err)
	}
	tracker.Store.DB = brokenDB
	if response, err := tracker.Wrap(base).RoundTrip(billingTestRequest(t, Request{Owner: "member"})); !errors.Is(err, ErrReceiptPersistenceUnavailable) || response != nil || calls != 2 {
		t.Fatalf("preflight failed to prevent paid dispatch: calls=%d err=%v", calls, err)
	}

	// Also cover a database outage that starts after preflight and after the
	// provider accepted the request. Preserve its actual response and alert.
	vanishingDB, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer vanishingDB.Close()
	tracker = &Tracker{Store: Store{DB: vanishingDB}, DBOnly: true}
	var logs bytes.Buffer
	oldLog := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(oldLog)
	base = transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		_ = vanishingDB.Close()
		return fakeBilledResponse("lost-after-payment"), nil
	})
	response, err := tracker.Wrap(base).RoundTrip(billingTestRequest(t, Request{Owner: "member"}))
	if err != nil || response == nil || response.StatusCode != 200 || calls != 3 {
		t.Fatalf("expected one paid call and its response: calls=%d err=%v", calls, err)
	}
	response.Body.Close()
	if err := db.QueryRow(`SELECT COUNT(*) FROM yzcm_usage_requests WHERE request_id='lost-after-payment'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("late-outage row count=%d err=%v", count, err)
	}
	if _, err := os.Lstat(dir); err == nil {
		t.Fatal("a recovery directory unexpectedly exists after the late outage")
	}
	if !strings.Contains(logs.String(), "receipt_not_durable") || !strings.Contains(logs.String(), receiptName("lost-after-payment")) || strings.Contains(logs.String(), "fixture-key") {
		t.Fatalf("missing sanitized alert for paid call without a durable receipt: %s", logs.String())
	}
}

func TestReceiptMySQLHiddenFirstClaimBlocksNewPaidCalls(t *testing.T) {
	db, _ := receiptRecoveryMySQL(t)
	parent := t.TempDir()
	dir, hidden := filepath.Join(parent, "recovery"), filepath.Join(parent, "hidden")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	first := &Tracker{Store: Store{DB: db}, ReceiptDir: dir}
	if err := first.InitializeReceiptStorage(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.Store.DB = nil // Simulate a ledger outage after this process validated its authority.
	second := &Tracker{Store: Store{DB: db}, ReceiptDir: dir}
	t.Cleanup(second.closeReceiptScan)
	ctx := context.Background()
	ownerA := Request{Owner: "member", TeamID: "team-a", ActorUsername: "founder"}
	ownerB := Request{Owner: "other", TeamID: "team-b", ActorUsername: "manager"}
	const firstID = "first-claim-hidden"
	saveFirst := func(id string) {
		t.Helper()
		response, err := first.Wrap(transportFunc(func(*http.Request) (*http.Response, error) {
			return fakeBilledResponse(id), nil
		})).RoundTrip(billingTestRequest(t, ownerA))
		if err != nil || response == nil {
			t.Fatalf("provider fixture failed: %v", err)
		}
		response.Body.Close()
		got, err := readReceipt(filepath.Join(dir, receiptName(id)))
		if err != nil || got.Request.Owner != ownerA.Owner {
			t.Fatalf("first local claim was not durable: %+v %v", got, err)
		}
	}
	move := func(src, dst string) {
		t.Helper()
		if err := os.Rename(src, dst); err != nil {
			t.Fatal(err)
		}
	}
	checkCount := func(id string, want int) {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM yzcm_usage_requests WHERE request_id=?`, id).Scan(&n); err != nil || n != want {
			t.Fatalf("request %s count=%d want %d error=%v", id, n, want, err)
		}
	}
	checkFirst := func(id string) {
		t.Helper()
		var owner, team, actor string
		if err := db.QueryRow(`SELECT owner,team_id,actor_username FROM yzcm_usage_requests WHERE request_id=?`, id).Scan(&owner, &team, &actor); err != nil || owner != ownerA.Owner || team != ownerA.TeamID || actor != ownerA.ActorUsername {
			t.Fatalf("request %s attribution=%q/%q/%q error=%v", id, owner, team, actor, err)
		}
	}
	saveFirst(firstID)
	move(dir, hidden)
	calls := 0
	base := transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return fakeBilledResponse(firstID), nil
	})
	checkBlocked := func() {
		t.Helper()
		unknown := &Tracker{Store: Store{DB: db}, ReceiptDir: dir}
		if resp, err := unknown.Wrap(base).RoundTrip(billingTestRequest(t, ownerB)); resp != nil || !errors.Is(err, ErrReceiptPersistenceUnavailable) || calls != 0 {
			t.Fatalf("hidden claim must block paid call: calls=%d error=%v", calls, err)
		}
		checkCount(firstID, 0)
	}
	checkBlocked()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("missing configured directory was recreated: %v", err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	checkBlocked()
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	move(hidden, dir)
	second.flushReceipts(ctx)
	checkFirst(firstID)

	const lateID = "first-claim-hidden-after-paid-call"
	saveFirst(lateID)
	var logs bytes.Buffer
	oldLog := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(oldLog)
	response, err := second.Wrap(transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		move(dir, hidden)
		return fakeBilledResponse(lateID), nil
	})).RoundTrip(billingTestRequest(t, ownerB))
	if err != nil || response == nil || response.StatusCode != 200 || calls != 1 {
		t.Fatalf("late outage changed provider result: calls=%d error=%v", calls, err)
	}
	response.Body.Close()
	checkCount(lateID, 0)
	if !strings.Contains(logs.String(), "receipt_not_durable") {
		t.Fatalf("missing late-outage alert: %s", logs.String())
	}
	backup := filepath.Join(parent, ".recovery-receipt-id.unverified")
	if files := receiptFiles(t, backup); len(files) != 1 {
		t.Fatalf("backup evidence missing: %v", files)
	}
	move(hidden, dir)
	second.flushReceipts(ctx)
	checkFirst(lateID)
}

func TestReceiptMySQLReadOnlyPreflightRejectsPaidCall(t *testing.T) {
	db, _ := receiptRecoveryMySQL(t)
	db.SetMaxOpenConns(1)
	tracker := &Tracker{Store: Store{DB: db}, DBOnly: true}
	if err := tracker.InitializeReceiptStorage(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`SET SESSION TRANSACTION READ ONLY`); err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("read-only host should still answer Ping: %v", err)
	}
	calls := 0
	base := transportFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return fakeBilledResponse("unexpected-charge"), nil
	})
	for i := 0; i < 2; i++ {
		resp, err := tracker.Wrap(base).RoundTrip(billingTestRequest(t, Request{Owner: "member"}))
		var sqlErr *mysql.MySQLError
		if resp != nil || calls != 0 || !errors.Is(err, ErrReceiptPersistenceUnavailable) || !errors.As(err, &sqlErr) || sqlErr.Number != 1792 {
			t.Fatalf("read-only preflight allowed payment: calls=%d response=%v error=%v", calls, resp, err)
		}
	}
	if _, err := db.Exec(`SET SESSION TRANSACTION READ WRITE`); err != nil {
		t.Fatal(err)
	}
	if err := (Store{DB: db}).probeReceiptWrite(context.Background()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM yzcm_usage_requests`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("write probe left usage rows: count=%d error=%v", count, err)
	}
}

func TestReceiptMySQLQuarantinePreservesConflictsAndMakesRecoveryFair(t *testing.T) {
	db, _ := receiptRecoveryMySQL(t)
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: t.TempDir()}
	t.Cleanup(tracker.closeReceiptScan)
	ctx := context.Background()
	const conflictCount = 111
	hash := Fingerprint("fixture-key")
	at := time.Now().UTC()
	want := make(map[string]receipt, conflictCount+1)
	for i := 0; i <= conflictCount; i++ {
		id := fmt.Sprintf("pending-%d", i)
		r := receipt{Request: Request{ID: id, Owner: "member", TeamID: "team-a", ActorUsername: "founder", Feature: "script"}, CredentialHash: hash, Status: 200, At: at}
		if err := tracker.saveReceipt(r); err != nil {
			t.Fatal(err)
		}
		want[receiptName(id)] = r
	}
	// Choose the tail of the actual native listing, regardless of filesystem order.
	stream, err := os.Open(tracker.ReceiptDir)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := stream.ReadDir(-1)
	stream.Close()
	if err != nil {
		t.Fatal(err)
	}
	var normalName string
	for _, entry := range entries {
		if _, fixture := want[entry.Name()]; fixture {
			normalName = entry.Name()
		}
	}
	normalID := want[normalName].Request.ID
	if normalID == "" {
		t.Fatal("no fixture in directory listing")
	}
	delete(want, normalName)
	seeds, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer seeds.Rollback()
	for _, r := range want {
		if _, err := seeds.Exec(`INSERT INTO yzcm_usage_requests (request_id,owner,team_id,actor_username,feature,credential_hash,created_at,http_status)
VALUES (?,'first-owner','first-team','first-actor','script',?,?,200)`, r.Request.ID, hash, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := seeds.Commit(); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(tracker.ReceiptDir, ".review")
	if err := os.WriteFile(blocked, []byte("block quarantine"), 0600); err != nil {
		t.Fatal(err)
	}
	const maxPasses = 12
	for i := 0; i < maxPasses; i++ {
		tracker.flushReceipts(ctx)
		if len(receiptFiles(t, tracker.ReceiptDir)) == conflictCount {
			break
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM yzcm_usage_requests WHERE request_id=?`, normalID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("normal receipt starved behind %d conflicts: rows=%d err=%v", conflictCount, count, err)
	}
	if files := receiptFiles(t, tracker.ReceiptDir); len(files) != conflictCount {
		t.Fatalf("blocked quarantine lost evidence: got %d, want %d", len(files), conflictCount)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxPasses && len(receiptFiles(t, tracker.ReceiptDir)) > 0; i++ {
		tracker.flushReceipts(ctx)
	}
	if files := receiptFiles(t, tracker.ReceiptDir); len(files) != 0 {
		t.Fatalf("%d confirmed conflicts still occupy the activity queue", len(files))
	}
	review := filepath.Join(tracker.ReceiptDir, ".review", "attribution-conflict")
	if files := receiptFiles(t, review); len(files) != conflictCount {
		t.Fatalf("review files=%d, want %d", len(files), conflictCount)
	}
	for name, expected := range want {
		got, err := readReceipt(filepath.Join(review, name))
		if err != nil || got.Request != expected.Request || got.CredentialHash != expected.CredentialHash || got.Status != expected.Status || !got.At.Equal(expected.At) {
			t.Fatalf("original evidence %s changed: %+v err=%v", name, got, err)
		}
	}
}

func billingTestRequest(t *testing.T, identity Request) *http.Request {
	t.Helper()
	ctx := WithAttribution(context.Background(), identity.Owner, identity.TeamID, identity.ActorUsername)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, Origin+"/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer fixture-key")
	return r
}

func fakeBilledResponse(id string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Oneapi-Request-Id": []string{id}}, Body: io.NopCloser(strings.NewReader("provider result"))}
}

func receiptRecoveryMySQL(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv("QIANTIE_TEST_MYSQL84_DSN")
	if dsn == "" {
		t.Skip("QIANTIE_TEST_MYSQL84_DSN must point to a disposable MySQL host")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	name := fmt.Sprintf("usage_receipt_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP DATABASE " + name); err != nil {
			t.Error(err)
		}
	})
	cfg.DBName, cfg.ParseTime = name, true
	dsn = cfg.FormatDSN()
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, stmt := range append(SchemaStatements(), `ALTER TABLE yzcm_usage_requests ADD COLUMN team_id VARCHAR(64) NULL, ADD COLUMN actor_username VARCHAR(32) NULL`, ReceiptBindingStatement()) {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	return db, dsn
}
