package screenplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// Optional capability; the existing Store interface and concrete layout stay intact.
func (s *MySQLStore) AssistantTextAttachments() *MySQLAssistantTextAttachments {
	return &MySQLAssistantTextAttachments{db: s.db}
}

type MySQLAssistantTextAttachments struct{ db *sql.DB }
type AssistantAttachmentGuard func(context.Context, *sql.Tx) error

func (r *MySQLAssistantTextAttachments) transaction(ctx context.Context, s AssistantReferenceScope, prepare bool, guard AssistantAttachmentGuard, fn func(*sql.Tx) error) error {
	if e := ctx.Err(); e != nil {
		return e
	}
	if s.ValidateRead() != nil || guard == nil {
		return ErrAccessDenied
	}
	if prepare && s.ValidatePrepare() != nil {
		return ErrAccessDenied
	}
	return (&MySQLAuthoringRepository{db: r.db}).sessionTransaction(ctx, s.owner, s.project, s.effective, "read", func(locked *sessionScope) error {
		if locked.permissions.Archived {
			return ErrAccessDenied
		}
		if s.session != "default" {
			if !assistantSessionVisible(locked.rows, s.session, s.effective) || !sessionActive(locked.rows, s.session) {
				return ErrAccessDenied
			}
		}
		if prepare && !sessionProjectAccess(locked.permissions, s.owner, s.effective, "edit") && !sessionProjectAccess(locked.permissions, s.owner, s.effective, "generate") {
			return ErrAccessDenied
		}
		if e := guard(ctx, locked.tx); e != nil {
			return e
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		return fn(locked.tx)
	})
}

const attachmentSelect = "SELECT body,state_revision,status,verified_at IS NOT NULL,first_published_at IS NOT NULL,work_token FROM screenplay_assistant_text_attachments "

type attachmentScanner interface{ Scan(...any) error }

func scanAssistantAttachment(row attachmentScanner) (a AssistantTextAttachment, token string, err error) {
	var raw []byte
	var rev int64
	var status string
	var verified, published bool
	if err = row.Scan(&raw, &rev, &status, &verified, &published, &token); err != nil {
		return
	}
	if err = json.Unmarshal(raw, &a); err != nil {
		return
	}
	a.StateRevision = rev
	a.Status = status
	a.Verified = verified
	a.Published = published
	return
}
func (a AssistantTextAttachment) matchesDraft(s AssistantReferenceScope) bool {
	return s.ValidatePrepare() == nil && a.Owner == s.owner && a.Project == s.project && a.Session == s.session && a.Uploader == s.effective && a.ActualUploader == s.actual
}
func (r *MySQLAssistantTextAttachments) BeginIntent(ctx context.Context, s AssistantReferenceScope, a AssistantTextAttachment, guard AssistantAttachmentGuard) (out AssistantTextAttachment, token string, err error) {
	if !a.matchesDraft(s) || !validAttachmentHash(a.RequestKeyHash) || a.ObjectKey != "assistant-text/"+a.ID || a.Status != "preparing" || a.ContentVersion != 1 || a.Verified || a.Published {
		return out, "", ErrInvalidArgument
	}
	if _, err = a.Snapshot(); err != nil {
		return
	}
	err = r.transaction(ctx, s, true, guard, func(tx *sql.Tx) error {
		existing, _, e := scanAssistantAttachment(tx.QueryRowContext(ctx, attachmentSelect+"WHERE uploader=? AND actual_uploader=? AND request_key_sha256=? FOR UPDATE", s.effective, s.actual, a.RequestKeyHash))
		if e == nil {
			if !existing.matchesDraft(s) || existing.RawSHA256 != a.RawSHA256 || existing.Filename != a.Filename || existing.Namespace != a.Namespace {
				return ErrRevisionConflict
			}
			out = existing
			return nil
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		token = newScreenplayID("attachment-work")
		raw, e := json.Marshal(a)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO screenplay_assistant_text_attachments(owner_username,project_id,attachment_id,session_id,uploader,actual_uploader,request_key_sha256,object_key,namespace_id,content_version,state_revision,body,status,work_token,work_deadline) VALUES(?,?,?,?,?,?,?,?,?,?,1,?,'preparing',?,DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL 30 SECOND))`, s.owner, s.project, a.ID, s.session, s.effective, s.actual, a.RequestKeyHash, a.ObjectKey, a.Namespace, a.ContentVersion, string(raw), token)
		if e == nil {
			out = a
		}
		return e
	})
	if err != nil {
		token = ""
	}
	return
}
func (r *MySQLAssistantTextAttachments) GetVisible(ctx context.Context, s AssistantReferenceScope, id string, guard AssistantAttachmentGuard) (out AssistantTextAttachment, err error) {
	err = r.transaction(ctx, s, false, guard, func(tx *sql.Tx) error {
		a, _, e := scanAssistantAttachment(tx.QueryRowContext(ctx, attachmentSelect+"WHERE owner_username=? AND project_id=? AND attachment_id=?", s.owner, s.project, id))
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return ErrNotFound
			}
			return e
		}
		if !a.Visible(s) {
			return ErrNotFound
		}
		out = a
		return nil
	})
	return
}

// InspectIntent is uploader-private, including preparing/failed states.
func (r *MySQLAssistantTextAttachments) InspectIntent(ctx context.Context, s AssistantReferenceScope, keyHash string, guard AssistantAttachmentGuard) (out AssistantTextAttachment, err error) {
	if !validAttachmentHash(keyHash) {
		return out, ErrInvalidArgument
	}
	err = r.transaction(ctx, s, true, guard, func(tx *sql.Tx) error {
		a, _, e := scanAssistantAttachment(tx.QueryRowContext(ctx, attachmentSelect+"WHERE uploader=? AND actual_uploader=? AND request_key_sha256=?", s.effective, s.actual, keyHash))
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return ErrNotFound
			}
			return e
		}
		if !a.matchesDraft(s) {
			return ErrNotFound
		}
		out = a
		return nil
	})
	return
}
func (r *MySQLAssistantTextAttachments) ClaimRetry(ctx context.Context, s AssistantReferenceScope, id, namespace string, expected int64, guard AssistantAttachmentGuard) (out AssistantTextAttachment, token string, err error) {
	err = r.transaction(ctx, s, true, guard, func(tx *sql.Tx) error {
		a, _, e := scanAssistantAttachment(tx.QueryRowContext(ctx, attachmentSelect+"WHERE owner_username=? AND project_id=? AND attachment_id=? FOR UPDATE", s.owner, s.project, id))
		if e != nil {
			return referenceSQLError(e)
		}
		if !a.matchesDraft(s) || a.Namespace != namespace || a.StateRevision != expected || (a.Status != "preparing" && a.Status != "failed") {
			return ErrRevisionConflict
		}
		token = newScreenplayID("attachment-work")
		res, e := tx.ExecContext(ctx, `UPDATE screenplay_assistant_text_attachments SET status='preparing',state_revision=state_revision+1,work_token=?,work_deadline=DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL 30 SECOND),updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=? AND project_id=? AND attachment_id=? AND state_revision=? AND (work_deadline IS NULL OR work_deadline<=CURRENT_TIMESTAMP(6))`, token, s.owner, s.project, id, expected)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrRevisionConflict
		}
		a.StateRevision++
		a.Status = "preparing"
		out = a
		return nil
	})
	if err != nil {
		token = ""
	}
	return
}

// Caller must have completed bounded original hash and private ACL verification
// outside SQL using this intent's fixed namespace/key. This method never networks.
func (r *MySQLAssistantTextAttachments) MarkReady(ctx context.Context, s AssistantReferenceScope, id, token, rawHash string, guard AssistantAttachmentGuard) error {
	if token == "" || !validAttachmentHash(rawHash) {
		return ErrInvalidArgument
	}
	return r.transaction(ctx, s, true, guard, func(tx *sql.Tx) error {
		a, _, e := scanAssistantAttachment(tx.QueryRowContext(ctx, attachmentSelect+"WHERE owner_username=? AND project_id=? AND attachment_id=? FOR UPDATE", s.owner, s.project, id))
		if e != nil {
			return referenceSQLError(e)
		}
		if !a.matchesDraft(s) || a.RawSHA256 != rawHash || a.Status != "preparing" {
			return ErrRevisionConflict
		}
		if _, e = a.Snapshot(); e != nil {
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		res, e := tx.ExecContext(ctx, `UPDATE screenplay_assistant_text_attachments SET status='ready',state_revision=state_revision+1,verified_at=CURRENT_TIMESTAMP(6),work_token='',work_deadline=NULL,updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=? AND project_id=? AND attachment_id=? AND work_token=? AND state_revision=? AND work_deadline>CURRENT_TIMESTAMP(6)`, s.owner, s.project, id, token, a.StateRevision)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrRevisionConflict
		}
		return nil
	})
}

// Lists scoped metadata only. Full text is available only through GetVisible.
func (r *MySQLAssistantTextAttachments) ListVisible(ctx context.Context, s AssistantReferenceScope, offset, limit int, guard AssistantAttachmentGuard) (out []AssistantTextAttachment, err error) {
	if offset < 0 || offset > 10000 || limit < 1 || limit > 100 {
		return nil, ErrInvalidArgument
	}
	out = []AssistantTextAttachment{}
	err = r.transaction(ctx, s, false, guard, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, attachmentSelect+"WHERE owner_username=? AND project_id=? AND session_id=? AND status='ready' AND verified_at IS NOT NULL AND (first_published_at IS NOT NULL OR (uploader=? AND actual_uploader=?)) ORDER BY created_at DESC,attachment_id LIMIT ? OFFSET ?", s.owner, s.project, s.session, s.effective, s.actual, limit, offset)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			a, _, e := scanAssistantAttachment(rows)
			if e != nil {
				return e
			}
			if !a.Visible(s) {
				return ErrAccessDenied
			}
			a.Text = ""
			out = append(out, a)
		}
		return rows.Err()
	})
	return
}
