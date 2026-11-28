package screenplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
)

// Caller holds the original project/authoring/session/authority locks. All rows
// below remain locked until the enclosing message save commits or rolls back.
func CheckAssistantReferencesScopedInTx(ctx context.Context, tx *sql.Tx, scope AssistantReferenceScope, c AssistantAttachmentMessageCommit) error {
	if tx == nil {
		return ErrAccessDenied
	}
	if e := ValidateAssistantAttachmentMessageCommit(scope, c); e != nil {
		return e
	}
	old := []AssistantReferenceSnapshot{}
	files := []AssistantReferenceSnapshot{}
	for _, ref := range c.References {
		if ref.Kind == "attachment_text" {
			files = append(files, ref)
		} else {
			old = append(old, ref)
		}
	}
	if e := CheckAssistantReferencesInTx(ctx, tx, scope.owner, scope.project, old); e != nil {
		return e
	}
	sort.Slice(files, func(i, j int) bool { return files[i].ID < files[j].ID })
	locked := make([]AssistantTextAttachment, 0, len(files))
	for _, ref := range files {
		a, _, e := scanAssistantAttachment(tx.QueryRowContext(ctx, attachmentSelect+"WHERE owner_username=? AND project_id=? AND attachment_id=? FOR UPDATE", scope.owner, scope.project, ref.ID))
		if e != nil {
			return referenceSQLError(e)
		}
		if a.ID != ref.ID || !a.Visible(scope) {
			return ErrRevisionConflict
		}
		current, e := a.Snapshot()
		if e != nil || !sameAssistantJSON(current, ref) {
			return ErrRevisionConflict
		}
		locked = append(locked, a)
	}
	var existing string
	e := tx.QueryRowContext(ctx, "SELECT operation_id FROM screenplay_assistant_text_attachment_messages WHERE owner_username=? AND project_id=? AND session_id=? AND operation_id=? LIMIT 1 FOR UPDATE", scope.owner, scope.project, scope.session, c.OperationID).Scan(&existing)
	if e == nil {
		return ErrRevisionConflict
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	for i, ref := range files {
		a := locked[i]
		if _, e = tx.ExecContext(ctx, `INSERT INTO screenplay_assistant_text_attachment_messages(owner_username,project_id,session_id,operation_id,attachment_id,content_version,snapshot_digest,effective_username,actual_actor) VALUES(?,?,?,?,?,?,?,?,?)`, scope.owner, scope.project, scope.session, c.OperationID, ref.ID, ref.Revision, ref.Digest, scope.effective, scope.actual); e != nil {
			return e
		}
		if !a.Published {
			res, e := tx.ExecContext(ctx, `UPDATE screenplay_assistant_text_attachments SET first_published_at=CURRENT_TIMESTAMP(6),state_revision=state_revision+1,updated_at=CURRENT_TIMESTAMP(6) WHERE owner_username=? AND project_id=? AND attachment_id=? AND state_revision=? AND first_published_at IS NULL AND status='ready'`, scope.owner, scope.project, a.ID, a.StateRevision)
			if e = oneShotRow(res, e); e != nil {
				return e
			}
		}
	}
	return nil
}

// New default-only path: legacy unscoped SaveAssistantReferences still rejects
// the attachment kind. The original authority guard always precedes publication.
func (r *MySQLAuthoringRepository) SaveAssistantReferencesScoped(ctx context.Context, st AuthoringState, scope AssistantReferenceScope, expected int64, operationID, userContent string, refs []AssistantReferenceSnapshot, guard AssistantAttachmentGuard) (out AuthoringState, err error) {
	if scope.ValidatePrepare() != nil || scope.session != "default" || st.Owner != scope.owner || st.ProjectID != scope.project || guard == nil {
		return out, ErrAccessDenied
	}
	err = r.sessionTransaction(ctx, scope.owner, scope.project, scope.effective, "generate", func(s *sessionScope) error {
		old := s.permissions
		if old.Revision != expected {
			return ErrRevisionConflict
		}
		if len(st.Proposals) < len(old.Proposals) {
			return ErrInvalidArgument
		}
		if len(st.Proposals) > len(old.Proposals) {
			if !sessionProjectAccess(old, scope.owner, scope.effective, "edit") {
				return ErrAccessDenied
			}
			for _, p := range st.Proposals[len(old.Proposals):] {
				if e := validateDefaultAssistantAttachmentProposal(old, p); e != nil {
					return e
				}
			}
		}
		if e := guard(ctx, s.tx); e != nil {
			return e
		}
		commit := AssistantAttachmentMessageCommit{OperationID: operationID, UserContent: userContent, PreviousMessages: old.Messages, CandidateMessages: st.Messages, References: refs}
		if e := CheckAssistantReferencesScopedInTx(ctx, s.tx, scope, commit); e != nil {
			return e
		}
		st.Revision = expected + 1
		for i := range st.Proposals {
			if st.Proposals[i].Status == "pending" && st.Proposals[i].Revision != st.Revision {
				st.Proposals[i].Status = "stale"
			}
		}
		if expected == 0 {
			raw, e := json.Marshal(st)
			if e != nil {
				return e
			}
			res, e := s.tx.ExecContext(ctx, "INSERT IGNORE INTO screenplay_authoring(project_id,owner_username,revision,body) VALUES(?,?,?,?)", st.ProjectID, st.Owner, st.Revision, string(raw))
			if e = oneShotRow(res, e); e != nil {
				return e
			}
		} else if e := saveAuthoringStateInTx(ctx, s.tx, st, expected); e != nil {
			return e
		}
		out = st
		return nil
	})
	return
}

// Default proposals may change an unpublished chapter without publishing a
// chapter collection. Independent-session validation keeps its own contract.
func validateDefaultAssistantAttachmentProposal(st AuthoringState, p AssistantProposal) error {
	if p.ID == "" || p.Status != "pending" || len(p.Actions) != 1 {
		return ErrInvalidArgument
	}
	if e := ValidateAuthoringActions(p.Actions); e != nil {
		return e
	}
	a := p.Actions[0]
	if a.Kind == "chapter" {
		evidence := a.ContentEdit
		if evidence == nil || evidence.AuthorRevision != st.Revision {
			return ErrRevisionConflict
		}
		index, exists := findChapter(st.Chapters, a.TargetID)
		if !exists {
			return ErrRevisionConflict
		}
		digest, e := assistantChapterDigest(st.Chapters[index])
		if e != nil {
			return e
		}
		if digest != evidence.ChapterSHA256 {
			return ErrRevisionConflict
		}
		if evidence.Publish {
			digest, e = assistantChapterDigest(st.Chapters)
			if e != nil {
				return e
			}
			if digest != evidence.CollectionSHA256 {
				return ErrRevisionConflict
			}
		}
	}
	return nil
}
