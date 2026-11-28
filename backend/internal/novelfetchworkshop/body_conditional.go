package novelfetchworkshop

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ConditionalBodyStore switches a prepared body's reference only while the
// document and its input/output references still match the caller's snapshot.
// Model work must finish before calling it. Unsupported stores fail closed.
type ConditionalBodyStore interface {
	PutBodyIfDocumentUnchanged(context.Context, string, BodyRecord, Document) (BodyRef, error)
}

func bodyWriteDocumentMatches(current, expected Document) bool {
	if current.BookID != expected.BookID || current.UpdatedAt != expected.UpdatedAt {
		return false
	}
	for _, key := range []string{"status", "taskStatus", "task_status", "queueState", "queue_state"} {
		state, _ := current.Meta[key].(string)
		switch strings.ToLower(strings.TrimSpace(state)) {
		case "stopped", "cancelled", "canceled":
			return false
		}
	}
	for _, version := range []string{"original", "original_raw"} {
		a, aOK := current.BodyRefs[version]
		b, bOK := expected.BodyRefs[version]
		if aOK != bOK || a.Revision != b.Revision || a.ContentHash != b.ContentHash || a.State != b.State {
			return false
		}
	}
	return true
}

func (s *MemoryStore) PutBodyIfDocumentUnchanged(ctx context.Context, owner string, body BodyRecord, expected Document) (BodyRef, error) {
	if err := ctx.Err(); err != nil {
		return BodyRef{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return BodyRef{}, err
	}
	current, ok := s.docs[owner][body.BookID]
	if !ok {
		return BodyRef{}, ErrNotFound
	}
	if !bodyWriteDocumentMatches(current, expected) {
		return BodyRef{}, ErrDocumentChanged
	}
	return s.putBodyLocked(owner, body)
}

func (s *MySQLStore) PutBodyIfDocumentUnchanged(ctx context.Context, owner string, body BodyRecord, expected Document) (BodyRef, error) {
	return s.putBody(ctx, owner, body, expected)
}

func checkBodyWriteDocument(ctx context.Context, tx *sql.Tx, owner, bookID string, expected Document) error {
	var raw []byte
	var updated time.Time
	err := tx.QueryRowContext(ctx, `SELECT document_json, updated_at FROM novel_fetch_workshop_documents WHERE owner_username=? AND book_id=? FOR UPDATE`, owner, bookID).Scan(&raw, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var current Document
	if err := json.Unmarshal(raw, &current); err != nil {
		return err
	}
	current.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	if !bodyWriteDocumentMatches(current, expected) {
		return ErrDocumentChanged
	}
	return nil
}
