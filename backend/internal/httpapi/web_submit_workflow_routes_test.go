package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/auth"
	"qiantie/backend/internal/videomanagementconnector"
)

type workflowProfileConnector struct{ recordingSubmitConnector }

func (*workflowProfileConnector) RefreshProfiles(context.Context, videomanagementconnector.Session) (any, error) {
	return map[string]any{"data": []any{map[string]any{"id": "1", "config_name": "fixture-profile", "config_data": map[string]any{"gender": "2", "platform_id": "2"}}}}, nil
}
func (*workflowProfileConnector) RefreshStyles(context.Context, videomanagementconnector.Session) (string, error) {
	return `<select id="style"><option value="203">年代通用</option></select>`, nil
}

func TestWebSubmitManagementRoutesIgnorePendingModelCleanup(t *testing.T) {
	for _, tc := range []struct {
		method, path, body string
		writes             int
	}{
		{"GET", "/config", "", 0},
		{"POST", "/config", `{"settings":{"enabled":true,"selected_profile":"fixture-profile"}}`, 1},
		{"GET", "/environment", "", 0},
		{"POST", "/sync-configs", `{"persist":true}`, 1},
		{"POST", "/sync-styles", `{"persist":true}`, 1},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			for _, databaseFailure := range []bool{false, true} {
				s := webSubmitWorkflowFixture(t)
				before, _ := s.MemoryStore.GetConfig(context.Background(), "alice")
				if databaseFailure {
					s.workflowErr = errors.New("workflow database unavailable")
				}
				dir := t.TempDir()
				if err := saveUploadTargetSession(dir, "alice", map[string]any{"cookie": "fixture-cookie", "targetUsername": "fixture-user"}); err != nil {
					t.Fatal(err)
				}
				target := &workflowProfileConnector{}
				now := time.Now()
				middleware := BridgeAuth{Secret: "test", Now: func() time.Time { return now }, AuthStore: &authTestStore{account: auth.Account{Username: "alice", Active: true}, session: auth.Session{Username: "alice", Token: "fixture-session", ExpiresAt: now.Add(time.Hour)}}}
				mux := http.NewServeMux()
				RegisterBatchRewriteConfigCompat(mux, middleware, s, dir, target)
				req := httptest.NewRequest(tc.method, "/api/batch-rewrite/web-submit"+tc.path, strings.NewReader(tc.body))
				req.Header.Set("Authorization", "Bearer fixture-session")
				response := httptest.NewRecorder()
				mux.ServeHTTP(response, req)
				wantStatus := http.StatusOK
				if databaseFailure {
					wantStatus = http.StatusInternalServerError
				}
				if response.Code != wantStatus {
					t.Fatalf("pending cleanup blocked management route: HTTP%d want%d %s", response.Code, wantStatus, response.Body)
				}
				wantWrites := tc.writes
				if databaseFailure {
					wantWrites = 0
				}
				if s.strictReads != 0 || s.writes != 0 || s.preferenceWrites != wantWrites || target.submitCalls != 0 {
					t.Fatalf("wrong config path or upload: strict=%d full=%d preferences=%d submits=%d", s.strictReads, s.writes, s.preferenceWrites, target.submitCalls)
				}
				after, _ := s.MemoryStore.GetConfig(context.Background(), "alice")
				for _, key := range []string{"modelCatalog", "modelCatalogLegacyVideoPending", "modelCatalogLegacyScrubPending"} {
					a, _ := json.Marshal(after[key])
					b, _ := json.Marshal(before[key])
					if string(a) != string(b) {
						t.Fatalf("protected model field changed: %s", key)
					}
				}
			}
		})
	}
}
