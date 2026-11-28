package httpapi

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"net/http"
	"net/http/httptest"
	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
	"qiantie/backend/internal/yzcmusage"
	"strings"
	"testing"
	"time"
)

func expectAttachmentPrivateChatScope(m sqlmock.Sqlmock, st screenplay.AuthoringState, revision int64, status string) {
	b, _ := json.Marshal(st)
	m.ExpectBegin()
	m.ExpectQuery("SELECT 1 FROM screenplay_projects").WithArgs("owner", "p").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(1))
	m.ExpectQuery("SELECT body,revision FROM screenplay_authoring.*FOR UPDATE").WithArgs("owner", "p").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(string(b), st.Revision))
	m.ExpectQuery("SELECT session_id,COALESCE").WithArgs("owner", "p").WillReturnRows(sqlmock.NewRows([]string{"id", "parent", "creator", "visibility", "mode", "title", "rev", "archived", "status", "actor"}).AddRow("s", "default", "owner", "private", "confirm", "private", revision, false, status, "owner"))
}
func attachmentSessionRequestFixture(t *testing.T) (context.Context, BridgeAuth, *http.Request, sessionAuthorityCallbacks, screenplay.AssistantReferenceScope) {
	t.Helper()
	person := accountcenter.Identity{Username: "owner", Role: accountcenter.RoleFounder, Active: true}
	ctx := context.WithValue(context.Background(), bridgeIdentityKey{}, BridgeIdentity{Username: "owner"})
	ctx = context.WithValue(ctx, accountCenterIdentityKey{}, accountcenter.RequestIdentity{Actor: person, Subject: person})
	request := httptest.NewRequest("POST", "/api/comic-screenplays/projects/p/assistant/sessions/s/chat", nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer original")
	request.SetPathValue("id", "p")
	request.SetPathValue("session", "s")
	center := &sessionAuthorityTestCenter{Store: &impersonationTestStore{identities: map[string]accountcenter.Identity{"owner": person}}, version: attachmentChatAuthorityVersion()}
	login := &authTestStore{account: auth.Account{Username: "owner", Active: true}, session: auth.Session{Token: "original", Username: "owner", ExpiresAt: time.Now().Add(time.Hour)}}
	authn := BridgeAuth{AuthStore: login, AccountCenterStore: center}
	authority := newSessionAuthority(authn, request, "owner")
	scope, _ := screenplay.NewAssistantReferenceScope("owner", "p", "s", "owner", "owner")
	return ctx, authn, request, authority, scope
}
func TestAssistantAttachmentSessionActualBudgetBeforeBegin(t *testing.T) {
	ctx, authn, _, authority, scope := attachmentSessionRequestFixture(t)
	db, m, _ := sqlmock.New()
	defer db.Close()
	sqlStore := screenplay.NewMySQLStore(db)
	repo := sqlStore.AuthoringRepository().(*screenplay.MySQLAuthoringRepository)
	memory := screenplay.NewMemoryStore()
	memory.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "owner", ModelID: "selection-key"})
	store := attachmentRouteStore{MemoryStore: memory, authoring: repo, attachments: sqlStore.AssistantTextAttachments()}
	svc := screenplay.AuthoringService{Store: store, Repository: repo}
	st := screenplay.AuthoringState{Owner: "owner", ProjectID: "p", Revision: 7}
	file, _ := screenplay.PrepareAssistantTextAttachment(scope, "file", "x.md", strings.Repeat("a", 64), []byte("CURRENT_FILE_DATA"))
	file.Status = "ready"
	file.Verified = true
	ref, _ := file.Snapshot()
	expectAttachmentChatLoad(m, st)
	expectAttachmentPrivateChatScope(m, st, 1, "")
	expectAttachmentChatAuthority(m, false)
	m.ExpectQuery("SELECT body,state_revision").WillReturnRows(attachmentHTTPRows(file, false))
	m.ExpectCommit()
	expectAttachmentChatLoad(m, st)
	expectAttachmentPrivateChatScope(m, st, 1, "")
	history, _ := json.Marshal(screenplay.AssistantSessionHistory{Messages: []screenplay.AssistantMessage{{Role: "user", Content: "selected session history"}}})
	m.ExpectQuery("SELECT body FROM screenplay_assistant_sessions").WithArgs("owner", "p", "s").WillReturnRows(sqlmock.NewRows([]string{"body"}).AddRow(string(history)))
	m.ExpectCommit()
	lookups := 0
	channel := NewScreenplaySessionChannel(authn.AuthStore, authn.AccountCenterStore, func(_ context.Context, owner, selection string) (*batchfactoryv11.OpenAICompatibleProvider, error) {
		lookups++
		if owner != "owner" || selection != "selection-key" {
			t.Fatal("selection key changed")
		}
		return &batchfactoryv11.OpenAICompatibleProvider{Endpoint: yzcmusage.SessionChatEndpoint, APIKey: "frozen-key", Model: strings.Repeat("<", 17000)}, nil
	}, &yzcmusage.Tracker{})
	g := sessionChannelGenerator{channel: channel, repo: repo, subject: "owner", verify: authority.verify, commitGuard: authority.commit}
	access := &assistantAttachmentAccess{Scope: scope, ReadGuard: authority.commit, VerifyPrepare: authority.verify, CommitPrepare: authority.commit, CanPrepare: true}
	out, e := runSessionChatWithAttachmentAccess(ctx, repo, svc, g, access, "owner", "p", "owner", "s", 1, "read file", ref.Selector())
	if !errors.Is(e, screenplay.ErrInvalidArgument) || lookups != 1 || len(out.Messages) != 0 {
		t.Fatal("actual model overflow did not reject before Begin", e, lookups)
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
func TestAssistantAttachmentSessionHandlerRejectsGenericReceiptGenerator(t *testing.T) {
	g := &captureReferenceGenerator{}
	repo := &referenceChatRepo{}
	memory := screenplay.NewMemoryStore()
	memory.CreateProject(context.Background(), screenplay.Project{ID: "p", OwnerUsername: "owner"})
	svc := screenplay.AuthoringService{Store: memory, Repository: screenplay.NewMemoryAuthoringRepository()}
	decode := func(w http.ResponseWriter, r *http.Request, dst any) bool {
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(dst) != nil {
			w.WriteHeader(400)
			return false
		}
		return true
	}
	handler := sessionChatHandler(repo, svc, decode, func(*http.Request, string) screenplay.TextGenerator { return g })
	request := httptest.NewRequest("POST", "/", strings.NewReader(`{"message":"read","expected_revision":1,"references":[{"kind":"attachment_text","id":"file","expected_digest":"`+strings.Repeat("a", 64)+`"}]}`))
	request.SetPathValue("id", "p")
	request.SetPathValue("session", "s")
	w := httptest.NewRecorder()
	handler(w, request, "owner", "owner")
	if w.Code != 503 || g.calls != 0 || repo.begins != 0 {
		t.Fatal("file path enabled a fake paid channel", w.Code, g.calls, repo.begins)
	}
}

type attachmentHistoryArgument struct {
	out *screenplay.AssistantSessionHistory
}

func (a attachmentHistoryArgument) Match(v driver.Value) bool {
	s, ok := v.(string)
	return ok && json.Unmarshal([]byte(s), a.out) == nil
}
func TestAssistantAttachmentSessionFinishOriginalAuthAndAtomicSave(t *testing.T) {
	for _, scenario := range []string{"valid", "revoked", "cas-failed", "bad-prefix"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, authn, _, authority, scope := attachmentSessionRequestFixture(t)
			if e := authority.verify(ctx); e != nil {
				t.Fatal(e)
			}
			db, m, _ := sqlmock.New()
			defer db.Close()
			sqlStore := screenplay.NewMySQLStore(db)
			repo := sqlStore.AuthoringRepository().(*screenplay.MySQLAuthoringRepository)
			file, _ := screenplay.PrepareAssistantTextAttachment(scope, "file", "x.md", strings.Repeat("a", 64), []byte("CURRENT_FILE_DATA"))
			file.Status = "ready"
			file.Verified = true
			ref, _ := file.Snapshot()
			st := screenplay.AuthoringState{Owner: "owner", ProjectID: "p", Revision: 7}
			previous := []screenplay.AssistantMessage{{Role: "user", Content: "old history"}}
			req := screenplay.AssistantSessionRequest{Owner: "owner", Project: "p", Actor: "owner", SessionID: "s", Token: "request-token", Revision: 2, History: screenplay.AssistantSessionHistory{Messages: previous}}
			history := req.History
			history.Messages = append(append([]screenplay.AssistantMessage(nil), previous...), screenplay.AssistantMessage{Role: "user", Content: "read file", ReferenceOperationID: "server-operation", References: []screenplay.AssistantReferenceSnapshot{ref}}, screenplay.AssistantMessage{Role: "assistant", Content: "file read"})
			if scenario == "bad-prefix" {
				history.Messages[0].Content = "changed old history"
			}
			channel := NewScreenplaySessionChannel(authn.AuthStore, authn.AccountCenterStore, func(context.Context, string, string) (*batchfactoryv11.OpenAICompatibleProvider, error) {
				t.Fatal("finish called model catalog")
				return nil, nil
			}, &yzcmusage.Tracker{})
			g := sessionChannelGenerator{channel: channel, repo: repo, subject: "owner", verify: authority.verify, commitGuard: authority.commit}
			access := assistantAttachmentAccess{Scope: scope, ReadGuard: authority.commit, CommitPrepare: authority.commit}
			expectAttachmentPrivateChatScope(m, st, 2, "running")
			expectAttachmentChatAuthority(m, scenario == "revoked")
			var saved screenplay.AssistantSessionHistory
			if scenario == "revoked" || scenario == "bad-prefix" {
				m.ExpectRollback()
			} else {
				m.ExpectQuery("SELECT body,state_revision").WillReturnRows(attachmentHTTPRows(file, false))
				m.ExpectQuery("SELECT operation_id FROM screenplay_assistant_text_attachment_messages").WillReturnError(sql.ErrNoRows)
				m.ExpectExec("INSERT INTO screenplay_assistant_text_attachment_messages").WithArgs("owner", "p", "s", "server-operation", file.ID, ref.Revision, ref.Digest, "owner", "owner").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec("UPDATE screenplay_assistant_text_attachments SET first_published_at").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec("INSERT INTO screenplay_assistant_request_history").WithArgs("completed", "completed", "owner", "p", "s").WillReturnResult(sqlmock.NewResult(0, 1))
				q := m.ExpectExec("UPDATE screenplay_assistant_sessions SET body").WithArgs(attachmentHistoryArgument{&saved}, "owner", "p", "s", "request-token", "owner", int64(2))
				rows := int64(1)
				if scenario == "cas-failed" {
					rows = 0
				}
				q.WillReturnResult(sqlmock.NewResult(0, rows))
				if rows == 0 {
					m.ExpectRollback()
				} else {
					m.ExpectCommit()
				}
			}
			e := finishAssistantAttachmentSession(ctx, repo, g, access, req, &history, "server-operation", "read file", []screenplay.AssistantReferenceSnapshot{ref})
			if scenario == "valid" {
				if e != nil || len(saved.Messages) != 3 || saved.Messages[1].ReferenceOperationID != "server-operation" {
					t.Fatal(e, saved)
				}
			} else if e == nil {
				t.Fatal("atomic guard failure admitted", scenario)
			}
			if e := m.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestAssistantAttachmentSessionScopeAndClientOperation(t *testing.T) {
	ctx, _, _, _, scope := attachmentSessionRequestFixture(t)
	for _, v := range [][4]string{{"other", "p", "owner", "s"}, {"owner", "other", "owner", "s"}, {"owner", "p", "other", "s"}, {"owner", "p", "owner", "other"}} {
		if e := checkAssistantAttachmentChatScope(ctx, scope, v[0], v[1], v[2], v[3]); !errors.Is(e, screenplay.ErrAccessDenied) {
			t.Fatal("request scope mismatch accepted", v, e)
		}
	}
	repo := &referenceChatRepo{}
	_, e := runSessionChat(ctx, repo, screenplay.AuthoringService{}, nil, "owner", "p", "owner", "s", 1, "hello", screenplay.AssistantReferenceSelector{Kind: "attachment_text", ID: "file"})
	if !errors.Is(e, screenplay.ErrAccessDenied) || repo.begins != 0 {
		t.Fatal("unscoped file fallback", e)
	}
	g := &captureReferenceGenerator{}
	decode := func(w http.ResponseWriter, r *http.Request, v any) bool {
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(v) != nil {
			w.WriteHeader(400)
			return false
		}
		return true
	}
	handler := sessionChatHandler(repo, screenplay.AuthoringService{}, decode, func(*http.Request, string) screenplay.TextGenerator { return g })
	w := httptest.NewRecorder()
	handler(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"message":"read","expected_revision":1,"reference_operation_id":"client-op"}`)), "owner", "owner")
	if w.Code != 400 || g.calls != 0 || repo.begins != 0 {
		t.Fatal("client operation enabled send", w.Code)
	}
}
