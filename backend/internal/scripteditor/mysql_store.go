package scripteditor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type MySQLStore struct {
	db    *sql.DB
	now   func() time.Time
	newID func(string) string
}

func NewMySQLStore(db *sql.DB) *MySQLStore {
	return &MySQLStore{db: db, now: time.Now, newID: randomID}
}

func (s *MySQLStore) CreateWork(ctx context.Context, owner string, input CreateWorkInput) (Work, error) {
	owner, err := normalizedOwner(owner)
	if err != nil || !input.Type.Valid() {
		return Work{}, ErrValidation
	}
	title, err := NormalizeTitle(input.Title)
	if err != nil {
		return Work{}, err
	}
	now := s.now().UTC()
	work := Work{ID: s.newID("work"), OwnerUsername: owner, Title: title, Type: input.Type, SourceFilename: strings.TrimSpace(input.SourceFilename), Mark: MarkNone, CreatedAt: now, UpdatedAt: now}
	_, err = s.db.ExecContext(ctx, `INSERT INTO script_editor_works (id,owner_username,title,work_type,source_filename,mark_status,revision,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`, work.ID, owner, title, string(work.Type), work.SourceFilename, string(work.Mark), work.Revision, now, now)
	return work, mapWriteError(err)
}

func (s *MySQLStore) ListWorks(ctx context.Context, owner string, filter WorkFilter) ([]WorkSummary, error) {
	query := `%` + strings.TrimSpace(filter.Query) + `%`
	rows, err := s.db.QueryContext(ctx, `SELECT w.id,w.owner_username,w.title,w.work_type,w.source_filename,w.mark_status,w.revision,w.created_at,w.updated_at,w.deleted_at,
 (SELECT COUNT(*) FROM script_editor_chapters c WHERE c.owner_username=w.owner_username AND c.work_id=w.id),
 (SELECT COUNT(*) FROM script_editor_scripts sc WHERE sc.owner_username=w.owner_username AND sc.work_id=w.id),
 (SELECT COALESCE(SUM(c.word_count),0) FROM script_editor_chapters c WHERE c.owner_username=w.owner_username AND c.work_id=w.id)
 FROM script_editor_works w WHERE w.owner_username=? AND w.deleted_at IS NULL AND (?='' OR w.work_type=?) AND (?='' OR w.mark_status=?) AND (?='%%' OR w.title LIKE ?)
 ORDER BY w.updated_at DESC`, owner, string(filter.Type), string(filter.Type), string(filter.Mark), string(filter.Mark), query, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkSummary{}
	for rows.Next() {
		var row WorkSummary
		if err := scanWorkSummary(rows, &row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		switch filter.Sort {
		case "name-asc":
			return out[i].Title < out[j].Title
		case "name-desc":
			return out[i].Title > out[j].Title
		case "updated-asc":
			return out[i].UpdatedAt.Before(out[j].UpdatedAt)
		default:
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
	})
	return out, nil
}

func (s *MySQLStore) GetWork(ctx context.Context, owner, id string) (WorkDetail, error) {
	work, err := s.getWork(ctx, owner, id)
	if err != nil {
		return WorkDetail{}, err
	}
	detail := WorkDetail{Work: work, Chapters: []Chapter{}, Scripts: []Script{}}
	chapters, err := s.listChapters(ctx, owner, id)
	if err != nil {
		return WorkDetail{}, err
	}
	scripts, err := s.ListScripts(ctx, owner, id)
	if err != nil {
		return WorkDetail{}, err
	}
	detail.Chapters, detail.Scripts = chapters, scripts
	for _, script := range scripts {
		for _, chapterID := range script.SourceChapterIDs {
			for index := range detail.Chapters {
				if detail.Chapters[index].ID == chapterID {
					detail.Chapters[index].ReferencedByScriptCount++
				}
			}
		}
	}
	return detail, nil
}

func (s *MySQLStore) UpdateWork(ctx context.Context, owner, id string, input UpdateWorkInput, expected int64) (Work, error) {
	work, err := s.getWork(ctx, owner, id)
	if err != nil {
		return Work{}, err
	}
	if work.Revision != expected {
		return Work{}, ErrConflict
	}
	if input.Title != nil {
		work.Title, err = NormalizeTitle(*input.Title)
		if err != nil {
			return Work{}, err
		}
	}
	if input.Mark != nil {
		if !input.Mark.Valid() {
			return Work{}, ErrValidation
		}
		work.Mark = *input.Mark
	}
	work.UpdatedAt = s.now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE script_editor_works SET title=?,mark_status=?,revision=revision+1,updated_at=? WHERE id=? AND owner_username=? AND revision=? AND deleted_at IS NULL`, work.Title, string(work.Mark), work.UpdatedAt, id, owner, expected)
	if err != nil {
		return Work{}, err
	}
	if err := requireAffected(result); err != nil {
		return Work{}, err
	}
	work.Revision++
	return work, nil
}

func (s *MySQLStore) DeleteWork(ctx context.Context, owner, id string, expected int64) error {
	now := s.now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE script_editor_works SET deleted_at=?,updated_at=?,revision=revision+1 WHERE id=? AND owner_username=? AND revision=? AND deleted_at IS NULL`, now, now, id, owner, expected)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func (s *MySQLStore) CreateChapter(ctx context.Context, owner, workID string, input CreateChapterInput) (Chapter, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Chapter{}, err
	}
	defer tx.Rollback()
	if err := lockWork(ctx, tx, owner, workID); err != nil {
		return Chapter{}, err
	}
	order := input.Order
	if order <= 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort_order),0)+1 FROM script_editor_chapters WHERE owner_username=? AND work_id=?`, owner, workID).Scan(&order); err != nil {
			return Chapter{}, err
		}
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = "新章节"
	}
	now := s.now().UTC()
	row := Chapter{ID: s.newID("chapter"), WorkID: workID, OwnerUsername: owner, Title: title, Order: order, Content: input.Content, WordCount: countWords(input.Content), CreatedAt: now, UpdatedAt: now}
	if _, err := tx.ExecContext(ctx, `INSERT INTO script_editor_chapters (id,work_id,owner_username,title,sort_order,content,word_count,revision,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, row.ID, workID, owner, title, order, row.Content, row.WordCount, row.Revision, now, now); err != nil {
		return Chapter{}, mapWriteError(err)
	}
	if err := touchWorkTx(ctx, tx, owner, workID, now); err != nil {
		return Chapter{}, err
	}
	return row, tx.Commit()
}

func (s *MySQLStore) UpdateChapter(ctx context.Context, owner, workID, id string, input UpdateChapterInput, expected int64) (Chapter, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Chapter{}, err
	}
	defer tx.Rollback()
	row, err := getChapterTx(ctx, tx, owner, workID, id, true)
	if err != nil {
		return Chapter{}, err
	}
	if row.Revision != expected {
		return Chapter{}, ErrConflict
	}
	if input.Title != nil {
		row.Title = strings.TrimSpace(*input.Title)
		if row.Title == "" {
			return Chapter{}, ErrValidation
		}
	}
	if input.Content != nil {
		row.Content, row.WordCount = *input.Content, countWords(*input.Content)
	}
	row.UpdatedAt = s.now().UTC()
	result, err := tx.ExecContext(ctx, `UPDATE script_editor_chapters SET title=?,content=?,word_count=?,revision=revision+1,updated_at=? WHERE id=? AND work_id=? AND owner_username=? AND revision=?`, row.Title, row.Content, row.WordCount, row.UpdatedAt, id, workID, owner, expected)
	if err != nil {
		return Chapter{}, err
	}
	if err := requireAffected(result); err != nil {
		return Chapter{}, err
	}
	if err := touchWorkTx(ctx, tx, owner, workID, row.UpdatedAt); err != nil {
		return Chapter{}, err
	}
	row.Revision++
	return row, tx.Commit()
}

func (s *MySQLStore) DeleteChapter(ctx context.Context, owner, workID, id string, expected int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM script_editor_chapters WHERE id=? AND work_id=? AND owner_username=? AND revision=?`, id, workID, owner, expected)
	if err != nil {
		return err
	}
	if err := requireAffected(result); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE script_editor_works SET updated_at=? WHERE id=? AND owner_username=? AND deleted_at IS NULL`, s.now().UTC(), workID, owner)
	return err
}

func (s *MySQLStore) ReorderChapters(ctx context.Context, owner, workID string, ids []string) ([]Chapter, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockWork(ctx, tx, owner, workID); err != nil {
		return nil, err
	}
	rows, err := listChaptersTx(ctx, tx, owner, workID)
	if err != nil {
		return nil, err
	}
	byID := map[string]Chapter{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	if len(ids) != len(byID) {
		return nil, ErrValidation
	}
	now := s.now().UTC()
	for index, id := range ids {
		if _, ok := byID[id]; !ok {
			return nil, ErrValidation
		}
		if _, err := tx.ExecContext(ctx, `UPDATE script_editor_chapters SET sort_order=?,revision=revision+1,updated_at=? WHERE id=? AND work_id=? AND owner_username=?`, -(index + 1), now, id, workID, owner); err != nil {
			return nil, err
		}
	}
	out := make([]Chapter, 0, len(ids))
	for index, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE script_editor_chapters SET sort_order=? WHERE id=? AND work_id=? AND owner_username=?`, index+1, id, workID, owner); err != nil {
			return nil, err
		}
		row := byID[id]
		row.Order, row.Revision, row.UpdatedAt = index+1, row.Revision+1, now
		out = append(out, row)
	}
	if err := touchWorkTx(ctx, tx, owner, workID, now); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func (s *MySQLStore) CreateScript(ctx context.Context, owner, workID string, input CreateScriptInput) (Script, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Script{}, err
	}
	defer tx.Rollback()
	row, err := s.createScriptTx(ctx, tx, owner, workID, input)
	if err != nil {
		return Script{}, err
	}
	return row, tx.Commit()
}

func (s *MySQLStore) createScriptTx(ctx context.Context, tx *sql.Tx, owner, workID string, input CreateScriptInput) (Script, error) {
	if err := lockWork(ctx, tx, owner, workID); err != nil {
		return Script{}, err
	}
	if input.Episode <= 0 {
		return Script{}, ErrValidation
	}
	version := input.Version
	if version <= 0 {
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM script_editor_scripts WHERE owner_username=? AND work_id=? AND episode=?`, owner, workID, input.Episode).Scan(&version); err != nil {
			return Script{}, err
		}
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = fmt.Sprintf("第%d集", input.Episode)
	}
	source := input.GenerationSource
	if source == "" {
		source = SourceManual
	}
	chapters, _ := json.Marshal(input.SourceChapterIDs)
	now := s.now().UTC()
	row := Script{ID: s.newID("script"), WorkID: workID, OwnerUsername: owner, Episode: input.Episode, Version: version, Title: title, Content: input.Content, SourceChapterIDs: append([]string(nil), input.SourceChapterIDs...), GenerationSource: source, ModelID: input.ModelID, PromptID: input.PromptID, PromptRevision: input.PromptRevision, CreatedAt: now, UpdatedAt: now}
	_, err := tx.ExecContext(ctx, `INSERT INTO script_editor_scripts (id,work_id,owner_username,episode,version,title,content,source_chapter_ids,preferred,generation_source,model_id,prompt_id,prompt_revision,revision,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.ID, workID, owner, row.Episode, row.Version, row.Title, row.Content, string(chapters), false, string(row.GenerationSource), row.ModelID, row.PromptID, row.PromptRevision, row.Revision, now, now)
	if err != nil {
		return Script{}, mapWriteError(err)
	}
	if err := touchWorkTx(ctx, tx, owner, workID, now); err != nil {
		return Script{}, err
	}
	return row, nil
}

func (s *MySQLStore) ListScripts(ctx context.Context, owner, workID string) ([]Script, error) {
	if _, err := s.getWork(ctx, owner, workID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, scriptSelect+` WHERE owner_username=? AND work_id=? ORDER BY episode,version,created_at`, owner, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Script{}
	for rows.Next() {
		row, err := scanScript(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *MySQLStore) UpdateScript(ctx context.Context, owner, workID, id string, input UpdateScriptInput, expected int64) (Script, error) {
	row, err := s.getScript(ctx, owner, workID, id)
	if err != nil {
		return Script{}, err
	}
	if row.Revision != expected {
		return Script{}, ErrConflict
	}
	if input.Title != nil {
		row.Title = strings.TrimSpace(*input.Title)
		if row.Title == "" {
			return Script{}, ErrValidation
		}
	}
	if input.Content != nil {
		row.Content = *input.Content
	}
	row.UpdatedAt = s.now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE script_editor_scripts SET title=?,content=?,revision=revision+1,updated_at=? WHERE id=? AND work_id=? AND owner_username=? AND revision=?`, row.Title, row.Content, row.UpdatedAt, id, workID, owner, expected)
	if err != nil {
		return Script{}, err
	}
	if err := requireAffected(result); err != nil {
		return Script{}, err
	}
	row.Revision++
	_, err = s.db.ExecContext(ctx, `UPDATE script_editor_works SET updated_at=? WHERE id=? AND owner_username=? AND deleted_at IS NULL`, row.UpdatedAt, workID, owner)
	return row, err
}

func (s *MySQLStore) DeleteScript(ctx context.Context, owner, workID, id string, expected int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM script_editor_scripts WHERE id=? AND work_id=? AND owner_username=? AND revision=?`, id, workID, owner, expected)
	if err != nil {
		return err
	}
	if err := requireAffected(result); err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE script_editor_works SET updated_at=? WHERE id=? AND owner_username=? AND deleted_at IS NULL`, s.now().UTC(), workID, owner)
	return err
}

func (s *MySQLStore) CopyScript(ctx context.Context, owner, workID, id string) (Script, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Script{}, err
	}
	defer tx.Rollback()
	row, err := getScriptTx(ctx, tx, owner, workID, id, true)
	if err != nil {
		return Script{}, err
	}
	copy, err := s.createScriptTx(ctx, tx, owner, workID, CreateScriptInput{Episode: row.Episode, Title: row.Title, Content: row.Content, SourceChapterIDs: row.SourceChapterIDs, GenerationSource: SourceCopy, ModelID: row.ModelID, PromptID: row.PromptID, PromptRevision: row.PromptRevision})
	if err != nil {
		return Script{}, err
	}
	return copy, tx.Commit()
}

func (s *MySQLStore) PreferScript(ctx context.Context, owner, workID, id string, expected int64) (Script, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Script{}, err
	}
	defer tx.Rollback()
	row, err := getScriptTx(ctx, tx, owner, workID, id, true)
	if err != nil {
		return Script{}, err
	}
	if row.Revision != expected {
		return Script{}, ErrConflict
	}
	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE script_editor_scripts SET preferred=0,revision=revision+1,updated_at=? WHERE owner_username=? AND work_id=? AND episode=? AND preferred=1 AND id<>?`, now, owner, workID, row.Episode, id); err != nil {
		return Script{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE script_editor_scripts SET preferred=1,revision=revision+1,updated_at=? WHERE id=? AND work_id=? AND owner_username=? AND revision=?`, now, id, workID, owner, expected)
	if err != nil {
		return Script{}, err
	}
	if err := requireAffected(result); err != nil {
		return Script{}, err
	}
	if err := touchWorkTx(ctx, tx, owner, workID, now); err != nil {
		return Script{}, err
	}
	row.Preferred, row.Revision, row.UpdatedAt = true, row.Revision+1, now
	return row, tx.Commit()
}

func (s *MySQLStore) getWork(ctx context.Context, owner, id string) (Work, error) {
	row := s.db.QueryRowContext(ctx, workSelect+` WHERE id=? AND owner_username=? AND deleted_at IS NULL`, id, owner)
	return scanWork(row)
}

func (s *MySQLStore) listChapters(ctx context.Context, owner, workID string) ([]Chapter, error) {
	rows, err := s.db.QueryContext(ctx, chapterSelect+` WHERE owner_username=? AND work_id=? ORDER BY sort_order`, owner, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chapter{}
	for rows.Next() {
		row, err := scanChapter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *MySQLStore) getScript(ctx context.Context, owner, workID, id string) (Script, error) {
	return scanScript(s.db.QueryRowContext(ctx, scriptSelect+` WHERE id=? AND work_id=? AND owner_username=?`, id, workID, owner))
}

const workSelect = `SELECT id,owner_username,title,work_type,source_filename,mark_status,revision,created_at,updated_at,deleted_at FROM script_editor_works`
const chapterSelect = `SELECT id,work_id,owner_username,title,sort_order,content,word_count,revision,created_at,updated_at FROM script_editor_chapters`
const scriptSelect = `SELECT id,work_id,owner_username,episode,version,title,content,source_chapter_ids,preferred,generation_source,model_id,prompt_id,prompt_revision,revision,created_at,updated_at FROM script_editor_scripts`

type scanner interface{ Scan(...any) error }

func scanWork(row scanner) (Work, error) {
	var out Work
	var typ, mark string
	var deleted sql.NullTime
	err := row.Scan(&out.ID, &out.OwnerUsername, &out.Title, &typ, &out.SourceFilename, &mark, &out.Revision, &out.CreatedAt, &out.UpdatedAt, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return Work{}, ErrNotFound
	}
	if err != nil {
		return Work{}, err
	}
	out.Type, out.Mark = WorkType(typ), MarkStatus(mark)
	if deleted.Valid {
		out.DeletedAt = &deleted.Time
	}
	return out, nil
}

func scanWorkSummary(row scanner, out *WorkSummary) error {
	var typ, mark string
	var deleted sql.NullTime
	if err := row.Scan(&out.ID, &out.OwnerUsername, &out.Title, &typ, &out.SourceFilename, &mark, &out.Revision, &out.CreatedAt, &out.UpdatedAt, &deleted, &out.ChapterCount, &out.ScriptCount, &out.WordCount); err != nil {
		return err
	}
	out.Type, out.Mark = WorkType(typ), MarkStatus(mark)
	if deleted.Valid {
		out.DeletedAt = &deleted.Time
	}
	return nil
}

func scanChapter(row scanner) (Chapter, error) {
	var out Chapter
	err := row.Scan(&out.ID, &out.WorkID, &out.OwnerUsername, &out.Title, &out.Order, &out.Content, &out.WordCount, &out.Revision, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Chapter{}, ErrNotFound
	}
	return out, err
}

func scanScript(row scanner) (Script, error) {
	var out Script
	var chapters []byte
	var source string
	err := row.Scan(&out.ID, &out.WorkID, &out.OwnerUsername, &out.Episode, &out.Version, &out.Title, &out.Content, &chapters, &out.Preferred, &source, &out.ModelID, &out.PromptID, &out.PromptRevision, &out.Revision, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Script{}, ErrNotFound
	}
	if err != nil {
		return Script{}, err
	}
	out.GenerationSource = GenerationSource(source)
	if err := json.Unmarshal(chapters, &out.SourceChapterIDs); err != nil {
		return Script{}, err
	}
	return out, nil
}

func getChapterTx(ctx context.Context, tx *sql.Tx, owner, workID, id string, lock bool) (Chapter, error) {
	query := chapterSelect + ` WHERE id=? AND work_id=? AND owner_username=?`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanChapter(tx.QueryRowContext(ctx, query, id, workID, owner))
}

func listChaptersTx(ctx context.Context, tx *sql.Tx, owner, workID string) ([]Chapter, error) {
	rows, err := tx.QueryContext(ctx, chapterSelect+` WHERE owner_username=? AND work_id=? ORDER BY sort_order FOR UPDATE`, owner, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chapter{}
	for rows.Next() {
		row, err := scanChapter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func getScriptTx(ctx context.Context, tx *sql.Tx, owner, workID, id string, lock bool) (Script, error) {
	query := scriptSelect + ` WHERE id=? AND work_id=? AND owner_username=?`
	if lock {
		query += ` FOR UPDATE`
	}
	return scanScript(tx.QueryRowContext(ctx, query, id, workID, owner))
}

func lockWork(ctx context.Context, tx *sql.Tx, owner, workID string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM script_editor_works WHERE id=? AND owner_username=? AND deleted_at IS NULL FOR UPDATE`, workID, owner).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func touchWorkTx(ctx context.Context, tx *sql.Tx, owner, workID string, now time.Time) error {
	result, err := tx.ExecContext(ctx, `UPDATE script_editor_works SET updated_at=? WHERE id=? AND owner_username=? AND deleted_at IS NULL`, now, workID, owner)
	if err != nil {
		return err
	}
	return requireAffected(result)
}

func requireAffected(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrConflict
	}
	return nil
}

func mapWriteError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "duplicate") || strings.Contains(message, "unique") {
		return ErrConflict
	}
	return err
}

func (s *MySQLStore) GetHandoff(ctx context.Context, owner, workID, requestKey string) (Handoff, error) {
	var row Handoff
	var episodes []byte
	err := s.db.QueryRowContext(ctx, `SELECT id,owner_username,work_id,request_key,screenplay_project_id,episodes_json,created_at FROM script_editor_handoffs WHERE owner_username=? AND work_id=? AND request_key=?`, owner, workID, requestKey).
		Scan(&row.ID, &row.OwnerUsername, &row.WorkID, &row.RequestKey, &row.ScreenplayProjectID, &episodes, &row.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Handoff{}, ErrNotFound
	}
	row.Episodes = append(row.Episodes[:0], episodes...)
	return row, err
}

func (s *MySQLStore) CreateHandoff(ctx context.Context, row Handoff) (Handoff, error) {
	if row.ID == "" {
		row.ID = s.newID("handoff")
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = s.now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO script_editor_handoffs (id,owner_username,work_id,request_key,screenplay_project_id,episodes_json,created_at) VALUES (?,?,?,?,?,?,?)`, row.ID, row.OwnerUsername, row.WorkID, row.RequestKey, row.ScreenplayProjectID, string(row.Episodes), row.CreatedAt)
	return row, mapWriteError(err)
}

const jobSelect = `SELECT id,owner_username,work_id,job_type,status,request_key,input_json,result_json,progress_current,progress_total,error_code,error_message,lease_token,lease_until,revision,created_at,updated_at FROM script_editor_jobs`

func scanJob(row scanner) (Job, error) {
	var out Job
	var status string
	var input, result []byte
	var lease sql.NullTime
	err := row.Scan(&out.ID, &out.OwnerUsername, &out.WorkID, &out.Type, &status, &out.RequestKey, &input, &result, &out.ProgressCurrent, &out.ProgressTotal, &out.ErrorCode, &out.ErrorMessage, &out.LeaseToken, &lease, &out.Revision, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, err
	}
	out.Status, out.Input, out.Result = JobStatus(status), append(json.RawMessage(nil), input...), append(json.RawMessage(nil), result...)
	if lease.Valid {
		out.LeaseUntil = lease.Time
	}
	return out, nil
}

func (s *MySQLStore) CreateJob(ctx context.Context, row Job) (Job, error) {
	if _, err := s.getWork(ctx, row.OwnerUsername, row.WorkID); err != nil {
		return Job{}, err
	}
	if row.ID == "" {
		row.ID = s.newID("job")
	}
	now := s.now().UTC()
	row.CreatedAt, row.UpdatedAt = now, now
	_, err := s.db.ExecContext(ctx, `INSERT INTO script_editor_jobs (id,owner_username,work_id,job_type,status,request_key,input_json,result_json,progress_current,progress_total,error_code,error_message,lease_token,lease_until,revision,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.ID, row.OwnerUsername, row.WorkID, row.Type, string(row.Status), row.RequestKey, string(row.Input), string(row.Result), row.ProgressCurrent, row.ProgressTotal, row.ErrorCode, row.ErrorMessage, row.LeaseToken, nil, row.Revision, now, now)
	if errors.Is(mapWriteError(err), ErrConflict) {
		return scanJob(s.db.QueryRowContext(ctx, jobSelect+` WHERE owner_username=? AND request_key=?`, row.OwnerUsername, row.RequestKey))
	}
	return row, err
}

func (s *MySQLStore) GetJob(ctx context.Context, owner, workID, id string) (Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, jobSelect+` WHERE id=? AND owner_username=? AND work_id=?`, id, owner, workID))
}

func (s *MySQLStore) ListJobs(ctx context.Context, owner, workID string) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, jobSelect+` WHERE owner_username=? AND work_id=? ORDER BY created_at DESC`, owner, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		row, e := scanJob(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *MySQLStore) ListRunnableJobs(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 || limit > 128 {
		limit = 32
	}
	rows, err := s.db.QueryContext(ctx, jobSelect+` WHERE status='queued' OR (status='running' AND (lease_until IS NULL OR lease_until<=?)) ORDER BY updated_at LIMIT ?`, s.now().UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		row, e := scanJob(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *MySQLStore) ClaimJob(ctx context.Context, owner, id, token string, until time.Time) (Job, error) {
	now := s.now().UTC()
	result, err := s.db.ExecContext(ctx, `UPDATE script_editor_jobs SET status='running',lease_token=?,lease_until=?,revision=revision+1,updated_at=? WHERE id=? AND owner_username=? AND (status='queued' OR (status='running' AND (lease_until IS NULL OR lease_until<=?)))`, token, until, now, id, owner, now)
	if err != nil {
		return Job{}, err
	}
	if err := requireAffected(result); err != nil {
		return Job{}, err
	}
	return scanJob(s.db.QueryRowContext(ctx, jobSelect+` WHERE id=? AND owner_username=?`, id, owner))
}

func (s *MySQLStore) UpdateJob(ctx context.Context, row Job, expected int64) (Job, error) {
	now := s.now().UTC()
	var lease any
	if !row.LeaseUntil.IsZero() {
		lease = row.LeaseUntil
	}
	result, err := s.db.ExecContext(ctx, `UPDATE script_editor_jobs SET status=?,input_json=?,result_json=?,progress_current=?,progress_total=?,error_code=?,error_message=?,lease_token=?,lease_until=?,revision=revision+1,updated_at=? WHERE id=? AND owner_username=? AND work_id=? AND revision=?`, string(row.Status), string(row.Input), string(row.Result), row.ProgressCurrent, row.ProgressTotal, row.ErrorCode, row.ErrorMessage, row.LeaseToken, lease, now, row.ID, row.OwnerUsername, row.WorkID, expected)
	if err != nil {
		return Job{}, err
	}
	if err := requireAffected(result); err != nil {
		return Job{}, err
	}
	row.Revision, row.UpdatedAt = expected+1, now
	return row, nil
}

func (s *MySQLStore) CreateReview(ctx context.Context, row Review) (Review, error) {
	if row.ID == "" {
		row.ID = s.newID("review")
	}
	if row.CreatedAt.IsZero() {
		row.CreatedAt = s.now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO script_editor_reviews (id,work_id,script_id,owner_username,review_type,result_json,model_id,prompt_id,created_at) VALUES (?,?,?,?,?,?,?,?,?)`, row.ID, row.WorkID, row.ScriptID, row.OwnerUsername, row.Type, string(row.Result), row.ModelID, row.PromptID, row.CreatedAt)
	return row, mapWriteError(err)
}

func (s *MySQLStore) UpsertWorkBinding(ctx context.Context, row WorkBinding) (WorkBinding, error) {
	if err := row.Validate(); err != nil {
		return WorkBinding{}, err
	}
	if row.ID == "" {
		row.ID = s.newID("binding")
	}
	now := s.now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO script_editor_work_bindings (id,owner_username,work_id,target_type,target_id,source_revision,sync_state,conflict_state,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE target_id=VALUES(target_id),source_revision=VALUES(source_revision),sync_state=VALUES(sync_state),conflict_state=VALUES(conflict_state),updated_at=VALUES(updated_at)`, row.ID, row.OwnerUsername, row.WorkID, string(row.TargetType), row.TargetID, row.SourceRevision, string(row.SyncState), string(row.ConflictState), row.CreatedAt, row.UpdatedAt)
	return row, mapWriteError(err)
}

func scanWorkBinding(row scanner) (WorkBinding, error) {
	var out WorkBinding
	var targetType, syncState, conflictState string
	err := row.Scan(&out.ID, &out.OwnerUsername, &out.WorkID, &targetType, &out.TargetID, &out.SourceRevision, &syncState, &conflictState, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkBinding{}, ErrNotFound
	}
	out.TargetType, out.SyncState, out.ConflictState = BindingTargetType(targetType), SyncState(syncState), ConflictState(conflictState)
	return out, err
}

func (s *MySQLStore) WorkBinding(ctx context.Context, owner, workID string, targetType BindingTargetType) (WorkBinding, error) {
	return scanWorkBinding(s.db.QueryRowContext(ctx, `SELECT id,owner_username,work_id,target_type,target_id,source_revision,sync_state,conflict_state,created_at,updated_at FROM script_editor_work_bindings WHERE owner_username=? AND work_id=? AND target_type=?`, owner, workID, string(targetType)))
}

func (s *MySQLStore) UpsertSourceLink(ctx context.Context, row SourceLink) (SourceLink, error) {
	if row.ID == "" {
		row.ID = s.newID("source_link")
	}
	now := s.now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO script_editor_source_links (id,owner_username,work_id,source_type,source_id,target_type,target_id,source_revision,sync_state,conflict_state,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE source_revision=VALUES(source_revision),sync_state=VALUES(sync_state),conflict_state=VALUES(conflict_state),updated_at=VALUES(updated_at)`, row.ID, row.OwnerUsername, row.WorkID, row.SourceType, row.SourceID, row.TargetType, row.TargetID, row.SourceRevision, string(row.SyncState), string(row.ConflictState), row.CreatedAt, row.UpdatedAt)
	return row, mapWriteError(err)
}

func (s *MySQLStore) ListSourceLinks(ctx context.Context, owner, workID, targetType string) ([]SourceLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,owner_username,work_id,source_type,source_id,target_type,target_id,source_revision,sync_state,conflict_state,created_at,updated_at FROM script_editor_source_links WHERE owner_username=? AND work_id=? AND (?='' OR target_type=?) ORDER BY created_at`, owner, workID, targetType, targetType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SourceLink{}
	for rows.Next() {
		var row SourceLink
		var syncState, conflictState string
		if err := rows.Scan(&row.ID, &row.OwnerUsername, &row.WorkID, &row.SourceType, &row.SourceID, &row.TargetType, &row.TargetID, &row.SourceRevision, &syncState, &conflictState, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.SyncState, row.ConflictState = SyncState(syncState), ConflictState(conflictState)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *MySQLStore) UpsertAssetLink(ctx context.Context, row AssetLink) (AssetLink, error) {
	if row.ID == "" {
		row.ID = s.newID("asset_link")
	}
	now := s.now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO script_editor_asset_links (id,owner_username,work_id,asset_id,target_type,target_id,source_revision,sync_state,conflict_state,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE source_revision=VALUES(source_revision),sync_state=VALUES(sync_state),conflict_state=VALUES(conflict_state),updated_at=VALUES(updated_at)`, row.ID, row.OwnerUsername, row.WorkID, row.AssetID, row.TargetType, row.TargetID, row.SourceRevision, string(row.SyncState), string(row.ConflictState), row.CreatedAt, row.UpdatedAt)
	return row, mapWriteError(err)
}

func (s *MySQLStore) ListAssetLinks(ctx context.Context, owner, workID, targetType string) ([]AssetLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,owner_username,work_id,asset_id,target_type,target_id,source_revision,sync_state,conflict_state,created_at,updated_at FROM script_editor_asset_links WHERE owner_username=? AND work_id=? AND (?='' OR target_type=?) ORDER BY created_at`, owner, workID, targetType, targetType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssetLink{}
	for rows.Next() {
		var row AssetLink
		var syncState, conflictState string
		if err := rows.Scan(&row.ID, &row.OwnerUsername, &row.WorkID, &row.AssetID, &row.TargetType, &row.TargetID, &row.SourceRevision, &syncState, &conflictState, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.SyncState, row.ConflictState = SyncState(syncState), ConflictState(conflictState)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *MySQLStore) UpsertJobLink(ctx context.Context, row JobLink) (JobLink, error) {
	if row.ID == "" {
		row.ID = s.newID("job_link")
	}
	now := s.now().UTC()
	if row.CreatedAt.IsZero() {
		row.CreatedAt = now
	}
	row.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO script_editor_job_links (id,owner_username,work_id,job_id,target_type,target_id,source_revision,sync_state,conflict_state,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE source_revision=VALUES(source_revision),sync_state=VALUES(sync_state),conflict_state=VALUES(conflict_state),updated_at=VALUES(updated_at)`, row.ID, row.OwnerUsername, row.WorkID, row.JobID, row.TargetType, row.TargetID, row.SourceRevision, string(row.SyncState), string(row.ConflictState), row.CreatedAt, row.UpdatedAt)
	return row, mapWriteError(err)
}

func (s *MySQLStore) ListJobLinks(ctx context.Context, owner, workID, targetType string) ([]JobLink, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,owner_username,work_id,job_id,target_type,target_id,source_revision,sync_state,conflict_state,created_at,updated_at FROM script_editor_job_links WHERE owner_username=? AND work_id=? AND (?='' OR target_type=?) ORDER BY created_at`, owner, workID, targetType, targetType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JobLink{}
	for rows.Next() {
		var row JobLink
		var syncState, conflictState string
		if err := rows.Scan(&row.ID, &row.OwnerUsername, &row.WorkID, &row.JobID, &row.TargetType, &row.TargetID, &row.SourceRevision, &syncState, &conflictState, &row.CreatedAt, &row.UpdatedAt); err != nil {
			return nil, err
		}
		row.SyncState, row.ConflictState = SyncState(syncState), ConflictState(conflictState)
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *MySQLStore) GetStoryboard(ctx context.Context, owner, workID string, episode int) (StoryboardDocument, error) {
	var row StoryboardDocument
	var shots []byte
	err := s.db.QueryRowContext(ctx, `SELECT id,owner_username,work_id,script_id,episode,shots_json,revision,created_at,updated_at FROM script_editor_storyboards WHERE owner_username=? AND work_id=? AND episode=?`, owner, workID, episode).Scan(&row.ID, &row.OwnerUsername, &row.WorkID, &row.ScriptID, &row.Episode, &shots, &row.Revision, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return StoryboardDocument{}, ErrNotFound
	}
	if err != nil {
		return StoryboardDocument{}, err
	}
	row.Shots, err = decodeStoryboardJSON(shots)
	return row, err
}

func (s *MySQLStore) SaveStoryboard(ctx context.Context, row StoryboardDocument, expected int64) (StoryboardDocument, error) {
	shots, err := storyboardJSON(row.Shots)
	if err != nil {
		return StoryboardDocument{}, ErrValidation
	}
	now := s.now().UTC()
	if strings.TrimSpace(row.ID) == "" {
		if expected != 0 {
			return StoryboardDocument{}, ErrConflict
		}
		row.ID, row.Revision, row.CreatedAt, row.UpdatedAt = randomID("storyboard"), 0, now, now
		_, err = s.db.ExecContext(ctx, `INSERT INTO script_editor_storyboards (id,owner_username,work_id,script_id,episode,shots_json,revision,created_at,updated_at) VALUES (?,?,?,?,?,?,?,?,?)`, row.ID, row.OwnerUsername, row.WorkID, row.ScriptID, row.Episode, shots, row.Revision, row.CreatedAt, row.UpdatedAt)
		if err != nil {
			return StoryboardDocument{}, mapWriteError(err)
		}
		return row, nil
	}
	row.UpdatedAt, row.Revision = now, expected+1
	result, err := s.db.ExecContext(ctx, `UPDATE script_editor_storyboards SET script_id=?,shots_json=?,revision=revision+1,updated_at=? WHERE id=? AND owner_username=? AND work_id=? AND episode=? AND revision=?`, row.ScriptID, shots, now, row.ID, row.OwnerUsername, row.WorkID, row.Episode, expected)
	if err != nil {
		return StoryboardDocument{}, err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return StoryboardDocument{}, ErrConflict
	}
	return row, nil
}

func (s *MySQLStore) GetWorkSettings(ctx context.Context, owner, workID string) (WorkSettings, error) {
	var row WorkSettings
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT settings_json,revision,updated_at FROM script_editor_settings WHERE owner_username=? AND work_id=?`, owner, workID).Scan(&raw, &row.Revision, &row.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkSettings{}, ErrNotFound
	}
	if err != nil {
		return WorkSettings{}, err
	}
	decoded, err := decodeSettingsJSON(raw)
	decoded.Revision, decoded.UpdatedAt = row.Revision, row.UpdatedAt
	return decoded, err
}
func (s *MySQLStore) SaveWorkSettings(ctx context.Context, owner, workID string, row WorkSettings, expected int64) (WorkSettings, error) {
	raw, err := settingsJSON(row)
	if err != nil {
		return WorkSettings{}, ErrValidation
	}
	now := s.now().UTC()
	current, getErr := s.GetWorkSettings(ctx, owner, workID)
	if getErr == ErrNotFound {
		if expected != 0 {
			return WorkSettings{}, ErrConflict
		}
		row.Revision, row.UpdatedAt = 0, now
		_, err = s.db.ExecContext(ctx, `INSERT INTO script_editor_settings (owner_username,work_id,settings_json,revision,updated_at) VALUES (?,?,?,?,?)`, owner, workID, raw, 0, now)
		return row, mapWriteError(err)
	}
	if getErr != nil {
		return WorkSettings{}, getErr
	}
	if current.Revision != expected {
		return WorkSettings{}, ErrConflict
	}
	result, err := s.db.ExecContext(ctx, `UPDATE script_editor_settings SET settings_json=?,revision=revision+1,updated_at=? WHERE owner_username=? AND work_id=? AND revision=?`, raw, now, owner, workID, expected)
	if err != nil {
		return WorkSettings{}, err
	}
	count, _ := result.RowsAffected()
	if count != 1 {
		return WorkSettings{}, ErrConflict
	}
	row.Revision, row.UpdatedAt = expected+1, now
	return row, nil
}
