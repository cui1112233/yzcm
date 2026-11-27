package httpapi

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"qiantie/backend/internal/screenplay"
)

func TestAssistantSessionHTTPSignedListFiltersPrivateHistory(t *testing.T) {
	db, m, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now()
	store := screenplay.NewMemoryStore()
	_, err = store.CreateProject(context.Background(), screenplay.Project{ID: "p", OwnerUsername: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	repo := screenplay.NewMySQLStore(db).AuthoringRepository()
	mux := http.NewServeMux()
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, store, repo, nil)
	m.ExpectBegin()
	m.ExpectQuery("SELECT 1 FROM screenplay_projects").WithArgs("alice", "p").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
	m.ExpectQuery("SELECT body,revision FROM screenplay_authoring").WithArgs("alice", "p").WillReturnError(sql.ErrNoRows)
	m.ExpectQuery("SELECT session_id,COALESCE").WithArgs("alice", "p").WillReturnRows(sqlmock.NewRows([]string{"id", "parent", "creator", "visibility", "mode", "title", "revision", "archived", "request_status", "request_actor"}).
		AddRow("hidden", "", "bob", "private", "confirm", "privatetitle", 1, false, "", "").
		AddRow("child", "hidden", "alice", "project", "confirm", "childtitle", 1, false, "", "").
		AddRow("visible", "default", "alice", "private", "confirm", "mytitle", 1, false, "", ""))
	m.ExpectCommit()
	res := signedJSONRequest(t, mux, now, "alice", "GET", "/api/comic-screenplays/projects/p/assistant/sessions", nil)
	body := res.Body.String()
	if res.Code != 200 || strings.Contains(body, "privatetitle") || strings.Contains(body, "childtitle") || !strings.Contains(body, "mytitle") || !strings.Contains(body, "default") {
		t.Fatal(res.Code, body)
	}
	if err = m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
