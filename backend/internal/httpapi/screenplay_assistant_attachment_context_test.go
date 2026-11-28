package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"net/http/httptest"
	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/screenplay"
	"testing"
	"time"
)

func TestAssistantAttachmentAccessHMACProfileStaysWeak(t *testing.T) {
	ctx := context.WithValue(context.Background(), bridgeIdentityKey{}, BridgeIdentity{Username: "owner"})
	person := accountcenter.Identity{Username: "owner", Active: true}
	ctx = context.WithValue(ctx, accountCenterIdentityKey{}, accountcenter.RequestIdentity{Actor: person, Subject: person})
	r := httptest.NewRequest("GET", "/p?session_id=default", nil).WithContext(ctx)
	r.SetPathValue("id", "p")
	s := screenplay.NewMemoryStore()
	s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "owner"})
	svc := screenplay.AuthoringService{Store: s, Repository: screenplay.NewMemoryAuthoringRepository()}
	access, e := assistantAttachmentRequestAccess(BridgeAuth{}, r, svc, "owner", "owner", "default", false)
	if e != nil || access.Scope.ValidateRead() != nil || access.Scope.ValidatePrepare() == nil || access.ReadGuard(ctx, nil) != nil {
		t.Fatal(access, e)
	}
	if _, e = assistantAttachmentRequestAccess(BridgeAuth{}, r, svc, "owner", "owner", "default", true); !errors.Is(e, screenplay.ErrAccessDenied) {
		t.Fatal("weak prepared", e)
	}
}
func TestAssistantAttachmentAccessStrongFailureNeverDowngrades(t *testing.T) {
	ctx := context.WithValue(context.Background(), bridgeIdentityKey{}, BridgeIdentity{Username: "owner"})
	person := accountcenter.Identity{Username: "owner", Active: true, TeamID: "team-a"}
	ctx = context.WithValue(ctx, accountCenterIdentityKey{}, accountcenter.RequestIdentity{Actor: person, Subject: person})
	r := httptest.NewRequest("GET", "/p", nil).WithContext(ctx)
	r.SetPathValue("id", "p")
	r.Header.Set("Authorization", "Bearer original")
	center := &sessionAuthorityTestCenter{Store: &impersonationTestStore{identities: map[string]accountcenter.Identity{"owner": person}}, version: "v1"}
	login := &authTestStore{account: auth.Account{Username: "owner", Active: true}, session: auth.Session{Token: "original", Username: "owner", ExpiresAt: time.Now().Add(time.Hour)}}
	authn := BridgeAuth{AuthStore: login, AccountCenterStore: center}
	s := screenplay.NewMemoryStore()
	s.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "owner"})
	svc := screenplay.AuthoringService{Store: s, Repository: screenplay.NewMemoryAuthoringRepository()}
	access, e := assistantAttachmentRequestAccess(authn, r, svc, "owner", "owner", "default", false)
	if e != nil || access.Scope.ValidatePrepare() != nil {
		t.Fatal(e)
	}
	db, m, _ := sqlmock.New()
	defer db.Close()
	m.ExpectBegin()
	tx, _ := db.Begin()
	m.ExpectQuery("SELECT import_key.*FOR UPDATE").WillReturnRows(sqlmock.NewRows([]string{"key"}).AddRow("imported"))
	m.ExpectQuery("SELECT a.username,a.active,p.role,p.archived_at.*FOR UPDATE").WithArgs("owner", "owner").WillReturnRows(sqlmock.NewRows([]string{"username", "active", "role", "archived"}).AddRow("owner", true, person.Role, nil))
	m.ExpectQuery("SELECT username FROM auth_sessions.*FOR UPDATE").WillReturnError(sql.ErrNoRows)
	m.ExpectRollback()
	if access.ReadGuard(ctx, tx) == nil {
		t.Fatal("SQL original login revoked but read admitted")
	}
	tx.Rollback()
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
	center.version = "v2"
	if access.VerifyPrepare(ctx) == nil {
		t.Fatal("authority version change ignored")
	}
	center.version = "v1"
	login.session = auth.Session{}
	if access.ReadGuard(ctx, nil) == nil {
		t.Fatal("late login revocation ignored")
	}
	if _, e = assistantAttachmentRequestAccess(authn, r, svc, "owner", "owner", "default", false); !errors.Is(e, screenplay.ErrAccessDenied) {
		t.Fatal("failed bearer downgraded", e)
	}
}
