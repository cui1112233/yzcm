package screenplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

// All session operations lock the project and authoring row first. No caller
// may perform network I/O in this short transaction.
type sessionScope struct {
	permissions           AuthoringState
	tx                    *sql.Tx
	ctx                   context.Context
	owner, project, actor string
	rows                  map[string]*AssistantSession
}

func (r *MySQLAuthoringRepository) sessionTransaction(ctx context.Context, owner, project, actor, right string, fn func(*sessionScope) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM screenplay_projects WHERE owner_username=? AND id=? AND deleted_at IS NULL FOR UPDATE", owner, project).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	st, err := loadAuthoringForUpdate(ctx, tx, owner, project)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if !sessionProjectAccess(st, owner, actor, right) {
		return ErrAccessDenied
	}
	scope := sessionScope{permissions: st, tx: tx, ctx: ctx, owner: owner, project: project, actor: actor, rows: map[string]*AssistantSession{}}
	result, err := tx.QueryContext(ctx, `SELECT session_id,COALESCE(parent_session_id,''),creator_username,visibility,mode,title,revision,archived_at IS NOT NULL,CASE WHEN request_status='running' AND request_deadline<=CURRENT_TIMESTAMP(6) THEN 'unknown' ELSE COALESCE(request_status,'') END, COALESCE(request_actor,'') FROM screenplay_assistant_sessions WHERE owner_username=? AND project_id=? ORDER BY session_id FOR UPDATE`, owner, project)
	if err != nil {
		return err
	}
	defer result.Close()
	for result.Next() {
		var item AssistantSession
		if err = result.Scan(&item.ID, &item.ParentID, &item.Creator, &item.Visibility, &item.Mode, &item.Title, &item.Revision, &item.Archived, &item.RequestStatus, &item.requestActor); err != nil {
			return err
		}
		scope.rows[item.ID] = &item
	}
	if err = result.Err(); err != nil {
		return err
	}
	if err = result.Close(); err != nil {
		return err
	}
	if err = fn(&scope); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *sessionScope) selected(id string) (*AssistantSession, error) {
	if !assistantSessionVisible(s.rows, id, s.actor) {
		return nil, ErrNotFound
	}
	return s.rows[id], nil
}

func (r *MySQLAuthoringRepository) ListAssistantSessions(ctx context.Context, owner, project, actor string) ([]AssistantSession, error) {
	list := []AssistantSession{{ID: "default", Title: "默认会话", Visibility: "project", Mode: "confirm"}}
	err := r.sessionTransaction(ctx, owner, project, actor, "read", func(s *sessionScope) error {
		for id, v := range s.rows {
			if assistantSessionVisible(s.rows, id, actor) {
				list = append(list, *v)
			}
		}
		sort.Slice(list[1:], func(i, j int) bool { return list[i+1].ID < list[j+1].ID })
		return nil
	})
	return list, err
}

func (r *MySQLAuthoringRepository) GetAssistantSession(ctx context.Context, owner, project, actor, id string) (*AssistantSession, AssistantSessionHistory, error) {
	var item *AssistantSession
	var history AssistantSessionHistory
	err := r.sessionTransaction(ctx, owner, project, actor, "read", func(s *sessionScope) error {
		var err error
		if item, err = s.selected(id); err != nil {
			return err
		}
		var raw []byte
		if err = s.tx.QueryRowContext(ctx, "SELECT body FROM screenplay_assistant_sessions WHERE owner_username=? AND project_id=? AND session_id=?", owner, project, id).Scan(&raw); err != nil {
			return err
		}
		return json.Unmarshal(raw, &history)
	})
	return item, history, err
}

func (r *MySQLAuthoringRepository) CreateAssistantSession(ctx context.Context, owner, project, actor string, input SessionCreate) (AssistantSession, error) {
	item := AssistantSession{ID: newScreenplayID("session"), ParentID: input.ParentID, Creator: actor, Title: strings.TrimSpace(input.Title), Visibility: input.Visibility, Mode: input.Mode, Revision: 1}
	if item.Visibility == "" {
		item.Visibility = "private"
	}
	if item.Mode == "" {
		item.Mode = "confirm"
	}
	if !validSessionTitle(item.Title) || (item.Visibility != "private" && item.Visibility != "project") || (item.Mode != "confirm" && item.Mode != "readonly") {
		return AssistantSession{}, ErrInvalidArgument
	}
	err := r.sessionTransaction(ctx, owner, project, actor, "edit", func(s *sessionScope) error {
		if item.ParentID != "" && item.ParentID != "default" {
			p, err := s.selected(item.ParentID)
			if err != nil {
				return err
			}
			if !sessionActive(s.rows, p.ID) || (p.Visibility == "private" && item.Visibility != "private") || (p.Mode == "readonly" && item.Mode != "readonly") {
				return ErrAccessDenied
			}
		}
		_, err := s.tx.ExecContext(ctx, `INSERT INTO screenplay_assistant_sessions(owner_username,project_id,session_id,parent_session_id,creator_username,visibility,mode,title,body) VALUES(?,?,?,NULLIF(?,''),?,?,?,?,JSON_OBJECT('messages',JSON_ARRAY(),'proposals',JSON_ARRAY()))`, owner, project, item.ID, item.ParentID, actor, item.Visibility, item.Mode, item.Title)
		return err
	})
	return item, err
}

func (r *MySQLAuthoringRepository) ChangeAssistantSession(ctx context.Context, owner, project, actor, id, action, title string, expected int64) error {
	right := "edit"
	if action == "stop" {
		right = "read"
	}
	return r.sessionTransaction(ctx, owner, project, actor, right, func(s *sessionScope) error {
		item, err := s.selected(id)
		if err != nil {
			return err
		}
		ownRequest := action == "stop" && item.requestActor == actor
		manager := actor == item.Creator || actor == owner && item.Visibility == "project"
		if !ownRequest && !(manager && sessionProjectAccess(s.permissions, owner, actor, "edit")) {
			return ErrAccessDenied
		}
		if expected != item.Revision {
			return ErrRevisionConflict
		}
		if !sessionActive(s.rows, item.ParentID) {
			return ErrInvalidState
		}
		switch action {
		case "rename":
			if item.RequestStatus != "" {
				return ErrInvalidState
			}
			title = strings.TrimSpace(title)
			if !validSessionTitle(title) {
				return ErrInvalidArgument
			}
			_, err = s.tx.ExecContext(ctx, "UPDATE screenplay_assistant_sessions SET title=?,revision=revision+1 WHERE owner_username=? AND project_id=? AND session_id=?", title, owner, project, id)
		case "archive", "restore":
			if item.Archived == (action == "archive") {
				return ErrInvalidState
			}
			if action == "archive" {
				for child, v := range s.rows {
					if child != id && v.RequestStatus != "" && sessionDescendant(s.rows, child, id) {
						if err = s.clearRequest(child); err != nil {
							return err
						}
					}
				}
			}
			_, err = s.tx.ExecContext(ctx, "UPDATE screenplay_assistant_sessions SET archived_at=IF(?,CURRENT_TIMESTAMP(6),NULL),request_actor=NULL,request_status=NULL,request_token=NULL,request_deadline=NULL,revision=revision+1 WHERE owner_username=? AND project_id=? AND session_id=?", action == "archive", owner, project, id)
		case "stop":
			if item.RequestStatus == "" {
				return ErrInvalidState
			}
			err = s.clearRequest(id)
		default:
			return ErrInvalidArgument
		}
		return err
	})
}

func (s *sessionScope) clearRequest(id string) error {
	_, err := s.tx.ExecContext(s.ctx, "UPDATE screenplay_assistant_sessions SET request_actor=NULL,request_status=NULL,request_token=NULL,request_deadline=NULL,revision=revision+1 WHERE owner_username=? AND project_id=? AND session_id=?", s.owner, s.project, id)
	return err
}

type AssistantSessionRequest struct {
	Owner, Project, Actor, SessionID, Token string
	Revision                                int64
}

func (r *MySQLAuthoringRepository) BeginAssistantRequest(ctx context.Context, owner, project, actor, id string, expected int64) (AssistantSessionRequest, error) {
	req := AssistantSessionRequest{Owner: owner, Project: project, Actor: actor, SessionID: id, Token: newScreenplayID("request"), Revision: expected + 1}
	err := r.sessionTransaction(ctx, owner, project, actor, "generate", func(s *sessionScope) error {
		item, err := s.selected(id)
		if err != nil {
			return err
		}
		if !sessionActive(s.rows, id) {
			return ErrInvalidState
		}
		if item.Revision != expected || item.RequestStatus != "" {
			return ErrRevisionConflict
		}
		_, err = s.tx.ExecContext(ctx, "UPDATE screenplay_assistant_sessions SET request_actor=?,request_token=?,request_status='running',request_deadline=DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL 30 MINUTE),revision=revision+1 WHERE owner_username=? AND project_id=? AND session_id=?", actor, req.Token, owner, project, id)
		return err
	})
	return req, err
}

// Unknown outcomes keep the reservation until an explicit stop.
func (r *MySQLAuthoringRepository) FinishAssistantRequest(ctx context.Context, req AssistantSessionRequest, history *AssistantSessionHistory) error {
	return r.sessionTransaction(ctx, req.Owner, req.Project, req.Actor, "generate", func(s *sessionScope) error {
		item, err := s.selected(req.SessionID)
		if err != nil {
			return err
		}
		if !sessionActive(s.rows, item.ID) || item.Revision != req.Revision || item.RequestStatus != "running" {
			return ErrRevisionConflict
		}
		query := "UPDATE screenplay_assistant_sessions SET request_status='unknown',revision=revision+1"
		args := []any{}
		if history != nil {
			if item.Mode == "readonly" && len(history.Proposals) > 0 {
				return ErrAccessDenied
			}
			body, err := json.Marshal(history)
			if err != nil {
				return err
			}
			query = "UPDATE screenplay_assistant_sessions SET body=?,request_status=NULL,request_actor=NULL,request_token=NULL,request_deadline=NULL,revision=revision+1"
			args = append(args, string(body))
		}
		args = append(args, req.Owner, req.Project, item.ID, req.Token, req.Actor, req.Revision)
		result, err := s.tx.ExecContext(ctx, query+" WHERE owner_username=? AND project_id=? AND session_id=? AND request_token=? AND request_actor=? AND revision=? AND request_deadline>CURRENT_TIMESTAMP(6)", args...)
		return oneShotRow(result, err)
	})
}

func revokedSessionActors(before, after AuthoringState) []string {
	next := map[string]MemberGrant{}
	for _, grant := range after.Members {
		next[grant.Username] = grant
	}
	var actors []string
	for _, g := range before.Members {
		n := next[g.Username]
		if (g.Read && !n.Read) || (g.Generate && !n.Generate) || (g.Edit && !n.Edit) {
			actors = append(actors, g.Username)
		}
	}
	return actors
}

func (r *MySQLAuthoringRepository) saveAndStopRequests(ctx context.Context, st AuthoringState, expected int64, actors []string, all bool) (AuthoringState, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthoringState{}, err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT 1 FROM screenplay_projects WHERE owner_username=? AND id=? AND deleted_at IS NULL FOR UPDATE", st.Owner, st.ProjectID).Scan(&exists); err != nil {
		return AuthoringState{}, err
	}
	st.Revision = expected + 1
	if expected == 0 {
		body, err := json.Marshal(st)
		if err != nil {
			return AuthoringState{}, err
		}
		result, err := tx.ExecContext(ctx, "INSERT IGNORE INTO screenplay_authoring(project_id,owner_username,revision,body) VALUES(?,?,?,?)", st.ProjectID, st.Owner, st.Revision, string(body))
		if err = oneShotRow(result, err); err != nil {
			return AuthoringState{}, err
		}
	} else if err = saveAuthoringStateInTx(ctx, tx, st, expected); err != nil {
		return AuthoringState{}, err
	}
	query := "UPDATE screenplay_assistant_sessions SET request_actor=NULL,request_status=NULL,request_token=NULL,request_deadline=NULL,revision=revision+1 WHERE owner_username=? AND project_id=? AND request_status IS NOT NULL"
	if all {
		_, err = tx.ExecContext(ctx, query, st.Owner, st.ProjectID)
	} else {
		for _, actor := range actors {
			if _, err = tx.ExecContext(ctx, query+" AND request_actor=?", st.Owner, st.ProjectID, actor); err != nil {
				break
			}
		}
	}
	if err != nil {
		return AuthoringState{}, err
	}
	return st, tx.Commit()
}
