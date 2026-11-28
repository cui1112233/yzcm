package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"qiantie/backend/internal/videomanagementconnector"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/novelfetchworkshop"
)

func TestSafetyResultsProviderKeyNeverLeavesErrorBoundary(t *testing.T) {
	const key = "unlabelled-fixture-credential-only"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte("debug echoed " + key + " HTTP bad request"))
	}))
	defer server.Close()
	p := &integrationTextProvider{Label: "fixture", Endpoint: server.URL, Model: "fixture", APIKey: key, Client: server.Client()}
	_, err := p.Complete(batchfactoryv11.WithSingleAttempt(context.Background()), batchfactoryv11.TextCompletionRequest{UserPrompt: "fixture", MaxTokens: 32})
	if err == nil || strings.Contains(err.Error(), key) {
		t.Fatalf("provider leaked credential: %v", err)
	}
	var status *batchfactoryv11.DirectorHTTPError
	if !errors.As(err, &status) || status.StatusCode != 400 || strings.Contains(status.Body, key) {
		t.Fatalf("lost safe typed HTTP classification: %v", err)
	}
	state := scheduleNovelFetchTaskRetry(map[string]any{}, novelFetchRetryStageRewrite, err, 0, time.Now())
	if strings.Contains(firstText(state["error"]), key) {
		t.Fatal("retry persisted credential")
	}
}

func TestSafetyResultsHistoricalErrorsAreRedactedOnRead(t *testing.T) {
	ctx := context.Background()
	s := novelfetchworkshop.NewMemoryStore()
	const key = "historical-fixture-secret"
	d := novelfetchworkshop.Document{BookID: "88888888", Gender: "女频", Original: "原始正文保持原样。", Meta: map[string]any{"style": "年代通用", "originalStatus": "done", "aiError": "api_key=" + key, "retry_schedule": map[string]any{"rewrite": map[string]any{"error": "Authorization: Bearer " + key}}}, Logs: []any{map[string]any{"event": "ai_failed", "error": "token=" + key}}}
	if err := s.PutDocument(ctx, "tester", d); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h := NewRouter(RouterOptions{BridgeSecret: "fixture", Now: func() time.Time { return now }, NovelFetchStore: s})
	for _, path := range []string{"/api/novel-fetch-workshop/tasks/88888888", "/api/novel-fetch-workshop/tasks"} {
		r := doNovelFetchRequest(t, h, http.MethodGet, path, nil, now, "fixture")
		if r.Code != 200 || strings.Contains(r.Body.String(), key) {
			t.Fatalf("historical HTTP error leaked on %s: status=%d", path, r.Code)
		}
	}
	saved, _ := s.GetDocument(ctx, "tester", d.BookID)
	if saved.Meta["aiError"] != "api_key="+key {
		t.Fatal("read-time filtering rewrote stored history")
	}
}

func TestSafetyResultsUploadAckIsNotReadbackConfirmation(t *testing.T) {
	ctx := context.Background()
	s := novelfetchworkshop.NewMemoryStore()
	d := readyIndependentUploadDocument("88888889")
	d.Meta["platformId"] = "2"
	_ = s.PutDocument(ctx, "tester", d)
	_ = s.PutConfig(ctx, "tester", map[string]any{"appConfig": map[string]any{"web_submit": map[string]any{"enabled": true, "min_file_size": 0, "organization": "fixture"}}})
	_, _ = s.PutBody(ctx, "tester", novelfetchworkshop.BodyRecord{BookID: d.BookID, BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai1", State: "ready"}, Content: "她站在旧木门前，终于认出了多年未见的亲人。"})
	dir := t.TempDir()
	_ = saveUploadTargetSession(dir, "tester", map[string]any{"cookie": "fixture"})
	fake := &uncertainSubmitConnector{}
	request := webSubmitRequest{TaskIDs: []string{d.BookID}, Versions: []string{"ai1"}}
	first, err := executeWebSubmit(ctx, "tester", dir, s, request, nil, fake)
	if err != nil || len(first.Results) != 1 {
		t.Fatal(first, err)
	}
	row := mapOrEmpty(first.Results[0])
	if first.Success != 1 || row["status"] != "submitted" || row["verified"] != false || row["readback_status"] != "pending" || row["content_verified"] != false {
		t.Fatalf("ack masqueraded as confirmation: %#v", row)
	}
	saved, _ := s.GetDocument(ctx, "tester", d.BookID)
	log := mapOrEmpty(saved.Logs[len(saved.Logs)-1])
	receipt := mapOrEmpty(log["remote_receipt"])
	if receipt["verified"] != false || !containsVersion(saved.Meta["siteSubmitDoneVersions"], "ai1") || !novelFetchSubmissionNeedsReadback(saved.Meta, "ai1") {
		t.Fatalf("accepted/pending evidence not durable: receipt=%#v", receipt)
	}
	request.ReadbackOnly = true
	second, err := executeWebSubmit(ctx, "tester", dir, s, request, nil, fake)
	if err != nil || len(second.Results) != 1 {
		t.Fatal(second, err)
	}
	row = mapOrEmpty(second.Results[0])
	if fake.submitCalls != 1 || fake.readbackCalls != 2 || row["verified"] != true || row["readback_status"] != "confirmed" || row["content_verified"] != false {
		t.Fatalf("readback resubmitted or invented content verification: calls=%d/%d row=%#v", fake.submitCalls, fake.readbackCalls, row)
	}
	saved, _ = s.GetDocument(ctx, "tester", d.BookID)
	raw, _ := json.Marshal(saved.Logs)
	if len(raw) == 0 {
		t.Fatal("submission history disappeared")
	}
}

func TestSafetyResultsErrorViewPreservesProseAndTitles(t *testing.T) {
	const text = "小说原文 api_key=keep-this-literally"
	d := novelfetchworkshop.Document{BookID: "1", Original: text, BookInfo: novelfetchworkshop.BookInfo{WorkTitle: text}, Meta: map[string]any{"style": "年代通用", "user_prompt": text, "aiError": "token=hide-this"}, Logs: []any{map[string]any{"before": text, "after": text, "error": "token=hide-this"}}}
	safe := safeNovelFetchDocumentErrors(d)
	if safe.Original != text || safe.BookInfo.WorkTitle != text || safe.Meta["user_prompt"] != text || mapOrEmpty(safe.Logs[0])["before"] != text || mapOrEmpty(safe.Logs[0])["after"] != text {
		t.Fatal("error filter modified prose/title/config text")
	}
	if strings.Contains(firstText(safe.Meta["aiError"]), "hide-this") || d.Meta["aiError"] != "token=hide-this" {
		t.Fatal("filter leaked or mutated source")
	}
}

func TestSafetyResultsRetrySanitizesRawErrorsBeforePersist(t *testing.T) {
	state := scheduleNovelFetchTaskRetry(map[string]any{}, novelFetchRetryStageRewrite, errors.New("HTTP 503 api_key=retry-raw-secret"), 0, time.Now())
	if strings.Contains(firstText(state["error"]), "retry-raw-secret") || !strings.Contains(firstText(state["error"]), "503") {
		t.Fatal("retry error not safely persisted")
	}
}

type safetyEvidenceConnector struct {
	recordingSubmitConnector
	ackID    string
	readback videomanagementconnector.Submission
}

func (c *safetyEvidenceConnector) Submit(_ context.Context, _ videomanagementconnector.Session, input videomanagementconnector.SubmissionInput) (videomanagementconnector.Submission, error) {
	c.submitCalls++
	c.lastInput = input
	return videomanagementconnector.Submission{ID: c.ackID, Status: "submitted"}, nil
}
func (c *safetyEvidenceConnector) GetSubmission(context.Context, videomanagementconnector.Session, string) (videomanagementconnector.Submission, error) {
	result := c.readback
	record := mapOrEmpty(result.ReadbackRaw)
	if record["content_sha256"] == "LOCAL" {
		record["content_sha256"] = fmt.Sprintf("%x", sha256.Sum256(c.lastInput.Content))
	}
	return result, nil
}
func TestSafetyResultsReadbackNeedsMatchingEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, ack, id, book, time string
		verified                  bool
	}{
		{name: "different_receipt", ack: "remote-1", id: "old-record", book: "88888889"},
		{name: "different_book", ack: "remote-1", id: "remote-1", book: "99999999"},
		{name: "old_record_without_ack_id", id: "old-record", book: "88888889", time: "2000-01-01 00:00:00"},
		{name: "hash_without_attempt_freshness", id: "old-record", book: "88888889"},
		{name: "matching_id_is_not_content_download", ack: "remote-1", id: "remote-1", book: "88888889", verified: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s := novelfetchworkshop.NewMemoryStore()
			d := readyIndependentUploadDocument("88888889")
			d.Meta["platformId"] = "2"
			_ = s.PutDocument(ctx, "tester", d)
			_ = s.PutConfig(ctx, "tester", map[string]any{"appConfig": map[string]any{"web_submit": map[string]any{"enabled": true, "min_file_size": 0, "organization": "fixture"}}})
			_, _ = s.PutBody(ctx, "tester", novelfetchworkshop.BodyRecord{BookID: d.BookID, BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai1", State: "ready"}, Content: "她站在旧木门前，终于认出了多年未见的亲人。"})
			dir := t.TempDir()
			_ = saveUploadTargetSession(dir, "tester", map[string]any{"cookie": "fixture"})
			c := &safetyEvidenceConnector{ackID: tc.ack, readback: videomanagementconnector.Submission{ID: tc.id, Status: "submitted", ReadbackRaw: map[string]any{"bookid": tc.book, "addtime": tc.time, "content_sha256": "LOCAL", "content_verified": true}}}
			result, err := executeWebSubmit(ctx, "tester", dir, s, webSubmitRequest{TaskIDs: []string{d.BookID}, Versions: []string{"ai1"}}, nil, c)
			if err != nil || len(result.Results) != 1 {
				t.Fatal(result, err)
			}
			row := mapOrEmpty(result.Results[0])
			if row["status"] != "submitted" || row["verified"] != tc.verified || row["content_verified"] != false || c.submitCalls != 1 {
				t.Fatalf("unreliable readback evidence accepted: %#v", row)
			}
		})
	}
}

func TestSafetyResultsUnknownReadbackCannotUseHashWithoutFreshness(t *testing.T) {
	now := time.Now().UTC()
	d := novelfetchworkshop.Document{BookID: "88888889", Meta: map[string]any{"siteSubmitPendingConfirmation": map[string]any{"ai1": "88888889"}, "siteSubmitAttempts": map[string]any{"ai1": map[string]any{"operation_id": "current-operation", "content_sha256": "same-content", "started_at": now.Format(time.RFC3339Nano)}}}}
	receipt := videomanagementconnector.Submission{ID: "some-existing-record", ReadbackRaw: map[string]any{"bookid": d.BookID, "content_sha256": "same-content"}}
	if workshopPendingReadbackMatches(receipt, d, d.BookID, "ai1") {
		t.Fatal("same old content without attempt freshness became verified")
	}
	record := mapOrEmpty(receipt.ReadbackRaw)
	record["operation_id"] = "current-operation"
	if !workshopPendingReadbackMatches(receipt, d, d.BookID, "ai1") {
		t.Fatal("current unique operation evidence rejected")
	}
	delete(record, "operation_id")
	record["addtime"] = now.In(time.FixedZone("Asia/Shanghai", 8*60*60)).Format(time.DateTime)
	if !workshopPendingReadbackMatches(receipt, d, d.BookID, "ai1") {
		t.Fatal("matching book/hash/current timestamp rejected")
	}
}

func TestSafetyResultsAcknowledgementWithSourceBookIDIsPreserved(t *testing.T) {
	d := novelfetchworkshop.Document{BookID: "internal-book", Meta: map[string]any{"source_book_id": "88888889", "siteSubmitPendingConfirmation": map[string]any{"ai1": "88888889"}, "siteSubmitAttempts": map[string]any{"ai1": map[string]any{"operation_id": "op"}}}, Logs: []any{map[string]any{"event": "site_submit", "version": "ai1", "operation_id": "op", "status": "submitted", "remote_receipt": map[string]any{"accepted": true}}}}
	if _, ok := workshopAcknowledgedPendingSubmission(d, "ai1"); !ok || workshopHasUnacceptedPending(d) {
		t.Fatal("internal ID hid accepted source-book acknowledgement")
	}
}
