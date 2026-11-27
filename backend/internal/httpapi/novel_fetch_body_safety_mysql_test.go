package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"qiantie/backend/internal/novelfetchworkshop"
	"qiantie/backend/internal/storage"
)

func TestBodySafetyMySQLRulesRoute(t *testing.T) {
	dsn := os.Getenv("QIANTIE_BODY_SAFETY_TEST_DSN")
	if dsn == "" {
		t.Skip("isolated body safety database not configured")
	}
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "nfw_body_safety_") {
		t.Fatal("requires isolated nfw_body_safety_ database")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := storage.RunMigrations(ctx, db, storage.NovelFetchWorkshopMigrations()); err != nil {
			t.Fatal(err)
		}
	}
	owner := "rules-safety-" + time.Now().UTC().Format("150405.000000000")
	book := "12345678901"
	defer func() {
		for _, table := range []string{"novel_fetch_workshop_documents", "novel_fetch_workshop_bodies", "novel_fetch_workshop_configs", "novel_fetch_body_object_retirements"} {
			if _, err := db.ExecContext(ctx, "DELETE FROM "+table+" WHERE owner_username=?", owner); err != nil {
				t.Error(err)
			}
		}
	}()
	objects := map[string]string{}
	store := novelfetchworkshop.NewMySQLStore(db)
	store.ObjectBodies = &novelfetchworkshop.BodyObjectStore{Bucket: "fake", Put: func(_ context.Context, _, key string, content []byte) error {
		objects[key] = string(content)
		return nil
	}, Get: func(_ context.Context, _, key string) ([]byte, error) {
		return []byte(objects[key]), nil
	}, Delete: func(_ context.Context, _, key string) error {
		delete(objects, key)
		return nil
	}}
	if err := store.PutConfig(ctx, owner, map[string]any{}); err != nil {
		t.Fatal(err)
	}
	ref, err := store.PutBody(ctx, owner, novelfetchworkshop.BodyRecord{BookID: book, BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai1", State: "ready"}, Content: "原有AI正文"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PutDocument(ctx, owner, novelfetchworkshop.Document{BookID: book, BodyRefs: map[string]novelfetchworkshop.BodyRef{"ai1": ref}, Versions: map[string]any{"ai1": map[string]any{"state": "ready", "versionId": "ai1"}}}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/novel-fetch-workshop/tasks/apply-rules", strings.NewReader(`{"ids":["12345678901"],"scope":"ai"}`))
	request = request.WithContext(context.WithValue(ctx, bridgeIdentityKey{}, BridgeIdentity{Username: owner}))
	response := httptest.NewRecorder()
	applySensitive(response, request, store, false, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var result struct {
		Updated int `json:"updated"`
		Failed  int `json:"failed"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 || result.Failed != 0 {
		t.Fatalf("result=%s", response.Body.String())
	}
	body, err := store.GetBody(ctx, owner, book, "ai1")
	if err != nil || body.Content != "原有AI正文" {
		t.Fatalf("content=%q error=%v", body.Content, err)
	}
	if len(objects) < 2 {
		t.Fatalf("old object was overwritten or deleted: objects=%d", len(objects))
	}
	t.Logf("HTTP=%d updated=%d failed=%d body=%q retained_objects=%d", response.Code, result.Updated, result.Failed, body.Content, len(objects))
}
