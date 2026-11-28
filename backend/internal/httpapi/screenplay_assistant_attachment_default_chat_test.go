package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"io"
	"net/http"
	"net/http/httptest"
	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func attachmentChatAuthorityVersion() string {
	h := sha256.New()
	fmt.Fprintf(h, "%d:%s", len("audit:0"), "audit:0")
	return hex.EncodeToString(h.Sum(nil))
}
func expectAttachmentChatLoad(m sqlmock.Sqlmock, st screenplay.AuthoringState) {
	b, _ := json.Marshal(st)
	q := regexp.QuoteMeta("SELECT body,revision FROM screenplay_authoring WHERE owner_username=? AND project_id=?") + "$"
	m.ExpectQuery(q).WithArgs("owner", "p").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(string(b), st.Revision))
}
func expectAttachmentChatScope(m sqlmock.Sqlmock, st screenplay.AuthoringState) {
	b, _ := json.Marshal(st)
	m.ExpectBegin()
	m.ExpectQuery("SELECT 1 FROM screenplay_projects").WithArgs("owner", "p").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
	m.ExpectQuery("SELECT body,revision FROM screenplay_authoring.*FOR UPDATE").WithArgs("owner", "p").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(string(b), st.Revision))
	m.ExpectQuery("SELECT session_id,COALESCE").WithArgs("owner", "p").WillReturnRows(sqlmock.NewRows([]string{"id", "parent", "creator", "visibility", "mode", "title", "revision", "archived", "status", "actor"}))
}
func expectAttachmentChatAuthority(m sqlmock.Sqlmock, revoked bool) {
	m.ExpectQuery("SELECT import_key.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"key"}).AddRow("imported"))
	m.ExpectQuery("SELECT a.username,a.active,p.role,p.archived_at.*FOR UPDATE").WithArgs("owner", "owner").WillReturnRows(sqlmock.NewRows([]string{"username", "active", "role", "archived"}).AddRow("owner", true, accountcenter.RoleFounder, nil))
	h := sha256.Sum256([]byte("original"))
	q := m.ExpectQuery("SELECT username FROM auth_sessions.*FOR UPDATE").WithArgs(hex.EncodeToString(h[:]))
	if revoked {
		q.WillReturnError(sql.ErrNoRows)
		return
	}
	q.WillReturnRows(sqlmock.NewRows([]string{"username"}).AddRow("owner"))
	m.ExpectQuery("SELECT id,username,team_id FROM account_center_memberships.*FOR UPDATE").WithArgs("owner", "owner").WillReturnRows(sqlmock.NewRows([]string{"id", "username", "team"}))
	m.ExpectQuery("SELECT id,username,capability,scope,team_id,granted_by FROM account_center_grants.*FOR UPDATE").WithArgs("owner", "owner").WillReturnRows(sqlmock.NewRows([]string{"id", "username", "capability", "scope", "team", "by"}))
	m.ExpectQuery("SELECT COALESCE.*MAX.*FOR UPDATE").WithArgs("owner", "owner", "").WillReturnRows(sqlmock.NewRows([]string{"watermark"}).AddRow(0))
}

type attachmentOpArgument struct{ out *string }

func (a attachmentOpArgument) Match(v driver.Value) bool {
	op, ok := v.(string)
	if !ok || op == "" {
		return false
	}
	*a.out = op
	return true
}

type attachmentStateArgument struct{ out *screenplay.AuthoringState }

func (a attachmentStateArgument) Match(v driver.Value) bool {
	body, ok := v.(string)
	return ok && json.Unmarshal([]byte(body), a.out) == nil
}
func TestAssistantAttachmentDefaultHTTPOriginalGuardHistoryAndRollback(t *testing.T) {
	for _, scenario := range []string{"count", "characters", "revoked-final", "save-failed", "proposal-save-failed", "proposal-success"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			now := time.Now()
			db, m, _ := sqlmock.New()
			defer db.Close()
			sqlStore := screenplay.NewMySQLStore(db)
			repo := sqlStore.AuthoringRepository().(*screenplay.MySQLAuthoringRepository)
			memory := screenplay.NewMemoryStore()
			memory.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "owner", SourceText: "ordinary source", ModelID: "selection-key"})
			store := attachmentRouteStore{MemoryStore: memory, authoring: repo, attachments: sqlStore.AssistantTextAttachments()}
			st := screenplay.AuthoringState{Owner: "owner", ProjectID: "p", Revision: 7, Chapters: []screenplay.Chapter{{ID: "chapter", Title: "old title", SourceText: "source"}}}
			count := 41
			text := "old message"
			if scenario == "characters" {
				count = 20
				text = strings.Repeat("字", 1300)
			}
			for i := 0; i < count; i++ {
				st.Messages = append(st.Messages, screenplay.AssistantMessage{Role: "user", Content: text})
			}
			scope, _ := screenplay.NewAssistantReferenceScope("owner", "p", "default", "owner", "owner")
			file, _ := screenplay.PrepareAssistantTextAttachment(scope, "file", "source.md", strings.Repeat("a", 64), []byte("CURRENT_FILE_DATA"))
			file.ObjectKey = "assistant-text/file"
			file.Status = "ready"
			file.Verified = true
			file.StateRevision = 2
			ref, _ := file.Snapshot()
			person := accountcenter.Identity{Username: "owner", Role: accountcenter.RoleFounder, Active: true}
			center := &sessionAuthorityTestCenter{Store: &impersonationTestStore{identities: map[string]accountcenter.Identity{"owner": person}}, version: attachmentChatAuthorityVersion()}
			login := &authTestStore{account: auth.Account{Username: "owner", Active: true}, session: auth.Session{Token: "original", Username: "owner", ExpiresAt: now.Add(time.Hour)}}
			requests := 0
			lookups := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				body, _ := ioReadAttachmentRequest(r)
				if strings.Contains(body, "reference_operation_id") || requests == 1 && !strings.Contains(body, "CURRENT_FILE_DATA") || requests > 1 && strings.Contains(body, "CURRENT_FILE_DATA") {
					t.Error("real model payload expanded history or omitted current file")
				}
				var payload map[string]any
				json.Unmarshal([]byte(body), &payload)
				if payload["model"] != "actual-model" || r.Header.Get("Authorization") != "Bearer frozen-key" {
					t.Error("sent materials changed")
				}
				reply := `{"reply":"file read"}`
				if requests == 1 && strings.HasPrefix(scenario, "proposal-") {
					reply = `{"reply":"file read","actions":[{"kind":"chapter","chapter":{"id":"chapter","title":"new title"}}]}`
				}
				outer, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": reply}}}})
				w.Header().Set("Content-Type", "application/json")
				w.Write(outer)
			}))
			defer server.Close()
			resolver := func(_ context.Context, owner, selection string) (batchfactoryv11.DirectorProvider, error) {
				lookups++
				if owner != "owner" || selection != "selection-key" {
					t.Fatal("lookup key changed")
				}
				return &batchfactoryv11.OpenAICompatibleProvider{Endpoint: server.URL + "/v1/chat/completions", APIKey: "frozen-key", Model: "actual-model", Client: server.Client()}, nil
			}
			mux := http.NewServeMux()
			RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{AuthStore: login, AccountCenterStore: center, Now: func() time.Time { return now }}, store, repo, resolver)
			for i := 0; i < 4; i++ {
				expectAttachmentChatLoad(m, st)
			}
			expectAttachmentChatScope(m, st)
			expectAttachmentChatAuthority(m, false)
			m.ExpectQuery("SELECT body,state_revision").WithArgs("owner", "p", file.ID).WillReturnRows(attachmentHTTPRows(file, false))
			m.ExpectCommit()
			expectAttachmentChatScope(m, st)
			expectAttachmentChatAuthority(m, scenario == "revoked-final")
			var saved screenplay.AuthoringState
			operation := ""
			if scenario == "revoked-final" {
				m.ExpectRollback()
			} else {
				m.ExpectQuery("SELECT body,state_revision").WithArgs("owner", "p", file.ID).WillReturnRows(attachmentHTTPRows(file, false))
				m.ExpectQuery("SELECT operation_id FROM screenplay_assistant_text_attachment_messages").WillReturnError(sql.ErrNoRows)
				m.ExpectExec("INSERT INTO screenplay_assistant_text_attachment_messages").WithArgs("owner", "p", "default", attachmentOpArgument{&operation}, file.ID, ref.Revision, ref.Digest, "owner", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec("UPDATE screenplay_assistant_text_attachments SET first_published_at").WillReturnResult(sqlmock.NewResult(0, 1))
				q := m.ExpectExec("UPDATE screenplay_authoring SET body").WithArgs(attachmentStateArgument{&saved}, int64(8), "p", "owner", int64(7))
				if strings.Contains(scenario, "save-failed") {
					q.WillReturnError(fmt.Errorf("controlled final write failure"))
					m.ExpectRollback()
				} else {
					q.WillReturnResult(sqlmock.NewResult(0, 1))
					m.ExpectCommit()
				}
			}
			send := func(input map[string]any) *httptest.ResponseRecorder {
				raw, _ := json.Marshal(input)
				request := httptest.NewRequest("POST", "/api/comic-screenplays/projects/p/assistant/chat", strings.NewReader(string(raw)))
				request.Header.Set("Authorization", "Bearer original")
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, request)
				return w
			}
			res := send(map[string]any{"message": "read file", "expected_revision": 7, "references": []screenplay.AssistantReferenceSelector{ref.Selector()}})
			want := 200
			if scenario == "revoked-final" {
				want = 403
			} else if strings.Contains(scenario, "save-failed") {
				want = 500
			}
			if res.Code != want || requests != 1 || lookups != 1 {
				t.Fatal(res.Code, res.Body.String(), requests, lookups)
			}
			if scenario != "revoked-final" {
				if len(saved.Messages) != count+2 || !reflect.DeepEqual(saved.Messages[:count], st.Messages) || saved.Messages[count].ReferenceOperationID != operation || operation == "" || !reflect.DeepEqual(saved.Messages[count].References, []screenplay.AssistantReferenceSnapshot{ref}) {
					t.Fatal("real user operation/history not bound")
				}
				if strings.HasPrefix(scenario, "proposal-") {
					if len(saved.Proposals) != 1 || saved.Proposals[0].Status != "pending" || saved.Chapters[0].Title != "old title" {
						t.Fatal("proposal not in same candidate state")
					}
					action := saved.Proposals[0].Actions[0]
					if action.Kind != "chapter" || action.TargetID != "chapter" || action.ContentEdit == nil || action.ContentEdit.Chapter == nil || action.ContentEdit.Chapter.Title == nil || *action.ContentEdit.Chapter.Title != "new title" || action.ContentEdit.Publish {
						t.Fatal("proposal changed target/content or applied a title-only plan")
					}
				}
			}
			if want == 200 {
				first := saved
				for i := 0; i < 3; i++ {
					expectAttachmentChatLoad(m, first)
				}
				m.ExpectExec("UPDATE screenplay_authoring SET body").WithArgs(attachmentStateArgument{&saved}, int64(9), "p", "owner", int64(8)).WillReturnResult(sqlmock.NewResult(0, 1))
				res = send(map[string]any{"message": "next round", "expected_revision": 8})
				if res.Code != 200 || requests != 2 || len(saved.Messages) != count+4 || !reflect.DeepEqual(saved.Messages[:count+2], first.Messages) {
					t.Fatal("norefs round lost original history", res.Code, res.Body.String())
				}
			}
			if e := m.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func ioReadAttachmentRequest(r *http.Request) (string, error) {
	var b strings.Builder
	_, e := io.Copy(&b, r.Body)
	return b.String(), e
}

func TestAssistantAttachmentDefaultClientCannotSupplyOperation(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := screenplay.NewMemoryStore()
	store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "owner"})
	mux := http.NewServeMux()
	calls := 0
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, store, screenplay.NewMemoryAuthoringRepository(), func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		calls++
		return nil, nil
	})
	res := signedJSONRequest(t, mux, now, "owner", "POST", "/api/comic-screenplays/projects/p/assistant/chat", map[string]any{"message": "hello", "expected_revision": 0, "reference_operation_id": "client-supplied"})
	if res.Code != 400 || calls != 0 {
		t.Fatal("client operation accepted", res.Code, calls)
	}
}
