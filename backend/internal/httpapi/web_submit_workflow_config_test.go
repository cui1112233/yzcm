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
	"qiantie/backend/internal/novelfetchworkshop"
)

func webSubmitWorkflowFixture(t *testing.T) *workflowWriteIsolationStore {
	t.Helper()
	s := &workflowWriteIsolationStore{workflowReadIsolationStore: workflowIsolationFixture(t)}
	cfg, err := s.MemoryStore.GetConfig(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	mapOrEmpty(cfg["appConfig"])["web_submit"] = map[string]any{"enabled": true, "min_file_size": 0, "organization": "fixture-org", "submit_versions": []any{"ai1"}}
	if err := s.MemoryStore.PutConfig(context.Background(), "alice", cfg); err != nil {
		t.Fatal(err)
	}
	doc := readyIndependentUploadDocument("12345678")
	doc.Meta["platformId"] = "2"
	if err := s.PutDocument(context.Background(), "alice", doc); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutBody(context.Background(), "alice", novelfetchworkshop.BodyRecord{BookID: doc.BookID, BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai1", State: "ready"}, Content: "她走进书店，与多年未见的朋友重逢。"}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWebSubmitWorkflowConfigIgnoresPendingCleanup(t *testing.T) {
	ctx := context.Background()
	t.Run("login_resume", func(t *testing.T) {
		s := webSubmitWorkflowFixture(t)
		before, _ := s.GetBody(ctx, "alice", "12345678", "ai1")
		for _, pair := range [][2]string{{"alice", "12345678"}, {"alice", "stopped"}, {"other", "waiting"}} {
			doc, err := s.GetDocument(ctx, pair[0], pair[1])
			if errors.Is(err, novelfetchworkshop.ErrNotFound) {
				doc = readyIndependentUploadDocument(pair[1])
			} else if err != nil {
				t.Fatal(err)
			}
			doc.Meta["siteSubmitStatus"] = "waiting_login"
			if pair[1] == "stopped" {
				doc.Meta["status"] = "stopped"
			}
			if err := s.PutDocument(ctx, pair[0], doc); err != nil {
				t.Fatal(err)
			}
		}
		// The pause is already durable before this fake login succeeds.
		cfg, _ := s.MemoryStore.GetConfig(ctx, "alice")
		cfg[workshopSubmitPauseKey] = map[string]any{"reason": "auth_expired", "session_fingerprint": "old-fixture"}
		if err := s.MemoryStore.PutConfig(ctx, "alice", cfg); err != nil {
			t.Fatal(err)
		}
		target := &expiredAccountConnector{}
		mux := http.NewServeMux()
		middleware := BridgeAuth{Secret: "test", AuthStore: &authTestStore{account: auth.Account{Username: "alice", Active: true}, session: auth.Session{Username: "alice", Token: "test-session", ExpiresAt: time.Now().Add(time.Hour)}}}
		registerNovelFetchUploadSessionWithStore(mux, middleware, t.TempDir(), s, target)
		req := httptest.NewRequest(http.MethodPost, "/api/novel-fetch-workshop/upload/login", strings.NewReader(`{"username":"fixture","password":"fixture-only"}`))
		req.Header.Set("Authorization", "Bearer test-session")
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if response.Code != http.StatusOK {
			t.Fatalf("login recovery blocked: HTTP%d %s", response.Code, response.Body)
		}
		resumed, _ := s.GetDocument(ctx, "alice", "12345678")
		if resumed.Meta["siteSubmitStatus"] != "pending_upload" {
			t.Fatalf("waiting book not resumed: %#v", resumed.Meta)
		}
		for _, pair := range [][2]string{{"alice", "stopped"}, {"other", "waiting"}} {
			doc, _ := s.GetDocument(ctx, pair[0], pair[1])
			if doc.Meta["siteSubmitStatus"] != "waiting_login" {
				t.Fatalf("resumed unrelated book %v", pair)
			}
		}
		after, _ := s.GetBody(ctx, "alice", "12345678", "ai1")
		if after.Content != before.Content || after.Revision != before.Revision || after.ContentHash != before.ContentHash {
			t.Fatal("login changed saved prose")
		}
		raw, _ := s.MemoryStore.GetConfig(ctx, "alice")
		if raw[workshopSubmitPauseKey] != nil {
			t.Fatal("account pause retained")
		}
		if s.strictReads != 0 || s.writes != 0 || s.preferenceWrites != 1 || target.submitCalls != 0 {
			t.Fatalf("wrong config path or real submit: strict=%d full=%d prefs=%d submits=%d", s.strictReads, s.writes, s.preferenceWrites, target.submitCalls)
		}
	})
	t.Run("preview", func(t *testing.T) {
		s := webSubmitWorkflowFixture(t)
		plan, status, message := buildWebSubmitPlanPayload(ctx, s, "alice", []string{"12345678"}, nil, false)
		if status != http.StatusOK || len(arrayOrEmpty(plan["groups"])) != 1 || plan["plan_complete"] != true {
			t.Fatalf("preview blocked: status=%d message=%s plan=%#v", status, message, plan)
		}
		if s.strictReads != 0 || s.writes != 0 || s.preferenceWrites != 0 {
			t.Fatalf("preview changed/read strict config: %#v", s)
		}
	})
	t.Run("submit", func(t *testing.T) {
		s := webSubmitWorkflowFixture(t)
		dir := t.TempDir()
		if err := saveUploadTargetSession(dir, "alice", map[string]any{"cookie": "fixture-cookie"}); err != nil {
			t.Fatal(err)
		}
		target := &recordingSubmitConnector{}
		result, err := executeWebSubmit(ctx, "alice", dir, s, webSubmitRequest{TaskIDs: []string{"12345678"}}, nil, target)
		if err != nil || result.Success != 1 || target.submitCalls != 1 {
			t.Fatalf("fake submit blocked: err=%v result=%#v calls=%d", err, result, target.submitCalls)
		}
		if s.strictReads != 0 || s.writes != 0 {
			t.Fatalf("submit used strict config: strict=%d full=%d", s.strictReads, s.writes)
		}
	})
	t.Run("due_readonly_probe", func(t *testing.T) {
		s := webSubmitWorkflowFixture(t)
		dir := t.TempDir()
		session := map[string]any{"cookie": "fixture-cookie"}
		if err := saveUploadTargetSession(dir, "alice", session); err != nil {
			t.Fatal(err)
		}
		cfg, _ := s.MemoryStore.GetConfig(ctx, "alice")
		cfg[workshopSubmitPauseKey] = map[string]any{"reason": "auth_expired", "session_fingerprint": submitSessionFingerprint(session), "next_probe_at": time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)}
		if err := s.MemoryStore.PutConfig(ctx, "alice", cfg); err != nil {
			t.Fatal(err)
		}
		target := &expiredAccountConnector{}
		recoverWorkshopSubmitAccount(ctx, s, nil, "alice", dir, target)
		if target.sessionChecks != 1 || target.submitCalls != 0 || s.strictReads != 0 {
			t.Fatalf("probe blocked/used upload: checks=%d submits=%d strict=%d", target.sessionChecks, target.submitCalls, s.strictReads)
		}
	})
}

type webSubmitWorkflowConflictStore struct {
	*workflowWriteIsolationStore
	attempts int
}

func (s *webSubmitWorkflowConflictStore) PutWorkflowConfig(ctx context.Context, owner string, prefs map[string]any) error {
	s.attempts++
	if s.attempts == 1 {
		raw, err := s.MemoryStore.GetConfig(ctx, owner)
		if err != nil {
			return err
		}
		raw["concurrent_preference"] = "keep"
		if err = s.MemoryStore.PutConfig(ctx, owner, raw); err != nil {
			return err
		}
		return novelfetchworkshop.ErrConfigConflict
	}
	return s.workflowWriteIsolationStore.PutWorkflowConfig(ctx, owner, prefs)
}
func TestWebSubmitWorkflowPausePreservesProtectedStateAndCAS(t *testing.T) {
	ctx := context.Background()
	s := &webSubmitWorkflowConflictStore{workflowWriteIsolationStore: webSubmitWorkflowFixture(t)}
	before, _ := s.MemoryStore.GetConfig(ctx, "alice")
	if err := pauseWorkshopSubmitAccount(ctx, s, "alice", map[string]any{"cookie": "fixture-cookie"}); err != nil {
		t.Fatal(err)
	}
	after, _ := s.MemoryStore.GetConfig(ctx, "alice")
	if s.attempts != 2 || s.strictReads != 0 || s.writes != 0 || after["concurrent_preference"] != "keep" || mapOrEmpty(after[workshopSubmitPauseKey])["reason"] != "auth_expired" {
		t.Fatalf("pause bypassed CAS/safe writer: attempts=%d strict=%d full=%d after=%#v", s.attempts, s.strictReads, s.writes, after)
	}
	for _, key := range []string{"modelCatalog", "modelCatalogLegacyVideoPending", "modelCatalogLegacyScrubPending"} {
		b, _ := json.Marshal(before[key])
		a, _ := json.Marshal(after[key])
		if string(a) != string(b) {
			t.Fatalf("pause changed protected %s", key)
		}
	}
}

func TestWebSubmitWorkflowConfigDatabaseErrorsRemainFailures(t *testing.T) {
	ctx := context.Background()
	s := webSubmitWorkflowFixture(t)
	failure := errors.New("workflow database unavailable")
	s.workflowErr = failure
	for name, call := range map[string]func() error{
		"pause":       func() error { return pauseWorkshopSubmitAccount(ctx, s, "alice", nil) },
		"pause_check": func() error { return checkWorkshopSubmitPause(ctx, s, "alice", nil, nil) },
		"resume":      func() error { return resumeWorkshopUploadsAfterLogin(ctx, s, "alice") },
	} {
		t.Run(name, func(t *testing.T) {
			if err := call(); !errors.Is(err, failure) {
				t.Fatalf("database error changed: %v", err)
			}
		})
	}
	_, status, _ := buildWebSubmitPlanPayload(ctx, s, "alice", []string{"12345678"}, nil, false)
	if status != http.StatusInternalServerError {
		t.Fatalf("preview DB status=%d", status)
	}
	target := &recordingSubmitConnector{}
	_, err := executeWebSubmit(ctx, "alice", t.TempDir(), s, webSubmitRequest{TaskIDs: []string{"12345678"}}, nil, target)
	var requestErr *webSubmitRequestError
	if !errors.As(err, &requestErr) || requestErr.Status != http.StatusInternalServerError || target.submitCalls != 0 {
		t.Fatalf("submit DB error swallowed: %v calls=%d", err, target.submitCalls)
	}
	if s.writes != 0 || s.preferenceWrites != 0 {
		t.Fatalf("DB failure wrote configuration: full=%d prefs=%d", s.writes, s.preferenceWrites)
	}
}
