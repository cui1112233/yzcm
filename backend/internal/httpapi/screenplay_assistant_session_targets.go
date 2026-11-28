package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"

	"qiantie/backend/internal/screenplay"
)

// Resolve selector names and shot numbers against what the model saw. Store
// supplies only reference checks for deletions. Actual writes always use the
// SQL transaction in ApplySessionProposal.
type sessionEditView struct {
	screenplay.Store
	assets []screenplay.Asset
	boards []screenplay.Storyboard
}

func captureSessionEditView(ctx context.Context, store screenplay.Store, owner, project string) (sessionEditView, error) {
	v := sessionEditView{Store: store}
	var err error
	if v.assets, err = store.ListAssets(ctx, owner, project, ""); err != nil {
		return v, err
	}
	v.boards, err = store.ListStoryboards(ctx, owner, project, "")
	return v, err
}

func (v sessionEditView) ListAssets(_ context.Context, _, _ string, kind screenplay.AssetType) ([]screenplay.Asset, error) {
	rows := make([]screenplay.Asset, 0)
	for _, a := range v.assets {
		if kind == "" || a.Type == kind {
			rows = append(rows, a)
		}
	}
	return rows, nil
}
func (v sessionEditView) ListStoryboards(_ context.Context, _, _ string, engine screenplay.StoryboardEngine) ([]screenplay.Storyboard, error) {
	rows := make([]screenplay.Storyboard, 0)
	for _, b := range v.boards {
		if engine == "" || b.Engine == engine {
			b.Shots = append([]screenplay.StoryboardShot(nil), b.Shots...)
			rows = append(rows, b)
		}
	}
	return rows, nil
}
func prepareSessionTool(ctx context.Context, store screenplay.Store, owner, project, tool string, args json.RawMessage) (screenplay.AuthoringAction, string, error) {
	switch {
	case screenplay.IsAssistantAssetEditTool(tool):
		return screenplay.PrepareAssistantAssetEdit(ctx, store, owner, project, tool, args)
	case screenplay.IsAssistantShotEditTool(tool):
		return screenplay.PrepareAssistantShotEdit(ctx, store, owner, project, tool, args)
	default:
		return screenplay.AuthoringAction{}, "", screenplay.ErrInvalidArgument
	}
}
func (v sessionEditView) prepare(ctx context.Context, owner, project, tool string, args json.RawMessage) (screenplay.AuthoringAction, string, error) {
	first, summary, err := prepareSessionTool(ctx, v, owner, project, tool, args)
	if err != nil {
		return screenplay.AuthoringAction{}, "", err
	}
	live, _, err := prepareSessionTool(ctx, v.Store, owner, project, tool, args)
	if err != nil {
		if errors.Is(err, screenplay.ErrNotFound) || errors.Is(err, screenplay.ErrInvalidArgument) {
			err = screenplay.ErrRevisionConflict
		}
		return screenplay.AuthoringAction{}, "", err
	}
	same := first.ExpectedRevision == live.ExpectedRevision
	switch {
	case first.Asset != nil && live.Asset != nil:
		a, b := *first.Asset, *live.Asset
		if first.AssetOperation == "add" && live.AssetOperation == "add" {
			a.ID = b.ID
		}
		same = same && reflect.DeepEqual(a, b)
	case first.CreativeStructure != nil && live.CreativeStructure != nil:
		a, b := first.CreativeStructure, live.CreativeStructure
		same = same && a.BoardID == b.BoardID && reflect.DeepEqual(a.Before, b.Before)
	case first.CreativeShot != nil && live.CreativeShot != nil:
		same = same && reflect.DeepEqual(first.CreativeShot, live.CreativeShot)
	default:
		same = false
	}
	if !same {
		return screenplay.AuthoringAction{}, "", screenplay.ErrRevisionConflict
	}
	return first, summary, nil
}
