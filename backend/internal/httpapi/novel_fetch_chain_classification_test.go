package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/novelfetchworkshop"
)

func TestChainClassificationFailureNeverDefaultsOrRepeats(t *testing.T) {
	ctx := context.Background()
	store := novelfetchworkshop.NewMemoryStore()
	_ = store.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Original: "小说正文", Meta: map[string]any{}})
	genderCalls, styleCalls := 0, 0
	judge := func(context.Context, novelfetchworkshop.BookInfo) (string, error) {
		genderCalls++
		return "", context.DeadlineExceeded
	}
	provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
		styleCalls++
		return "unsafe-response DO_NOT_LEAK", nil
	})
	err := ensureWorkshopDocumentClassification(ctx, store, "alice", "123", judge, provider)
	if !errors.Is(err, errNovelFetchClassificationPending) {
		t.Fatalf("classification failure not held: %v", err)
	}
	doc, _ := store.GetDocument(ctx, "alice", "123")
	if !genderNeedsResolution(doc.Gender) || !styleNeedsResolution(doc.Meta) {
		t.Fatalf("invented classification: gender=%q style=%v", doc.Gender, doc.Meta["style"])
	}
	if firstText(doc.Meta["classifyError"]) == "" || firstText(doc.Meta["styleError"]) == "" || strings.Contains(firstText(doc.Meta["classifyError"]), "DO_NOT_LEAK") {
		t.Fatalf("missing safe failure: %#v", doc.Meta)
	}
	if novelFetchAutomationEligible(doc, time.Now().Add(time.Hour)) {
		t.Fatal("failed classification became automatic retry candidate")
	}
	_ = ensureWorkshopDocumentClassification(ctx, store, "alice", "123", judge, provider)
	if genderCalls != 1 || styleCalls != 1 {
		t.Fatalf("classification repeated without explicit retry: gender=%d style=%d", genderCalls, styleCalls)
	}
	resetNovelFetchAutomationRetryState(&doc)
	doc.Meta["status"], doc.Meta["queueState"] = "queued", "queued"
	_ = store.PutDocument(ctx, "alice", doc)
	goodJudge := func(context.Context, novelfetchworkshop.BookInfo) (string, error) {
		genderCalls++
		return `{"gender":"男频","confidence":0.01}`, nil
	}
	goodStyle := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
		styleCalls++
		return `{"style":"玄幻","confidence":0.01}`, nil
	})
	if err = ensureWorkshopDocumentClassification(ctx, store, "alice", "123", goodJudge, goodStyle); err != nil {
		t.Fatalf("manual retry did not recover: %v", err)
	}
	_ = ensureWorkshopDocumentClassification(ctx, store, "alice", "123", goodJudge, goodStyle)
	doc, _ = store.GetDocument(ctx, "alice", "123")
	if firstText(doc.Meta["classifyError"]) != "" {
		t.Fatalf("resolved classification retained old failure: %v", doc.Meta["classifyError"])
	}
	if doc.Gender != "男频" || doc.Meta["style"] != "玄幻" || genderCalls != 2 || styleCalls != 2 {
		t.Fatalf("valid low-confidence classification not reused: gender=%q meta=%#v calls=%d/%d", doc.Gender, doc.Meta, genderCalls, styleCalls)
	}
}

func TestChainWorkerSensitiveOffStillClassifiesStyle(t *testing.T) {
	ctx := context.Background()
	store := novelfetchworkshop.NewMemoryStore()
	_ = store.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", OriginalRaw: strings.Repeat("故事正文", 40), Gender: "男频", Meta: map[string]any{}})
	calls := 0
	worker := workshopProcessWorker{store: store, queue: novelfetchworkshop.NewMemoryQueue(), registry: newWorkshopProcessRegistry(), resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
			calls++
			return `{"style":"玄幻"}`, nil
		}), nil
	}}
	if err := worker.execute(ctx, novelfetchworkshop.ProcessJob{ID: "style-off", Owner: "alice", BookIDs: []string{"123"}, SensitiveAIEnabled: false}); err != nil {
		t.Fatal(err)
	}
	doc, _ := store.GetDocument(ctx, "alice", "123")
	if calls != 1 || doc.Meta["style"] != "玄幻" || doc.Meta["styleSource"] != "ai" {
		t.Fatalf("sensitive toggle bypassed style AI: calls=%d style=%v source=%v", calls, doc.Meta["style"], doc.Meta["styleSource"])
	}
}

func TestChainGenerateOnlyClassificationFailureBlocksRewrite(t *testing.T) {
	for _, unresolved := range []string{"gender", "style"} {
		t.Run(unresolved, func(t *testing.T) {
			ctx := context.Background()
			store := admissionTestStore(t)
			doc, _ := store.GetDocument(ctx, "alice", "123")
			if unresolved == "gender" {
				doc.Gender = ""
				doc.Meta["style"] = "玄幻"
			} else {
				delete(doc.Meta, "style")
				doc.Gender = "男频"
			}
			_ = store.PutDocument(ctx, "alice", doc)
			classificationCalls, rewriteCalls := 0, 0
			worker := workshopProcessWorker{store: store, queue: novelfetchworkshop.NewMemoryQueue(), registry: newWorkshopProcessRegistry(), resolveGenderJudge: func(context.Context, string) novelfetchworkshop.AIGenderJudge {
				return func(context.Context, novelfetchworkshop.BookInfo) (string, error) {
					classificationCalls++
					return `{"gender":"未知"}`, nil
				}
			}, resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
				return rewriteProviderFunc(func(_ context.Context, r novelfetchworkshop.RewriteRequest) (string, error) {
					if strings.Contains(r.SystemPrompt, "小说分类器") {
						classificationCalls++
						return `{"style":"无效"}`, nil
					}
					rewriteCalls++
					return "改后正文", nil
				}), nil
			}}
			job := novelfetchworkshop.ProcessJob{ID: "generate-missing-label", Owner: "alice", BookIDs: []string{"123"}, GenerateOnly: true, SelectedVersions: []string{"ai1"}}
			err := worker.execute(ctx, job)
			if !errors.Is(err, errNovelFetchClassificationPending) || rewriteCalls != 0 || classificationCalls != 1 {
				t.Fatalf("missing classification reached rewrite: err=%v classifiers=%d rewrites=%d", err, classificationCalls, rewriteCalls)
			}
			if _, err = store.GetBody(ctx, "alice", "123", "ai1"); err == nil {
				t.Fatal("blocked generation saved an AI body")
			}
			_ = worker.execute(ctx, job)
			if rewriteCalls != 0 || classificationCalls != 1 {
				t.Fatalf("reclaimed generation repeated classification: %d/%d", classificationCalls, rewriteCalls)
			}
		})
	}
}

func TestChainGenerateOnlyReusesValidLabels(t *testing.T) {
	ctx := context.Background()
	store := admissionTestStore(t)
	doc, _ := store.GetDocument(ctx, "alice", "123")
	doc.Gender = "女频"
	doc.GenderConfidence = 0.01
	doc.Meta["style"] = "现代甜文"
	doc.Meta["styleConfidence"] = 0.01
	_ = store.PutDocument(ctx, "alice", doc)
	calls := 0
	worker := workshopProcessWorker{store: store, queue: novelfetchworkshop.NewMemoryQueue(), registry: newWorkshopProcessRegistry(), resolveGenderJudge: func(context.Context, string) novelfetchworkshop.AIGenderJudge {
		return func(context.Context, novelfetchworkshop.BookInfo) (string, error) {
			t.Fatal("valid gender rejudged")
			return "", nil
		}
	}, resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		return rewriteProviderFunc(func(_ context.Context, r novelfetchworkshop.RewriteRequest) (string, error) {
			if strings.Contains(r.SystemPrompt, "小说分类器") {
				t.Fatal("valid style rejudged")
			}
			calls++
			return "改后首行", nil
		}), nil
	}}
	if err := worker.execute(ctx, novelfetchworkshop.ProcessJob{ID: "valid-generate", Owner: "alice", BookIDs: []string{"123"}, GenerateOnly: true, SelectedVersions: []string{"ai1"}}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("rewrite calls=%d", calls)
	}
}

func TestChainGenerateAdmissionPreservesSensitiveSnapshotAndNeverUploads(t *testing.T) {
	ctx := context.Background()
	store := admissionTestStore(t)
	doc, _ := store.GetDocument(ctx, "alice", "123")
	doc.Meta["processSnapshot"] = map[string]any{"sensitive_ai_enabled": true, "selected_versions": []string{"ai1"}, "auto_submit": true}
	_ = store.PutDocument(ctx, "alice", doc)
	queue := &admissionRecordingQueue{MemoryQueue: novelfetchworkshop.NewMemoryQueue(), jobs: make(chan novelfetchworkshop.ProcessJob, 2)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/novel-fetch-workshop/tasks/{bookId}/generate-ai", func(w http.ResponseWriter, r *http.Request) { queueWorkshopGenerate(w, r, store, queue, 1) })
	response := admissionGenerate(mux)
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	select {
	case job := <-queue.jobs:
		if !job.GenerateOnly || !job.SensitiveAIEnabled || job.AutoSubmit {
			t.Fatalf("lost generation intent/sensitive snapshot: %+v", job)
		}
	default:
		t.Fatal("no job")
	}
}

func TestChainClassificationPersistsOneFailureAcrossNewJobIDs(t *testing.T) {
	ctx := context.Background()
	s := novelfetchworkshop.NewMemoryStore()
	_ = s.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Original: "原文", Gender: "男频", Meta: map[string]any{}})
	calls := 0
	p := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
		calls++
		return `{"style":"invalid"}`, nil
	})
	if err := ensureWorkshopDocumentClassification(ctx, s, "alice", "123", nil, p); !errors.Is(err, errNovelFetchClassificationPending) {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := scheduleNovelFetchDocumentRetry(ctx, s, "alice", "123", novelFetchRetryStageClassify, errNovelFetchClassificationPending, time.Now()); err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetDocument(ctx, "alice", "123")
	state := novelFetchRetrySchedule(d.Meta)[novelFetchRetryStageClassify]
	if intValue(state["attempt"]) != 1 || !novelFetchRetryStateIsManual(state) {
		t.Fatalf("classification failure not counted exactly once: %#v", state)
	}
	d.Meta["status"], d.Meta["queueState"], d.Meta["currentProcessJobId"] = "queued", "queued", "new-scanner-job"
	_ = s.PutDocument(ctx, "alice", d)
	_ = ensureWorkshopDocumentClassification(ctx, s, "alice", "123", nil, p)
	if calls != 1 {
		t.Fatalf("new job ID reset classification round: calls=%d", calls)
	}
	resetNovelFetchAutomationRetryState(&d)
	d.Meta["status"], d.Meta["queueState"] = "queued", "queued"
	_ = s.PutDocument(ctx, "alice", d)
	_ = ensureWorkshopDocumentClassification(ctx, s, "alice", "123", nil, p)
	if calls != 2 {
		t.Fatalf("explicit retry failed to reopen round: calls=%d", calls)
	}
}

func TestChainClassificationResolverFailureIsVisibleAndSafe(t *testing.T) {
	ctx := context.Background()
	s := admissionTestStore(t)
	d, _ := s.GetDocument(ctx, "alice", "123")
	delete(d.Meta, "style")
	_ = s.PutDocument(ctx, "alice", d)
	worker := workshopProcessWorker{store: s, queue: novelfetchworkshop.NewMemoryQueue(), registry: newWorkshopProcessRegistry(), resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		return nil, errors.New("HTTP 401 DO_NOT_LEAK")
	}}
	err := worker.execute(ctx, novelfetchworkshop.ProcessJob{ID: "bad-provider", Owner: "alice", BookIDs: []string{"123"}, GenerateOnly: true, SelectedVersions: []string{"ai1"}})
	d, _ = s.GetDocument(ctx, "alice", "123")
	if !errors.Is(err, errNovelFetchClassificationPending) || !styleNeedsResolution(d.Meta) || firstText(d.Meta["styleError"]) == "" || strings.Contains(firstText(d.Meta["classifyError"]), "DO_NOT_LEAK") {
		t.Fatalf("resolver error bypassed safe classification failure: err=%v meta=%#v", err, d.Meta)
	}
}

func TestChainGenerateOnlyKnownSensitiveFailureBlocksRewrite(t *testing.T) {
	for _, state := range []string{"waiting_ai", "processing", "failed", "waiting_sensitive_ai"} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			s := admissionTestStore(t)
			d, _ := s.GetDocument(ctx, "alice", "123")
			d.Meta["sensitiveStatus"] = state
			_ = s.PutDocument(ctx, "alice", d)
			calls := 0
			worker := workshopProcessWorker{store: s, queue: novelfetchworkshop.NewMemoryQueue(), registry: newWorkshopProcessRegistry(), resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
				return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
					calls++
					return "改后首行", nil
				}), nil
			}}
			err := worker.execute(ctx, novelfetchworkshop.ProcessJob{ID: "sensitive-held", Owner: "alice", BookIDs: []string{"123"}, GenerateOnly: true, SelectedVersions: []string{"ai1"}})
			if err == nil || calls != 0 {
				t.Fatalf("known failed sensitive stage reached paid rewrite: err=%v calls=%d", err, calls)
			}
			d, _ = s.GetDocument(ctx, "alice", "123")
			body, _ := novelfetchworkshop.ReadBodyText(ctx, s, "alice", d, "original")
			if body != "首行原文" {
				t.Fatal("sensitive gate changed the saved original")
			}
		})
	}
}

type chainClassificationReadFailureStore struct {
	novelfetchworkshop.Store
	err error
}

func (s chainClassificationReadFailureStore) GetDocument(context.Context, string, string) (novelfetchworkshop.Document, error) {
	return novelfetchworkshop.Document{}, s.err
}

func TestChainClassificationRetryDeduplicationPreservesErrorsAndOtherStages(t *testing.T) {
	ctx := context.Background()
	s := novelfetchworkshop.NewMemoryStore()
	now := time.Now()
	_ = s.PutDocument(ctx, "alice", novelfetchworkshop.Document{BookID: "123", Meta: map[string]any{}})
	if err := scheduleNovelFetchDocumentRetry(ctx, s, "alice", "123", novelFetchRetryStageClassify, errNovelFetchClassificationPending, now); err != nil {
		t.Fatal(err)
	}
	if err := scheduleNovelFetchDocumentRetry(ctx, s, "alice", "123", novelFetchRetryStageClassify, errNovelFetchClassificationPending, now); err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetDocument(ctx, "alice", "123")
	if intValue(novelFetchRetrySchedule(d.Meta)[novelFetchRetryStageClassify]["attempt"]) != 1 {
		t.Fatal("worker counted the same classification failure twice")
	}
	failure := errors.New("storage read unavailable")
	if err := scheduleNovelFetchDocumentRetry(ctx, chainClassificationReadFailureStore{Store: s, err: failure}, "alice", "123", novelFetchRetryStageClassify, errNovelFetchClassificationPending, now); !errors.Is(err, failure) {
		t.Fatalf("read failure was swallowed: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := scheduleNovelFetchDocumentRetry(ctx, s, "alice", "123", novelFetchRetryStageRewrite, errors.New("temporary service failure"), now); err != nil {
			t.Fatal(err)
		}
	}
	d, _ = s.GetDocument(ctx, "alice", "123")
	if intValue(novelFetchRetrySchedule(d.Meta)[novelFetchRetryStageRewrite]["attempt"]) != 2 {
		t.Fatal("classification deduplication suppressed another stage")
	}
}

func TestChainSensitiveManualHoldBlocksFullUploadUntilRepair(t *testing.T) {
	ctx := context.Background()
	s := admissionTestStore(t)
	if _, err := s.PutBody(ctx, "alice", novelfetchworkshop.BodyRecord{BookID: "123", BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai1"}, Content: "已保存的旧AI正文"}); err != nil {
		t.Fatal(err)
	}
	d, _ := s.GetDocument(ctx, "alice", "123")
	d.Meta["autoSubmitReady"], d.Meta["autoSubmitReadyVersions"] = true, []string{"ai1"}
	d.Meta["autoSubmitReadyFingerprint"] = novelFetchUploadReadyFingerprint(ctx, s, "alice", d, []string{"ai1"})
	d.Meta["sensitiveManualRetryRequired"], d.Meta["sensitiveStatus"], d.Meta["sensitiveError"] = true, "failed", "敏感AI服务失败"
	_ = s.PutDocument(ctx, "alice", d)
	resolves, calls, uploads := 0, 0, 0
	worker := workshopProcessWorker{store: s, queue: novelfetchworkshop.NewMemoryQueue(), registry: newWorkshopProcessRegistry(), resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		resolves++
		return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
			calls++
			return "改后首行", nil
		}), nil
	}, autoSubmit: func(context.Context, novelfetchworkshop.ProcessJob) error { uploads++; return nil }}
	job := novelfetchworkshop.ProcessJob{ID: "manual-sensitive-hold", Owner: "alice", BookIDs: []string{"123"}, AutoSubmit: true, SelectedVersions: []string{"ai1"}}
	worker.process(ctx, job)
	d, _ = s.GetDocument(ctx, "alice", "123")
	if calls != 0 || resolves != 0 || uploads != 0 || d.Meta["manualOnly"] == true {
		t.Fatalf("manual-sensitive marker did not block reuse before models/upload: resolves=%d models=%d uploads=%d manualOnly=%v", resolves, calls, uploads, d.Meta["manualOnly"])
	}
	old, _ := s.GetBody(ctx, "alice", "123", "ai1")
	if old.Content != "已保存的旧AI正文" {
		t.Fatal("manual-sensitive hold replaced old AI")
	}
	// This is the state committed by a later successful explicit sensitive repair.
	d.Meta["sensitiveManualRetryRequired"], d.Meta["sensitiveStatus"] = false, "done"
	delete(d.Meta, "sensitiveError")
	_ = s.PutDocument(ctx, "alice", d)
	job.ID = "after-successful-sensitive-repair"
	worker.process(ctx, job)
	if uploads != 1 || calls != 0 {
		t.Fatalf("cleared marker did not resume stored ready upload: models=%d uploads=%d", calls, uploads)
	}
}

func TestChainSensitiveManualHoldDoesNotLatchGenerateManualOnly(t *testing.T) {
	ctx := context.Background()
	s := admissionTestStore(t)
	d, _ := s.GetDocument(ctx, "alice", "123")
	d.Meta["sensitiveManualRetryRequired"], d.Meta["sensitiveStatus"] = true, "failed"
	_ = s.PutDocument(ctx, "alice", d)
	calls := 0
	worker := workshopProcessWorker{store: s, queue: novelfetchworkshop.NewMemoryQueue(), registry: newWorkshopProcessRegistry(), resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
			calls++
			return "改后首行", nil
		}), nil
	}}
	job := novelfetchworkshop.ProcessJob{ID: "single-sensitive-hold", Owner: "alice", BookIDs: []string{"123"}, GenerateOnly: true, SelectedVersions: []string{"ai1"}}
	if err := worker.execute(ctx, job); !errors.Is(err, errWorkshopRetryHeld) {
		t.Fatalf("manual sensitive hold lost: %v", err)
	}
	d, _ = s.GetDocument(ctx, "alice", "123")
	if calls != 0 || d.Meta["manualOnly"] == true {
		t.Fatalf("manual repair marker latched a separate hold: calls=%d manualOnly=%v", calls, d.Meta["manualOnly"])
	}
	d.Meta["sensitiveManualRetryRequired"], d.Meta["sensitiveStatus"] = false, "done"
	_ = s.PutDocument(ctx, "alice", d)
	job.ID = "single-after-sensitive-repair"
	if err := worker.execute(ctx, job); err != nil || calls != 1 {
		t.Fatalf("successful sensitive repair did not reopen generation: err=%v calls=%d", err, calls)
	}
}
