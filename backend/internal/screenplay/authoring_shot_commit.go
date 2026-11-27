package screenplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type AuthoringShotCommitter interface {
	CommitAuthoringShot(context.Context, AuthoringRepository, string, string, string, int64) (AuthoringState, error)
}

func pendingShotAction(st AuthoringState, id string, v int64) (int, AuthoringAction, error) {
	if st.Revision != v {
		return 0, AuthoringAction{}, ErrRevisionConflict
	}
	for i, p := range st.Proposals {
		if p.ID != id {
			continue
		}
		if p.Status != "pending" || p.Revision != v || len(p.Actions) != 1 {
			return 0, AuthoringAction{}, ErrRevisionConflict
		}
		a := p.Actions[0]
		if a.Kind != "creative_shot" {
			return 0, a, ErrInvalidArgument
		}
		if e := validateCreativeShotAction(a); e != nil {
			return 0, a, e
		}
		return i, a, nil
	}
	return 0, AuthoringAction{}, ErrNotFound
}
func markShotApplied(st *AuthoringState, index int, v int64) {
	st.Proposals[index].Status = "applied"
	st.Revision = v + 1
	for i := range st.Proposals {
		if st.Proposals[i].Status == "pending" && st.Proposals[i].Revision != v+1 {
			st.Proposals[i].Status = "stale"
		}
	}
}

func (m *MemoryStore) CommitAuthoringShot(_ context.Context, repository AuthoringRepository, o, p, id string, v int64) (AuthoringState, error) {
	r, ok := repository.(*MemoryAuthoringRepository)
	if !ok || r == nil {
		return AuthoringState{}, ErrInvalidState
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r.mu.Lock()
	defer r.mu.Unlock()
	key := projectKey(o, p)
	if _, ok = m.projects[key]; !ok {
		return AuthoringState{}, ErrNotFound
	}
	st := cloneAuthoring(r.rows[key])
	i, a, e := pendingShotAction(st, id, v)
	if e != nil {
		return AuthoringState{}, e
	}
	if d := a.CreativeShot.Binding; d != nil {
		for _, expected := range d.Assets {
			found := false
			for _, asset := range m.assets[key] {
				if asset.ID == expected.ID {
					found = asset.Type == expected.Type && asset.Name == expected.Name && asset.Revision == expected.Revision
					break
				}
			}
			if !found {
				return AuthoringState{}, ErrRevisionConflict
			}
		}
	}
	for j, b := range m.storyboards[key] {
		if b.ID != a.CreativeShot.BoardID {
			continue
		}
		b = cloneStoryboard(b)
		if e = applyCreativeShot(&b, a); e != nil {
			return AuthoringState{}, e
		}
		b.Revision++
		b.UpdatedAt = time.Now().UTC()
		markShotApplied(&st, i, v)
		m.storyboards[key][j] = cloneStoryboard(b)
		r.rows[key] = cloneAuthoring(st)
		return cloneAuthoring(st), nil
	}
	return AuthoringState{}, ErrNotFound
}

func (s *MySQLStore) CommitAuthoringShot(ctx context.Context, repository AuthoringRepository, o, p, id string, v int64) (AuthoringState, error) {
	r, ok := repository.(*MySQLAuthoringRepository)
	if !ok || r == nil || s == nil || s.db == nil || r.db != s.db {
		return AuthoringState{}, fmt.Errorf("%w: 分镜和确认记录必须使用同一MySQL数据库", ErrInvalidState)
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if e != nil {
		return AuthoringState{}, e
	}
	defer tx.Rollback()
	var existing string
	if e = tx.QueryRowContext(ctx, `SELECT id FROM screenplay_projects WHERE id=? AND owner_username=? AND deleted_at IS NULL FOR UPDATE`, p, o).Scan(&existing); e != nil {
		return AuthoringState{}, e
	}
	var body []byte
	var revision int64
	if e = tx.QueryRowContext(ctx, `SELECT body,revision FROM screenplay_authoring WHERE owner_username=? AND project_id=? FOR UPDATE`, o, p).Scan(&body, &revision); e != nil {
		return AuthoringState{}, e
	}
	var st AuthoringState
	if e = json.Unmarshal(body, &st); e != nil {
		return st, e
	}
	st.Revision = revision
	st.Owner = o
	st.ProjectID = p
	i, a, e := pendingShotAction(st, id, v)
	if e != nil {
		return AuthoringState{}, e
	}
	d := a.CreativeShot
	b := Storyboard{ProjectID: p, OwnerUsername: o, EpisodeNo: d.Episode, Engine: d.Engine}
	if e = tx.QueryRowContext(ctx, `SELECT id,revision FROM screenplay_storyboards WHERE project_id=? AND owner_username=? AND episode_no=? AND engine=? FOR UPDATE`, p, o, d.Episode, string(d.Engine)).Scan(&b.ID, &b.Revision); e != nil {
		return AuthoringState{}, e
	}
	if b.ID != d.BoardID || b.Revision != a.ExpectedRevision {
		return AuthoringState{}, ErrRevisionConflict
	}
	var sh StoryboardShot
	var protocol []byte
	e = tx.QueryRowContext(ctx, `SELECT id,storyboard_id,project_id,owner_username,seq,scene_id,duration_sec,characters_text,scene_name,props_text,video_desc,protocol_json FROM screenplay_storyboard_shots WHERE id=? AND storyboard_id=? AND project_id=? AND owner_username=? FOR UPDATE`, a.TargetID, b.ID, p, o).Scan(&sh.ID, &sh.StoryboardID, &sh.ProjectID, &sh.OwnerUsername, &sh.Seq, &sh.SceneID, &sh.Duration, &sh.Characters, &sh.Scene, &sh.Props, &sh.VideoDesc, &protocol)
	if e != nil {
		return AuthoringState{}, e
	}
	var fields storyboardProtocol
	if len(protocol) > 0 {
		if e = json.Unmarshal(protocol, &fields); e != nil {
			return AuthoringState{}, e
		}
		fields.apply(&sh)
	}
	b.Shots = []StoryboardShot{sh}
	if e = applyCreativeShot(&b, a); e != nil {
		return AuthoringState{}, e
	}
	// Serialize through the existing protocol contract, then merge only requested
	// fields into the original object so unknown extension keys survive as well.
	encoded, e := marshalStoryboardProtocol(d.After)
	if e != nil {
		return AuthoringState{}, e
	}
	known := map[string]json.RawMessage{}
	original := map[string]json.RawMessage{}
	if e = json.Unmarshal([]byte(encoded), &known); e != nil {
		return AuthoringState{}, e
	}
	if len(protocol) > 0 {
		if e = json.Unmarshal(protocol, &original); e != nil {
			return AuthoringState{}, e
		}
	}
	if original == nil {
		original = map[string]json.RawMessage{}
	}
	if d.Binding != nil {
		for _, expected := range d.Binding.Assets {
			var current bindingAsset
			current.ID = expected.ID
			e = tx.QueryRowContext(ctx, `SELECT asset_type,name,revision FROM screenplay_assets WHERE id=? AND project_id=? AND owner_username=? FOR UPDATE`, expected.ID, p, o).Scan(&current.Type, &current.Name, &current.Revision)
			if errors.Is(e, sql.ErrNoRows) || e == nil && current != expected {
				return AuthoringState{}, ErrRevisionConflict
			}
			if e != nil {
				return AuthoringState{}, e
			}
		}
		original["protocol_fields"] = known["protocol_fields"]
	} else {
		for _, c := range d.Changes {
			if c.Field != "video_desc" {
				original[c.Field] = known[c.Field]
			}
		}
	}
	merged, e := json.Marshal(original)
	if e != nil {
		return AuthoringState{}, e
	}
	now := time.Now().UTC()
	var result sql.Result
	if d.Binding != nil {
		result, e = tx.ExecContext(ctx, `UPDATE screenplay_storyboard_shots SET characters_text=?,scene_name=?,props_text=?,protocol_json=?,updated_at=? WHERE id=? AND storyboard_id=? AND project_id=? AND owner_username=?`, d.After.Characters, d.After.Scene, d.After.Props, string(merged), now, a.TargetID, b.ID, p, o)
	} else {
		result, e = tx.ExecContext(ctx, `UPDATE screenplay_storyboard_shots SET video_desc=?,protocol_json=?,updated_at=? WHERE id=? AND storyboard_id=? AND project_id=? AND owner_username=?`, d.After.VideoDesc, string(merged), now, a.TargetID, b.ID, p, o)
	}
	if e = oneShotRow(result, e); e != nil {
		return AuthoringState{}, e
	}
	result, e = tx.ExecContext(ctx, `UPDATE screenplay_storyboards SET revision=revision+1,updated_at=? WHERE id=? AND project_id=? AND owner_username=? AND revision=?`, now, b.ID, p, o, a.ExpectedRevision)
	if e = oneShotRow(result, e); e != nil {
		return AuthoringState{}, e
	}
	markShotApplied(&st, i, v)
	body, e = json.Marshal(st)
	if e != nil {
		return AuthoringState{}, e
	}
	result, e = tx.ExecContext(ctx, `UPDATE screenplay_authoring SET body=?,revision=? WHERE project_id=? AND owner_username=? AND revision=?`, string(body), st.Revision, p, o, v)
	if e = oneShotRow(result, e); e != nil {
		return AuthoringState{}, e
	}
	if e = tx.Commit(); e != nil {
		return AuthoringState{}, e
	}
	return st, nil
}
func oneShotRow(result sql.Result, e error) error {
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrRevisionConflict
	}
	return nil
}
