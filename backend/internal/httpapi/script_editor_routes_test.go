package httpapi

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"qiantie/backend/internal/scripteditor"
)

func newScriptEditorTestRouter(now time.Time) http.Handler {
	return NewRouter(RouterOptions{
		BridgeSecret: "secret",
		Now:          func() time.Time { return now },
		ScriptEditor: scripteditor.NewService(scripteditor.NewMemoryStore()),
	})
}

func scriptEditorID(t *testing.T, rec *httptest.ResponseRecorder, key string) string {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v body=%s", err, rec.Body.String())
	}
	var row struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload[key], &row); err != nil || row.ID == "" {
		t.Fatalf("decode %s id: %v body=%s", key, err, rec.Body.String())
	}
	return row.ID
}

func signedMultipartRequest(t *testing.T, api http.Handler, now time.Time, username, path, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	SignBridgeRequest(req, username, false, now, "secret")
	rec := httptest.NewRecorder()
	api.ServeHTTP(rec, req)
	return rec
}

func TestScriptEditorRoutesKeepAccountsIsolatedAndDetectConflicts(t *testing.T) {
	now := time.Unix(1800000000, 0).UTC()
	api := newScriptEditorTestRouter(now)
	created := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works", map[string]any{"title": "甲", "type": "novel"})
	if created.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	id := scriptEditorID(t, created, "work")
	foreign := signedJSONRequest(t, api, now, "bob", http.MethodGet, "/api/script-editor/works/"+id, nil)
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign status=%d body=%s", foreign.Code, foreign.Body.String())
	}
	updated := signedJSONRequest(t, api, now, "alice", http.MethodPatch, "/api/script-editor/works/"+id, map[string]any{"title": "甲改", "expectedRevision": 0})
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	stale := signedJSONRequest(t, api, now, "alice", http.MethodPatch, "/api/script-editor/works/"+id, map[string]any{"title": "旧窗口", "expectedRevision": 0})
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale status=%d body=%s", stale.Code, stale.Body.String())
	}
	list := signedJSONRequest(t, api, now, "alice", http.MethodGet, "/api/script-editor/works?q=%E7%94%B2&sort=name-asc", nil)
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte("甲改")) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
}

func TestScriptEditorRoutesManageChaptersAndScriptVersions(t *testing.T) {
	now := time.Unix(1800000000, 0).UTC()
	api := newScriptEditorTestRouter(now)
	created := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works", map[string]any{"title": "长夜", "type": "novel"})
	workID := scriptEditorID(t, created, "work")
	first := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works/"+workID+"/chapters", map[string]any{"title": "第一章", "content": "开场"})
	second := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works/"+workID+"/chapters", map[string]any{"title": "第二章", "content": "冲突"})
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("chapters statuses=%d/%d", first.Code, second.Code)
	}
	firstID, secondID := scriptEditorID(t, first, "chapter"), scriptEditorID(t, second, "chapter")
	reordered := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works/"+workID+"/chapters/reorder", map[string]any{"chapterIds": []string{secondID, firstID}})
	if reordered.Code != http.StatusOK {
		t.Fatalf("reorder status=%d body=%s", reordered.Code, reordered.Body.String())
	}
	script := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works/"+workID+"/scripts", map[string]any{"episode": 1, "title": "第1集", "content": "镜头一", "sourceChapterIds": []string{firstID}})
	if script.Code != http.StatusCreated {
		t.Fatalf("script status=%d body=%s", script.Code, script.Body.String())
	}
	scriptID := scriptEditorID(t, script, "script")
	preferred := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works/"+workID+"/scripts/"+scriptID+"/prefer", map[string]any{"expectedRevision": 0})
	if preferred.Code != http.StatusOK || !bytes.Contains(preferred.Body.Bytes(), []byte(`"preferred":true`)) {
		t.Fatalf("prefer status=%d body=%s", preferred.Code, preferred.Body.String())
	}
	copied := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works/"+workID+"/scripts/"+scriptID+"/copy", nil)
	if copied.Code != http.StatusCreated || !bytes.Contains(copied.Body.Bytes(), []byte(`"version":2`)) {
		t.Fatalf("copy status=%d body=%s", copied.Code, copied.Body.String())
	}
}

func TestScriptEditorJobRoutesCreateListCancelAndRetry(t *testing.T) {
	now := time.Unix(1800000000, 0).UTC()
	api := newScriptEditorTestRouter(now)
	created := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works", map[string]any{"title": "任务作品", "type": "novel"})
	workID := scriptEditorID(t, created, "work")
	chapter := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works/"+workID+"/chapters", map[string]any{"title": "第一章", "content": "开场冲突"})
	chapterID := scriptEditorID(t, chapter, "chapter")
	jobResponse := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works/"+workID+"/jobs/script-generation", map[string]any{
		"requestKey": "route-job-1", "episodes": []int{1}, "chapterIds": []string{chapterID}, "modelId": "model-1",
	})
	if jobResponse.Code != http.StatusAccepted {
		t.Fatalf("create job status=%d body=%s", jobResponse.Code, jobResponse.Body.String())
	}
	jobID := scriptEditorID(t, jobResponse, "job")
	list := signedJSONRequest(t, api, now, "alice", http.MethodGet, "/api/script-editor/works/"+workID+"/jobs", nil)
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte(jobID)) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	cancelled := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works/"+workID+"/jobs/"+jobID+"/cancel", nil)
	if cancelled.Code != http.StatusOK || !bytes.Contains(cancelled.Body.Bytes(), []byte(`"status":"cancelled"`)) {
		t.Fatalf("cancel status=%d body=%s", cancelled.Code, cancelled.Body.String())
	}
	retried := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/works/"+workID+"/jobs/"+jobID+"/retry", nil)
	if retried.Code != http.StatusAccepted || !bytes.Contains(retried.Body.Bytes(), []byte(`"status":"queued"`)) {
		t.Fatalf("retry status=%d body=%s", retried.Code, retried.Body.String())
	}
}

func TestScriptEditorImportPreviewAndCommitAreSeparate(t *testing.T) {
	now := time.Unix(1800000000, 0).UTC()
	api := newScriptEditorTestRouter(now)
	preview := signedMultipartRequest(t, api, now, "alice", "/api/script-editor/import/preview", "demo.txt", []byte("第一章 开始\n正文甲\n第二章 转折\n正文乙"))
	if preview.Code != http.StatusOK || !bytes.Contains(preview.Body.Bytes(), []byte("正文乙")) {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	list := signedJSONRequest(t, api, now, "alice", http.MethodGet, "/api/script-editor/works", nil)
	if list.Code != http.StatusOK || !bytes.Contains(list.Body.Bytes(), []byte(`"works":[]`)) {
		t.Fatalf("preview must not persist: %s", list.Body.String())
	}
	var result struct {
		Preview scripteditor.ImportPreview `json:"preview"`
	}
	if err := json.Unmarshal(preview.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	commit := signedJSONRequest(t, api, now, "alice", http.MethodPost, "/api/script-editor/import/commit", map[string]any{
		"title": "导入作品", "type": "novel", "filename": "demo.txt", "sections": result.Preview.Sections,
		"selectedSectionIds": []string{result.Preview.Sections[1].ID},
	})
	if commit.Code != http.StatusCreated || !bytes.Contains(commit.Body.Bytes(), []byte("正文乙")) {
		t.Fatalf("commit status=%d body=%s", commit.Code, commit.Body.String())
	}
	oldDoc := signedMultipartRequest(t, api, now, "alice", "/api/script-editor/import/preview", "old.doc", []byte("old"))
	if oldDoc.Code != http.StatusUnprocessableEntity || !bytes.Contains(oldDoc.Body.Bytes(), []byte("DOCX")) {
		t.Fatalf("doc status=%d body=%s", oldDoc.Code, oldDoc.Body.String())
	}
}
