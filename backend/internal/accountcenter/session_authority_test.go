package accountcenter

import (
	"context"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func TestSessionAuthorityVersionTracksMembershipGrantAndTeamHistory(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewMySQLStore(db)
	versions := []string{}
	for _, tokens := range [][]string{{"audit:7", "grant:g1", "membership:10"}, {"audit:7", "grant:g1", "membership:11"}, {"audit:9", "grant:g1", "membership:11"}, {"audit:9", "grant:g2", "membership:11"}} {
		rows := sqlmock.NewRows([]string{"token"})
		for _, token := range tokens {
			rows.AddRow(token)
		}
		m.ExpectQuery("SELECT CONCAT.*membership:").WithArgs("actor", "subject", "actor", "subject", "actor", "subject", "team").WillReturnRows(rows)
		version, err := store.SessionAuthorityVersion(context.Background(), "actor", "subject", "team")
		if err != nil || version == "" {
			t.Fatal(version, err)
		}
		versions = append(versions, version)
	}
	for i := 1; i < len(versions); i++ {
		if versions[i] == versions[i-1] {
			t.Fatal("restored relationship reused frozen authority")
		}
	}
	m.ExpectQuery("SELECT CONCAT.*membership:").WillReturnError(errors.New("offline"))
	if _, err := store.SessionAuthorityVersion(context.Background(), "actor", "subject", "team"); err == nil {
		t.Fatal("authority read failure accepted")
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
