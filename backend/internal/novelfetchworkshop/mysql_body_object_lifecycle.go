package novelfetchworkshop

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const bodyObjectGracePeriod = time.Hour

var errRetiredBodyObjectDelete = errors.New("retired body object deletion failed; retained for retry")

// lockBody is shared by direct deletion and both cleanup entry points.
const lockBodySQL = `SELECT revision, content_encoding, content_blob, state, releasable_at, expires_at FROM novel_fetch_workshop_bodies WHERE owner_username=? AND book_id=? AND version_id=? FOR UPDATE`

type lockedBody struct {
	owner, bookID, versionID string
	revision                 uint64
	encoding                 string
	blob                     []byte
	state                    string
	released, expires        sql.NullTime
}

func lockBody(ctx context.Context, tx *sql.Tx, owner, bookID, versionID string) (lockedBody, error) {
	b := lockedBody{owner: owner, bookID: bookID, versionID: versionID}
	err := tx.QueryRowContext(ctx, lockBodySQL, owner, bookID, versionID).Scan(&b.revision, &b.encoding, &b.blob, &b.state, &b.released, &b.expires)
	return b, err
}

func retirementState(b lockedBody) string {
	if immutableBodyObjectKey(string(b.blob), b.owner, b.bookID, b.versionID) {
		return "pending"
	}
	return "held"
}

func registerBodyRetirement(ctx context.Context, tx *sql.Tx, b lockedBody, now time.Time) error {
	if b.encoding != "tos" || len(b.blob) == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO novel_fetch_body_object_retirements(object_key,owner_username,book_id,version_id,registered_at,not_before,state,attempts,last_error) VALUES(?,?,?,?,?,?,?,0,NULL)`, string(b.blob), b.owner, b.bookID, b.versionID, now, now.Add(bodyObjectGracePeriod), retirementState(b))
	return err
}

func (s *MySQLStore) switchBodyReference(ctx context.Context, owner, bookID, versionID, encoding string, blob []byte, hash string, chars int64, state string) (BodyRef, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return BodyRef{}, err
	}
	defer tx.Rollback()
	previous, err := lockBody(ctx, tx, owner, bookID, versionID)
	missing := errors.Is(err, sql.ErrNoRows)
	if err != nil && !missing {
		return BodyRef{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	revision := uint64(1)
	if !missing {
		revision = previous.revision + 1
	}
	if missing {
		// An absent-row race may produce a duplicate/deadlock. Return the error,
		// retaining the uploaded object; never retry an uncertain write implicitly.
		_, err = tx.ExecContext(ctx, `INSERT INTO novel_fetch_workshop_bodies(owner_username,book_id,version_id,revision,content_encoding,content_blob,content_hash,char_count,state,created_at,updated_at,last_needed_at,releasable_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,NULL,NULL)`, owner, bookID, versionID, revision, encoding, blob, hash, chars, state, now, now, now)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE novel_fetch_workshop_bodies SET revision=?,content_encoding=?,content_blob=?,content_hash=?,char_count=?,state=?,updated_at=?,last_needed_at=?,releasable_at=NULL,expires_at=NULL WHERE owner_username=? AND book_id=? AND version_id=?`, revision, encoding, blob, hash, chars, state, now, now, owner, bookID, versionID)
	}
	if err != nil {
		return BodyRef{}, fmt.Errorf("switch body reference: %w", err)
	}
	if !missing {
		if err := registerBodyRetirement(ctx, tx, previous, now); err != nil {
			return BodyRef{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return BodyRef{}, err
	}
	return BodyRef{VersionID: versionID, Revision: revision, ContentHash: hash, CharCount: chars, State: state, UpdatedAt: now.Format(time.RFC3339Nano)}, nil
}

func removeBody(ctx context.Context, tx *sql.Tx, b lockedBody, now time.Time) error {
	result, err := tx.ExecContext(ctx, `DELETE FROM novel_fetch_workshop_bodies WHERE owner_username=? AND book_id=? AND version_id=? AND revision=? AND content_encoding=? AND content_blob=?`, b.owner, b.bookID, b.versionID, b.revision, b.encoding, b.blob)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("body changed during deletion")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE novel_fetch_workshop_documents SET document_json=JSON_REMOVE(document_json, ?), updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=? AND book_id=?`, bodyRefJSONPath(b.versionID), b.owner, b.bookID); err != nil {
		return err
	}
	return registerBodyRetirement(ctx, tx, b, now)
}

// ReapRetiredBodyObjects touches only committed retirement rows, never scans a
// bucket. Immutable keys cannot become a later writer's address. The row lock
// serializes reapers; failures and ambiguous remote deletes remain retryable.
func (s *MySQLStore) ReapRetiredBodyObjects(ctx context.Context, now time.Time, limit int) (int, error) {
	now = normalizeCleanupNow(now)
	limit = normalizeCleanupLimit(limit)
	if limit > 500 {
		limit = 500
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT object_key FROM novel_fetch_body_object_retirements WHERE state='pending' AND not_before<=? ORDER BY not_before ASC, object_key ASC LIMIT ?`, now, limit)
	if err != nil {
		return 0, err
	}
	keys := []string{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return 0, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	deleted := 0
	var deletionErrors error
	for _, key := range keys {
		removed, err := s.reapBodyObject(ctx, key, now)
		if err != nil {
			if errors.Is(err, errRetiredBodyObjectDelete) {
				deletionErrors = errors.Join(deletionErrors, err)
				continue
			}
			return deleted, errors.Join(deletionErrors, err)
		}
		if removed {
			deleted++
		}
	}
	return deleted, deletionErrors
}

func (s *MySQLStore) reapBodyObject(ctx context.Context, key string, now time.Time) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	b := lockedBody{encoding: "tos", blob: []byte(key)}
	var state string
	var notBefore time.Time
	err = tx.QueryRowContext(ctx, `SELECT owner_username,book_id,version_id,state,not_before FROM novel_fetch_body_object_retirements WHERE object_key=? FOR UPDATE`, key).Scan(&b.owner, &b.bookID, &b.versionID, &state, &notBefore)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if state != "pending" || notBefore.After(now) {
		return false, nil
	}
	if retirementState(b) != "pending" {
		if _, err := tx.ExecContext(ctx, `UPDATE novel_fetch_body_object_retirements SET state='held',last_error='legacy or mismatched object key' WHERE object_key=?`, key); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	var active int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM novel_fetch_workshop_bodies WHERE content_encoding='tos' AND content_blob=?`, []byte(key)).Scan(&active); err != nil {
		return false, err
	}
	if active != 0 {
		return false, nil
	}
	// Establish the attempt before touching the remote object. If the database
	// cannot record the operation, stop with zero physical deletion.
	if _, err := tx.ExecContext(ctx, `UPDATE novel_fetch_body_object_retirements SET attempts=attempts+1,last_error='object deletion in progress' WHERE object_key=?`, key); err != nil {
		return false, err
	}
	objectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	deletionError := s.ObjectBodies.DeleteContent(objectCtx, key)
	cancel()
	status, lastError := "deleted", ""
	if deletionError != nil {
		status, lastError = "pending", "object deletion failed"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE novel_fetch_body_object_retirements SET state=?,last_error=?,not_before=? WHERE object_key=?`, status, lastError, now.Add(bodyObjectGracePeriod), key); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if deletionError != nil {
		return false, errRetiredBodyObjectDelete
	}
	return true, nil
}
