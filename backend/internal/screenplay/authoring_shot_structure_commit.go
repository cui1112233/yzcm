package screenplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

type AuthoringShotStructureCommitter interface {
	CommitAuthoringShotStructure(context.Context, AuthoringRepository, string, string, string, int64) (AuthoringState, error)
}

func pendingShotStructure(st AuthoringState, id string, v int64) (int, AuthoringAction, error) {
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
		if a.Kind != "creative_shot_structure" {
			return 0, a, ErrInvalidArgument
		}
		return i, a, validateShotStructure(a)
	}
	return 0, AuthoringAction{}, ErrNotFound
}
func (m *MemoryStore) CommitAuthoringShotStructure(_ context.Context, repository AuthoringRepository, o, p, id string, v int64) (AuthoringState, error) {
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
	i, a, e := pendingShotStructure(st, id, v)
	if e != nil {
		return AuthoringState{}, e
	}
	for j, b := range m.storyboards[key] {
		if b.ID != a.CreativeStructure.BoardID {
			continue
		}
		b = cloneStoryboard(b)
		if e = applyShotStructure(&b, a); e != nil {
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
func (s *MySQLStore) CommitAuthoringShotStructure(ctx context.Context, repository AuthoringRepository, o, p, id string, v int64) (AuthoringState, error) {
	r, ok := repository.(*MySQLAuthoringRepository)
	if !ok || r == nil || s == nil || s.db == nil || s.db != r.db {
		return AuthoringState{}, ErrInvalidState
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if e != nil {
		return AuthoringState{}, e
	}
	defer tx.Rollback()
	var project string
	if e = tx.QueryRowContext(ctx, `SELECT id FROM screenplay_projects WHERE id=? AND owner_username=? AND deleted_at IS NULL FOR UPDATE`, p, o).Scan(&project); e != nil {
		return AuthoringState{}, e
	}
	var body []byte
	var rev int64
	if e = tx.QueryRowContext(ctx, `SELECT body,revision FROM screenplay_authoring WHERE owner_username=? AND project_id=? FOR UPDATE`, o, p).Scan(&body, &rev); e != nil {
		return AuthoringState{}, e
	}
	var st AuthoringState
	if e = json.Unmarshal(body, &st); e != nil {
		return st, e
	}
	st.Revision = rev
	st.Owner = o
	st.ProjectID = p
	i, a, e := pendingShotStructure(st, id, v)
	if e != nil {
		return AuthoringState{}, e
	}
	if e = applyAssistantShotStructureInTx(ctx, tx, o, p, a); e != nil {
		return AuthoringState{}, e
	}
	markShotApplied(&st, i, v)
	body, e = json.Marshal(st)
	if e != nil {
		return AuthoringState{}, e
	}
	result, e := tx.ExecContext(ctx, `UPDATE screenplay_authoring SET body=?,revision=? WHERE project_id=? AND owner_username=? AND revision=?`, string(body), st.Revision, p, o, v)
	if e = oneShotRow(result, e); e != nil {
		return AuthoringState{}, e
	}
	if e = tx.Commit(); e != nil {
		return AuthoringState{}, e
	}
	return st, nil
}

func applyAssistantShotStructureInTx(ctx context.Context, tx *sql.Tx, o, p string, a AuthoringAction) error {
	var e error
	d := a.CreativeStructure
	b := Storyboard{ProjectID: p, OwnerUsername: o, EpisodeNo: d.Episode, Engine: d.Engine, Shots: []StoryboardShot{}}
	if e = tx.QueryRowContext(ctx, `SELECT id,revision FROM screenplay_storyboards WHERE project_id=? AND owner_username=? AND episode_no=? AND engine=? FOR UPDATE`, p, o, d.Episode, string(d.Engine)).Scan(&b.ID, &b.Revision); e != nil {
		return e
	}
	if b.ID != d.BoardID || b.Revision != a.ExpectedRevision {
		return ErrRevisionConflict
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,storyboard_id,project_id,owner_username,seq,scene_id,duration_sec,characters_text,scene_name,props_text,video_desc,protocol_json FROM screenplay_storyboard_shots WHERE storyboard_id=? AND project_id=? AND owner_username=? ORDER BY seq FOR UPDATE`, b.ID, p, o)
	if e != nil {
		return e
	}
	for rows.Next() {
		var sh StoryboardShot
		var protocol []byte
		if e = rows.Scan(&sh.ID, &sh.StoryboardID, &sh.ProjectID, &sh.OwnerUsername, &sh.Seq, &sh.SceneID, &sh.Duration, &sh.Characters, &sh.Scene, &sh.Props, &sh.VideoDesc, &protocol); e != nil {
			rows.Close()
			return e
		}
		if len(protocol) > 0 {
			var f storyboardProtocol
			if e = json.Unmarshal(protocol, &f); e != nil {
				rows.Close()
				return e
			}
			f.apply(&sh)
		}
		b.Shots = append(b.Shots, sh)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if e = applyShotStructure(&b, a); e != nil {
		return e
	}
	now := time.Now().UTC()
	// Move surviving sequence numbers into a disjoint positive range first. This
	// preserves IDs/media rows and avoids transient collisions under UNIQUE(board,seq).
	old := map[string]StoryboardShot{}
	maxSeq := 0
	for _, sh := range d.Before {
		old[sh.ID] = sh
		if sh.Seq > maxSeq {
			maxSeq = sh.Seq
		}
	}
	if maxSeq > 1000000000 {
		return ErrInvalidState
	}
	if d.Operation == "delete" {
		for _, sh := range d.Targets {
			result, err := tx.ExecContext(ctx, `DELETE FROM screenplay_storyboard_shots WHERE id=? AND storyboard_id=? AND project_id=? AND owner_username=?`, sh.ID, b.ID, p, o)
			if e = oneShotRow(result, err); e != nil {
				return e
			}
		}
	}
	for index, sh := range d.After {
		if before, ok := old[sh.ID]; ok && before.Seq != sh.Seq {
			result, err := tx.ExecContext(ctx, `UPDATE screenplay_storyboard_shots SET seq=?,updated_at=? WHERE id=? AND storyboard_id=? AND project_id=? AND owner_username=?`, maxSeq+len(d.After)+index+1, now, sh.ID, b.ID, p, o)
			if e = oneShotRow(result, err); e != nil {
				return e
			}
		}
	}
	for _, sh := range d.After {
		before, ok := old[sh.ID]
		if ok {
			if before.Seq != sh.Seq {
				result, err := tx.ExecContext(ctx, `UPDATE screenplay_storyboard_shots SET seq=?,updated_at=? WHERE id=? AND storyboard_id=? AND project_id=? AND owner_username=?`, sh.Seq, now, sh.ID, b.ID, p, o)
				if e = oneShotRow(result, err); e != nil {
					return e
				}
			}
			continue
		}
		if sh.ProjectID != p || sh.OwnerUsername != o {
			return ErrInvalidArgument
		}
		protocol, err := marshalStoryboardProtocol(sh)
		if err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO screenplay_storyboard_shots (id,storyboard_id,project_id,owner_username,seq,scene_id,duration_sec,characters_text,scene_name,props_text,video_desc,protocol_json,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, sh.ID, b.ID, p, o, sh.Seq, sh.SceneID, sh.Duration, sh.Characters, sh.Scene, sh.Props, sh.VideoDesc, protocol, now, now)
		if e = oneShotRow(result, err); e != nil {
			return e
		}
	}
	result, e := tx.ExecContext(ctx, `UPDATE screenplay_storyboards SET shot_count=?,revision=revision+1,updated_at=? WHERE id=? AND project_id=? AND owner_username=? AND revision=?`, len(b.Shots), now, b.ID, p, o, a.ExpectedRevision)
	if e = oneShotRow(result, e); e != nil {
		return e
	}
	return nil
}
