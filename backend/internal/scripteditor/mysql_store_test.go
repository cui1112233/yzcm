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
