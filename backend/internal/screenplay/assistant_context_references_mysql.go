package screenplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
)

// Caller already holds project, authoring and (for independent chats) session
// locks. Target locks remain held through the enclosing save commit.
func CheckAssistantReferencesInTx(ctx context.Context, tx *sql.Tx, owner, project string, refs []AssistantReferenceSnapshot) error {
	if len(refs) == 0 {
		return nil
	}
	v := AssistantReferenceView{Project: Project{ID: project}}
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(source_text,'') FROM screenplay_projects WHERE owner_username=? AND id=? AND deleted_at IS NULL FOR UPDATE", owner, project).Scan(&v.Project.SourceText); err != nil {
		return referenceSQLError(err)
	}
	st, err := loadAuthoringForUpdate(ctx, tx, owner, project)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	v.Chapters = st.Chapters
	ordered := append([]AssistantReferenceSnapshot(nil), refs...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Kind+ordered[i].ID < ordered[j].Kind+ordered[j].ID })
	for _, ref := range ordered {
		switch ref.Kind {
		case "document":
			var d Document
			err = tx.QueryRowContext(ctx, "SELECT id,status,version,revision,content FROM screenplay_documents WHERE owner_username=? AND project_id=? AND id=? FOR UPDATE", owner, project, ref.ID).Scan(&d.ID, &d.Status, &d.Version, &d.Revision, &d.Content)
			v.Documents = append(v.Documents, d)
		case "asset_text":
			var a Asset
			err = tx.QueryRowContext(ctx, "SELECT id,name,asset_type,COALESCE(identity_text,''),COALESCE(description,''),COALESCE(prompt,''),revision FROM screenplay_assets WHERE owner_username=? AND project_id=? AND id=? FOR UPDATE", owner, project, ref.ID).Scan(&a.ID, &a.Name, &a.Type, &a.Identity, &a.Description, &a.Prompt, &a.Revision)
			v.Assets = append(v.Assets, a)
		case "storyboard":
			var b Storyboard
			err = tx.QueryRowContext(ctx, "SELECT id,episode_no,engine,revision FROM screenplay_storyboards WHERE owner_username=? AND project_id=? AND id=? FOR UPDATE", owner, project, ref.ID).Scan(&b.ID, &b.EpisodeNo, &b.Engine, &b.Revision)
			if err != nil {
				return referenceSQLError(err)
			}
			rows, e := tx.QueryContext(ctx, "SELECT id,seq,characters_text,scene_name,props_text,video_desc,protocol_json FROM screenplay_storyboard_shots WHERE owner_username=? AND project_id=? AND storyboard_id=? ORDER BY seq FOR UPDATE", owner, project, ref.ID)
			if e != nil {
				return e
			}
			for rows.Next() {
				var s StoryboardShot
				var raw []byte
				if e = rows.Scan(&s.ID, &s.Seq, &s.Characters, &s.Scene, &s.Props, &s.VideoDesc, &raw); e != nil {
					rows.Close()
					return e
				}
				if len(raw) > 0 {
					var p storyboardProtocol
					if e = json.Unmarshal(raw, &p); e != nil {
						rows.Close()
						return e
					}
					p.apply(&s)
				}
				b.Shots = append(b.Shots, s)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			v.Boards = append(v.Boards, b)
		}
		if err != nil {
			return referenceSQLError(err)
		}
		now, e := v.Describe(ref.Selector())
		if e != nil {
			return referenceSQLError(e)
		}
		if now.Digest != ref.Digest {
			return ErrRevisionConflict
		}
	}
	return nil
}
func referenceSQLError(err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, ErrNotFound) {
		return ErrRevisionConflict
	}
	return err
}

// No provider stage was invoked. Record that fact without creating an unknown
// provider outcome or appending messages. The old token is released atomically.
func (r *MySQLAuthoringRepository) ReleaseAssistantReferencesUnsent(ctx context.Context, req AssistantSessionRequest) error {
	return r.sessionTransaction(ctx, req.Owner, req.Project, req.Actor, "generate", func(s *sessionScope) error {
		item, err := s.selected(req.SessionID)
		if err != nil {
			return err
		}
		if item.Revision != req.Revision || item.RequestStatus != "running" {
			return ErrRevisionConflict
		}
		var token string
		if err = s.tx.QueryRowContext(ctx, "SELECT request_token FROM screenplay_assistant_sessions WHERE owner_username=? AND project_id=? AND session_id=?", req.Owner, req.Project, req.SessionID).Scan(&token); err != nil {
			return err
		}
		if token != req.Token {
			return ErrRevisionConflict
		}
		if err = recordSessionRequests(ctx, s.tx, "completed", "unsent", sessionHistoryFilter, req.Owner, req.Project, req.SessionID); err != nil {
			return err
		}
		return s.clearRequest(req.SessionID)
	})
}

// A reference-bearing default message stays in the legacy row, but its
// dependencies, current grant and original login are checked atomically.
func (r *MySQLAuthoringRepository) SaveAssistantReferences(ctx context.Context, st AuthoringState, actor string, expected int64, refs []AssistantReferenceSnapshot, guard func(context.Context, *sql.Tx) error) (AuthoringState, error) {
	if guard == nil {
		return AuthoringState{}, ErrAccessDenied
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthoringState{}, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT 1 FROM screenplay_projects WHERE owner_username=? AND id=? AND deleted_at IS NULL FOR UPDATE", st.Owner, st.ProjectID).Scan(&exists); err != nil {
		return AuthoringState{}, err
	}
	old, err := loadAuthoringForUpdate(ctx, tx, st.Owner, st.ProjectID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return AuthoringState{}, err
	}
	if old.Revision != expected {
		return AuthoringState{}, ErrRevisionConflict
	}
	if !sessionProjectAccess(old, st.Owner, actor, "generate") {
		return AuthoringState{}, ErrAccessDenied
	}
	if len(st.Proposals) > len(old.Proposals) && !sessionProjectAccess(old, st.Owner, actor, "edit") {
		return AuthoringState{}, ErrAccessDenied
	}
	if err = guard(ctx, tx); err != nil {
		return AuthoringState{}, err
	}
	if err = CheckAssistantReferencesInTx(ctx, tx, st.Owner, st.ProjectID, refs); err != nil {
		return AuthoringState{}, err
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
			return AuthoringState{}, e
		}
		res, e := tx.ExecContext(ctx, "INSERT IGNORE INTO screenplay_authoring(project_id,owner_username,revision,body) VALUES(?,?,?,?)", st.ProjectID, st.Owner, st.Revision, string(raw))
		err = oneShotRow(res, e)
	} else {
		err = saveAuthoringStateInTx(ctx, tx, st, expected)
	}
	if err != nil {
		return AuthoringState{}, err
	}
	if err = tx.Commit(); err != nil {
		return AuthoringState{}, err
	}
	return st, nil
}
