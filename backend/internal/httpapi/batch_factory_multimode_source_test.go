package httpapi

import (
	"context"
	"errors"
	"qiantie/backend/internal/batchfactoryv11"
	"testing"
	"time"
)

func TestMultiModeOCRWaitDoesNotFetchBookstoreOrUseUnverifiedProcessingText(t *testing.T) {
	c := &BatchAutomationCoordinator{now: time.Now}
	_, err := c.prepareMultiModeSource(context.Background(), "alice", batchfactoryv11.CreateBookInput{BookID: "738579", SourceText: "only a short processed excerpt", SourceMetadata: map[string]any{"originalReadStrategy": "ocr_first", "rawSourceStatus": "unverified"}})
	if !errors.Is(err, batchfactoryv11.ErrMultiModeSourcePending) {
		t.Fatalf("OCR was bypassed or processed text trusted: %v", err)
	}
	entry := map[string]any{}
	writes := 0
	if !c.scheduleMultiModeSourceWait(entry, err, func() { writes++ }) || entry["status"] != "scheduled" || entry["retryRequested"] != true || writes != 1 {
		t.Fatalf("lost resumable wait: %+v", entry)
	}
}

func TestMultiModeCustomCopyRequiresVerifiedOriginal(t *testing.T) {
	c := &BatchAutomationCoordinator{}
	_, err := c.prepareMultiModeSource(context.Background(), "alice", batchfactoryv11.CreateBookInput{SourceText: "excerpt", SourceMetadata: map[string]any{"sourceMode": "custom_copy", "rawSourceStatus": "unverified"}})
	if err == nil || errors.Is(err, batchfactoryv11.ErrMultiModeSourcePending) {
		t.Fatalf("missing authoritative source was accepted: %v", err)
	}
}
