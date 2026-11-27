package novelfetchworkshop

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
)

func (s *MySQLStore) MarkBodyReleasable(ctx context.Context, owner, bookID, versionID string, releasableAt time.Time, retentionDays int) (BodyRef, error) {
	bookID = strings.TrimSpace(bookID)
	versionID = strings.TrimSpace(versionID)
	if bookID == "" || versionID == "" {
		return BodyRef{}, ErrNotFound
	}
	released := normalizeCleanupNow(releasableAt)
	expires := released.Add(time.Duration(NormalizeBodyRetentionDays(retentionDays)) * 24 * time.Hour)
	result, err := s.DB.ExecContext(ctx, `UPDATE novel_fetch_workshop_bodies SET state='releasable', releasable_at=?, expires_at=? WHERE owner_username=? AND book_id=? AND version_id=?`, released, expires, owner, bookID, versionID)
	if err != nil {
		return BodyRef{}, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return BodyRef{}, err
	}
	if affected == 0 {
		return BodyRef{}, ErrNotFound
	}

	ref := BodyRef{VersionID: versionID}
	var updated time.Time
	var lastNeeded time.Time
	var storedReleased time.Time
	var storedExpires time.Time
	err = s.DB.QueryRowContext(ctx, `SELECT revision, content_hash, char_count, state, updated_at, last_needed_at, releasable_at, expires_at FROM novel_fetch_workshop_bodies WHERE owner_username=? AND book_id=? AND version_id=?`, owner, bookID, versionID).
		Scan(&ref.Revision, &ref.ContentHash, &ref.CharCount, &ref.State, &updated, &lastNeeded, &storedReleased, &storedExpires)
	if errors.Is(err, sql.ErrNoRows) {
		return BodyRef{}, ErrNotFound
	}
	if err != nil {
		return BodyRef{}, err
	}
	ref.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	ref.LastNeededAt = lastNeeded.UTC().Format(time.RFC3339Nano)
	ref.ReleasableAt = storedReleased.UTC().Format(time.RFC3339Nano)
	ref.ExpiresAt = storedExpires.UTC().Format(time.RFC3339Nano)
	return ref, nil
}

type mysqlCleanupCandidate struct {
	owner, bookID, versionID, encoding string
	revision                           uint64
	blob                               []byte
	releasableAt                       time.Time
}

func (s *MySQLStore) CleanupBodies(ctx context.Context, owner string, request BodyCleanupRequest) (BodyCleanupResult, error) {
	if request.Reason != BodyCleanupReasonExpired && request.Reason != BodyCleanupReasonCapacity {
		return BodyCleanupResult{}, errors.New("invalid body cleanup reason")
	}
	return s.cleanupBodyCandidates(ctx, owner, request, false)
}

func (s *MySQLStore) CleanupExpiredBodies(ctx context.Context, now time.Time, limit int) (BodyCleanupResult, error) {
	return s.cleanupBodyCandidates(ctx, "", BodyCleanupRequest{Now: now, Limit: limit, Reason: BodyCleanupReasonExpired}, true)
}

func (s *MySQLStore) cleanupBodyCandidates(ctx context.Context, owner string, request BodyCleanupRequest, allOwners bool) (BodyCleanupResult, error) {
	now := normalizeCleanupNow(request.Now)
	limit := normalizeCleanupLimit(request.Limit)
	query := `SELECT owner_username, book_id, version_id, revision, content_encoding, content_blob, releasable_at FROM novel_fetch_workshop_bodies WHERE `
	args := []any{}
	if !allOwners {
		query += `owner_username=? AND `
		args = append(args, owner)
	}
	query += `state='releasable' AND releasable_at IS NOT NULL`
	if request.Reason == BodyCleanupReasonExpired {
		query += ` AND expires_at IS NOT NULL AND expires_at<=?`
		args = append(args, now)
	}
	query += ` ORDER BY releasable_at ASC, owner_username ASC, book_id ASC, version_id ASC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return BodyCleanupResult{}, err
	}
	candidates := []mysqlCleanupCandidate{}
	for rows.Next() {
		var c mysqlCleanupCandidate
		if err := rows.Scan(&c.owner, &c.bookID, &c.versionID, &c.revision, &c.encoding, &c.blob, &c.releasableAt); err != nil {
			rows.Close()
			return BodyCleanupResult{}, err
		}
		candidates = append(candidates, c)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return BodyCleanupResult{}, err
	}
	if err := rows.Close(); err != nil {
		return BodyCleanupResult{}, err
	}
	result := BodyCleanupResult{Results: make([]BodyCleanupRecord, 0, len(candidates))}
	if len(candidates) == 0 {
		return result, nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return BodyCleanupResult{}, err
	}
	defer tx.Rollback()
	for _, c := range candidates {
		b, err := lockBody(ctx, tx, c.owner, c.bookID, c.versionID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return BodyCleanupResult{}, err
		}
		if b.revision != c.revision || b.encoding != c.encoding || !bytes.Equal(b.blob, c.blob) || b.state != "releasable" || !b.released.Valid || !b.released.Time.Equal(c.releasableAt) {
			continue
		}
		if request.Reason == BodyCleanupReasonExpired && (!b.expires.Valid || b.expires.Time.After(now)) {
			continue
		}
		if err := removeBody(ctx, tx, b, now); err != nil {
			return BodyCleanupResult{}, err
		}
		result.Deleted++
		if b.encoding == "tos" && len(b.blob) > 0 {
			if retirementState(b) == "pending" {
				result.ObjectsPending++
			} else {
				result.ObjectsHeld++
			}
		}
		result.Results = append(result.Results, BodyCleanupRecord{BookID: b.bookID, VersionID: b.versionID, Reason: request.Reason})
	}
	if err := tx.Commit(); err != nil {
		return BodyCleanupResult{}, err
	}
	return result, nil
}

func bodyRefJSONPath(versionID string) string { return "$.bodyRefs." + strconv.Quote(versionID) }
