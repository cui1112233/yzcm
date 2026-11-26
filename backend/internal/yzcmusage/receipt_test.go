package yzcmusage

import (
	"context"
	"encoding/json"
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

func (t *Tracker) saveReceipt(r receipt) error {
	b, err := t.bindReceiptDirectory(context.Background())
	if err != nil {
		return err
	}
	return saveReceiptAt(b.dir, r)
}

func (t *Tracker) recordReceipt(ctx context.Context, r Request, hash string, status int, at time.Time) error {
	b, err := t.bindReceiptDirectory(ctx)
	if err != nil {
		return err
	}
	return t.recordBoundReceipt(ctx, &b, receipt{Request: r, CredentialHash: hash, Status: status, At: at})
}

// File-recovery unit tests start with an already validated binding. The
// database initialization and cold-start protocol have their own tests.
func cacheReceiptTestBinding(t *testing.T, tracker *Tracker) {
	t.Helper()
	dir, err := filepath.Abs(tracker.ReceiptDir)
	if err != nil {
		t.Fatal(err)
	}
	b := receiptBinding{dir: dir, anchor: filepath.Join(filepath.Dir(dir), "."+filepath.Base(dir)+"-receipt-id"), id: strings.Repeat("a", 32), pathDigest: receiptPathDigest(dir)}
	row := receiptBindingRow{StoreID: b.id, Origin: receiptBindingUpgrade, SyncParentLevels: 1}
	if _, err := preparePendingReceiptDirectory(context.Background(), b, row, nil); err != nil {
		t.Fatal(err)
	}
	b.info, err = visibleReceiptDirectory(dir)
	if err != nil {
		t.Fatal(err)
	}
	b.objects, err = b.filesystemObjects()
	if err != nil {
		t.Fatal(err)
	}
	tracker.receiptBound = &b
}

func TestFailedReceiptSurvivesRestartWithoutKeyOrContent(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dir := t.TempDir()
	at := time.Date(2026, 11, 24, 2, 0, 0, 0, time.UTC)
	r := Request{ID: "request1", Owner: "alice", Feature: "image", TeamID: "team-a", ActorUsername: "founder"}
	hash := Fingerprint("secret-key")
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: dir}
	cacheReceiptTestBinding(t, tracker)
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
	cacheReceiptTestBinding(t, restart)
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
	dir := t.TempDir()
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: dir}
	cacheReceiptTestBinding(t, tracker)
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
	cacheReceiptTestBinding(t, tracker)
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
	cacheReceiptTestBinding(t, tracker)
	cacheReceiptTestBinding(t, second)
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

func TestReceiptActorAndTeamConflictQuarantinesClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: t.TempDir()}
	cacheReceiptTestBinding(t, tracker)
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
	tracker.flushReceipts(context.Background())
	if files := receiptFiles(t, tracker.ReceiptDir); len(files) != 0 {
		t.Fatalf("conflict still occupies recovery: %v", files)
	}
	files := receiptFiles(t, receiptReviewDirectory(tracker.ReceiptDir))
	if len(files) != 1 {
		t.Fatalf("conflict record was discarded: %v", files)
	}
	if saved, err := readReceipt(files[0]); err != nil || saved.Request != original {
		t.Fatalf("original claim changed: %+v %v", saved.Request, err)
	}
	if err := tracker.recordReceipt(context.Background(), Request{ID: "same", Owner: "other", TeamID: "team-b", ActorUsername: "manager", Feature: "image"}, hash, 200, at); !errors.Is(err, ErrReceiptAttributionConflict) {
		t.Fatalf("duplicate of quarantined receipt returned %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReceiptQuarantineResumesAfterPublishingReviewEvidence(t *testing.T) {
	tracker := &Tracker{ReceiptDir: t.TempDir()}
	cacheReceiptTestBinding(t, tracker)
	original := receipt{
		Request:        Request{ID: "quarantine-restart", Owner: "member", TeamID: "team-a", ActorUsername: "founder", Feature: "script"},
		CredentialHash: Fingerprint("fixture-key"), Status: 200, At: time.Now().UTC(),
	}
	if err := tracker.saveReceipt(original); err != nil {
		t.Fatal(err)
	}
	name := receiptName(original.Request.ID)
	review := receiptReviewDirectory(tracker.ReceiptDir)
	if err := os.MkdirAll(review, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(tracker.ReceiptDir, name), filepath.Join(review, name)); err != nil {
		t.Fatal(err)
	}
	// Recovery starts after the review link was published, before active removal.
	tracker.flushReceipts(context.Background())
	if files := receiptFiles(t, tracker.ReceiptDir); len(files) != 0 {
		t.Fatalf("active link retained: %v", files)
	}
	got, err := readReceipt(filepath.Join(review, name))
	if err != nil || got.Request != original.Request || !got.At.Equal(original.At) {
		t.Fatalf("quarantined original changed: %+v %v", got, err)
	}
}

func TestReceiptQuarantineKeepsRacedDuplicateEvidence(t *testing.T) {
	tracker := &Tracker{ReceiptDir: t.TempDir()}
	cacheReceiptTestBinding(t, tracker)
	first := receipt{
		Request:        Request{ID: "raced", Owner: "member", TeamID: "team-a", ActorUsername: "founder", Feature: "script"},
		CredentialHash: Fingerprint("fixture-key"), Status: 200, At: time.Now().UTC(),
	}
	if err := tracker.saveReceipt(first); err != nil {
		t.Fatal(err)
	}
	name := receiptName(first.Request.ID)
	if err := quarantineReceipt(tracker.ReceiptDir, name); !errors.Is(err, ErrReceiptAttributionConflict) || errors.Is(err, errReceiptQuarantineFailed) {
		t.Fatal(err)
	}
	// This publisher passed its review check before the first evidence moved.
	second := first
	second.Request.Owner, second.Request.TeamID, second.Request.ActorUsername = "other", "team-b", "manager"
	raw, err := json.Marshal(second)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tracker.ReceiptDir, name), raw, 0600); err != nil {
		t.Fatal(err)
	}
	tracker.flushReceipts(context.Background())
	if files := receiptFiles(t, tracker.ReceiptDir); len(files) != 0 {
		t.Fatalf("raced receipt still active: %v", files)
	}
	review := receiptReviewDirectory(tracker.ReceiptDir)
	if got, err := readReceipt(filepath.Join(review, name)); err != nil || got.Request != first.Request {
		t.Fatalf("first evidence changed: %+v %v", got, err)
	}
	if got, err := readReceipt(filepath.Join(review, "duplicate-"+Fingerprint(string(raw)), name)); err != nil || got.Request != second.Request {
		t.Fatalf("raced evidence lost: %+v %v", got, err)
	}
}

func TestReceiptFallbackUsesFirstClaimWhenLockDirectoryIsUnavailable(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: t.TempDir()}
	cacheReceiptTestBinding(t, tracker)
	original := receipt{
		Request:        Request{ID: "fallback", Owner: "member", TeamID: "team-a", ActorUsername: "founder", Feature: "script"},
		CredentialHash: Fingerprint("fixture-key"), Status: 200, At: time.Now().UTC(),
	}
	if err := tracker.saveReceipt(original); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tracker.ReceiptDir, ".locks"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	mock.ExpectExec("INSERT INTO yzcm_usage_requests").WithArgs(original.Request.ID, original.Request.Owner, original.Request.TeamID,
		original.Request.ActorUsername, original.Request.Feature, original.CredentialHash, original.At, 200).WillReturnResult(sqlmock.NewResult(0, 1))
	duplicate := Request{ID: "fallback", Owner: "other", TeamID: "team-b", ActorUsername: "manager", Feature: "image"}
	if err := tracker.recordReceipt(context.Background(), duplicate, Fingerprint("other-key"), 200, original.At.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	if files := receiptFiles(t, tracker.ReceiptDir); len(files) != 1 {
		t.Fatalf("fallback without the lock should retain the first claim: %v", files)
	}
}

func TestReceiptLockReleasesOnProcessExitAndOrphanTempIsReaped(t *testing.T) {
	at := time.Date(2026, 11, 26, 8, 0, 0, 0, time.UTC)
	original := Request{ID: "crash-receipt", Owner: "member", TeamID: "team-a", ActorUsername: "founder", Feature: "script"}
	hash := Fingerprint("fixture-key")
	if dir := os.Getenv("QIANTIE_RECEIPT_LOCK_TEST_CHILD"); dir != "" {
		tracker := &Tracker{ReceiptDir: dir}
		cacheReceiptTestBinding(t, tracker)
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
	restarted := &Tracker{Store: Store{DB: db}, ReceiptDir: dir}
	cacheReceiptTestBinding(t, restarted)
	restarted.flushReceipts(ctx)
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
	dir := t.TempDir()
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: dir}
	cacheReceiptTestBinding(t, tracker)
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
	tracker := &Tracker{Store: Store{DB: db}, ReceiptDir: t.TempDir()}
	cacheReceiptTestBinding(t, tracker)
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

func TestReceiptScanBoundsDirectoryWorkAndCancellation(t *testing.T) {
	var smallScan, smallCanceled float64
	for _, entries := range []int{100, 3000, 6000} {
		t.Run(fmt.Sprint(entries), func(t *testing.T) {
			tracker := &Tracker{ReceiptDir: t.TempDir()}
			t.Cleanup(tracker.closeReceiptScan)
			for i := range entries {
				if err := os.WriteFile(filepath.Join(tracker.ReceiptDir, fmt.Sprintf("fixture-%06d", i)), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			cacheReceiptTestBinding(t, tracker)
			ctx := context.Background()
			canceled, stop := context.WithCancel(ctx)
			stop()
			canceledAllocs := testing.AllocsPerRun(5, func() { tracker.flushReceipts(canceled) })
			if canceledAllocs > 2 || tracker.receiptScan != nil {
				t.Fatalf("canceled scan opened or allocated for directory: %f", canceledAllocs)
			}
			scanAllocs := testing.AllocsPerRun(3, func() {
				tracker.closeReceiptScan()
				tracker.flushReceipts(ctx)
			})
			if entries == 100 {
				smallScan, smallCanceled = scanAllocs, canceledAllocs
			} else if scanAllocs > smallScan+64 || canceledAllocs > smallCanceled+2 {
				t.Fatalf("scan allocs scaled with directory size: active=%f/%f canceled=%f/%f", scanAllocs, smallScan, canceledAllocs, smallCanceled)
			}
			if tracker.receiptScan == nil {
				t.Fatal("directory stream was not retained at the 100-item bound")
			}
			remaining := 0
			for {
				batch, err := tracker.receiptScan.ReadDir(100)
				remaining += len(batch)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			// The binding marker lives beside the fixtures and counts as one entry.
			if want := entries + 1 - 100; remaining != want {
				t.Fatalf("one pass consumed %d entries, want 100", entries+1-remaining)
			}
			t.Logf("entries=%d per-pass allocs=%.0f canceled allocs=%.0f", entries, scanAllocs, canceledAllocs)
		})
	}
}

func TestReceiptWriterProbeCoalescesConcurrentFallbackChecks(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tracker := &Tracker{DBOnly: true, Store: Store{DB: db}}
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO yzcm_usage_requests").WillDelayFor(150 * time.Millisecond).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	const callers = 10
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			results <- tracker.checkReceiptWriter(context.Background())
		}()
	}
	close(start)
	for range callers {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestKnownReceiptBindingRejectsReplacementOfEntireParent(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	dir := filepath.Join(parent, "recovery")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	tracker := &Tracker{ReceiptDir: dir}
	cacheReceiptTestBinding(t, tracker)
	if _, err := tracker.preflightReceipt(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(parent, filepath.Join(root, "hidden")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := tracker.preflightReceipt(context.Background()); !errors.Is(err, ErrReceiptPersistenceUnavailable) {
		t.Fatalf("live tracker adopted a replacement parent: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, receiptDirectoryIDFile)); !os.IsNotExist(err) {
		t.Fatalf("replacement was stamped: %v", err)
	}
}
