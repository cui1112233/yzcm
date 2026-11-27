package scripteditor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"qiantie/backend/internal/agentworkspace"
)

type CanvasSyncItem struct {
	ScriptID string `json:"scriptId"`
	NodeID   string `json:"nodeId,omitempty"`
	Episode  int    `json:"episode"`
	Version  int    `json:"version"`
	State    string `json:"state"`
	Reason   string `json:"reason,omitempty"`
}
type CanvasSyncPreview struct {
	Binding        WorkBinding      `json:"binding"`
	CanvasRevision int64            `json:"canvasRevision"`
	Items          []CanvasSyncItem `json:"items"`
}
type CanvasSyncInput struct {
	ScriptIDs              []string          `json:"scriptIds"`
	Decisions              map[string]string `json:"decisions,omitempty"`
	ExpectedCanvasRevision int64             `json:"expectedCanvasRevision"`
	RequestKey             string            `json:"requestKey,omitempty"`
}

func (s *Service) EnsureCanvas(ctx context.Context, owner, workID string) (WorkBinding, error) {
	if s == nil || s.AgentWorkspace == nil {
		return WorkBinding{}, ErrValidation
	}
	bindings, ok := s.Store.(BindingStore)
	if !ok {
		return WorkBinding{}, ErrValidation
	}
	work, err := s.Store.GetWork(ctx, owner, workID)
	if err != nil {
		return WorkBinding{}, err
	}
	if row, bindErr := bindings.WorkBinding(ctx, owner, workID, BindingCMCanvas); bindErr == nil {
		if _, projectErr := s.AgentWorkspace.Project(ctx, owner, row.TargetID); projectErr == nil {
			return row, nil
		}
	}
	now := time.Now().UTC()
	projectID := randomID("cm")
	project := agentworkspace.Project{ID: projectID, Title: work.Title + " · CM 画布", TitleMode: "manual", ProjectType: "comic", Preview: "由剧本编辑绑定", CreatedAt: now, UpdatedAt: now, Messages: []agentworkspace.Message{}, Canvas: agentworkspace.EmptyCanvas(), Executions: []agentworkspace.Execution{}}
	if err := s.AgentWorkspace.CreateProject(ctx, owner, project); err != nil && !errors.Is(err, agentworkspace.ErrProjectExists) {
		return WorkBinding{}, err
	}
	return bindings.UpsertWorkBinding(ctx, WorkBinding{OwnerUsername: owner, WorkID: workID, TargetType: BindingCMCanvas, TargetID: projectID, SourceRevision: work.Revision, SyncState: SyncPending, ConflictState: ConflictNone})
}

func (s *Service) PreviewCanvasSync(ctx context.Context, owner, workID string, input CanvasSyncInput) (CanvasSyncPreview, error) {
	binding, err := s.EnsureCanvas(ctx, owner, workID)
	if err != nil {
		return CanvasSyncPreview{}, err
	}
	detail, err := s.Store.GetWork(ctx, owner, workID)
	if err != nil {
		return CanvasSyncPreview{}, err
	}
	canvas, err := s.AgentWorkspace.Canvas(ctx, owner, binding.TargetID)
	if err != nil {
		return CanvasSyncPreview{}, err
	}
	wanted := map[string]bool{}
	for _, id := range input.ScriptIDs {
		wanted[strings.TrimSpace(id)] = true
	}
	items := []CanvasSyncItem{}
	for _, script := range detail.Scripts {
		if !wanted[script.ID] {
			continue
		}
		item := CanvasSyncItem{ScriptID: script.ID, Episode: script.Episode, Version: script.Version, State: "create"}
		for _, node := range canvas.Nodes {
			if fmt.Sprint(node.Config["scriptEditorScriptId"]) != script.ID {
				continue
			}
			item.NodeID = node.ID
			item.State = "update"
			if fmt.Sprint(node.Config["scriptEditorManaged"]) != "true" || node.Summary != script.Content && fmt.Sprint(node.Config["sourceRevision"]) == fmt.Sprint(script.Revision) {
				item.State, item.Reason = "conflict", "画布节点已被手工修改"
			}
			break
		}
		items = append(items, item)
		delete(wanted, script.ID)
	}
	if len(wanted) > 0 {
		return CanvasSyncPreview{}, ErrForbidden
	}
	return CanvasSyncPreview{Binding: binding, CanvasRevision: canvas.Revision, Items: items}, nil
}

func (s *Service) ApplyCanvasSync(ctx context.Context, owner, workID string, input CanvasSyncInput) (agentworkspace.Canvas, error) {
	preview, err := s.PreviewCanvasSync(ctx, owner, workID, input)
	if err != nil {
		return agentworkspace.Canvas{}, err
	}
	if input.ExpectedCanvasRevision != preview.CanvasRevision {
		return agentworkspace.Canvas{}, ErrConflict
	}
	detail, _ := s.Store.GetWork(ctx, owner, workID)
	scripts := map[string]Script{}
	for _, row := range detail.Scripts {
		scripts[row.ID] = row
	}
	canvas, err := s.AgentWorkspace.Canvas(ctx, owner, preview.Binding.TargetID)
	if err != nil {
		return agentworkspace.Canvas{}, err
	}
	bindings := s.Store.(BindingStore)
	for index, item := range preview.Items {
		decision := input.Decisions[item.ScriptID]
		if item.State == "conflict" && decision != "overwrite" {
			continue
		}
		script := scripts[item.ScriptID]
		nodeID := item.NodeID
		if nodeID == "" {
			nodeID = randomID("node")
		}
		next := agentworkspace.Node{ID: nodeID, Type: "text", Position: agentworkspace.Position{X: 80, Y: float64(80 + index*190)}, Title: fmt.Sprintf("第%d集 V%d · %s", script.Episode, script.Version, script.Title), Version: 1, Status: "ready", Summary: script.Content, Config: map[string]any{"scriptEditorManaged": true, "scriptEditorWorkId": workID, "scriptEditorScriptId": script.ID, "sourceRevision": script.Revision}}
		found := false
		for nodeIndex := range canvas.Nodes {
			if canvas.Nodes[nodeIndex].ID == nodeID {
				next.Position = canvas.Nodes[nodeIndex].Position
				next.Version = canvas.Nodes[nodeIndex].Version + 1
				canvas.Nodes[nodeIndex] = next
				found = true
				break
			}
		}
		if !found {
			canvas.Nodes = append(canvas.Nodes, next)
		}
		_, err = bindings.UpsertSourceLink(ctx, SourceLink{OwnerUsername: owner, WorkID: workID, SourceType: "script", SourceID: script.ID, TargetType: "cm_node", TargetID: nodeID, SourceRevision: script.Revision, SyncState: SyncSynced, ConflictState: ConflictNone})
		if err != nil {
			return agentworkspace.Canvas{}, err
		}
	}
	saved, err := s.AgentWorkspace.SaveCanvas(ctx, owner, preview.Binding.TargetID, canvas, preview.CanvasRevision, time.Now().UTC())
	if err != nil {
		if errors.Is(err, agentworkspace.ErrRevisionConflict) {
			return agentworkspace.Canvas{}, ErrConflict
		}
		return agentworkspace.Canvas{}, err
	}
	_, _ = bindings.UpsertWorkBinding(ctx, WorkBinding{OwnerUsername: owner, WorkID: workID, TargetType: BindingCMCanvas, TargetID: preview.Binding.TargetID, SourceRevision: detail.Revision, SyncState: SyncSynced, ConflictState: ConflictNone})
	return saved, nil
}
