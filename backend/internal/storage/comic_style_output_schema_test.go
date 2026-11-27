package storage

import "testing"

func TestComicStyleMigrationRegisteredOnce(t *testing.T) {
	migrations := ComicStyleOutputMigrations()
	if len(migrations) != 1 || migrations[0].Version != 8800501 || migrations[0].Adopt == nil || migrations[0].Reconcile == nil {
		t.Fatal("missing resumable style migration")
	}
	count := 0
	for _, m := range AppMigrations() {
		if m.Version == 8800501 {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("registered %d times", count)
	}
}
