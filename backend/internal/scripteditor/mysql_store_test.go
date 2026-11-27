package scripteditor

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMySQLStoreCreateWorkWritesOwnerScopedRow(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewMySQLStore(db)
	now := time.Date(2026, 11, 27, 9, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	s.newID = func(string) string { return "work-1" }
	mock.ExpectExec("INSERT INTO script_editor_works").
		WithArgs("work-1", "alice", "测试作品", string(WorkNovel), "book.txt", string(MarkNone), int64(0), now, now).
		WillReturnResult(sqlmock.NewResult(1, 1))
	got, err := s.CreateWork(context.Background(), "alice", CreateWorkInput{Title: " 测试作品 ", Type: WorkNovel, SourceFilename: "book.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "work-1" || got.Title != "测试作品" || got.OwnerUsername != "alice" {
		t.Fatalf("work = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLStoreCreateScriptWritesJSONAsText(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewMySQLStore(db)
	now := time.Date(2026, 11, 27, 9, 30, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	s.newID = func(string) string { return "script-1" }

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM script_editor_works").
		WithArgs("work-1", "alice").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("work-1"))
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(version\\),0\\)\\+1 FROM script_editor_scripts").
		WithArgs("alice", "work-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(1))
	mock.ExpectExec("INSERT INTO script_editor_scripts").
		WithArgs("script-1", "work-1", "alice", 1, 1, "第1集", "正文", "null", false, string(SourceManual), "", "", int64(0), int64(0), now, now).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE script_editor_works SET updated_at").
		WithArgs(now, "work-1", "alice").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := s.CreateScript(context.Background(), "alice", "work-1", CreateScriptInput{Episode: 1, Content: "正文"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "script-1" || got.Version != 1 {
		t.Fatalf("script = %#v", got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestMySQLStoreUpsertWorkBinding(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewMySQLStore(db)
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	s.newID = func(string) string { return "binding-1" }
	mock.ExpectExec("INSERT INTO script_editor_work_bindings").
		WithArgs("binding-1", "alice", "work-1", string(BindingCMCanvas), "task-1", int64(2), string(SyncPending), string(ConflictNone), now, now).
		WillReturnResult(sqlmock.NewResult(1, 1))
	row, err := s.UpsertWorkBinding(context.Background(), WorkBinding{OwnerUsername: "alice", WorkID: "work-1", TargetType: BindingCMCanvas, TargetID: "task-1", SourceRevision: 2, SyncState: SyncPending, ConflictState: ConflictNone})
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != "binding-1" {
		t.Fatalf("binding = %#v", row)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
