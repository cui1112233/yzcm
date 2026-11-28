package screenplay

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type AssistantAttachmentPreparationEntry struct {
	Attachment   AssistantTextAttachment
	WorkDeadline time.Time
}
type preparationDeadlineScanner struct {
	row      attachmentScanner
	deadline *sql.NullTime
}

func (s preparationDeadlineScanner) Scan(values ...any) error {
	return s.row.Scan(append(values, s.deadline)...)
}
func scanPreparationEntry(row attachmentScanner) (out AssistantAttachmentPreparationEntry, err error) {
	var d sql.NullTime
	out.Attachment, _, err = scanAssistantAttachment(preparationDeadlineScanner{row, &d})
	if d.Valid {
		out.WorkDeadline = d.Time
	}
	return
}

var preparationSelect = strings.Replace(attachmentSelect, "work_token FROM", "work_token,work_deadline FROM", 1)

func (r *MySQLAssistantTextAttachments) CheckScope(ctx context.Context, s AssistantReferenceScope, prepare bool, g AssistantAttachmentGuard) error {
	return r.transaction(ctx, s, prepare, g, func(*sql.Tx) error { return nil })
}
func (r *MySQLAssistantTextAttachments) ListPreparationEntries(ctx context.Context, s AssistantReferenceScope, offset, limit int, g AssistantAttachmentGuard) (out []AssistantAttachmentPreparationEntry, err error) {
	if offset < 0 || offset > 10000 || limit < 1 || limit > 100 {
		return nil, ErrInvalidArgument
	}
	out = []AssistantAttachmentPreparationEntry{}
	err = r.transaction(ctx, s, false, g, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, preparationSelect+`WHERE owner_username=? AND project_id=? AND session_id=? AND ((status='ready' AND verified_at IS NOT NULL AND first_published_at IS NOT NULL) OR (uploader=? AND actual_uploader=? AND status IN ('preparing','failed','ready'))) ORDER BY created_at DESC,attachment_id LIMIT ? OFFSET ?`, s.owner, s.project, s.session, s.effective, s.actual, limit+1, offset)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			v, e := scanPreparationEntry(rows)
			if e != nil {
				return e
			}
			if !v.Attachment.Visible(s) && !v.Attachment.matchesDraft(s) {
				return ErrNotFound
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return
}
func (r *MySQLAssistantTextAttachments) InspectOwnIntentByID(ctx context.Context, s AssistantReferenceScope, id string, g AssistantAttachmentGuard) (out AssistantAttachmentPreparationEntry, err error) {
	if s.ValidatePrepare() != nil {
		return out, ErrNotFound
	}
	err = r.transaction(ctx, s, false, g, func(tx *sql.Tx) error {
		v, e := scanPreparationEntry(tx.QueryRowContext(ctx, preparationSelect+"WHERE owner_username=? AND project_id=? AND attachment_id=?", s.owner, s.project, id))
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		if !v.Attachment.matchesDraft(s) {
			return ErrNotFound
		}
		out = v
		return nil
	})
	return
}

func (a AssistantTextAttachment) UploadedBy(s AssistantReferenceScope) bool { return a.matchesDraft(s) }
