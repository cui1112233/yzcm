package scripteditor

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func exportFixture(t *testing.T) (*Service, string) {
	t.Helper()
	store := NewMemoryStore()
	service := NewService(store)
	work, _ := store.CreateWork(context.Background(), "alice", CreateWorkInput{Title: "归途", Type: WorkScript})
	one, _ := store.CreateScript(context.Background(), "alice", work.ID, CreateScriptInput{Episode: 1, Title: "第1集", Content: "第一集正文", GenerationSource: SourceManual})
	_, _ = store.PreferScript(context.Background(), "alice", work.ID, one.ID, one.Revision)
	two, _ := store.CreateScript(context.Background(), "alice", work.ID, CreateScriptInput{Episode: 2, Title: "第2集", Content: "第二集正文", GenerationSource: SourceManual})
	_, _ = store.PreferScript(context.Background(), "alice", work.ID, two.ID, two.Revision)
	return service, work.ID
}

func TestExportPreferredScriptsAsTXTJSONAndZIP(t *testing.T) {
	service, workID := exportFixture(t)
	for _, format := range []string{"txt", "json", "zip"} {
		result, err := service.Export(context.Background(), "alice", workID, ExportInput{Format: format})
		if err != nil {
			t.Fatalf("format=%s err=%v", format, err)
		}
		if len(result.Content) == 0 || !strings.Contains(result.Filename, "归途") {
			t.Fatalf("format=%s result=%+v", format, result)
		}
		if format == "json" {
			var payload map[string]any
			if json.Unmarshal(result.Content, &payload) != nil {
				t.Fatal("invalid json")
			}
		}
		if format == "zip" {
			reader, err := zip.NewReader(bytes.NewReader(result.Content), int64(len(result.Content)))
			if err != nil || len(reader.File) != 3 {
				t.Fatalf("zip files=%d err=%v", len(reader.File), err)
			}
			file, _ := reader.File[0].Open()
			raw, _ := io.ReadAll(file)
			if !bytes.Contains(raw, []byte("第一集正文")) {
				t.Fatalf("zip first=%s", raw)
			}
		}
	}
}

func TestExportRejectsEpisodeWithoutPreferredVersion(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store)
	work, _ := store.CreateWork(context.Background(), "alice", CreateWorkInput{Title: "未定稿", Type: WorkScript})
	_, _ = store.CreateScript(context.Background(), "alice", work.ID, CreateScriptInput{Episode: 1, Content: "草稿", GenerationSource: SourceManual})
	if _, err := service.Export(context.Background(), "alice", work.ID, ExportInput{Format: "txt"}); err == nil {
		t.Fatal("missing preferred must fail")
	}
}
