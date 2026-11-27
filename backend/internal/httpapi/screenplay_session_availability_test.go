package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"net/http"
	"net/http/httptest"
	"qiantie/backend/internal/screenplay"
	"testing"
)

type sessionCapabilityFixture struct {
	checks      int
	unavailable bool
}

func (g *sessionCapabilityFixture) Generate(context.Context, screenplay.TextRequest) (screenplay.TextResponse, error) {
	panic("availability cannot call model")
}
func (g *sessionCapabilityFixture) GenerateSessionStage(context.Context, screenplay.AssistantSessionRequest, string, screenplay.TextRequest) (screenplay.TextResponse, error) {
	panic("availability cannot call model")
}
func (g *sessionCapabilityFixture) checkSessionModel(context.Context, string) error {
	g.checks++
	if g.unavailable {
		return errors.New("invalid receipt binding")
	}
	return nil
}
func TestSessionAvailabilityRequiresServerChannelModelBindingAndGrant(t *testing.T) {
	ctx := context.Background()
	store := screenplay.NewMemoryStore()
	_, err := store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "owner", ModelID: "selected"})
	if err != nil {
		t.Fatal(err)
	}
	svc := screenplay.AuthoringService{Store: store, Repository: screenplay.NewMemoryAuthoringRepository()}
	fixture := &sessionCapabilityFixture{}
	if !sessionChatAvailable(ctx, svc, fixture, "owner", "p", "owner") {
		t.Fatal("installed valid server capability not projected")
	}
	fixture.unavailable = true
	if sessionChatAvailable(ctx, svc, fixture, "owner", "p", "owner") {
		t.Fatal("invalid storage binding exposed as available")
	}
	if sessionChatAvailable(ctx, svc, nil, "owner", "p", "owner") {
		t.Fatal("disabled channel exposed as available")
	}
	fixture.unavailable = false
	before := fixture.checks
	if sessionChatAvailable(ctx, svc, fixture, "owner", "p", "stranger") || fixture.checks != before {
		t.Fatal("missing generate grant accepted")
	}
}

func TestSessionManagementProjectsAvailabilityInListAndDetail(t *testing.T) {
	for _, ready := range []bool{false, true} {
		for _, detail := range []bool{false, true} {
			db, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			repo := screenplay.NewMySQLStore(db).AuthoringRepository().(*screenplay.MySQLAuthoringRepository)
			mux := http.NewServeMux()
			calls := 0
			registerAssistantSessionManagement(repo, func(pattern, right string, fn func(http.ResponseWriter, *http.Request, string, string)) {
				mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) { fn(w, r, "alice", "alice") })
			}, func(http.ResponseWriter, *http.Request, any) bool { return false }, func(*http.Request, string, string) bool { calls++; return ready })
			m.ExpectBegin()
			m.ExpectQuery("SELECT 1 FROM screenplay_projects").WithArgs("alice", "p").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
			m.ExpectQuery("SELECT body,revision FROM screenplay_authoring").WithArgs("alice", "p").WillReturnError(sql.ErrNoRows)
			m.ExpectQuery("SELECT session_id,COALESCE").WithArgs("alice", "p").WillReturnRows(sqlmock.NewRows([]string{"id", "parent", "creator", "visibility", "mode", "title", "revision", "archived", "request_status", "request_actor"}).AddRow("one", "default", "alice", "project", "readonly", "one", 1, false, "", "").AddRow("two", "default", "alice", "project", "confirm", "two", 1, false, "", ""))
			path := "/api/comic-screenplays/projects/p/assistant/sessions"
			if detail {
				path += "/one"
				m.ExpectQuery("SELECT body FROM screenplay_assistant_sessions").WithArgs("alice", "p", "one").WillReturnRows(sqlmock.NewRows([]string{"body"}).AddRow(`{}`))
			}
			m.ExpectCommit()
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
			if response.Code != 200 {
				t.Fatal(response.Code, response.Body.String())
			}
			var body struct {
				Session  screenplay.AssistantSession   `json:"session"`
				Sessions []screenplay.AssistantSession `json:"sessions"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if detail {
				if body.Session.ChatAvailable != ready {
					t.Fatal("detail", ready, response.Body.String())
				}
			} else {
				for _, item := range body.Sessions {
					want := ready && item.ID != "default"
					if item.ChatAvailable != want {
						t.Fatal("list", ready, response.Body.String())
					}
				}
			}
			if calls != 1 {
				t.Fatal("capability should be checked once per response", calls)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
			db.Close()
		}
	}
}
