package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"qiantie/backend/internal/novelfetchworkshop"
	"testing"
	"time"
)

func TestChainExplicitEmptyVersionsNeverAutoUpload(t *testing.T) {
	ctx := context.Background()
	s := novelfetchworkshop.NewMemoryStore()
	d := readyIndependentUploadDocument("empty-selection")
	d.Versions["ai1"] = "她站在门边，终于鼓起勇气说出真相。"
	if err := s.PutDocument(ctx, "tester", d); err != nil {
		t.Fatal(err)
	}
	uploads := 0
	w := &workshopProcessWorker{store: s, queue: novelfetchworkshop.NewMemoryQueue(), registry: newWorkshopProcessRegistry(), autoSubmit: func(context.Context, novelfetchworkshop.ProcessJob) error { uploads++; return nil }}
	w.process(ctx, novelfetchworkshop.ProcessJob{ID: "explicit-empty", Owner: "tester", BatchID: "batch", BookIDs: []string{d.BookID}, AutoSubmit: true, SelectedVersions: []string{}})
	if uploads != 0 {
		t.Fatalf("explicit empty selection uploaded %d books", uploads)
	}
}

func TestChainExplicitEmptyUploadRejectedBeforeSessionOrConfig(t *testing.T) {
	_, err := executeWebSubmit(context.Background(), "tester", t.TempDir(), novelfetchworkshop.NewMemoryStore(), webSubmitRequest{TaskIDs: []string{"book"}, Versions: []string{}}, nil)
	var requestErr *webSubmitRequestError
	if !errors.As(err, &requestErr) || requestErr.Status != http.StatusBadRequest || requestErr.Code != "UPLOAD_VERSIONS_REQUIRED" {
		t.Fatalf("explicit empty selection must fail before choosing defaults: %v", err)
	}
}

func TestChainExplicitEmptyPreviewNeverFallsBackToAI1(t *testing.T) {
	s := novelfetchworkshop.NewMemoryStore()
	payload, status, _ := buildWebSubmitPlanPayload(context.Background(), s, "tester", []string{"book"}, []string{}, false)
	if status != http.StatusBadRequest || payload != nil {
		t.Fatalf("empty selection became an upload plan: status=%d payload=%v", status, payload)
	}
	_, status, _ = buildWebSubmitPlanPayload(context.Background(), s, "tester", []string{"book"}, nil, false)
	if status != http.StatusOK {
		t.Fatalf("omitted legacy versions should still resolve defaults: %d", status)
	}
}

func TestChainTwentyRequestedVersionsAreAllCandidates(t *testing.T) {
	s := novelfetchworkshop.NewMemoryStore()
	versions := []string{}
	for i := 1; i <= 20; i++ {
		versions = append(versions, fmt.Sprintf("ai%d", i))
	}
	got, err := missingWorkshopAIBodies(context.Background(), s, "tester", novelfetchworkshop.Document{BookID: "book"}, versions)
	if err != nil || len(got) != 20 || got[19] != "ai20" {
		t.Fatalf("twenty selected versions became %v (%v)", got, err)
	}
	canonical, err := normalizeNovelFetchAIVersions([]string{"AI00020", "ai020", "original"})
	if err != nil || len(canonical) != 2 || canonical[0] != "ai20" {
		t.Fatalf("bounded aliases rejected: %v %v", canonical, err)
	}
	for _, v := range []string{"ai21", "ai0", "ai-1", "ai99999999999999999999"} {
		if _, err := normalizeNovelFetchAIVersions([]string{v}); err == nil {
			t.Fatalf("unbounded version accepted: %s", v)
		}
	}
}

func TestChainSensitiveManualHoldIsNotAnAutomationCandidate(t *testing.T) {
	d := novelfetchworkshop.Document{BookID: "held", OriginalRawAvailable: true, Meta: map[string]any{"status": "completed", "originalStatus": "done", "siteSubmitStatus": "pending_upload", "sensitiveStatus": "failed", "sensitiveManualRetryRequired": true}}
	if novelFetchAutomationEligible(d, time.Now()) {
		t.Fatal("explicit sensitive failure was queued automatically")
	}
	delete(d.Meta, "sensitiveManualRetryRequired")
	if !novelFetchAutomationEligible(d, time.Now()) {
		t.Fatal("successful repair did not release the scoped scheduling hold")
	}
}

func TestChainSensitiveManualHoldBlocksSubmissionAndPreview(t *testing.T) {
	ctx := context.Background()
	s := novelfetchworkshop.NewMemoryStore()
	dir := t.TempDir()
	s.PutConfig(ctx, "tester", map[string]any{"appConfig": map[string]any{"web_submit": map[string]any{"enabled": true, "min_file_size": 0, "organization": "org-1"}}})
	d := readyIndependentUploadDocument("12345678")
	d.Meta["platformId"] = "2"
	d.Meta["sensitiveManualRetryRequired"] = true
	s.PutDocument(ctx, "tester", d)
	s.PutBody(ctx, "tester", novelfetchworkshop.BodyRecord{BookID: d.BookID, BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai1", State: "ready"}, Content: "她推开书店的门，与旧友再次重逢。"})
	if err := saveUploadTargetSession(dir, "tester", map[string]any{"cookie": "fixture", "targetUsername": "external"}); err != nil {
		t.Fatal(err)
	}
	target := &configuredSubmitConnector{}
	result, err := executeWebSubmit(ctx, "tester", dir, s, webSubmitRequest{TaskIDs: []string{d.BookID}, Versions: []string{"ai1"}}, nil, target)
	if err != nil {
		t.Fatal(err)
	}
	if target.submitCalls != 0 || len(result.Results) != 1 || mapOrEmpty(result.Results[0])["reason"] != "sensitive_repair_required" {
		t.Fatalf("failed repair allowed dispatch: calls=%d results=%v", target.submitCalls, result.Results)
	}
	payload, status, _ := buildWebSubmitPlanPayload(ctx, s, "tester", []string{d.BookID}, []string{"ai1"}, false)
	if status != http.StatusOK || len(arrayOrEmpty(payload["groups"])) != 0 || len(arrayOrEmpty(payload["deferred_items"])) != 1 {
		t.Fatalf("failed repair still planned: %v %d", payload, status)
	}
	if mapOrEmpty(arrayOrEmpty(payload["deferred_items"])[0])["reason"] != "sensitive_repair_required" {
		t.Fatalf("wrong reason: %v", payload)
	}
}

func TestChainVersionSelectionKeepsOmittedAndExplicitEmptyDistinct(t *testing.T) {
	explicit := novelfetchworkshop.ProcessJob{SelectedVersions: []string{}, AutoSubmit: true}
	data, err := json.Marshal(explicit)
	if err != nil {
		t.Fatal(err)
	}
	var restored novelfetchworkshop.ProcessJob
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.SelectedVersions == nil {
		t.Fatal("queue serialization erased the explicit empty selection")
	}
	ctx := context.Background()
	s := novelfetchworkshop.NewMemoryStore()
	s.PutDocument(ctx, "tester", novelfetchworkshop.Document{BookID: "legacy"})
	uploads := 0
	w := &workshopProcessWorker{store: s, queue: novelfetchworkshop.NewMemoryQueue(), registry: newWorkshopProcessRegistry(), autoSubmit: func(context.Context, novelfetchworkshop.ProcessJob) error { uploads++; return nil }}
	w.process(ctx, novelfetchworkshop.ProcessJob{ID: "legacy-omitted", Owner: "tester", BatchID: "batch", BookIDs: []string{"legacy"}, AutoSubmit: true})
	if uploads != 1 {
		t.Fatalf("omitted legacy versions lost existing default upload behavior: %d", uploads)
	}
}

func TestChainProcessStartDistinguishesOmittedAndEmptyVersions(t *testing.T) {
	now := time.Now()
	secret := "chain-fixture"
	store := novelfetchworkshop.NewMemoryStore()
	q := &uniqueRecordingNovelFetchQueue{}
	handler := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: store, NovelFetchQueue: q})
	for i, explicit := range []bool{false, true} {
		input := map[string]any{"inputText": fmt.Sprintf("1234567890%d\t书名", i), "platform_id": "2"}
		if explicit {
			input["selected_versions"] = []string{}
		}
		res := doNovelFetchRequest(t, handler, http.MethodPost, "/api/novel-fetch-workshop/process/start", input, now, secret)
		if res.Code != http.StatusAccepted || len(q.jobs) != i+1 {
			t.Fatalf("start status=%d jobs=%d body=%s", res.Code, len(q.jobs), res.Body.String())
		}
		if q.jobs[i].AutoSubmit == explicit {
			t.Fatalf("omitted=%v autoSubmit=%v", !explicit, q.jobs[i].AutoSubmit)
		}
	}
}
