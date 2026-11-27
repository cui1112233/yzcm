package novelfetchworkshop

import "testing"

func TestDeriveTaskStatusDoesNotTreatVersionMetadataAsAIText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
	}{
		{"failed metadata", map[string]any{"state": "failed", "error": "provider rejected"}},
		{"ready metadata", map[string]any{"state": "ready", "revision": 2}},
		{"boolean", true},
		{"number", 12},
		{"text boolean", map[string]any{"text": true}},
		{"text object", map[string]any{"text": map[string]any{"state": "ready"}}},
		{"whitespace", map[string]any{"text": " \n\t "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := Document{Meta: map[string]any{"originalStatus": "done"}, Versions: map[string]any{"ai1": tc.value}}
			if got := DeriveTaskStatus(doc); got != TaskStatusInputReady {
				t.Fatalf("metadata became AI body: status=%q value=%#v", got, tc.value)
			}
		})
	}
}

func TestDeriveTaskStatusAcceptsExplicitLegacyAIText(t *testing.T) {
	for _, value := range []any{"真正的AI正文", map[string]any{"text": "真正的AI正文", "state": "ready"}} {
		doc := Document{Meta: map[string]any{"originalStatus": "done"}, Versions: map[string]any{"ai1": value}}
		if got := DeriveTaskStatus(doc); got != TaskStatusPendingSubmit {
			t.Fatalf("real AI body lost: status=%q value=%#v", got, value)
		}
	}
}

func TestDeriveTaskStatusKeepsLegacyDoneMarkerWithoutInlineAIText(t *testing.T) {
	doc := Document{Meta: map[string]any{"originalStatus": "done", "aiStatus": "ai_done"}, Versions: map[string]any{"ai1": map[string]any{"state": "ready"}}}
	if got := DeriveTaskStatus(doc); got != TaskStatusPendingSubmit {
		t.Fatalf("legacy ai_done marker changed: %q", got)
	}
}

func TestDeriveTaskStatusTreatsSubmittedAIResultAsCompleted(t *testing.T) {
	document := Document{
		BookID: "12345678901",
		Meta: map[string]any{
			"originalStatus":   "done",
			"aiStatus":         "ai_done",
			"siteSubmitStatus": "submitted",
		},
	}
	if got := DeriveTaskStatus(document); got != TaskStatusCompleted {
		t.Fatalf("DeriveTaskStatus() = %q, want %q", got, TaskStatusCompleted)
	}
}

func TestDeriveTaskStatusTreatsProcessedButUnsubmittedTaskAsPendingSubmit(t *testing.T) {
	document := Document{
		BookID:           "12345678901",
		OriginalRawChars: 27831,
		Meta: map[string]any{
			"status":         "completed",
			"originalStatus": "done",
			"aiStatus":       "ai_done",
		},
	}
	if got := DeriveTaskStatus(document); got != TaskStatusPendingSubmit {
		t.Fatalf("DeriveTaskStatus() = %q, want %q", got, TaskStatusPendingSubmit)
	}
}

func TestDeriveTaskStatusKeepsSubmissionConfigurationBlockActionable(t *testing.T) {
	document := Document{Meta: map[string]any{
		"status":           "waiting_configuration",
		"siteSubmitStatus": "waiting_configuration",
		"originalStatus":   "done",
		"aiStatus":         "waiting_ai_config",
	}}
	if got := DeriveTaskStatus(document); got != "waiting_configuration" {
		t.Fatalf("DeriveTaskStatus() = %q, want waiting_configuration", got)
	}
}

func TestDeriveTaskStatusKeepsSubmissionLoginBlockActionable(t *testing.T) {
	document := Document{Meta: map[string]any{
		"status":           "waiting_login",
		"siteSubmitStatus": "waiting_login",
		"originalStatus":   "done",
		"aiStatus":         "ai_done",
	}}
	if got := DeriveTaskStatus(document); got != "waiting_login" {
		t.Fatalf("DeriveTaskStatus() = %q, want waiting_login", got)
	}
}

func TestDeriveTaskStatusKeepsWaitingRetrySeparateFromProcessing(t *testing.T) {
	document := Document{Meta: map[string]any{
		"status": "waiting_retry",
	}}
	if got := DeriveTaskStatus(document); got != TaskStatusWaitingRetry {
		t.Fatalf("DeriveTaskStatus() = %q, want %q", got, TaskStatusWaitingRetry)
	}
}

func TestDeriveTaskStatusExplainsSensitiveAIWait(t *testing.T) {
	document := Document{Meta: map[string]any{"originalStatus": "waiting_sensitive_ai"}}
	if got := DeriveTaskStatus(document); got != TaskStatusWaitingSensitiveAI {
		t.Fatalf("DeriveTaskStatus() = %q, want %q", got, TaskStatusWaitingSensitiveAI)
	}
}

func TestDeriveTaskStatusPrefersActiveQueueOverStaleTerminalStatus(t *testing.T) {
	document := Document{Meta: map[string]any{
		"status":     "completed",
		"queueState": "running",
		"aiStatus":   "failed",
	}}
	if got := DeriveTaskStatus(document); got != TaskStatusProcessing {
		t.Fatalf("DeriveTaskStatus() = %q, want %q", got, TaskStatusProcessing)
	}
	document.Meta["queueState"] = "queued"
	if got := DeriveTaskStatus(document); got != TaskStatusQueued {
		t.Fatalf("DeriveTaskStatus() = %q, want %q", got, TaskStatusQueued)
	}
}

func TestDeriveTaskStatusHonorsScheduledHoldAndStopped(t *testing.T) {
	scheduled := Document{Meta: map[string]any{"status": "completed", "scheduledHold": true}}
	if got := DeriveTaskStatus(scheduled); got != TaskStatusScheduledWaiting {
		t.Fatalf("scheduled DeriveTaskStatus() = %q", got)
	}
	stopped := Document{Meta: map[string]any{"status": "input_ready", "queueState": "stopped"}}
	if got := DeriveTaskStatus(stopped); got != TaskStatusStopped {
		t.Fatalf("stopped DeriveTaskStatus() = %q", got)
	}
}

func TestDeriveTaskStatusUsesBodyRefsForPendingSubmit(t *testing.T) {
	document := Document{
		OriginalRawChars: 1200,
		BodyRefs:         map[string]BodyRef{"ai1": {VersionID: "ai1", State: "ready"}},
		Meta:             map[string]any{"originalStatus": "done"},
	}
	if got := DeriveTaskStatus(document); got != TaskStatusPendingSubmit {
		t.Fatalf("DeriveTaskStatus() = %q, want %q", got, TaskStatusPendingSubmit)
	}
}

func TestDeriveTaskStatusKeepsUploadPendingAheadOfSubmitFailure(t *testing.T) {
	document := Document{Meta: map[string]any{
		"status":           "upload_pending",
		"originalStatus":   "done",
		"aiStatus":         "ai_done",
		"siteSubmitStatus": "failed",
	}}
	if got := DeriveTaskStatus(document); got != TaskStatusUploadPending {
		t.Fatalf("DeriveTaskStatus() = %q, want %q", got, TaskStatusUploadPending)
	}
}
