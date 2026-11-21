package batchfactoryv11

import (
	"context"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestLoadMergeJobRestoresAudioTimingMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now().UTC()
	query := regexp.QuoteMeta("SELECT id,batch_id,COALESCE(book_id,''),COALESCE(opening_variant_index,0),request_id,timing_mode,speed,COALESCE(tts_speed,0),COALESCE(audio_duration_seconds,0),COALESCE(audio_source,'original'),audio_artifact_id,provider_task_id,status,progress_phase,progress_current,progress_total,output_url,error_message,created_at,updated_at FROM batch_factory_v12_merge_jobs WHERE id=? AND owner_username=?")
	mock.ExpectQuery(query).WithArgs("merge-1", "alice").WillReturnRows(sqlmock.NewRows([]string{
		"id", "batch_id", "book_id", "opening_variant_index", "request_id", "timing_mode", "speed", "tts_speed", "audio_duration_seconds", "audio_source", "audio_artifact_id", "provider_task_id", "status", "progress_phase", "progress_current", "progress_total", "output_url", "error_message", "created_at", "updated_at",
	}).AddRow("merge-1", "batch-1", "book-1", 0, "request-1", "audio", 1.036552, 0, 14.66, "tts", "tts-1", "provider-1", "succeeded", "completed", 2, 2, "/merge.mp4", "", now, now))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT video_id,ordinal,media_url,target_duration_seconds,requested_duration_seconds,actual_duration_seconds FROM batch_factory_v12_merge_sources WHERE job_id=? AND owner_username=? ORDER BY ordinal,video_id")).WithArgs("merge-1", "alice").WillReturnRows(sqlmock.NewRows([]string{"video_id", "ordinal", "media_url", "target_duration_seconds", "requested_duration_seconds", "actual_duration_seconds"}))

	job, err := loadMergeJob(context.Background(), db, "alice", "merge-1")
	if err != nil {
		t.Fatal(err)
	}
	field := reflect.ValueOf(job).FieldByName("AudioDurationSeconds")
	if !field.IsValid() || field.Float() != 14.66 {
		t.Fatalf("audio duration was not restored: %+v", job)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMergeJobAcceptsLegacyNullAudioTimingMetadata(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now().UTC()
	query := regexp.QuoteMeta("SELECT id,batch_id,COALESCE(book_id,''),COALESCE(opening_variant_index,0),request_id,timing_mode,speed,COALESCE(tts_speed,0),COALESCE(audio_duration_seconds,0),COALESCE(audio_source,'original'),audio_artifact_id,provider_task_id,status,progress_phase,progress_current,progress_total,output_url,error_message,created_at,updated_at FROM batch_factory_v12_merge_jobs WHERE id=? AND owner_username=?")
	mock.ExpectQuery(query).WithArgs("merge-legacy", "alice").WillReturnRows(sqlmock.NewRows([]string{
		"id", "batch_id", "book_id", "opening_variant_index", "request_id", "timing_mode", "speed", "tts_speed", "audio_duration_seconds", "audio_source", "audio_artifact_id", "provider_task_id", "status", "progress_phase", "progress_current", "progress_total", "output_url", "error_message", "created_at", "updated_at",
	}).AddRow("merge-legacy", "batch-1", "book-1", 0, "request-legacy", "audio", 1.036552, 0, 0, "tts", nil, "provider-legacy", "succeeded", "completed", 2, 2, "/legacy.mp4", nil, now, now))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT video_id,ordinal,media_url,target_duration_seconds,requested_duration_seconds,actual_duration_seconds FROM batch_factory_v12_merge_sources WHERE job_id=? AND owner_username=? ORDER BY ordinal,video_id")).WithArgs("merge-legacy", "alice").WillReturnRows(sqlmock.NewRows([]string{"video_id", "ordinal", "media_url", "target_duration_seconds", "requested_duration_seconds", "actual_duration_seconds"}))

	job, err := loadMergeJob(context.Background(), db, "alice", "merge-legacy")
	if err != nil {
		t.Fatal(err)
	}
	if job.TTSSpeed != 0 || job.AudioDurationSeconds != 0 {
		t.Fatalf("legacy null timing metadata must default to zero: %+v", job)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
