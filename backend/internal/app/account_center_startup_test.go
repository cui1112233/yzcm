package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/config"
)

func TestAccountCenterImportRunsAfterLegacyAccountCommit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	source := t.TempDir()
	accountFile := filepath.Join(source, "accounts.json")
	if err := os.WriteFile(accountFile, []byte(`[{"username":"owner","passwordHash":"scrypt$test","isOwner":true,"active":true,"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM auth_migrations`).WithArgs("node-accounts-v1").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO accounts`).WithArgs("owner", "scrypt$test", true, true, sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO auth_migrations`).WithArgs("node-accounts-v1", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO account_center_imports`).WithArgs("member-center-legacy-v1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT completed_at IS NOT NULL FROM account_center_imports`).WithArgs("member-center-legacy-v1").WillReturnRows(sqlmock.NewRows([]string{"completed"}).AddRow(true))
	mock.ExpectRollback()
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO account_center_imports`).WithArgs("account-center-reserve-legacy-names-v1").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT completed_at IS NOT NULL FROM account_center_imports`).WithArgs("account-center-reserve-legacy-names-v1").WillReturnRows(sqlmock.NewRows([]string{"completed"}).AddRow(true))
	mock.ExpectRollback()

	ctx := context.Background()
	store, err := prepareAccountCenter(ctx, db, auth.NewMySQLStore(db), config.Config{LegacyAccountsPath: accountFile, MemberSystemDir: source})
	if err != nil || store == nil {
		t.Fatalf("startup store = %v, error = %v", store, err)
	}
	mock.ExpectQuery(`SELECT a.username, COALESCE`).WithArgs("owner").WillReturnRows(sqlmock.NewRows([]string{"username", "display_name", "role", "active", "avatar_url", "archived_at", "team_id"}).AddRow("owner", "Owner", "founder", true, "", nil, ""))
	identity, err := store.ResolveIdentity(ctx, "owner")
	if err != nil || identity.Role != "founder" {
		t.Fatalf("startup store resolved identity = %+v, error = %v", identity, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestAccountCenterImportFailuresStopStartup(t *testing.T) {
	for _, failAccountImport := range []bool{true, false} {
		name := "account-center import fails"
		if failAccountImport {
			name = "legacy account import fails"
		}
		t.Run(name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			cfg := config.Config{LegacyAccountsPath: "legacy-file", MemberSystemDir: t.TempDir()}
			failure := errors.New("database unavailable")
			accountImport := mock.ExpectQuery(`SELECT EXISTS\(SELECT 1 FROM auth_migrations`).WithArgs("node-accounts-v1")
			if failAccountImport {
				accountImport.WillReturnError(failure)
			} else {
				accountImport.WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
				mock.ExpectBegin()
				mock.ExpectExec(`INSERT INTO account_center_imports`).WillReturnError(failure)
				mock.ExpectRollback()
			}
			store, err := prepareAccountCenter(context.Background(), db, auth.NewMySQLStore(db), cfg)
			if !errors.Is(err, failure) || store != nil {
				t.Fatalf("startup store = %v, error = %v, want failure without a store", store, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
