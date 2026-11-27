package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"qiantie/backend/internal/config"
	"qiantie/backend/internal/imagecapacity"
	"qiantie/backend/internal/safehttp"
	"qiantie/backend/internal/yzcmusage"
)

func TestBuildMySQLInitializesReceiptStorageBeforeFirstPaidRequest(t *testing.T) {
	dsn := os.Getenv("QIANTIE_TEST_MYSQL84_DSN")
	if dsn == "" {
		t.Skip("QIANTIE_TEST_MYSQL84_DSN must point to a disposable MySQL host")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	mysqlConfig, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	mysqlConfig.DBName = ""
	admin, err := sql.Open("mysql", mysqlConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	name := fmt.Sprintf("app_receipt_startup_test_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, "DROP DATABASE "+name); err != nil {
			t.Error(err)
		}
	})
	mysqlConfig.DBName, mysqlConfig.ParseTime = name, true
	db, err := sql.Open("mysql", mysqlConfig.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	root := t.TempDir()
	cfg := config.Config{
		PresetPath:               filepath.Join(root, "presets.json"),
		MemberSystemDir:          filepath.Join(root, "member-system"),
		ScriptHistoryDir:         filepath.Join(root, "script-history"),
		LocalExecutorArtifactDir: filepath.Join(root, "artifacts"),
	}
	if err := os.WriteFile(cfg.PresetPath, []byte(`[]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(cfg.MemberSystemDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cfg.MemberSystemDir, "usage-receipts")); !os.IsNotExist(err) {
		t.Fatalf("default receipt directory should be absent before startup: %v", err)
	}
	previousAdmission := imagecapacity.Enabled()
	t.Cleanup(func() { imagecapacity.SetEnabled(previousAdmission) })
	// Build constructs the asynchronous usage client before installing its own
	// decorator. Keep that client's reconciliation requests offline as well.
	t.Cleanup(safehttp.SetManagedOutboundDecorator(func(http.RoundTripper, time.Duration) http.RoundTripper {
		return appReceiptTestTransport(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("reconciliation is offline in this test")
		})
	}))
	if _, err := Build(ctx, cfg, db, nil); err != nil {
		t.Fatal(err)
	}
	var state, filesystemIdentity string
	if err := db.QueryRowContext(ctx, `SELECT state, filesystem_identity FROM yzcm_usage_receipt_bindings`).Scan(&state, &filesystemIdentity); err != nil || state != "active" || !strings.HasPrefix(filesystemIdentity, "fs2:") || len(filesystemIdentity) != 68 {
		t.Fatalf("startup binding state=%q filesystemIdentity=%q error=%v", state, filesystemIdentity, err)
	}

	calls := 0
	client := safehttp.NewClient(safehttp.Options{Transport: appReceiptTestTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"X-Oneapi-Request-Id": []string{"first-paid-after-default-startup"}},
			Body:       io.NopCloser(strings.NewReader("fixture result")),
		}, nil
	})})
	request, err := http.NewRequestWithContext(yzcmusage.WithAttribution(ctx, "member", "team-a", "founder"), http.MethodPost, yzcmusage.Origin+"/v1/chat/completions", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer app-startup-fixture")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	var rows int
	var owner, team, actor string
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM yzcm_usage_requests`).Scan(&rows); err != nil || rows != 1 || calls != 1 || response.StatusCode != http.StatusOK {
		t.Fatalf("first paid response=%d provider calls=%d ledger rows=%d error=%v", response.StatusCode, calls, rows, err)
	}
	if err := db.QueryRowContext(ctx, `SELECT owner, team_id, actor_username FROM yzcm_usage_requests WHERE request_id=?`, "first-paid-after-default-startup").Scan(&owner, &team, &actor); err != nil || owner != "member" || team != "team-a" || actor != "founder" {
		t.Fatalf("first receipt attribution=%q/%q/%q error=%v", owner, team, actor, err)
	}
}

type appReceiptTestTransport func(*http.Request) (*http.Response, error)

func (f appReceiptTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}
