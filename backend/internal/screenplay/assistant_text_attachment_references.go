package screenplay

import (
	"context"
	"database/sql"
)

func ResolveAssistantReferencesScoped(ctx context.Context, view AssistantReferenceView, r *MySQLAssistantTextAttachments, scope AssistantReferenceScope, selectors []AssistantReferenceSelector, guard AssistantAttachmentGuard) ([]AssistantReferenceSnapshot, error) {
	hasAttachment := false
	for _, sel := range selectors {
		if sel.Kind == "attachment_text" {
			hasAttachment = true
			break
		}
	}
	if !hasAttachment {
		return view.Resolve(selectors)
	}
	if scope.ValidateRead() != nil || view.Project.ID != scope.project || view.Project.OwnerUsername != scope.owner || r == nil || guard == nil {
		return nil, ErrAccessDenied
	}
	return resolveAssistantReferences(selectors, func(sel AssistantReferenceSelector) (AssistantReferenceSnapshot, error) {
		if sel.Kind != "attachment_text" {
			return view.Describe(sel)
		}
		if !validAssistantAttachmentID(sel.ID) || len(sel.ShotIDs) > 0 {
			return AssistantReferenceSnapshot{}, ErrInvalidArgument
		}
		a, e := r.GetVisible(ctx, scope, sel.ID, guard)
		if e != nil {
			return AssistantReferenceSnapshot{}, e
		}
		return a.Snapshot()
	})
}
func (r *MySQLAssistantTextAttachments) ListReferenceDirectory(ctx context.Context, s AssistantReferenceScope, offset, limit int, g AssistantAttachmentGuard) (out []AssistantReferenceSnapshot, total int, err error) {
	if offset < 0 || offset > 10000 || limit < 0 || limit > 100 {
		return nil, 0, ErrInvalidArgument
	}
	out = []AssistantReferenceSnapshot{}
	err = r.transaction(ctx, s, false, g, func(tx *sql.Tx) error {
		where := `WHERE owner_username=? AND project_id=? AND session_id=? AND status='ready' AND verified_at IS NOT NULL AND (first_published_at IS NOT NULL OR (uploader=? AND actual_uploader=?))`
		args := []any{s.owner, s.project, s.session, s.effective, s.actual}
		if e := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM screenplay_assistant_text_attachments "+where, args...).Scan(&total); e != nil {
			return e
		}
		if limit == 0 {
			return nil
		}
		rows, e := tx.QueryContext(ctx, attachmentSelect+where+" ORDER BY created_at DESC,attachment_id LIMIT ? OFFSET ?", append(args, limit, offset)...)
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
				return ErrNotFound
			}
			snapshot, e := a.Snapshot()
			if e != nil {
				return e
			}
			snapshot.Text = ""
			out = append(out, snapshot)
		}
		return rows.Err()
	})
	return
}
