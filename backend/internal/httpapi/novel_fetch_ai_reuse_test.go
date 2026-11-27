package httpapi

import (
	"context"
	"testing"

	"qiantie/backend/internal/novelfetchworkshop"
)

func TestAIReuseCandidatesAndRetryUseValidProse(t *testing.T) {
	ctx := context.Background()
	s := admissionTestStore(t)
	s.PutBody(ctx, "alice", novelfetchworkshop.BodyRecord{BookID: "123", BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai1", State: "ready"}, Content: "```json\n{}\n```"})
	s.PutBody(ctx, "alice", novelfetchworkshop.BodyRecord{BookID: "123", BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai2", State: "ready"}, Content: "有效第二版正文。"})
	d, _ := s.GetDocument(ctx, "alice", "123")
	missing, err := missingWorkshopAIBodies(ctx, s, "alice", d, []string{"ai1", "ai2"})
	if err != nil || len(missing) != 1 || missing[0] != "ai1" {
		t.Fatalf("invalid old AI omitted from candidates: %v %v", missing, err)
	}
	if novelFetchVersionsGenerated(ctx, s, "alice", d, []string{"ai1"}) {
		t.Fatal("manual retry falsely skips invalid AI")
	}
	d.Meta["siteSubmitDoneVersions"] = []string{"ai1"}
	missing, err = missingWorkshopAIBodies(ctx, s, "alice", d, []string{"ai1"})
	if err != nil || len(missing) != 0 || !novelFetchVersionsGenerated(ctx, s, "alice", d, []string{"ai1"}) {
		t.Fatal("confirmed historical version not protected")
	}
}

func TestAIReuseWorkerRegeneratesOnlyUnconfirmedInvalidBody(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		ctx := context.Background()
		s := admissionTestStore(t)
		d, _ := s.GetDocument(ctx, "alice", "123")
		d.Gender = "女频"
		d.GenderSource = novelfetchworkshop.GenderManual
		d.Meta["style"] = "现代甜文"
		d.Meta["styleStatus"] = "done"
		d.Meta["styleSource"] = "manual"
		if confirmed {
			d.Meta["siteSubmitDoneVersions"] = []string{"ai1"}
		}
		s.PutDocument(ctx, "alice", d)
		old := "```json\n{}\n```"
		s.PutBody(ctx, "alice", novelfetchworkshop.BodyRecord{BookID: "123", BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai1", State: "ready"}, Content: old})
		calls := 0
		w := workshopProcessWorker{store: s, queue: novelfetchworkshop.NewMemoryQueue(), registry: newWorkshopProcessRegistry(), resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
			return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
				calls++
				return "新的有效改写正文。", nil
			}), nil
		}}
		job := novelfetchworkshop.ProcessJob{ID: "reuse-audit", Owner: "alice", BookIDs: []string{"123"}, SelectedVersions: []string{"ai1"}, AutoSubmit: false}
		if err := w.execute(ctx, job); err != nil {
			t.Fatal(err)
		}
		body, err := s.GetBody(ctx, "alice", "123", "ai1")
		if err != nil {
			t.Fatal(err)
		}
		if confirmed {
			if calls != 0 || body.Content != old {
				t.Fatalf("confirmed body replaced: calls=%d content=%q", calls, body.Content)
			}
		} else if calls != 1 || body.Content != "新的有效改写正文。" {
			t.Fatalf("invalid old body not replaced: calls=%d content=%q", calls, body.Content)
		}
	}
}

func TestAIReuseUploadReadyFingerprintCannotBypassValidation(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		ctx := context.Background()
		s := admissionTestStore(t)
		d, _ := s.GetDocument(ctx, "alice", "123")
		d.Gender = "女频"
		d.GenderSource = novelfetchworkshop.GenderManual
		d.Meta["style"] = "现代甜文"
		d.Meta["styleStatus"] = "done"
		d.Meta["styleSource"] = "manual"
		if confirmed {
			d.Meta["siteSubmitDoneVersions"] = []string{"ai1"}
		}
		s.PutDocument(ctx, "alice", d)
		old := "```json\n{}\n```"
		s.PutBody(ctx, "alice", novelfetchworkshop.BodyRecord{BookID: "123", BodyRef: novelfetchworkshop.BodyRef{VersionID: "ai1", State: "ready"}, Content: old})
		d, _ = s.GetDocument(ctx, "alice", "123")
		d.Meta["autoSubmitReady"] = true
		d.Meta["autoSubmitReadyVersions"] = []string{"ai1"}
		d.Meta["autoSubmitReadyFingerprint"] = novelFetchUploadReadyFingerprint(ctx, s, "alice", d, []string{"ai1"})
		s.PutDocument(ctx, "alice", d)
		calls := 0
		w := workshopProcessWorker{store: s, queue: novelfetchworkshop.NewMemoryQueue(), registry: newWorkshopProcessRegistry(), resolveRewriteProvider: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
			return rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
				calls++
				return "新的有效改写正文。", nil
			}), nil
		}}
		job := novelfetchworkshop.ProcessJob{ID: "reuse-upload-ready", Owner: "alice", BookIDs: []string{"123"}, SelectedVersions: []string{"ai1"}, AutoSubmit: true}
		if err := w.execute(ctx, job); err != nil {
			t.Fatal(err)
		}
		body, _ := s.GetBody(ctx, "alice", "123", "ai1")
		if confirmed {
			if calls != 0 || body.Content != old {
				t.Fatal("confirmed version was regenerated")
			}
		} else if calls != 1 || body.Content != "新的有效改写正文。" {
			t.Fatalf("matching stale readiness fingerprint bypassed validation: calls=%d body=%q", calls, body.Content)
		}
	}
}
