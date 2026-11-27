package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	txt "qiantie/backend/internal/batchfactorytext"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/localartifact"
	"sync/atomic"
	"testing"
	"time"
)

func TestTextLocalRecoveryHasPersistentBoundAndNeverRetriesUnknownPublish(t *testing.T) {
	now := time.Unix(1700000000, 0)
	entry := map[string]any{}
	for i := 0; i < 3; i++ {
		if !scheduleTextLocalRecovery(entry, "text_collection", context.DeadlineExceeded, now) {
			t.Fatalf("retry %d not scheduled", i)
		}
		if entry["textRecoveryAttempts"] != i+1 || entry["needsManual"] == true || entry["retryAt"] != now.Add(30*time.Second*time.Duration(1<<i)) {
			t.Fatalf("entry=%+v", entry)
		}
		raw, _ := json.Marshal(entry)
		if err := json.Unmarshal(raw, &entry); err != nil {
			t.Fatal(err)
		}
	}
	if scheduleTextLocalRecovery(entry, "text_validate", context.DeadlineExceeded, now) {
		t.Fatal("retry budget reset across reload")
	}
	for _, stage := range []string{"text_process", "text_publish"} {
		fresh := map[string]any{}
		if scheduleTextLocalRecovery(fresh, stage, context.DeadlineExceeded, now) || len(fresh) != 0 {
			t.Fatalf("unknown remote call repeated at %s", stage)
		}
	}
	if scheduleTextLocalRecovery(map[string]any{}, "text_render", errors.New("invalid font"), now) {
		t.Fatal("deterministic invalid configuration retried")
	}
	if !scheduleTextLocalRecovery(map[string]any{}, "text_collection", txt.ErrConflict, now) {
		t.Fatal("local revision conflict not retried")
	}
}

func TestTextWorkflowStageBudgetsLeaveSeparateTimeForUpload(t *testing.T) {
	if automationTextStageTimeout("text_render") < 35*time.Minute || automationTextStageTimeout("text_publish") != 45*time.Minute || automationTextStageTimeout("text_validate") != 10*time.Minute {
		t.Fatal("wrong phase budget")
	}
}
func TestTextLocalRecoveryManualRetryResetsOnlySelectedBudget(t *testing.T) {
	selected := map[string]any{"status": "failed", "stage": "text_collection", "textRecoveryAttempts": 3, "needsManual": true, "textPendingPublishCollectionId": "saved"}
	other := map[string]any{"status": "failed", "stage": "text_collection", "textRecoveryAttempts": 2}
	resetRetryableAutomationBooks(map[string]any{"one": selected, "two": other}, map[string]bool{"one": true}, time.Now())
	if selected["textRecoveryAttempts"] != nil || other["textRecoveryAttempts"] != 2 || selected["textPendingPublishCollectionId"] != "saved" {
		t.Fatalf("selected=%v other=%v", selected, other)
	}
}

func TestTextLocalRecoveryDispatchesSavedRetry(t *testing.T) {
	var clock atomic.Int64
	clock.Store(1700000000)
	now := func() time.Time { return time.Unix(clock.Load(), 0) }
	store := batchfactoryv11.NewMemoryStore()
	batch := preflightComicBatch(t, store, 1, 0)
	batch = savePreflightBatchSettings(t, store, batch, batchfactoryv11.SettingsPatch{"productionMode": json.RawMessage(`"text"`)})
	presets, err := NewFileAutomationPresetStore(filepath.Join(t.TempDir(), "presets.json"), now)
	if err != nil {
		t.Fatal(err)
	}
	preset, err := presets.Create("alice", "text recovery", map[string]any{"productionMode": "text", "sensitiveMode": "off"})
	if err != nil {
		t.Fatal(err)
	}
	coord := NewBatchAutomationCoordinator(store, presets, &batchfactoryv11.BookStageService{Store: store}, nil, nil, nil, nil, NewFileAutomationStatusReader(filepath.Join(t.TempDir(), "state.json")), now)
	var calls atomic.Int32
	coord.textEnsure = func(context.Context, txt.EnsureRequest) (txt.EnsureResult, error) {
		if calls.Add(1) == 1 {
			return txt.EnsureResult{Stage: "text_collection"}, context.DeadlineExceeded
		}
		return txt.EnsureResult{Stage: "text_collection", Collection: txt.Collection{ID: "reused-images"}}, nil
	}
	if _, err = coord.Start(context.Background(), "alice", batch.ID, map[string]any{"presetId": preset.ID, "productionMode": "text", "runMode": "video_no_submit"}); err != nil {
		t.Fatal(err)
	}
	var first map[string]any
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); {
		first = coord.loadJobForTest(t, "alice", batch.ID)
		e := mapOrEmpty(mapOrEmpty(first["books"])[batch.Books[0].ID])
		if e["status"] == "scheduled" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	entry := mapOrEmpty(mapOrEmpty(first["books"])[batch.Books[0].ID])
	if entry["status"] != "scheduled" || !automationBoolValue(entry["retryRequested"]) {
		t.Fatalf("retry not saved: %v", entry)
	}
	clock.Add(31)
	if err = coord.DispatchDueJobs(context.Background()); err != nil {
		t.Fatal(err)
	}
	last := waitForTerminalAutomationJob(t, coord, "alice", batch.ID)
	entry = mapOrEmpty(mapOrEmpty(last["books"])[batch.Books[0].ID])
	if calls.Load() != 2 || entry["status"] != "completed" || entry["textCollectionId"] != "reused-images" {
		t.Fatalf("calls=%d entry=%v", calls.Load(), entry)
	}
}

type timeoutTextArtifactRestorer struct{}

func (timeoutTextArtifactRestorer) RestoreStorageRef(context.Context, string) error {
	return context.DeadlineExceeded
}
func TestTextArtifactReadTimeoutDoesNotRegenerateSavedImage(t *testing.T) {
	files := localartifact.NewStore(t.TempDir(), 1<<20)
	files.SetMissingRestorer(timeoutTextArtifactRestorer{})
	a := textAPI{files: files}
	err := a.verifyArtifactFile(textRequest{Context: context.Background()}, txt.PageArtifact{ArtifactFile: txt.ArtifactFile{StorageRef: "saved.png"}}, "第1页")
	if !errors.Is(err, localartifact.ErrRestoreTimeout) || errors.Is(err, txt.ErrNotReady) {
		t.Fatalf("transient read was mistaken for missing image: %v", err)
	}
	if !scheduleTextLocalRecovery(map[string]any{}, "text_validate", err, time.Now()) {
		t.Fatalf("read timeout cannot resume: %v", err)
	}
}

func TestTextUploadDeadlineIsNotPreemptedByGenericWatchdog(t *testing.T) {
	last := time.Now()
	for _, stage := range []string{"text_render", "text_publish"} {
		if automationProgressStalledInStage(last, last.Add(41*time.Minute), stage) {
			t.Fatalf("%s killed before its own deadline", stage)
		}
		if !automationProgressStalledInStage(last, last.Add(46*time.Minute), stage) {
			t.Fatalf("%s can stall forever", stage)
		}
	}
	if !automationProgressStalledInStage(last, last.Add(41*time.Minute), "comic_images") {
		t.Fatal("other modes watchdog changed")
	}
}
