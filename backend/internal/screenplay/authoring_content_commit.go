package screenplay

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

type AuthoringContentCommitter interface {
	CommitAuthoringContent(ctx context.Context, repository AuthoringRepository, actor, owner, project, proposalID string, expected int64) (AuthoringState, error)
}

func pendingContentAction(st AuthoringState, actor, id string, expected int64) (int, AuthoringAction, error) {
	editable := actor == st.Owner
	for _, member := range st.Members {
		if member.Username == actor && member.Read && member.Edit {
			editable = true
		}
	}
	if !editable {
		return 0, AuthoringAction{}, ErrAccessDenied
	}
	if st.Archived {
		return 0, AuthoringAction{}, ErrInvalidState
	}
	if st.Revision != expected {
		return 0, AuthoringAction{}, ErrRevisionConflict
	}
	for i, proposal := range st.Proposals {
		if proposal.ID != id {
			continue
		}
		if proposal.Status != "pending" || proposal.Revision != expected || len(proposal.Actions) != 1 {
			return 0, AuthoringAction{}, ErrRevisionConflict
		}
		a := proposal.Actions[0]
		if !isAssistantContentAction(a.Kind) {
			return 0, AuthoringAction{}, ErrInvalidArgument
		}
		if err := validateAssistantContentAction(a); err != nil {
			return 0, AuthoringAction{}, err
		}
		if a.ContentEdit.AuthorRevision != expected-1 {
			return 0, AuthoringAction{}, ErrRevisionConflict
		}
		return i, a, nil
	}
	return 0, AuthoringAction{}, ErrNotFound
}

func checkAssistantContentTarget(st AuthoringState, action AuthoringAction, source string, active Document) error {
	evidence := action.ContentEdit
	if evidence.SourceSHA256 != contentDigest(source) || evidence.Publish && !sameAssistantDocument(evidence.Document, assistantDocumentEvidence(active)) {
		return ErrRevisionConflict
	}
	if action.Kind == "document" {
		return nil
	}
	index, ok := findChapter(st.Chapters, evidence.Chapter.ID)
	if !ok {
		return ErrRevisionConflict
	}
	digest, err := assistantChapterDigest(st.Chapters[index])
	if err != nil {
		return err
	}
	if digest != evidence.ChapterSHA256 {
		return ErrRevisionConflict
	}
	if evidence.Publish {
		digest, err = assistantChapterDigest(st.Chapters)
		if err != nil {
			return err
		}
		if digest != evidence.CollectionSHA256 {
			return ErrRevisionConflict
		}
	}
	return nil
}

// Rebuild the edited chapter from the locked row. Only explicitly selected
// fields come from the proposal. The rest of the chapter stays server-owned.
func prepareAssistantContentCommit(st *AuthoringState, action AuthoringAction, source string, active Document) (ScreenplayResult, error) {
	if err := checkAssistantContentTarget(*st, action, source, active); err != nil {
		return ScreenplayResult{}, err
	}
	content := action.Content
	if action.Kind == "chapter" {
		patch := action.ContentEdit.Chapter
		index, _ := findChapter(st.Chapters, patch.ID)
		next, err := patchAssistantChapter(st.Chapters[index], *patch)
		if err != nil {
			return ScreenplayResult{}, err
		}
		st.Chapters[index] = next
		content, err = assistantChapterPublication(st.Chapters)
		if err != nil {
			return ScreenplayResult{}, err
		}
		if (content != "") != action.ContentEdit.Publish {
			return ScreenplayResult{}, ErrRevisionConflict
		}
	}
	if content == "" {
		return ScreenplayResult{}, nil
	}
	return prepareScreenplayPublication(st.Owner, st.ProjectID, "chapters", content, 1)
}

func (m *MemoryStore) CommitAuthoringContent(ctx context.Context, repository AuthoringRepository, actor, owner, project, id string, expected int64) (AuthoringState, error) {
	repo, ok := repository.(*MemoryAuthoringRepository)
	if !ok || repo == nil {
		return AuthoringState{}, ErrInvalidState
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return AuthoringState{}, err
	}
	key := projectKey(owner, project)
	p, projectExists := m.projects[key]
	current, stateExists := repo.rows[key]
	if !projectExists || !stateExists {
		return AuthoringState{}, ErrNotFound
	}
	st := cloneAuthoring(current)
	st.Owner, st.ProjectID = owner, project
	normalizeAuthoringChapters(st.Chapters)
	index, action, err := pendingContentAction(st, actor, id, expected)
	if err != nil {
		return AuthoringState{}, err
	}
	active, err := m.activeDocumentLocked(owner, project)
	if err != nil && !errors.Is(err, ErrNoActiveScript) {
		return AuthoringState{}, err
	}
	prepared, err := prepareAssistantContentCommit(&st, action, p.SourceText, active)
	if err != nil {
		return AuthoringState{}, err
	}
	if prepared.Document.ID != "" {
		if _, err = m.saveScreenplayLocked(prepared.Document, prepared.Shots, prepared.Characters, prepared.Scenes); err != nil {
			return AuthoringState{}, err
		}
	}
	markShotApplied(&st, index, expected)
	repo.rows[key] = cloneAuthoring(st)
	return st, nil
}

func (s *MySQLStore) CommitAuthoringContent(ctx context.Context, repository AuthoringRepository, actor, owner, project, id string, expected int64) (AuthoringState, error) {
	repo, ok := repository.(*MySQLAuthoringRepository)
	if !ok || repo == nil || s == nil || s.db == nil || repo.db != s.db {
		return AuthoringState{}, fmt.Errorf("%w: 正文、章节和确认记录必须使用同一MySQL数据库", ErrInvalidState)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return AuthoringState{}, err
	}
	defer tx.Rollback()
	var source string
	err = tx.QueryRowContext(ctx, `SELECT source_text FROM screenplay_projects WHERE id=? AND owner_username=? AND deleted_at IS NULL FOR UPDATE`, project, owner).Scan(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthoringState{}, ErrNotFound
	}
	if err != nil {
		return AuthoringState{}, err
	}
	st, err := loadAuthoringForUpdate(ctx, tx, owner, project)
	if err != nil {
		return AuthoringState{}, err
	}
	index, action, err := pendingContentAction(st, actor, id, expected)
	if err != nil {
		return AuthoringState{}, err
	}
	if err = applyAssistantContentInTx(ctx, tx, &st, action, source); err != nil {
		return AuthoringState{}, err
	}
	markShotApplied(&st, index, expected)
	if err = saveAuthoringStateInTx(ctx, tx, st, expected); err != nil {
		return AuthoringState{}, err
	}
	if err = tx.Commit(); err != nil {
		return AuthoringState{}, err
	}
	return st, nil
}

func loadAuthoringForUpdate(ctx context.Context, tx *sql.Tx, owner, project string) (AuthoringState, error) {
	var raw []byte
	var revision int64
	err := tx.QueryRowContext(ctx, `SELECT body,revision FROM screenplay_authoring WHERE owner_username=? AND project_id=? FOR UPDATE`, owner, project).Scan(&raw, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthoringState{}, ErrNotFound
	}
	if err != nil {
		return AuthoringState{}, err
	}
	var st AuthoringState
	if err = json.Unmarshal(raw, &st); err != nil {
		return AuthoringState{}, err
	}
	st.Owner, st.ProjectID, st.Revision = owner, project, revision
	normalizeAuthoringChapters(st.Chapters)
	return st, nil
}

func saveAuthoringStateInTx(ctx context.Context, tx *sql.Tx, st AuthoringState, expected int64) error {
	body, err := json.Marshal(st)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE screenplay_authoring SET body=?,revision=? WHERE project_id=? AND owner_username=? AND revision=?`, string(body), st.Revision, st.ProjectID, st.Owner, expected)
	return oneShotRow(result, err)
}

func applyAssistantContentInTx(ctx context.Context, tx *sql.Tx, st *AuthoringState, action AuthoringAction, source string) error {
	owner, project := st.Owner, st.ProjectID
	var err error
	var active Document
	if action.ContentEdit.Publish {
		err = tx.QueryRowContext(ctx, `SELECT id,version,revision,content FROM screenplay_documents WHERE project_id=? AND owner_username=? AND status='active' ORDER BY version DESC LIMIT 1 FOR UPDATE`, project, owner).Scan(&active.ID, &active.Version, &active.Revision, &active.Content)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	prepared, err := prepareAssistantContentCommit(st, action, source, active)
	if err != nil {
		return err
	}
	if prepared.Document.ID != "" {
		if _, err = saveScreenplayInTx(ctx, tx, prepared.Document, prepared.Shots, prepared.Characters, prepared.Scenes); err != nil {
			return err
		}
	}
	return nil
}
