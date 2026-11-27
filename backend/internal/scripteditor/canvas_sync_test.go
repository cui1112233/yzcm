package scripteditor

import (
	"context"
	"qiantie/backend/internal/agentworkspace"
	"testing"
	"time"
)

func TestEnsureCanvasIsIdempotent(t *testing.T) {
	store := NewMemoryStore()
	service := NewService(store)
	service.AgentWorkspace = agentworkspace.NewMemoryStore()
	work, _ := store.CreateWork(context.Background(), "alice", CreateWorkInput{Title: "甲", Type: WorkScript})
	first, err := service.EnsureCanvas(context.Background(), "alice", work.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.EnsureCanvas(context.Background(), "alice", work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if first.TargetID != second.TargetID {
		t.Fatalf("canvas changed: %s %s", first.TargetID, second.TargetID)
	}
}

func TestPreviewCanvasSyncDoesNotOverwriteManualNode(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	service := NewService(store)
	service.AgentWorkspace = agentworkspace.NewMemoryStore()
	work, _ := store.CreateWork(ctx, "alice", CreateWorkInput{Title: "甲", Type: WorkScript})
	script, _ := store.CreateScript(ctx, "alice", work.ID, CreateScriptInput{Episode: 1, Title: "一", Content: "原文"})
	preview, _ := service.PreviewCanvasSync(ctx, "alice", work.ID, CanvasSyncInput{ScriptIDs: []string{script.ID}})
	_, err := service.ApplyCanvasSync(ctx, "alice", work.ID, CanvasSyncInput{ScriptIDs: []string{script.ID}, ExpectedCanvasRevision: preview.CanvasRevision})
	if err != nil {
		t.Fatal(err)
	}
	binding, _ := service.EnsureCanvas(ctx, "alice", work.ID)
	canvas, _ := service.AgentWorkspace.Canvas(ctx, "alice", binding.TargetID)
	canvas.Nodes[0].Summary = "手工内容"
	_, _ = service.AgentWorkspace.SaveCanvas(ctx, "alice", binding.TargetID, canvas, canvas.Revision, time.Now())
	next, err := service.PreviewCanvasSync(ctx, "alice", work.ID, CanvasSyncInput{ScriptIDs: []string{script.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if next.Items[0].State != "conflict" {
		t.Fatalf("item=%#v", next.Items[0])
	}
}
