package accountcenter

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func sessionGuardVersion(tokens ...string) string {
	d := sha256.New()
	for _, token := range tokens {
		fmt.Fprintf(d, "%d:%s", len(token), token)
	}
	return hex.EncodeToString(d.Sum(nil))
}
func TestSessionCommitGuardLocksOriginalLoginAndCurrentAuthority(t *testing.T) {
	for _, scenario := range []string{"valid", "revoked_login", "changed_authority"} {
		t.Run(scenario, func(t *testing.T) {
			db, m, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			m.ExpectBegin()
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			m.ExpectQuery("SELECT import_key.*FOR UPDATE").WithArgs(legacyImportKey).WillReturnRows(sqlmock.NewRows([]string{"key"}).AddRow(legacyImportKey))
			m.ExpectQuery("SELECT a.username,a.active,p.role,p.archived_at.*FOR UPDATE").WithArgs("founder", "founder").WillReturnRows(sqlmock.NewRows([]string{"user", "active", "role", "archived"}).AddRow("founder", true, RoleFounder, nil))
			login := m.ExpectQuery("SELECT username FROM auth_sessions.*FOR UPDATE").WithArgs("original-hash")
			if scenario == "revoked_login" {
				login.WillReturnError(sql.ErrNoRows)
			} else {
				login.WillReturnRows(sqlmock.NewRows([]string{"user"}).AddRow("founder"))
				m.ExpectQuery("SELECT id,username,team_id FROM account_center_memberships.*FOR UPDATE").WithArgs("founder", "founder").WillReturnRows(sqlmock.NewRows([]string{"id", "user", "team"}))
				m.ExpectQuery("SELECT id,username,capability,scope,team_id,granted_by FROM account_center_grants.*FOR UPDATE").WithArgs("founder", "founder").WillReturnRows(sqlmock.NewRows([]string{"id", "user", "capability", "scope", "team", "grantor"}))
				watermark := 0
				if scenario == "changed_authority" {
					watermark = 1
				}
				m.ExpectQuery("SELECT COALESCE.*MAX.*FOR UPDATE").WithArgs("founder", "founder", "").WillReturnRows(sqlmock.NewRows([]string{"watermark"}).AddRow(watermark))
			}
			person := Identity{Username: "founder", Role: RoleFounder, Active: true}
			err = GuardSessionCommit(context.Background(), tx, SessionCommitAuthority{Identity: RequestIdentity{Actor: person, Subject: person}, LoginTokenHash: "original-hash", Version: sessionGuardVersion("audit:0")})
			if scenario == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				m.ExpectCommit()
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
			} else {
				if err == nil {
					t.Fatal("revoked authority accepted")
				}
				m.ExpectRollback()
				tx.Rollback()
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
