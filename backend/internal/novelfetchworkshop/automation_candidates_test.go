package novelfetchworkshop

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestAutomationCandidatesProjectOnlyReadOnlySummary(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	raw := `{"gender":"男频","meta":{"status":"waiting_retry"},"bodyRefs":{"original_raw":{"charCount":12000}},"maxTxt":4000}`
	mock.ExpectQuery(regexp.QuoteMeta(automationCandidateSQL)).WithArgs("alice", "123", "batch-1", "batch-1", 250).WillReturnRows(sqlmock.NewRows([]string{"book_id", "summary", "has_original", "updated_at"}).AddRow("456", raw, true, now))
	docs, err := NewMySQLStore(db).ListAutomationCandidates(context.Background(), "alice", "batch-1", "123", 1000)
	if err != nil || len(docs) != 1 {
		t.Fatalf("docs=%v err=%v", docs, err)
	}
	d := docs[0]
	if !d.OriginalRawAvailable {
		t.Fatal("candidate lost real original-body presence")
	}
	if d.BookID != "456" || d.Gender != "男频" || d.BodyRefs["original_raw"].CharCount != 12000 || d.MaxTxt != 4000 || d.OriginalRaw != "" || len(d.Logs) != 0 || len(d.Versions) != 0 || d.Original != "" || d.ProcessedText != "" {
		t.Fatalf("bad summary: %+v", d)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
	// Presence is scheduling metadata only. Neither direct reads nor a JSON
	// persistence round trip may turn it into text or a stored presence claim.
	if text, err := ReadBodyText(context.Background(), nil, "alice", d, "original_raw"); err != nil || text != "" {
		t.Fatalf("presence became body text: %q %v", text, err)
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"originalRaw", "original", "processedText", "OriginalRawAvailable", "originalRawAvailable"} {
		if _, exists := payload[key]; exists {
			t.Fatalf("candidate persisted fake body/presence %s: %s", key, encoded)
		}
	}
	var persisted Document
	if err := json.Unmarshal(encoded, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.OriginalRawAvailable || persisted.OriginalRaw != "" {
		t.Fatalf("presence leaked into persisted document: %+v", persisted)
	}
	if err := json.Unmarshal([]byte(`{"OriginalRawAvailable":true,"originalRawAvailable":true}`), &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.OriginalRawAvailable {
		t.Fatal("JSON input forged internal presence")
	}
}
