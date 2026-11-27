package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"qiantie/backend/internal/mergeworker"
	"qiantie/backend/internal/novelfetchworkshop"
)

// Explicit local acceptance only. No router/worker is started and no model or
// submission provider is created. GetBody only refreshes its last-needed time.
func TestNovelFetchRecoveredBookLocalAcceptance(t *testing.T) {
	if os.Getenv("QIANTIE_NOVEL_FETCH_RECOVERY_LIVE") != "1" {
		t.Skip("explicit local recovery acceptance only")
	}
	db, err := sql.Open("mysql", os.Getenv("QIANTIE_MYSQL_DSN"))
	if err != nil {
		t.Fatal("open local database failed")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var database string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&database); err != nil || database != "qiantie_go_local" {
		t.Fatal("refuse non-local recovery database")
	}
	store := novelfetchworkshop.NewMySQLStore(db)
	transport, err := mergeworker.NewRedisStore(os.Getenv("QIANTIE_REDIS_URL"), "qiantie:novel-fetch")
	if err != nil {
		t.Fatal("open local queue failed")
	}
	queue := novelfetchworkshop.NewRedisQueueWithDo("qiantie:novel-fetch", novelfetchworkshop.RedisDoFunc(transport.Do))
	runs, err := store.ListRuns(ctx, "choushiyiguai", "")
	if err != nil || len(runs) == 0 {
		t.Fatal("missing historical run evidence")
	}
	old := persistedWorkshopRun(runs[0])
	checked := verifiedWorkshopJobView(ctx, store, queue, "choushiyiguai", old)
	if checked["status_reason"] != "queue_missing" || checked["status"] != "failed" || old["status"] != "queued" {
		t.Fatalf("old/new queue projection mismatch: old=%v new=%v", old["status"], checked["status"])
	}
	doc, err := store.GetDocument(ctx, "choushiyiguai", "7705784637234105369")
	if err != nil || doc.Meta["manualOnly"] != true || doc.Meta["createdAtSource"] != "recovery" {
		t.Fatal("restored document must remain manually held and provenance labelled")
	}
	if status := novelfetchworkshop.DeriveTaskStatus(doc); status != "pending_submit" {
		t.Fatalf("restored status=%s", status)
	}
	if novelFetchAutomationEligible(doc, time.Now().UTC()) {
		t.Fatal("recovery must not automatically call generation or submit")
	}
	for _, tc := range []struct {
		version string
		chars   int64
		hash    string
	}{
		{"original_raw", 5656, "acf8648b89050c16a82109371bad1b532eaf3789f535cd7271fdbb4eefae6b5f"},
		{"original", 3734, "08db4bfb3874992a9f0916edbbfce6c493a695a77b44293d9affc60881cb017d"},
		{"ai1", 3733, "e53a6dfaac8146f5fb3a8929544186cbe35ba1037d6ce142602e473cd7d4c7aa"},
	} {
		body, err := store.GetBody(ctx, "choushiyiguai", doc.BookID, tc.version)
		if err != nil {
			t.Fatalf("%s not readable: %v", tc.version, err)
		}
		if body.State != "ready" || body.CharCount != tc.chars || fmt.Sprintf("%x", sha256.Sum256([]byte(body.Content))) != tc.hash {
			t.Fatalf("%s body changed or not ready", tc.version)
		}
	}
	status, err := novelFetchAutomationStatus(ctx, store, newWorkshopProcessRegistry(), "choushiyiguai", queue)
	if err != nil || status["active_job_id"] != "" || status["processing_count"] != 0 || status["pending_count"] != 0 {
		t.Fatalf("unexpected active queue after recovery: %v %v", status, err)
	}
	t.Log("live DB/Redis A/B: old queued -> queue_missing; restored 5656/3734/3733 bodies readable and unchanged; no automatic eligibility")
}
