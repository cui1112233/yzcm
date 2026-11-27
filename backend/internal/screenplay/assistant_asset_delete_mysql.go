package screenplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

func deleteAssistantAssetInTx(ctx context.Context, tx *sql.Tx, owner, project string, action AuthoringAction) error {
	var asset Asset
	asset.ID = action.TargetID
	err := tx.QueryRowContext(ctx, `SELECT asset_type,name,revision FROM screenplay_assets WHERE id=? AND project_id=? AND owner_username=? FOR UPDATE`, action.TargetID, project, owner).Scan(&asset.Type, &asset.Name, &asset.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if asset.Revision != action.ExpectedRevision || asset.Type != action.Asset.Type || asset.Name != action.Asset.Name {
		return ErrRevisionConflict
	}
	// Lock the complete project ranges, including empty ranges, so a concurrent
	// saved shot cannot introduce a reference between this check and the delete.
	rows, err := tx.QueryContext(ctx, `SELECT id,character_names,scene_name,prop_names FROM screenplay_shots WHERE project_id=? AND owner_username=? FOR UPDATE`, project, owner)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, scene string
		var characters, props []byte
		if err = rows.Scan(&id, &characters, &scene, &props); err != nil {
			rows.Close()
			return err
		}
		if assistantAssetReference(asset, string(characters), scene, string(props), nil) {
			rows.Close()
			return assistantAssetReferencedError(asset, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,characters_text,scene_name,props_text,protocol_json FROM screenplay_storyboard_shots WHERE project_id=? AND owner_username=? FOR UPDATE`, project, owner)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, scene string
		var characters, props []byte
		var protocol []byte
		if err = rows.Scan(&id, &characters, &scene, &props, &protocol); err != nil {
			rows.Close()
			return err
		}
		var saved storyboardProtocol
		if len(protocol) > 0 {
			if err = json.Unmarshal(protocol, &saved); err != nil {
				rows.Close()
				return err
			}
		}
		if assistantAssetReference(asset, string(characters), scene, string(props), saved.ProtocolFields) {
			rows.Close()
			return assistantAssetReferencedError(asset, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM screenplay_assets WHERE id=? AND project_id=? AND owner_username=? AND revision=?`, asset.ID, project, owner, action.ExpectedRevision)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}
