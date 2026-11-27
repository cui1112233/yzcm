package scripteditor

import (
	"errors"
	"testing"
)

func TestWorkBindingValidateRequiresStableIdentity(t *testing.T) {
	err := (WorkBinding{WorkID: "work_1", TargetType: BindingCMCanvas}).Validate()
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("Validate() error = %v, want ErrValidation", err)
	}
}

func TestWorkBindingValidateAcceptsKnownTarget(t *testing.T) {
	err := (WorkBinding{
		OwnerUsername: "alice",
		WorkID:        "work_1",
		TargetType:    BindingCMCanvas,
		TargetID:      "agent_task_1",
		SyncState:     SyncPending,
		ConflictState: ConflictNone,
	}).Validate()
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestBindingEnumsRejectUnknownValues(t *testing.T) {
	if SyncState("mystery").Valid() || ConflictState("mystery").Valid() || BindingTargetType("mystery").Valid() {
		t.Fatal("unknown binding enum unexpectedly valid")
	}
}
