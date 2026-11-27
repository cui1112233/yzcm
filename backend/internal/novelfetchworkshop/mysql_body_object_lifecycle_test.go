package novelfetchworkshop

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"regexp"
	"testing"
	"time"
)

func expectLockedBody(mock sqlmock.Sqlmock, owner, book, version string, rev uint64, encoding string, blob []byte, state string, released, expires driver.Value) {
	mock.ExpectQuery(regexp.QuoteMeta(lockBodySQL)).WithArgs(owner, book, version).WillReturnRows(sqlmock.NewRows([]string{"revision", "content_encoding", "content_blob", "state", "releasable_at", "expires_at"}).AddRow(rev, encoding, blob, state, released, expires))
}
func expectBodyRemoval(mock sqlmock.Sqlmock, owner, book, version string, rev uint64, encoding string, blob []byte) {
	mock.ExpectExec("DELETE FROM novel_fetch_workshop_bodies").WithArgs(owner, book, version, rev, encoding, blob).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE novel_fetch_workshop_documents SET document_json=JSON_MERGE_PATCH").WithArgs(bodyRefJSONPath(version), sqlmock.AnyArg(), owner, book).WillReturnResult(sqlmock.NewResult(0, 1))
}
func fakeBodyObjects(s *MySQLStore, objects map[string]string, deletes *int) {
	s.ObjectBodies = &BodyObjectStore{Bucket: "fake", Put: func(_ context.Context, _, key string, b []byte) error { objects[key] = string(b); return nil }, Get: func(_ context.Context, _, key string) ([]byte, error) { return []byte(objects[key]), nil }, Delete: func(_ context.Context, _, key string) error { *deletes++; delete(objects, key); return nil }}
}
func assertBodyExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Originally reproduced with the old upsert protocol: its failure deleted the
// only fixed object. Under the new protocol every failure retains both objects.
func TestAudit20261127FailedTOSRewritePreservesPreviousBody(t *testing.T) {
	for _, failure := range []string{"begin", "lock", "write", "retirement", "commit"} {
		t.Run(failure, func(t *testing.T) {
			_, mock, s := newBodySQLMock(t)
			key := BodyObjectKey("alice", "123", "ai1")
			objects := map[string]string{key: "old successful body"}
			deletes := 0
			fakeBodyObjects(s, objects, &deletes)
			injected := errors.New("injected SQL failure")
			if failure == "begin" {
				mock.ExpectBegin().WillReturnError(injected)
			} else {
				mock.ExpectBegin()
				if failure == "lock" {
					mock.ExpectQuery(regexp.QuoteMeta(lockBodySQL)).WillReturnError(injected)
					mock.ExpectRollback()
				} else {
					expectLockedBody(mock, "alice", "123", "ai1", 3, "tos", []byte(key), "ready", nil, nil)
					write := mock.ExpectExec("UPDATE novel_fetch_workshop_bodies SET revision").WithArgs(uint64(4), "tos", sqlmock.AnyArg(), sqlmock.AnyArg(), int64(11), "ready", sqlmock.AnyArg(), sqlmock.AnyArg(), "alice", "123", "ai1")
					if failure == "write" {
						write.WillReturnError(injected)
						mock.ExpectRollback()
					} else {
						write.WillReturnResult(sqlmock.NewResult(0, 1))
						retire := mock.ExpectExec("INSERT INTO novel_fetch_body_object_retirements")
						if failure == "retirement" {
							retire.WillReturnError(injected)
							mock.ExpectRollback()
						} else {
							retire.WillReturnResult(sqlmock.NewResult(0, 1))
							expectBodyMetadataWrite(mock, "alice", "123")
							mock.ExpectCommit().WillReturnError(injected)
						}
					}
				}
			}
			_, err := s.PutBody(context.Background(), "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "ai1"}, Content: "new attempt"})
			if err == nil {
				t.Fatal("failure injection not exercised")
			}
			assertBodyExpectations(t, mock)
			if objects[key] != "old successful body" || deletes != 0 || len(objects) != 2 {
				t.Fatalf("body loss: objects=%v deletes=%d", objects, deletes)
			}
		})
	}
}

func TestTOSPutFailureDoesNotBeginReferenceTransaction(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	s.ObjectBodies = &BodyObjectStore{Bucket: "fake", Put: func(context.Context, string, string, []byte) error { return errors.New("upload failed") }}
	if _, err := s.PutBody(context.Background(), "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "ai1"}, Content: "body"}); err == nil {
		t.Fatal("expected upload failure")
	}
	assertBodyExpectations(t, mock)
}

func TestTOSWritesUseImmutableKeysAndReturnTheirExactRevision(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	old := BodyObjectKey("alice", "123", "ai1")
	objects := map[string]string{old: "old"}
	deletes := 0
	fakeBodyObjects(s, objects, &deletes)
	previous := old
	for i := uint64(2); i <= 3; i++ {
		mock.ExpectBegin()
		expectLockedBody(mock, "alice", "123", "ai1", i-1, "tos", []byte(previous), "ready", nil, nil)
		mock.ExpectExec("UPDATE novel_fetch_workshop_bodies SET revision").WithArgs(i, "tos", sqlmock.AnyArg(), sqlmock.AnyArg(), int64(4), "ready", sqlmock.AnyArg(), sqlmock.AnyArg(), "alice", "123", "ai1").WillReturnResult(sqlmock.NewResult(0, 1))
		expectedState := "pending"
		if i == 2 {
			expectedState = "held"
		}
		mock.ExpectExec("INSERT INTO novel_fetch_body_object_retirements").WithArgs(previous, "alice", "123", "ai1", sqlmock.AnyArg(), sqlmock.AnyArg(), expectedState).WillReturnResult(sqlmock.NewResult(0, 1))
		expectBodyMetadataWrite(mock, "alice", "123")
		mock.ExpectCommit()
		ref, err := s.PutBody(context.Background(), "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "ai1"}, Content: "body"})
		if err != nil || ref.Revision != i || ref.UpdatedAt == "" {
			t.Fatalf("ref=%+v err=%v", ref, err)
		}
		for key := range objects {
			if key != old && key != previous {
				previous = key
				break
			}
		}
		if !immutableBodyObjectKey(previous, "alice", "123", "ai1") || immutableBodyObjectKey(previous, "bob", "123", "ai1") {
			t.Fatal("unsafe key")
		}
	}
	assertBodyExpectations(t, mock)
	if len(objects) != 3 || deletes != 0 || objects[old] != "old" {
		t.Fatalf("immutable writes changed previous bodies: %v", objects)
	}
}

func TestTOSFirstInsertConflictRetainsUploadedObject(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	objects := map[string]string{}
	deletes := 0
	fakeBodyObjects(s, objects, &deletes)
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(lockBodySQL)).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec("INSERT INTO novel_fetch_workshop_bodies").WillReturnError(errors.New("duplicate/deadlock"))
	mock.ExpectRollback()
	if _, err := s.PutBody(context.Background(), "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "ai1"}, Content: "body"}); err == nil {
		t.Fatal("conflict must be returned")
	}
	if len(objects) != 1 || deletes != 0 {
		t.Fatal("uncertain insert removed upload")
	}
	assertBodyExpectations(t, mock)
}

func expectCleanupCandidate(mock sqlmock.Sqlmock, owner, book, version string, revision uint64, encoding, key string, released time.Time) {
	mock.ExpectQuery("SELECT owner_username, book_id, version_id, revision, content_encoding, content_blob, releasable_at").WillReturnRows(sqlmock.NewRows([]string{"owner_username", "book_id", "version_id", "revision", "content_encoding", "content_blob", "releasable_at"}).AddRow(owner, book, version, revision, encoding, []byte(key), released))
}

func TestAudit20261127CleanupCannotDeleteReactivatedTOSBody(t *testing.T) {
	for _, entry := range []string{"capacity", "expired", "cross-owner-expired"} {
		for _, changed := range []string{"state", "revision", "key", "release", "expiry"} {
			t.Run(entry+"/"+changed, func(t *testing.T) {
				_, mock, s := newBodySQLMock(t)
				key := BodyObjectKey("alice", "123", "ai1")
				objects := map[string]string{key: "newly saved body"}
				deletes := 0
				fakeBodyObjects(s, objects, &deletes)
				released := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
				now := released.Add(8 * 24 * time.Hour)
				expectCleanupCandidate(mock, "alice", "123", "ai1", 3, "tos", key, released)
				mock.ExpectBegin()
				rev, blob, state, release, expiry := uint64(3), []byte(key), "releasable", released, now.Add(-time.Hour)
				switch changed {
				case "state":
					state = "ready"
				case "revision":
					rev++
				case "key":
					blob = []byte("other-key")
				case "release":
					release = release.Add(time.Second)
				case "expiry":
					expiry = now.Add(time.Hour)
				}
				expectLockedBody(mock, "alice", "123", "ai1", rev, "tos", blob, state, release, expiry)
				// Capacity cleanup does not use expiry, so it is still eligible in this case.
				if entry == "capacity" && changed == "expiry" {
					expectBodyRemoval(mock, "alice", "123", "ai1", rev, "tos", blob)
					mock.ExpectExec("INSERT INTO novel_fetch_body_object_retirements").WillReturnResult(sqlmock.NewResult(0, 1))
				}
				mock.ExpectCommit()
				var result BodyCleanupResult
				var err error
				if entry == "cross-owner-expired" {
					result, err = s.CleanupExpiredBodies(context.Background(), now, 1)
				} else {
					result, err = s.CleanupBodies(context.Background(), "alice", BodyCleanupRequest{Now: now, Reason: BodyCleanupReason(entry), Limit: 1})
				}
				if err != nil {
					t.Fatal(err)
				}
				assertBodyExpectations(t, mock)
				if deletes != 0 || objects[key] != "newly saved body" {
					t.Fatal("cleanup deleted active object")
				}
				if !(entry == "capacity" && changed == "expiry") && result.Deleted != 0 {
					t.Fatal("cleanup removed changed record")
				}
			})
		}
	}
}

func TestBodyDeletionRecordsRetirementOnlyAfterSQLConfirmation(t *testing.T) {
	for _, entry := range []string{"direct", "capacity", "expired", "cross-owner-expired"} {
		for _, failure := range []string{"none", "delete", "document", "retirement", "commit"} {
			t.Run(entry+"/"+failure, func(t *testing.T) {
				_, mock, s := newBodySQLMock(t)
				key, _ := newBodyObjectKey("alice", "123", "ai1")
				objects := map[string]string{key: "body"}
				deletes := 0
				fakeBodyObjects(s, objects, &deletes)
				now := time.Now().UTC()
				released := now.Add(-8 * 24 * time.Hour)
				if entry != "direct" {
					expectCleanupCandidate(mock, "alice", "123", "ai1", 4, "tos", key, released)
				}
				mock.ExpectBegin()
				expectLockedBody(mock, "alice", "123", "ai1", 4, "tos", []byte(key), "releasable", released, now)
				injected := errors.New("injected failure")
				deletion := mock.ExpectExec("DELETE FROM novel_fetch_workshop_bodies").WithArgs("alice", "123", "ai1", uint64(4), "tos", []byte(key))
				if failure == "delete" {
					deletion.WillReturnError(injected)
					mock.ExpectRollback()
				} else {
					deletion.WillReturnResult(sqlmock.NewResult(0, 1))
					document := mock.ExpectExec("UPDATE novel_fetch_workshop_documents SET document_json=JSON_MERGE_PATCH")
					if failure == "document" {
						document.WillReturnError(injected)
						mock.ExpectRollback()
					} else {
						document.WillReturnResult(sqlmock.NewResult(0, 1))
						retire := mock.ExpectExec("INSERT INTO novel_fetch_body_object_retirements").WithArgs(key, "alice", "123", "ai1", sqlmock.AnyArg(), sqlmock.AnyArg(), "pending")
						if failure == "retirement" {
							retire.WillReturnError(injected)
							mock.ExpectRollback()
						} else {
							retire.WillReturnResult(sqlmock.NewResult(0, 1))
							commit := mock.ExpectCommit()
							if failure == "commit" {
								commit.WillReturnError(injected)
							}
						}
					}
				}
				var err error
				var result BodyCleanupResult
				if entry == "direct" {
					_, err = s.DeleteBody(context.Background(), "alice", "123", "ai1")
				} else if entry == "cross-owner-expired" {
					result, err = s.CleanupExpiredBodies(context.Background(), now, 1)
				} else {
					result, err = s.CleanupBodies(context.Background(), "alice", BodyCleanupRequest{Now: now, Reason: BodyCleanupReason(entry), Limit: 1})
				}
				if (failure == "none") != (err == nil) {
					t.Fatalf("failure=%s err=%v", failure, err)
				}
				if err == nil && entry != "direct" && (result.Deleted != 1 || result.ObjectsPending != 1 || result.ObjectsHeld != 0) {
					t.Fatalf("result=%+v", result)
				}
				if deletes != 0 || objects[key] != "body" {
					t.Fatal("physical deletion before grace/reaper")
				}
				assertBodyExpectations(t, mock)
			})
		}
	}
}

func TestBodyRetirementGraceAndLegacyHolding(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	key := BodyObjectKey("alice", "123", "ai1")
	now := time.Now().UTC()
	released := now.Add(-time.Hour)
	expectCleanupCandidate(mock, "alice", "123", "ai1", 1, "tos", key, released)
	mock.ExpectBegin()
	expectLockedBody(mock, "alice", "123", "ai1", 1, "tos", []byte(key), "releasable", released, nil)
	expectBodyRemoval(mock, "alice", "123", "ai1", 1, "tos", []byte(key))
	mock.ExpectExec("INSERT INTO novel_fetch_body_object_retirements").WithArgs(key, "alice", "123", "ai1", now, now.Add(time.Hour), "held").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	result, err := s.CleanupBodies(context.Background(), "alice", BodyCleanupRequest{Now: now, Reason: BodyCleanupReasonCapacity})
	if err != nil || result.ObjectsHeld != 1 || result.ObjectsPending != 0 || result.Deleted != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	assertBodyExpectations(t, mock)
}

func TestReapRetiredBodyObjectsPersistsFailuresAndRetriesExactKey(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	key, _ := newBodyObjectKey("alice", "123", "ai1")
	now := time.Now().UTC()
	deletes := 0
	s.ObjectBodies = &BodyObjectStore{Bucket: "fake", Delete: func(_ context.Context, _, got string) error {
		if got != key {
			t.Fatal("wrong object")
		}
		deletes++
		if deletes == 1 {
			return errors.New("secret provider details")
		}
		return nil
	}}
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			now = now.Add(2 * time.Hour)
		}
		mock.ExpectQuery("SELECT object_key FROM novel_fetch_body_object_retirements WHERE state='pending' AND not_before<=").WithArgs(now, 500).WillReturnRows(sqlmock.NewRows([]string{"object_key"}).AddRow(key))
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT owner_username,book_id,version_id,state,not_before").WithArgs(key).WillReturnRows(sqlmock.NewRows([]string{"owner", "book", "version", "state", "not_before"}).AddRow("alice", "123", "ai1", "pending", now.Add(-time.Hour)))
		mock.ExpectQuery("SELECT COUNT").WithArgs([]byte(key)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectExec("UPDATE novel_fetch_body_object_retirements SET attempts=").WithArgs(key).WillReturnResult(sqlmock.NewResult(0, 1))
		state, lastError := "deleted", ""
		if attempt == 0 {
			state, lastError = "pending", "object deletion failed"
		}
		mock.ExpectExec("UPDATE novel_fetch_body_object_retirements SET state=\\?,last_error=\\?").WithArgs(state, lastError, now.Add(time.Hour), key).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		deleted, err := s.ReapRetiredBodyObjects(context.Background(), now, 999999)
		if attempt == 0 {
			if err == nil || deleted != 0 {
				t.Fatalf("failed delete reported success: %d %v", deleted, err)
			}
		} else if err != nil || deleted != 1 {
			t.Fatalf("retry failed: %d %v", deleted, err)
		}
	}
	assertBodyExpectations(t, mock)
}

func TestReaperNeverDeletesHeldPrematureActiveOrOtherAccountKeys(t *testing.T) {
	for _, kind := range []string{"held", "premature", "active", "legacy", "other-account", "query-error", "record-error"} {
		t.Run(kind, func(t *testing.T) {
			_, mock, s := newBodySQLMock(t)
			key, _ := newBodyObjectKey("alice", "123", "ai1")
			if kind == "legacy" {
				key = BodyObjectKey("alice", "123", "ai1")
			}
			now := time.Now().UTC()
			deletedCalls := 0
			s.ObjectBodies = &BodyObjectStore{Bucket: "fake", Delete: func(context.Context, string, string) error {
				deletedCalls++
				return errors.New("object delete failure")
			}}
			mock.ExpectQuery("SELECT object_key FROM novel_fetch_body_object_retirements").WillReturnRows(sqlmock.NewRows([]string{"object_key"}).AddRow(key))
			mock.ExpectBegin()
			owner, state, due := "alice", "pending", now.Add(-time.Hour)
			if kind == "held" {
				state = "held"
			}
			if kind == "premature" {
				due = now.Add(time.Hour)
			}
			if kind == "other-account" {
				owner = "bob"
			}
			mock.ExpectQuery("SELECT owner_username,book_id,version_id,state,not_before").WillReturnRows(sqlmock.NewRows([]string{"owner", "book", "version", "state", "not_before"}).AddRow(owner, "123", "ai1", state, due))
			switch kind {
			case "legacy", "other-account":
				mock.ExpectExec("UPDATE novel_fetch_body_object_retirements SET state='held'").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectCommit()
			case "active":
				mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
				mock.ExpectRollback()
			case "query-error":
				mock.ExpectQuery("SELECT COUNT").WillReturnError(errors.New("db unavailable"))
				mock.ExpectRollback()
			case "record-error":
				mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
				mock.ExpectExec("UPDATE novel_fetch_body_object_retirements SET attempts=").WillReturnError(errors.New("db unavailable"))
				mock.ExpectRollback()
			default:
				mock.ExpectRollback()
			}
			deleted, err := s.ReapRetiredBodyObjects(context.Background(), now, 1)
			if (kind == "query-error" || kind == "record-error") && err == nil {
				t.Fatal("database failure reported success")
			}
			expectedCalls := 0
			if deleted != 0 || deletedCalls != expectedCalls {
				t.Fatal("unsafe deletion")
			}
			assertBodyExpectations(t, mock)
		})
	}
}

func TestReaperFailureDoesNotStarveOtherObjects(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	a, _ := newBodyObjectKey("alice", "123", "ai1")
	b, _ := newBodyObjectKey("alice", "456", "ai1")
	now := time.Now().UTC()
	calls := []string{}
	s.ObjectBodies = &BodyObjectStore{Bucket: "fake", Delete: func(_ context.Context, _, key string) error {
		calls = append(calls, key)
		if key == a {
			return errors.New("injected failure")
		}
		return nil
	}}
	mock.ExpectQuery("SELECT object_key FROM novel_fetch_body_object_retirements").WillReturnRows(sqlmock.NewRows([]string{"object_key"}).AddRow(a).AddRow(b))
	for _, item := range []struct {
		key, book string
		fail      bool
	}{{a, "123", true}, {b, "456", false}} {
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT owner_username,book_id,version_id,state,not_before").WithArgs(item.key).WillReturnRows(sqlmock.NewRows([]string{"owner", "book", "version", "state", "not_before"}).AddRow("alice", item.book, "ai1", "pending", now.Add(-time.Hour)))
		mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
		mock.ExpectExec("UPDATE novel_fetch_body_object_retirements SET attempts=").WithArgs(item.key).WillReturnResult(sqlmock.NewResult(0, 1))

		state, lastError := "deleted", ""
		if item.fail {
			state, lastError = "pending", "object deletion failed"
		}
		mock.ExpectExec("UPDATE novel_fetch_body_object_retirements SET state=").WithArgs(state, lastError, now.Add(time.Hour), item.key).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
	}
	deleted, err := s.ReapRetiredBodyObjects(context.Background(), now, 10)
	if err == nil || deleted != 1 || len(calls) != 2 || calls[1] != b {
		t.Fatalf("failed object starved next: deleted=%d calls=%v err=%v", deleted, calls, err)
	}
	assertBodyExpectations(t, mock)
}

func expectBodyMetadataWrite(mock sqlmock.Sqlmock, owner, book string) {
	mock.ExpectExec("UPDATE novel_fetch_workshop_documents SET document_json=JSON_MERGE_PATCH").WithArgs(sqlmock.AnyArg(), owner, book).WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestMySQLBodyMetadataFailureRollsBackReferenceAndRetirement(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	key := BodyObjectKey("alice", "123", "ai1")
	objects := map[string]string{key: "old body"}
	deletes := 0
	fakeBodyObjects(s, objects, &deletes)
	mock.ExpectBegin()
	expectLockedBody(mock, "alice", "123", "ai1", 1, "tos", []byte(key), "ready", nil, nil)
	mock.ExpectExec("UPDATE novel_fetch_workshop_bodies SET revision").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO novel_fetch_body_object_retirements").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE novel_fetch_workshop_documents SET document_json=JSON_MERGE_PATCH").WillReturnError(errors.New("metadata write failed"))
	mock.ExpectRollback()
	if _, err := s.PutBody(context.Background(), "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "ai1"}, Content: "new body"}); err == nil {
		t.Fatal("metadata failure was ignored")
	}
	if deletes != 0 || objects[key] != "old body" {
		t.Fatal("metadata failure destroyed body")
	}
	assertBodyExpectations(t, mock)
}

type bodyPatchSQLArgument struct {
	version  string
	chars    int64
	revision uint64
}

func (arg bodyPatchSQLArgument) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var patch map[string]any
	if json.Unmarshal([]byte(raw), &patch) != nil {
		return false
	}
	refs, ok := patch["bodyRefs"].(map[string]any)
	if !ok {
		return false
	}
	ref, ok := refs[arg.version].(map[string]any)
	if !ok {
		return false
	}
	if ref["revision"] != float64(arg.revision) || ref["charCount"] != float64(arg.chars) || ref["contentHash"] == "" {
		return false
	}
	return patch["processedChars"] == float64(arg.chars) && patch["original"] == nil && patch["processedText"] == nil && patch["originalRawChars"] == nil
}

func TestMySQLPutBodySynchronizesMetadataInTheSameTransaction(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	mock.ExpectBegin()
	expectLockedBody(mock, "alice", "123", "original", 6, "gzip", []byte("previous"), "ready", nil, nil)
	mock.ExpectExec("UPDATE novel_fetch_workshop_bodies SET revision").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE novel_fetch_workshop_documents SET document_json=JSON_MERGE_PATCH").WithArgs(bodyPatchSQLArgument{version: "original", chars: 13, revision: 7}, "alice", "123").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	ref, err := s.PutBody(context.Background(), "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "original"}, Content: "现在正文已经修改为更长内容"})
	if err != nil || ref.CharCount != 13 || ref.Revision != 7 {
		t.Fatalf("ref=%+v err=%v", ref, err)
	}
	assertBodyExpectations(t, mock)
}

func TestMySQLDeleteBodyClearsCurrentBodySummary(t *testing.T) {
	_, mock, s := newBodySQLMock(t)
	mock.ExpectBegin()
	expectLockedBody(mock, "alice", "123", "original", 2, "gzip", []byte("old"), "ready", nil, nil)
	mock.ExpectExec("DELETE FROM novel_fetch_workshop_bodies").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE novel_fetch_workshop_documents SET document_json=JSON_MERGE_PATCH").WithArgs(bodyRefJSONPath("original"), sqlmock.AnyArg(), "alice", "123").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if removed, err := s.DeleteBody(context.Background(), "alice", "123", "original"); err != nil || !removed {
		t.Fatal(removed, err)
	}
	assertBodyExpectations(t, mock)
}
