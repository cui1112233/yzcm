package scripteditor

import (
	"context"
	"testing"
)

func TestAgentContextRejectsForeignReferences(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	alice, _ := store.CreateWork(ctx, "alice", CreateWorkInput{Title: "甲", Type: WorkScript})
	bob, _ := store.CreateWork(ctx, "bob", CreateWorkInput{Title: "乙", Type: WorkScript})
	foreign, _ := store.CreateScript(ctx, "bob", bob.ID, CreateScriptInput{Episode: 1, Title: "第一集", Content: "别人的", GenerationSource: SourceManual})
	bridge := AgentBridge{Store: store}

	_, err := bridge.BuildContext(ctx, "alice", alice.ID, []AgentReference{{Type: "script", ID: foreign.ID}})
	if err != ErrForbidden {
		t.Fatalf("expected forbidden, got %v", err)
	}
}

func TestApplyAgentDraftCreatesAuditedVersion(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	work, _ := store.CreateWork(ctx, "alice", CreateWorkInput{Title: "甲", Type: WorkScript})
	source, _ := store.CreateScript(ctx, "alice", work.ID, CreateScriptInput{Episode: 2, Title: "第二集", Content: "旧稿", GenerationSource: SourceManual})
	bridge := AgentBridge{Store: store}

	result, err := bridge.ApplyDraft(ctx, "alice", AgentDraft{ID: "draft-1", WorkID: work.ID, ScriptID: source.ID, Title: "第二集 Agent 稿", Content: "新稿", Mode: "confirm"}, source.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if result.Script.GenerationSource != SourceAI || result.Script.Version != 2 || result.AuditID == "" {
		t.Fatalf("unexpected result: %#v", result)
	}
}
