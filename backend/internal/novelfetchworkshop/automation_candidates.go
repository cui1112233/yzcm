package novelfetchworkshop

import (
	"context"
	"encoding/json"
	"time"
)

// AutomationCandidateStore is optional: older and in-memory stores retain the
// ListDocuments fallback. Summaries are read-only and must never be saved back.
type AutomationCandidateStore interface {
	ListAutomationCandidates(context.Context, string, string, string, int) ([]Document, error)
}

const automationCandidateSQL = `SELECT book_id,
 JSON_OBJECT('meta',JSON_EXTRACT(document_json,'$.meta'),
 'gender',JSON_EXTRACT(document_json,'$.gender'),
 'bodyRefs',JSON_EXTRACT(document_json,'$.bodyRefs'),
 'maxTxt',JSON_EXTRACT(document_json,'$.maxTxt')),
 COALESCE(CHAR_LENGTH(TRIM(JSON_UNQUOTE(JSON_EXTRACT(document_json,'$.originalRaw')))),0)>0,
 updated_at FROM novel_fetch_workshop_documents
 WHERE owner_username=? AND book_id>?
 AND (?='' OR COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(document_json,'$.meta.currentBatchId')),''),JSON_UNQUOTE(JSON_EXTRACT(document_json,'$.meta.current_batch_id'))) = ?)
 ORDER BY book_id ASC LIMIT ?`

func (s *MySQLStore) ListAutomationCandidates(ctx context.Context, owner, batch, after string, limit int) ([]Document, error) {
	if limit <= 0 || limit > 250 {
		limit = 250
	}
	rows, err := s.DB.QueryContext(ctx, automationCandidateSQL, owner, after, batch, batch, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	docs := []Document{}
	for rows.Next() {
		var doc Document
		var raw []byte
		var hasOriginal bool
		var updated time.Time
		if err := rows.Scan(&doc.BookID, &raw, &hasOriginal, &updated); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return nil, err
		}
		doc.OriginalRawAvailable = hasOriginal
		doc.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}
