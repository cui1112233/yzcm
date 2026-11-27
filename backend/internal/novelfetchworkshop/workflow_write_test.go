package novelfetchworkshop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func TestWorkflowWritePreservesRawModelsAndCleanupEvidence(t *testing.T) {
	_, mock, store := newBodySQLMock(t)
	writer, ok := any(store).(interface {
		PutWorkflowConfig(context.Context, string, map[string]any) error
	})
	if !ok {
		t.Fatal("missing workflow-only writer")
	}
	previous := `{"configRevision":"4","modelCatalog":[{"credential":"opaque-bound-secret","id":"text"}],"modelCatalogSecretVersion":"bound-v3","modelCatalogLegacyScrubPending":"held-proof","modelCatalogLegacyScrubEntries":[{"fingerprint":"held"}],"appConfig":{"videoProviders":{"old":{"apiKey":"retained-original"}},"maxTxt":1000}}`
	expected := map[string]any{}
	if err := json.Unmarshal([]byte(previous), &expected); err != nil {
		t.Fatal(err)
	}
	expected["configRevision"] = "5"
	expected["novel_fetch_current_batch_id"] = "new-batch"
	expected["appConfig"].(map[string]any)["maxTxt"] = float64(4000)
	raw, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectExec("INSERT IGNORE INTO novel_fetch_workshop_configs").WithArgs("alice").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=\\? FOR UPDATE").WithArgs("alice").WillReturnRows(sqlmock.NewRows([]string{"settings_json"}).AddRow(previous))
	mock.ExpectExec("UPDATE novel_fetch_workshop_configs SET settings_json=").WithArgs(string(raw), "alice").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	prefs := map[string]any{"configRevision": "4", "novel_fetch_current_batch_id": "new-batch", "appConfig": map[string]any{"maxTxt": 4000}}
	if err := writer.PutWorkflowConfig(context.Background(), "alice", prefs); err != nil {
		t.Fatal(err)
	}
	if prefs[ConfigRevisionKey] != "5" {
		t.Fatal("successful write did not advance revision")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestWorkflowReadViewRemovesProtectedMetadataAndHasRevision(t *testing.T) {
	prefs := workflowPreferencesForWrite(map[string]any{"modelCatalogVersion": 7, "videoProviderLegacyMigrated": true, "settings": map[string]any{"appConfig": map[string]any{"modelCatalogLegacyNewField": "held", "maxTxt": 4000}}, "rows": []any{map[string]any{"videoProviders": map[string]any{"old": "retained"}}}})
	if prefs[ConfigRevisionKey] != "0" || containsProtectedWorkflowField(prefs) {
		t.Fatal("workflow read cannot be safely written back")
	}
}

func TestWorkflowRestoreKeepsOpaqueModelNumbersAndDeletedParent(t *testing.T) {
	raw := []byte(`{"appConfig":{"modelCatalog":{"unknownInteger":9007199254740993123,"credential":"opaque"}},"rows":[{"videoProviders":{"old":"opaque"}}]}`)
	var previous map[string]json.RawMessage
	if err := json.Unmarshal(raw, &previous); err != nil {
		t.Fatal(err)
	}
	current := map[string]json.RawMessage{"label": json.RawMessage(`"changed"`)}
	if _, err := restoreProtectedWorkflowJSON(previous, current); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`9007199254740993123`)) || !bytes.Equal(current["appConfig"], previous["appConfig"]) || !bytes.Equal(current["rows"], previous["rows"]) {
		t.Fatal("opaque model material changed or vanished with its parent")
	}
}

func TestWorkflowWriteRejectsModelChangesAndRequiresReadRevision(t *testing.T) {
	for _, prefs := range []map[string]any{
		{"modelCatalog": []any{}},
		{"configRevision": "4", "appConfig": map[string]any{"videoProviders": nil}},
		{"configRevision": "4", "settings": map[string]any{"modelCatalogLegacyScrubPending": "forged"}},
		{"label": "no read version"},
		{"configRevision": "4", "appConfig": json.RawMessage(`{"modelCatalog":[]}`)},
		{"configRevision": "4", "rows": []map[string]any{{"videoProviders": nil}}},
		{"configRevision": "4", "settings": map[string]string{"modelCatalogLegacyNew": "forged"}},
	} {
		_, mock, store := newBodySQLMock(t)
		writer, ok := any(store).(interface {
			PutWorkflowConfig(context.Context, string, map[string]any) error
		})
		if !ok {
			t.Fatal("missing workflow-only writer")
		}
		if err := writer.PutWorkflowConfig(context.Background(), "alice", prefs); !errors.Is(err, ErrConfigConflict) {
			t.Fatalf("unsafe preferences accepted: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWorkflowWriteRejectsStaleRevisionWithoutChangingStorage(t *testing.T) {
	_, mock, store := newBodySQLMock(t)
	writer, ok := any(store).(interface {
		PutWorkflowConfig(context.Context, string, map[string]any) error
	})
	if !ok {
		t.Fatal("missing workflow-only writer")
	}
	mock.ExpectBegin()
	mock.ExpectExec("INSERT IGNORE INTO novel_fetch_workshop_configs").WithArgs("alice").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT settings_json FROM novel_fetch_workshop_configs WHERE owner_username=\\? FOR UPDATE").WithArgs("alice").WillReturnRows(sqlmock.NewRows([]string{"settings_json"}).AddRow(`{"configRevision":"5","modelCatalogLegacyScrubPending":"held"}`))
	mock.ExpectRollback()
	if err := writer.PutWorkflowConfig(context.Background(), "alice", map[string]any{"configRevision": "4"}); !errors.Is(err, ErrConfigConflict) {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
