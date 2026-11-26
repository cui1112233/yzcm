package yzcmusage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"
)

func receiptFiles(t *testing.T, dir string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "receipt-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestFailedReceiptSurvivesRestartWithoutKeyOrContent(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dir := filepath.Join(t.TempDir(), "receipts")
	at := time.Date(2026, 11, 24, 2, 0, 0, 0, time.UTC)
	r := Request{ID: "request1", Owner: "alice", Feature: "image", TeamID: "team-a", ActorUsername: "founder"}
	hash := Fingerprint("secret-key")
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: dir}
	m.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs(r.ID, r.Owner, r.TeamID, r.ActorUsername, r.Feature, hash, at, 200).WillReturnError(errors.New("db temporarily unavailable"))
	tracker.recordReceipt(context.Background(), r, hash, 200, at)
	entries := receiptFiles(t, dir)
	if len(entries) != 1 {
		t.Fatalf("durable receipt absent %v", entries)
	}
	raw, err := os.ReadFile(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-key") || strings.Contains(string(raw), "prompt") || strings.Contains(string(raw), "Authorization") {
		t.Fatal("sensitive content persisted")
	}
	restart := &Tracker{Store: Store{DB: db}, ReceiptDir: dir}
	m.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs(r.ID, r.Owner, r.TeamID, r.ActorUsername, r.Feature, hash, at, 200).WillReturnResult(sqlmock.NewResult(0, 1))
	restart.flushReceipts(context.Background())
	entries = receiptFiles(t, dir)
	if len(entries) != 0 {
		t.Fatal("successful durable receipt not removed")
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestReceiptDuplicateCannotMoveOwnerAndFailedFlushRetainsFile(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	dir := filepath.Join(t.TempDir(), "receipts")
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: dir}
	at := time.Now().UTC()
	hash := Fingerprint("secret")
	if err := tracker.saveReceipt(receipt{Request: Request{ID: "same", Owner: "alice", Feature: "image"}, CredentialHash: hash, Status: 200, At: at}); err != nil {
		t.Fatal(err)
	}
	if err := tracker.saveReceipt(receipt{Request: Request{ID: "same", Owner: "bob", Feature: "image"}, CredentialHash: hash, Status: 200, At: at}); err != nil {
		t.Fatal(err)
	}
	m.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs("same", "alice", "", "", "image", hash, at, 200).WillReturnError(errors.New("still down"))
	tracker.flushReceipts(context.Background())
	entries := receiptFiles(t, dir)
	if len(entries) != 1 {
		t.Fatal("uncommitted receipt deleted")
	}
	m.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs("same", "alice", "", "", "image", hash, at, 200).WillReturnResult(sqlmock.NewResult(0, 0))
	tracker.flushReceipts(context.Background())
	entries = receiptFiles(t, dir)
	if len(entries) != 0 {
		t.Fatal("idempotent alreadyrecorded receipt retained")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDuplicateActorAndTeamUseOriginalSpooledReceiptAfterDatabaseRecovers(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: t.TempDir()}
	at := time.Now().UTC()
	original := Request{ID: "same", Owner: "member", TeamID: "team-a", ActorUsername: "founder", Feature: "script"}
	hash := Fingerprint("fixture-key")
	mock.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs(original.ID, original.Owner, original.TeamID, original.ActorUsername, original.Feature, hash, at, 200).
		WillReturnError(errors.New("database unavailable"))
	tracker.recordReceipt(context.Background(), original, hash, 200, at)
	mock.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs(original.ID, original.Owner, original.TeamID, original.ActorUsername, original.Feature, hash, at, 200).
		WillReturnResult(sqlmock.NewResult(0, 1))
	tracker.recordReceipt(context.Background(), Request{ID: "same", Owner: "other", TeamID: "team-b", ActorUsername: "manager", Feature: "image"}, Fingerprint("another-key"), 201, at.Add(time.Minute))
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentTrackersKeepFirstActorAndTeam(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherFunc(func(expected, actual string) error {
		once.Do(func() { close(started) })
		return sqlmock.QueryMatcherRegexp.Match(expected, actual)
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: t.TempDir()}
	second := &Tracker{Store: Store{DB: db}, ReceiptDir: tracker.ReceiptDir}
	at := time.Now().UTC()
	original := Request{ID: "same", Owner: "member", TeamID: "team-a", ActorUsername: "founder", Feature: "script"}
	hash := Fingerprint("fixture-key")
	mock.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs(original.ID, original.Owner, original.TeamID, original.ActorUsername, original.Feature, hash, at, 200).
		WillDelayFor(50 * time.Millisecond).WillReturnError(errors.New("database unavailable"))
	mock.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs(original.ID, original.Owner, original.TeamID, original.ActorUsername, original.Feature, hash, at, 200).
		WillReturnResult(sqlmock.NewResult(0, 1))
	done := make(chan struct{})
	go func() {
		defer close(done)
		tracker.recordReceipt(context.Background(), original, hash, 200, at)
	}()
	<-started
	if err := second.recordReceipt(context.Background(), Request{ID: "same", Owner: "other", TeamID: "team-b", ActorUsername: "manager", Feature: "image"}, Fingerprint("another-key"), 201, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	<-done
	if files := receiptFiles(t, tracker.ReceiptDir); len(files) != 0 {
		t.Fatalf("successfully committed claim was retained: %v", files)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReceiptActorAndTeamConflictPreservesClaimUntilLedgerAgrees(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: t.TempDir()}
	at := time.Now().UTC()
	original := Request{ID: "same", Owner: "member", TeamID: "team-a", ActorUsername: "founder", Feature: "script"}
	hash := Fingerprint("fixture-key")
	expectDuplicate := func(owner, team, actor string) {
		mock.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs(original.ID, original.Owner, original.TeamID, original.ActorUsername, original.Feature, hash, at, 200).
			WillReturnError(&mysql.MySQLError{Number: 1062})
		mock.ExpectQuery("SELECT owner,COALESCE").WithArgs(original.ID).
			WillReturnRows(sqlmock.NewRows([]string{"owner", "team", "actor"}).AddRow(owner, team, actor))
	}
	expectDuplicate("other", "team-b", "manager")
	if err := tracker.recordReceipt(context.Background(), original, hash, 200, at); !errors.Is(err, ErrReceiptAttributionConflict) {
		t.Fatalf("conflicting stored receipt returned %v", err)
	}
	expectDuplicate("other", "team-b", "manager")
	tracker.flushReceipts(context.Background())
	files := receiptFiles(t, tracker.ReceiptDir)
	if len(files) != 1 {
		t.Fatalf("conflict record was discarded: %v", files)
	}
	if saved, err := readReceipt(files[0]); err != nil || saved.Request != original {
		t.Fatalf("original claim changed: %+v %v", saved.Request, err)
	}
	expectDuplicate("member", "team-a", "founder")
	tracker.flushReceipts(context.Background())
	if files := receiptFiles(t, tracker.ReceiptDir); len(files) != 0 {
		t.Fatalf("matching claim was retained: %v", files)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReceiptLockReleasesOnProcessExitAndOrphanTempIsReaped(t *testing.T) {
	at := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
	original := Request{ID: "crash-receipt", Owner: "member", TeamID: "team-a", ActorUsername: "founder", Feature: "script"}
	hash := Fingerprint("fixture-key")
	if dir := os.Getenv("QIANTIE_RECEIPT_LOCK_TEST_CHILD"); dir != "" {
		tracker := &Tracker{ReceiptDir: dir}
		if err := tracker.saveReceipt(receipt{Request: original, CredentialHash: hash, Status: 200, At: at}); err != nil {
			t.Fatal(err)
		}
		lock, err := openReceiptLock(context.Background(), receiptLockPath(dir, receiptName(original.ID)))
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		fmt.Println("locked")
		time.Sleep(time.Minute)
		return
	}
	dir := t.TempDir()
	lockPath := receiptLockPath(dir, receiptName(original.ID))
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestReceiptLockReleasesOnProcessExitAndOrphanTempIsReaped$")
	child.Env = append(os.Environ(), "QIANTIE_RECEIPT_LOCK_TEST_CHILD="+dir)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	var result string
	if _, err := fmt.Fscan(stdout, &result); err != nil || result != "locked" {
		t.Fatalf("child lock result=%q err=%v", result, err)
	}
	wait, release := context.WithTimeout(ctx, 50*time.Millisecond)
	defer release()
	if lock, err := openReceiptLock(wait, lockPath); !errors.Is(err, context.DeadlineExceeded) {
		if lock != nil {
			_ = lock.Close()
		}
		t.Fatalf("another process holds the lock, got %v", err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	if lock, err := openReceiptLock(ctx, lockPath); err != nil {
		t.Fatal(err)
	} else {
		_ = lock.Close()
	}
	orphan := filepath.Join(dir, ".receipt-orphan.tmp")
	fresh := filepath.Join(dir, ".receipt-fresh.tmp")
	for _, path := range []string{orphan, fresh} {
		if err := os.WriteFile(path, nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(orphan, old, old); err != nil {
		t.Fatal(err)
	}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs(original.ID, original.Owner, original.TeamID, original.ActorUsername, original.Feature, hash, at, 200).
		WillReturnResult(sqlmock.NewResult(0, 1))
	(&Tracker{Store: Store{DB: db}, ReceiptDir: dir}).flushReceipts(ctx)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if files := receiptFiles(t, dir); len(files) != 0 {
		t.Fatalf("claim from interrupted worker retained after replay: %v", files)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("orphan cleanup: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh temp should survive cleanup: %v", err)
	}
}

func TestReceiptRejectsUntrustedNamesAndDirectories(t *testing.T) {
	tracker := &Tracker{ReceiptDir: filepath.Join(t.TempDir(), "r")}
	if err := tracker.saveReceipt(receipt{Request: Request{ID: "../escape", Owner: "alice", Feature: "image"}, CredentialHash: "invalid", At: time.Now()}); err == nil {
		t.Fatal("invalid receipt accepted")
	}
	if _, err := os.Stat(tracker.ReceiptDir); !os.IsNotExist(err) {
		t.Fatal("invalid receipt created directory")
	}
}
func TestTrackerDatabaseFailureKeepsGenerationResponse(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	dir := filepath.Join(t.TempDir(), "r")
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: dir}
	m.ExpectExec("INSERT INTO yzcm_usage_requests").WillReturnError(errors.New("db down"))
	transport := tracker.Wrap(transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Oneapi-Request-Id": []string{"actual-call"}}, Body: io.NopCloser(strings.NewReader("unchanged private generation"))}, nil
	}))
	r, _ := http.NewRequestWithContext(WithIdentity(context.Background(), "alice", "script"), http.MethodPost, Origin+"/v1/chat/completions", strings.NewReader("private prompt"))
	r.Header.Set("Authorization", "Bearer private-key")
	response, err := transport.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(response.Body)
	if string(raw) != "unchanged private generation" {
		t.Fatal("response changed")
	}
	entries := receiptFiles(t, dir)
	if len(entries) != 1 {
		t.Fatal("no receipt")
	}
	receiptRaw, _ := os.ReadFile(entries[0])
	if strings.Contains(string(receiptRaw), "private") {
		t.Fatal("secret content persisted")
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestTrackerStartupFlushesOldReceipt(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: filepath.Join(t.TempDir(), "r")}
	at := time.Now().UTC()
	hash := Fingerprint("key")
	if err := tracker.saveReceipt(receipt{Request: Request{ID: "old", Owner: "alice", Feature: "image"}, CredentialHash: hash, Status: 200, At: at}); err != nil {
		t.Fatal(err)
	}
	m.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs("old", "alice", "", "", "image", hash, at, 200).WillReturnResult(sqlmock.NewResult(0, 1))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tracker.Run(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for {
		entries := receiptFiles(t, tracker.ReceiptDir)
		if len(entries) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startup did not flush old receipt")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
