package yzcmusage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

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
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("durable receipt absent %v %v", entries, err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-key") || strings.Contains(string(raw), "prompt") || strings.Contains(string(raw), "Authorization") {
		t.Fatal("sensitive content persisted")
	}
	restart := &Tracker{Store: Store{DB: db}, ReceiptDir: dir}
	m.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs(r.ID, r.Owner, r.TeamID, r.ActorUsername, r.Feature, hash, at, 200).WillReturnResult(sqlmock.NewResult(0, 1))
	restart.flushReceipts(context.Background())
	entries, _ = os.ReadDir(dir)
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
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("uncommitted receipt deleted")
	}
	m.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs("same", "alice", "", "", "image", hash, at, 200).WillReturnResult(sqlmock.NewResult(0, 0))
	tracker.flushReceipts(context.Background())
	entries, _ = os.ReadDir(dir)
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

func TestConcurrentDuplicateActorAndTeamWaitForFirstReceipt(t *testing.T) {
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
	tracker.recordReceipt(context.Background(), Request{ID: "same", Owner: "other", TeamID: "team-b", ActorUsername: "manager", Feature: "image"}, Fingerprint("another-key"), 201, at.Add(time.Minute))
	<-done
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
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
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("no receipt %v", err)
	}
	receiptRaw, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
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
		entries, _ := os.ReadDir(tracker.ReceiptDir)
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
