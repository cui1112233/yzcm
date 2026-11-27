package screenplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// AuthoringAssetCommitter keeps content and its confirmation receipt in the
// same database transaction. Stores without this capability retain their
// existing in-memory test path; production MySQL always uses this contract.
type AuthoringAssetCommitter interface {
	CommitAuthoringAsset(context.Context, AuthoringRepository, string, string, string, int64) (AuthoringState, error)
}

func (s *MySQLStore) CommitAuthoringAsset(ctx context.Context, repository AuthoringRepository, owner, project, proposalID string, expected int64) (AuthoringState, error) {
	repo, ok := repository.(*MySQLAuthoringRepository)
	if !ok || repo == nil || s == nil || s.db == nil || repo.db != s.db {
		return AuthoringState{}, fmt.Errorf("%w: 资产和确认记录必须使用同一MySQL数据库", ErrInvalidState)
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if e != nil {
		return AuthoringState{}, e
	}
	defer tx.Rollback()
	var existing string
	e = tx.QueryRowContext(ctx, `SELECT id FROM screenplay_projects WHERE id=? AND owner_username=? AND deleted_at IS NULL FOR UPDATE`, project, owner).Scan(&existing)
	if errors.Is(e, sql.ErrNoRows) {
		return AuthoringState{}, ErrNotFound
	}
	if e != nil {
		return AuthoringState{}, e
	}
	var raw []byte
	var revision int64
	e = tx.QueryRowContext(ctx, `SELECT body,revision FROM screenplay_authoring WHERE owner_username=? AND project_id=? FOR UPDATE`, owner, project).Scan(&raw, &revision)
	if errors.Is(e, sql.ErrNoRows) {
		return AuthoringState{}, ErrNotFound
	}
	if e != nil {
		return AuthoringState{}, e
	}
	if revision != expected {
		return AuthoringState{}, ErrRevisionConflict
	}
	var state AuthoringState
	if e = json.Unmarshal(raw, &state); e != nil {
		return AuthoringState{}, e
	}
	state.Owner = owner
	state.ProjectID = project
	state.Revision = revision
	index := -1
	for i, proposal := range state.Proposals {
		if proposal.ID == proposalID {
			index = i
			break
		}
	}
	if index < 0 {
		return AuthoringState{}, ErrNotFound
	}
	proposal := state.Proposals[index]
	if proposal.Status != "pending" || proposal.Revision != expected || len(proposal.Actions) != 1 {
		return AuthoringState{}, ErrRevisionConflict
	}
	if e = ValidateAuthoringActions(proposal.Actions); e != nil {
		return AuthoringState{}, e
	}
	action := proposal.Actions[0]
	if action.Kind != "asset" {
		return AuthoringState{}, ErrInvalidArgument
	}
	if action.Asset.OwnerUsername != "" && action.Asset.OwnerUsername != owner || action.Asset.ProjectID != "" && action.Asset.ProjectID != project {
		return AuthoringState{}, ErrInvalidArgument
	}
	switch action.AssetOperation {
	case "add":
		if action.Asset.ID != action.TargetID || action.ExpectedRevision != 0 {
			return AuthoringState{}, ErrInvalidArgument
		}
		asset := *action.Asset
		asset.OwnerUsername = owner
		asset.ProjectID = project
		_, e = createAssetInTx(ctx, tx, asset)
	case "delete":
		e = deleteAssistantAssetInTx(ctx, tx, owner, project, action)
	default:
		_, e = updateAssetInTx(ctx, tx, owner, project, action.TargetID, *action.Asset, action.ExpectedRevision)
	}
	if e != nil {
		return AuthoringState{}, e
	}
	state.Proposals[index].Status = "applied"
	for i := range state.Proposals {
		if state.Proposals[i].Status == "pending" && state.Proposals[i].Revision != expected+1 {
			state.Proposals[i].Status = "stale"
		}
	}
	state.Revision = expected + 1
	body, e := json.Marshal(state)
	if e != nil {
		return AuthoringState{}, e
	}
	result, e := tx.ExecContext(ctx, `UPDATE screenplay_authoring SET body=?,revision=? WHERE project_id=? AND owner_username=? AND revision=?`, string(body), state.Revision, project, owner, expected)
	if e != nil {
		return AuthoringState{}, e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return AuthoringState{}, e
	}
	if n != 1 {
		return AuthoringState{}, ErrRevisionConflict
	}
	if e = tx.Commit(); e != nil {
		return AuthoringState{}, e
	}
	return state, nil
}
