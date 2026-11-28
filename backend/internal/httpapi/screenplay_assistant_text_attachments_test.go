package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/screenplay"
	"strings"
	"testing"
	"time"
)

func attachmentMultipart(t *testing.T, fields [][2]string, files int, size int) *httptest.ResponseRecorder {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, f := range fields {
		w.WriteField(f[0], f[1])
	}
	for i := 0; i < files; i++ {
		part, _ := w.CreateFormFile("file", "x.md")
		part.Write([]byte(strings.Repeat("x", size)))
	}
	w.Close()
	r := httptest.NewRequest("POST", "/", &b)
	r.Header.Set("Content-Type", w.FormDataContentType())
	out := httptest.NewRecorder()
	_, e := readAssistantAttachmentMultipart(out, r, false)
	if e != nil {
		out.WriteHeader(400)
	} else {
		out.WriteHeader(204)
	}
	return out
}
func TestAssistantAttachmentMultipartStrictBounds(t *testing.T) {
	fields := [][2]string{{"session_id", "default"}, {"request_key", "stable-request-key"}}
	if attachmentMultipart(t, fields, 1, 64<<10).Code != 204 {
		t.Fatal("raw max refused")
	}
	for _, v := range []struct {
		fields   [][2]string
		files, n int
	}{{fields, 1, (64 << 10) + 1}, {fields, 2, 1}, {append(append([][2]string{}, fields...), [2]string{"owner", "forged"}), 1, 1}, {append(append([][2]string{}, fields...), [2]string{"session_id", "other"}), 1, 1}, {fields, 0, 0}} {
		if attachmentMultipart(t, v.fields, v.files, v.n).Code != 400 {
			t.Fatal("invalid multipart accepted")
		}
	}
}
func TestAssistantAttachmentDTOUnreadyAndPrivateFields(t *testing.T) {
	a := screenplay.AssistantTextAttachment{ID: "id", Filename: "x.md", Status: "preparing", ObjectKey: "PRIVATE_OBJECT_KEY", RequestKeyHash: "PRIVATE_HASH", Text: "PRIVATE_TEXT", StateRevision: 2}
	entry := attachmentEntryDTO(screenplay.AssistantAttachmentPreparationEntry{Attachment: a}, false, time.Now())
	b, _ := json.Marshal(entry)
	if strings.Contains(string(b), "PRIVATE_") || entry.CanReference || entry.Selector != nil || entry.ID != "id" {
		t.Fatal(string(b))
	}
}

type attachmentRouteStore struct {
	*screenplay.MemoryStore
	authoring   screenplay.AuthoringRepository
	attachments *screenplay.MySQLAssistantTextAttachments
}

func (s attachmentRouteStore) AuthoringRepository() screenplay.AuthoringRepository {
	return s.authoring
}
func (s attachmentRouteStore) AssistantTextAttachments() *screenplay.MySQLAssistantTextAttachments {
	return s.attachments
}
func attachmentWeakSQLScope(m sqlmock.Sqlmock) {
	m.ExpectBegin()
	m.ExpectQuery("SELECT 1 FROM screenplay_projects").WithArgs("owner", "p").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
	m.ExpectQuery("SELECT body,revision FROM screenplay_authoring").WithArgs("owner", "p").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(`{}`, 1))
	m.ExpectQuery("SELECT session_id,COALESCE").WithArgs("owner", "p").WillReturnRows(sqlmock.NewRows([]string{"id", "parent", "creator", "visibility", "mode", "title", "rev", "archived", "status", "actor"}))
}
func attachmentHTTPFixture(t *testing.T, objects ...screenplay.AssistantTextAttachmentObjects) (*http.ServeMux, sqlmock.Sqlmock, *sql.DB, time.Time, screenplay.AssistantTextAttachment) {
	t.Helper()
	db, m, _ := sqlmock.New()
	now := time.Now()
	s := screenplay.NewMemoryStore()
	s.CreateProject(context.Background(), screenplay.Project{ID: "p", OwnerUsername: "owner"})
	store := attachmentRouteStore{MemoryStore: s, authoring: screenplay.NewMemoryAuthoringRepository(), attachments: screenplay.NewMySQLStore(db).AssistantTextAttachments()}
	mux := http.NewServeMux()
	center := &impersonationTestStore{identities: map[string]accountcenter.Identity{"owner": {Username: "owner", Active: true}}}
	authn := BridgeAuth{Secret: "secret", Now: func() time.Time { return now }, AccountCenterStore: center}
	var object screenplay.AssistantTextAttachmentObjects
	if len(objects) > 0 {
		object = objects[0]
	}
	RegisterScreenplayAssistantTextAttachments(mux, authn, store, object, "not_configured")
	RegisterScreenplayAuthoringRoutes(mux, authn, store, store.authoring, nil)
	scope, _ := screenplay.NewAssistantReferenceScope("owner", "p", "default", "owner", "owner")
	a, _ := screenplay.PrepareAssistantTextAttachment(scope, "id", "x.md", strings.Repeat("a", 64), []byte("ready shared text"))
	a.ObjectKey = "PRIVATE_OBJECT_KEY"
	a.RequestKeyHash = "PRIVATE_REQUEST_HASH"
	a.Status = "ready"
	a.Verified = true
	return mux, m, db, now, a
}
func attachmentHTTPRows(a screenplay.AssistantTextAttachment, deadline bool) *sqlmock.Rows {
	b, _ := json.Marshal(a)
	names := []string{"body", "revision", "status", "verified", "published", "token"}
	if deadline {
		names = append(names, "deadline")
	}
	rows := sqlmock.NewRows(names)
	if deadline {
		return rows.AddRow(string(b), a.StateRevision, a.Status, a.Verified, a.Published, "PRIVATE_WORK_TOKEN", nil)
	}
	return rows.AddRow(string(b), a.StateRevision, a.Status, a.Verified, a.Published, "PRIVATE_WORK_TOKEN")
}
func TestAssistantAttachmentHTTPWeakProfilePublishedAndDraft(t *testing.T) {
	mux, m, db, now, a := attachmentHTTPFixture(t)
	defer db.Close()
	base := "/api/comic-screenplays/projects/p/assistant/attachments"
	for _, published := range []bool{true, false} {
		a.Published = published
		attachmentWeakSQLScope(m)
		m.ExpectQuery("SELECT body,state_revision").WithArgs("owner", "p", a.ID).WillReturnRows(attachmentHTTPRows(a, false))
		if published {
			m.ExpectCommit()
		} else {
			m.ExpectRollback()
		}
		res := signedJSONRequest(t, mux, now, "owner", "GET", base+"/id?session_id=default", nil)
		want := 200
		if !published {
			want = 404
		}
		if res.Code != want || strings.Contains(res.Body.String(), "PRIVATE_") {
			t.Fatal(res.Code, res.Body.String())
		}
	}
	attachmentWeakSQLScope(m)
	m.ExpectCommit()
	res := signedJSONRequest(t, mux, now, "owner", "GET", base+"/availability?session_id=default", nil)
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"upload_available":false`) {
		t.Fatal(res.Code, res.Body.String())
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestAssistantAttachmentHTTPIntentPageBound(t *testing.T) {
	mux, m, db, now, a := attachmentHTTPFixture(t)
	defer db.Close()
	a.Published = true
	attachmentWeakSQLScope(m)
	rows := attachmentHTTPRows(a, true)
	for i := 1; i < 101; i++ {
		a.ID = fmt.Sprintf("attachment%d", i)
		b, _ := json.Marshal(a)
		rows.AddRow(string(b), a.StateRevision, a.Status, true, true, "PRIVATE_WORK_TOKEN", nil)
	}
	m.ExpectQuery("SELECT body,state_revision").WithArgs("owner", "p", "default", "owner", "", 101, 0).WillReturnRows(rows)
	m.ExpectCommit()
	res := signedJSONRequest(t, mux, now, "owner", "GET", "/api/comic-screenplays/projects/p/assistant/attachments?session_id=default&limit=100", nil)
	var page struct {
		Attachments []assistantAttachmentEntry `json:"attachments"`
		More        bool                       `json:"has_more"`
	}
	if json.Unmarshal(res.Body.Bytes(), &page) != nil || res.Code != 200 || len(page.Attachments) != 100 || !page.More || strings.Contains(res.Body.String(), "PRIVATE_") {
		t.Fatal(res.Code, res.Body.String())
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

func TestAssistantAttachmentHTTPScopedResolve(t *testing.T) {
	mux, m, db, now, a := attachmentHTTPFixture(t)
	defer db.Close()
	a.Published = true
	snapshot, _ := a.Snapshot()
	attachmentWeakSQLScope(m)
	m.ExpectQuery("SELECT body,state_revision").WithArgs("owner", "p", a.ID).WillReturnRows(attachmentHTTPRows(a, false))
	m.ExpectCommit()
	res := signedJSONRequest(t, mux, now, "owner", "POST", "/api/comic-screenplays/projects/p/assistant/references/resolve", map[string]any{"session_id": "default", "references": []screenplay.AssistantReferenceSelector{snapshot.Selector()}})
	if res.Code != 200 || !strings.Contains(res.Body.String(), a.Text) || strings.Contains(res.Body.String(), "PRIVATE_") {
		t.Fatal(res.Code, res.Body.String())
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

type attachmentDownloadObjects struct{}

func (attachmentDownloadObjects) NamespaceID() string                { return strings.Repeat("a", 64) }
func (attachmentDownloadObjects) CheckPrivacy(context.Context) error { return nil }
func (attachmentDownloadObjects) PutVerified(context.Context, screenplay.AssistantTextAttachment, []byte) error {
	return errors.New("unexpected Put")
}
func (attachmentDownloadObjects) OpenVerified(context.Context, screenplay.AssistantTextAttachment) ([]byte, error) {
	return []byte("ready shared text"), nil
}
func TestAssistantAttachmentHTTPDownloadChecksAfterNetwork(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		mux, m, db, now, a := attachmentHTTPFixture(t, attachmentDownloadObjects{})
		a.Published = true
		attachmentWeakSQLScope(m)
		m.ExpectQuery("SELECT body,state_revision").WillReturnRows(attachmentHTTPRows(a, false))
		m.ExpectCommit()
		if !revoked {
			attachmentWeakSQLScope(m)
			m.ExpectQuery("SELECT body,state_revision").WillReturnRows(attachmentHTTPRows(a, false))
			m.ExpectCommit()
		} else {
			m.ExpectBegin()
			m.ExpectQuery("SELECT 1 FROM screenplay_projects").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
			m.ExpectQuery("SELECT body,revision FROM screenplay_authoring").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(`{"archived":true}`, 2))
			m.ExpectQuery("SELECT session_id,COALESCE").WillReturnRows(sqlmock.NewRows([]string{"id", "parent", "creator", "visibility", "mode", "title", "rev", "archived", "status", "actor"}))
			m.ExpectRollback()
		}
		res := signedJSONRequest(t, mux, now, "owner", "GET", "/api/comic-screenplays/projects/p/assistant/attachments/id/content?session_id=default", nil)
		if revoked {
			if res.Code != 403 || strings.Contains(res.Body.String(), "ready shared text") || res.Header().Get("Content-Disposition") != "" {
				t.Fatal("revoked original released", res.Code, res.Body.String())
			}
		} else if res.Code != 200 || res.Body.String() != "ready shared text" || res.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.HasPrefix(res.Header().Get("Content-Disposition"), "attachment;") || res.Header().Get("Content-Type") != "application/octet-stream" {
			t.Fatal("unsafe download", res.Code, res.Header())
		}
		if e := m.ExpectationsWereMet(); e != nil {
			t.Fatal(e)
		}
		db.Close()
	}
}
func TestAssistantAttachmentHTTPErrorWhitelist(t *testing.T) {
	w := httptest.NewRecorder()
	attachmentHTTPError(w, errors.New("PRIVATE_SQL_OR_SDK_ERROR"), nil)
	if w.Code != 500 || strings.Contains(w.Body.String(), "PRIVATE_") {
		t.Fatal(w.Body.String())
	}
}
func TestAssistantAttachmentHTTPStorageCheckRejectsTrailingJSON(t *testing.T) {
	mux, _, db, now, _ := attachmentHTTPFixture(t)
	defer db.Close()
	r := httptest.NewRequest("POST", "/api/comic-screenplays/projects/p/assistant/attachments/storage/check", strings.NewReader(`{"session_id":"default"}{"actor":"forged"}`))
	r.Header.Set("Content-Type", "application/json")
	SignBridgeRequest(r, "owner", false, now, "secret")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("trailing JSON accepted", w.Code)
	}
}
