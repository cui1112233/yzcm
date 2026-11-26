package batchfactoryv11

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

func (s *MySQLStore) CreateMultiModeBatches(ctx context.Context, owner string, input CreateMultiModeInput) (MultiModeCreationResult, error) {
	input, digest, err := normalizeMultiModeInput(input)
	if err != nil {
		return MultiModeCreationResult{}, err
	}
	if strings.TrimSpace(owner) == "" {
		return MultiModeCreationResult{}, ErrInvalid
	}
	groupID, err := newID("group")
	if err != nil {
		return MultiModeCreationResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MultiModeCreationResult{}, err
	}
	defer tx.Rollback()
	// ON DUPLICATE acquires the existing unique request row as well, so racing
	// retries cannot create two groups or partially duplicate child projects.
	_, err = tx.ExecContext(ctx, `INSERT INTO batch_factory_v12_creation_groups(id,owner_username,request_id,input_digest,created_at) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE id=id`, groupID, owner, input.RequestID, digest, time.Now().UTC())
	if err != nil {
		return MultiModeCreationResult{}, err
	}
	var storedID, storedDigest string
	if err = tx.QueryRowContext(ctx, `SELECT id,input_digest FROM batch_factory_v12_creation_groups WHERE owner_username=? AND request_id=? FOR UPDATE`, owner, input.RequestID).Scan(&storedID, &storedDigest); err != nil {
		return MultiModeCreationResult{}, err
	}
	if storedDigest != digest {
		return MultiModeCreationResult{}, ErrConflict
	}
	if storedID != groupID {
		if err = tx.Commit(); err != nil {
			return MultiModeCreationResult{}, err
		}
		return s.GetMultiModeCreation(ctx, owner, storedID)
	}
	for _, job := range newMultiModeSources(input, groupID) {
		raw, _ := json.Marshal(job.Input)
		if _, err = tx.ExecContext(ctx, `INSERT INTO batch_factory_v12_creation_sources(group_id,source_key,input_json) VALUES(?,?,?)`, groupID, job.Key, string(raw)); err != nil {
			return MultiModeCreationResult{}, err
		}
	}
	for i, project := range input.Projects {
		batch, createErr := createBatchTx(ctx, tx, owner, multiModeBatchInput(input, project, groupID), "")
		if createErr != nil {
			return MultiModeCreationResult{}, createErr
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO batch_factory_v12_creation_projects(group_id,owner_username,mode,batch_id,preset_id,execution_mode,ordinal) VALUES(?,?,?,?,?,?,?)`, groupID, owner, string(project.Mode), batch.ID, project.PresetID, project.ExecutionMode, i); err != nil {
			return MultiModeCreationResult{}, err
		}
		for j, book := range batch.Books {
			if _, err = tx.ExecContext(ctx, `INSERT INTO batch_factory_v12_creation_targets(group_id,source_key,batch_id,book_id,expected_revision) VALUES(?,?,?,?,?)`, groupID, multiModeSourceKey(input.Books[j]), batch.ID, book.ID, book.Revision); err != nil {
				return MultiModeCreationResult{}, err
			}
		}
	}
	if err = tx.Commit(); err != nil {
		return MultiModeCreationResult{}, err
	}
	return s.GetMultiModeCreation(ctx, owner, groupID)
}
func (s *MySQLStore) GetMultiModeCreation(ctx context.Context, o, g string) (MultiModeCreationResult, error) {
	var one int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM batch_factory_v12_creation_groups WHERE id=? AND owner_username=?`, g, o).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return MultiModeCreationResult{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT mode,batch_id,preset_id,execution_mode FROM batch_factory_v12_creation_projects WHERE group_id=? AND owner_username=? ORDER BY ordinal`, g, o)
	if err != nil {
		return MultiModeCreationResult{}, err
	}
	result := MultiModeCreationResult{GroupID: g, Batches: []Batch{}, Projects: []MultiModeProject{}}
	for rows.Next() {
		var p MultiModeProject
		if err = rows.Scan(&p.Mode, &p.BatchID, &p.PresetID, &p.ExecutionMode); err != nil {
			rows.Close()
			return result, err
		}
		result.Projects = append(result.Projects, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	for _, p := range result.Projects {
		batch, readErr := s.GetBatch(ctx, o, p.BatchID)
		if readErr != nil {
			return result, readErr
		}
		result.Batches = append(result.Batches, batch)
	}
	return result, nil
}
func (s *MySQLStore) ListMultiModeSources(ctx context.Context, o, g string) ([]MultiModeSourceJob, error) {
	if _, err := s.GetMultiModeCreation(ctx, o, g); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source_key,input_json,COALESCE(snapshot_json,JSON_OBJECT()),status,COALESCE(last_error,'') FROM batch_factory_v12_creation_sources WHERE group_id=? ORDER BY source_key`, g)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MultiModeSourceJob
	for rows.Next() {
		job := MultiModeSourceJob{GroupID: g}
		var raw, snapshot []byte
		if err = rows.Scan(&job.Key, &raw, &snapshot, &job.Status, &job.Error); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &job.Input); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(snapshot, &job.Snapshot); err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}
func (s *MySQLStore) ClaimMultiModeSource(ctx context.Context, o, g, k string) (MultiModeSourceJob, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return MultiModeSourceJob{}, false, err
	}
	defer tx.Rollback()
	job := MultiModeSourceJob{GroupID: g, Key: k}
	var raw []byte
	var lease sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT s.input_json,s.status,s.lease_until FROM batch_factory_v12_creation_sources s JOIN batch_factory_v12_creation_groups g ON g.id=s.group_id WHERE s.group_id=? AND s.source_key=? AND g.owner_username=? FOR UPDATE`, g, k, o).Scan(&raw, &job.Status, &lease)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err != nil {
		return job, false, err
	}
	if err = json.Unmarshal(raw, &job.Input); err != nil {
		return job, false, err
	}
	if job.Status == "ready" || job.Status == "fetching" && lease.Valid && time.Now().Before(lease.Time) {
		return job, false, nil
	}
	token, err := newID("source")
	if err != nil {
		return job, false, err
	}
	job.Token, job.Status, job.LeaseUntil = token, "fetching", time.Now().UTC().Add(10*time.Minute)
	if _, err = tx.ExecContext(ctx, `UPDATE batch_factory_v12_creation_sources SET status='fetching',lease_token=?,lease_until=?,last_error=NULL WHERE group_id=? AND source_key=?`, token, job.LeaseUntil, g, k); err != nil {
		return job, false, err
	}
	return job, true, tx.Commit()
}
func (s *MySQLStore) CompleteMultiModeSource(ctx context.Context, o, g, k, token string, snapshot MultiModeSourceSnapshot, workErr error) error {
	status, message := "ready", ""
	if workErr != nil {
		status, message = "failed", workErr.Error()
		if errors.Is(workErr, ErrMultiModeSourcePending) {
			status = "pending"
		}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE batch_factory_v12_creation_sources s JOIN batch_factory_v12_creation_groups g ON g.id=s.group_id SET s.status=?,s.snapshot_json=?,s.last_error=?,s.lease_until=NULL WHERE s.group_id=? AND s.source_key=? AND s.lease_token=? AND s.status='fetching' AND g.owner_username=?`, status, string(raw), message, g, k, token, o)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return ErrConflict
	}
	return err
}
func (s *MySQLStore) DeliverMultiModeSources(ctx context.Context, o, g string) error {
	if _, err := s.GetMultiModeCreation(ctx, o, g); err != nil {
		return err
	}
	// Each target is committed atomically with its source write. A process may
	// stop between targets; delivered targets are never written twice.
	rows, err := s.db.QueryContext(ctx, `SELECT t.batch_id,t.book_id,t.source_key,t.expected_revision,s.snapshot_json FROM batch_factory_v12_creation_targets t JOIN batch_factory_v12_creation_sources s ON s.group_id=t.group_id AND s.source_key=t.source_key WHERE t.group_id=? AND t.status='pending' AND s.status='ready' ORDER BY t.batch_id,t.book_id`, g)
	if err != nil {
		return err
	}
	type target struct {
		b, k, key string
		revision  int64
		raw       []byte
	}
	var targets []target
	for rows.Next() {
		var t target
		if err = rows.Scan(&t.b, &t.k, &t.key, &t.revision, &t.raw); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, t := range targets {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = s.deliverMultiModeTarget(ctx, o, g, t.b, t.k, t.key, t.revision, t.raw); err != nil {
			return err
		}
	}
	return nil
}
func (s *MySQLStore) deliverMultiModeTarget(ctx context.Context, o, g, b, k, key string, expected int64, raw []byte) error {
	var snapshot MultiModeSourceSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM batch_factory_v12_creation_targets WHERE group_id=? AND batch_id=? AND book_id=? FOR UPDATE`, g, b, k).Scan(&state); err != nil {
		return err
	}
	if state != "pending" {
		return nil
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT revision FROM batch_factory_v12_books WHERE id=? AND batch_id=? AND owner_username=? FOR UPDATE`, k, b, o).Scan(&revision); err != nil {
		return err
	}
	state = "skipped_manual"
	{
		var currentSource, currentRaw sql.NullString
		var currentMetadata []byte
		if err = tx.QueryRowContext(ctx, `SELECT source_text,txt_text,source_metadata_json FROM batch_factory_v12_book_records WHERE book_id=? FOR UPDATE`, k).Scan(&currentSource, &currentRaw, &currentMetadata); err != nil {
			return err
		}
		manual := revision != expected
		metadata := multiModeSourceMetadata(snapshot.Metadata)
		if manual {
			for key, value := range decodeSourceMetadata(currentMetadata) {
				if value != "" {
					metadata[key] = value
				}
			}
		}
		preserveMultiModeControls(metadata, decodeSourceMetadata(currentMetadata))
		metadata["multimodeGroupId"], metadata["multimodeSourceKey"], metadata["multiModeSourceState"], metadata["rawSourceStatus"] = g, key, "delivered", "verified"
		metadata["contentPending"] = false
		encoded, _ := json.Marshal(metadata)
		title, source, raw := snapshot.Title, snapshot.OriginalRaw, snapshot.OriginalRaw
		if manual {
			title = ""
			if strings.TrimSpace(currentSource.String) != "" || strings.TrimSpace(currentRaw.String) != "" {
				source, raw = currentSource.String, currentRaw.String
			}
		}
		if _, err = tx.ExecContext(ctx, `UPDATE batch_factory_v12_book_records SET title=IF(?='',title,?),source_text=?,txt_text=?,source_metadata_json=? WHERE book_id=?`, title, title, source, raw, string(encoded), k); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE batch_factory_v12_books SET revision=revision+1,updated_at=? WHERE id=? AND owner_username=?`, time.Now().UTC(), k, o); err != nil {
			return err
		}
		state = "delivered"
	}
	if _, err = tx.ExecContext(ctx, `UPDATE batch_factory_v12_creation_targets SET status=? WHERE group_id=? AND batch_id=? AND book_id=?`, state, g, b, k); err != nil {
		return err
	}
	return tx.Commit()
}
