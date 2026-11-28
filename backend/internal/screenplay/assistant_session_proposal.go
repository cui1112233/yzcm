package screenplay

import (
	"context"
	"database/sql"
	"encoding/json"
)

func (s *sessionScope) editableHistory(id string, expected int64) (AssistantSessionHistory, error) {
	item, err := s.selected(id)
	if err != nil {
		return AssistantSessionHistory{}, err
	}
	if item.Mode == "readonly" {
		return AssistantSessionHistory{}, ErrAccessDenied
	}
	if !sessionActive(s.rows, id) {
		return AssistantSessionHistory{}, ErrInvalidState
	}
	if item.Revision != expected || item.RequestStatus != "" {
		return AssistantSessionHistory{}, ErrRevisionConflict
	}
	return s.history(id)
}

func (s *sessionScope) saveHistory(id string, expected int64, history AssistantSessionHistory) error {
	data, err := json.Marshal(history)
	if err != nil {
		return err
	}
	result, err := s.tx.ExecContext(s.ctx, "UPDATE screenplay_assistant_sessions SET body=?,revision=revision+1 WHERE owner_username=? AND project_id=? AND session_id=? AND revision=?", string(data), s.owner, s.project, id, expected)
	return oneShotRow(result, err)
}

func (r *MySQLAuthoringRepository) ApplySessionProposal(ctx context.Context, owner, project, actor, id, proposalID string, expected int64) (AssistantSessionHistory, error) {
	var saved AssistantSessionHistory
	err := r.sessionTransaction(ctx, owner, project, actor, "edit", func(s *sessionScope) error {
		history, err := s.editableHistory(id, expected)
		if err != nil {
			return err
		}
		for i := range history.Proposals {
			p := &history.Proposals[i]
			if p.ID != proposalID {
				continue
			}
			if p.Status != "pending" || len(p.Actions) != 1 {
				return ErrRevisionConflict
			}
			if err = ValidateAuthoringActions(p.Actions); err != nil {
				return err
			}
			a := p.Actions[0]
			switch a.Kind {
			case "asset":
				if !a.AssetPrepared || a.Asset == nil {
					return ErrInvalidArgument
				}
				err = applyAssistantAssetInTx(ctx, s.tx, owner, project, a)
			case "creative_shot":
				err = applyAssistantShotInTx(ctx, s.tx, owner, project, a)
			case "creative_shot_structure":
				err = applyAssistantShotStructureInTx(ctx, s.tx, owner, project, a)
			case "document", "chapter":
				err = s.applySessionContent(a)
			default:
				return ErrInvalidArgument
			}
			if err != nil {
				return err
			}
			p.Status = "applied"
			if err = s.saveHistory(id, expected, history); err != nil {
				return err
			}
			saved = history
			return nil
		}
		return ErrNotFound
	})
	if err != nil {
		return AssistantSessionHistory{}, err
	}
	return saved, nil
}

func (s *sessionScope) applySessionContent(action AuthoringAction) error {
	if err := validateAssistantContentAction(action); err != nil {
		return err
	}
	state := s.permissions
	state.Owner, state.ProjectID = s.owner, s.project
	if action.Kind == "chapter" {
		digest, err := assistantChapterDigest(state.Chapters)
		if err != nil {
			return err
		}
		if digest != action.ContentEdit.CollectionSHA256 {
			return ErrRevisionConflict
		}
	}
	var source sql.NullString
	err := s.tx.QueryRowContext(s.ctx, "SELECT source_text FROM screenplay_projects WHERE owner_username=? AND id=?", s.owner, s.project).Scan(&source)
	if err != nil {
		return err
	}
	if err = applyAssistantContentInTx(s.ctx, s.tx, &state, action, source.String); err != nil {
		return err
	}
	if action.Kind == "chapter" {
		chapters, err := json.Marshal(state.Chapters)
		if err != nil {
			return err
		}
		// Patch only shared chapters. The default conversation may contain
		// extension fields which the current Go message structs do not know.
		result, err := s.tx.ExecContext(s.ctx, `UPDATE screenplay_authoring SET body=JSON_SET(body,'$.chapters',CAST(? AS JSON),'$.revision',revision+1),revision=revision+1 WHERE owner_username=? AND project_id=? AND revision=?`, string(chapters), s.owner, s.project, state.Revision)
		return oneShotRow(result, err)
	}
	return nil
}

func PrepareSessionContent(project Project, st AuthoringState, active Document, raw json.RawMessage) (AuthoringAction, string, error) {
	action, summary, err := PrepareAssistantContentEdit(project, st, active, raw, true)
	if err != nil {
		return AuthoringAction{}, "", err
	}
	if action.Kind == "chapter" {
		action.ContentEdit.CollectionSHA256, err = assistantChapterDigest(st.Chapters)
	}
	return action, summary, err
}

func validateSessionProposal(st AuthoringState, p AssistantProposal) error {
	if len(p.Actions) != 1 || p.Status != "pending" || p.ID == "" {
		return ErrInvalidArgument
	}
	if err := ValidateAuthoringActions(p.Actions); err != nil {
		return err
	}
	a := p.Actions[0]
	switch a.Kind {
	case "chapter":
		digest, err := assistantChapterDigest(st.Chapters)
		if err != nil {
			return err
		}
		if digest != a.ContentEdit.CollectionSHA256 {
			return ErrRevisionConflict
		}
	case "document", "asset", "creative_shot", "creative_shot_structure":
	default:
		return ErrInvalidArgument
	}
	return nil
}

func (r *MySQLAuthoringRepository) SaveSessionProposal(ctx context.Context, owner, project, actor, id string, expected int64, proposal AssistantProposal) (AssistantSessionHistory, error) {
	var out AssistantSessionHistory
	err := r.sessionTransaction(ctx, owner, project, actor, "edit", func(s *sessionScope) error {
		history, err := s.editableHistory(id, expected)
		if err != nil {
			return err
		}
		if err = validateSessionProposal(s.permissions, proposal); err != nil {
			return err
		}
		proposal.Revision = expected + 1
		history.Proposals = append(history.Proposals, proposal)
		if err = s.saveHistory(id, expected, history); err != nil {
			return err
		}
		out = history
		return nil
	})
	return out, err
}
