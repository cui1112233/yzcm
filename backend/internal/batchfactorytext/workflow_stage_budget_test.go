package batchfactorytext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

type collectionLookupFailureRepo struct{ *workflowRepo }

func (r collectionLookupFailureRepo) ListCollections(context.Context, Scope) ([]Collection, error) {
	return nil, context.DeadlineExceeded
}

func TestTextWorkflowCollectionReadTimeoutIsNotValidationFailure(t *testing.T) {
	snapshot := json.RawMessage(`{"layout":{},"sourceVersion":"v"}`)
	repo := collectionLookupFailureRepo{&workflowRepo{doc: Document{ID: "doc", ActiveVersionID: "v", PaginationVersion: 1, Revision: 1, ConfigSnapshot: snapshot}, pages: []Page{{ID: "p", VersionID: "v", PaginationVersion: 1, Ordinal: 1, ActiveArtifactID: "image", RenderVersion: 1}}}}
	validated := false
	w := Workflow{Repo: repo,
		Track: func(_ context.Context, _ Scope, _ StageCheckpoint, _ bool, run func() error) error { return run() },
		Process: func(context.Context, EnsureRequest, bool) (WorkflowProduction, error) {
			return WorkflowProduction{VersionInput: VersionInput{ProductionText: "正文"}, Snapshot: snapshot}, nil
		},
		Prepare: func(context.Context, EnsureRequest, WorkflowProduction, Document, bool) error {
			t.Fatal("existing pages must be retained")
			return nil
		},
		Render: func(context.Context, EnsureRequest, Document, Page) error {
			t.Fatal("existing image must not be regenerated")
			return nil
		},
		VerifyFile: func(context.Context, PageArtifact, string) error { return nil },
		Validate:   func(context.Context, EnsureRequest, Document) error { validated = true; return nil },
	}
	result, err := w.Ensure(context.Background(), EnsureRequest{Scope: Scope{"owner", "batch", "book"}, Target: "collection"})
	if !validated || !errors.Is(err, context.DeadlineExceeded) || result.Stage != "text_collection" {
		t.Fatalf("validated=%v stage=%s err=%v", validated, result.Stage, err)
	}
}

func TestTextWorkflowBudgetsFollowPhaseAndKeepCancellation(t *testing.T) {
	for _, cancelDuringRender := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelDuringRender), func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			snapshot := json.RawMessage(`{"layout":{},"sourceVersion":"v"}`)
			repo := &workflowRepo{doc: Document{ID: "doc", ActiveVersionID: "v", PaginationVersion: 1, Revision: 1, ConfigSnapshot: snapshot}, pages: []Page{{ID: "p", VersionID: "v", PaginationVersion: 1, Ordinal: 1, RenderVersion: 1}}}
			var renderCtx context.Context
			validated := false
			published := false
			w := Workflow{Repo: repo, StageTimeout: func(string) time.Duration { return time.Minute },
				Track: func(ctx context.Context, _ Scope, _ StageCheckpoint, _ bool, run func() error) error {
					if _, ok := ctx.Deadline(); !ok {
						t.Fatal("phase is unbounded")
					}
					err := run()
					if err != nil {
						return err
					}
					return ctx.Err()
				},
				Process: func(context.Context, EnsureRequest, bool) (WorkflowProduction, error) {
					return WorkflowProduction{VersionInput: VersionInput{ProductionText: "正文"}, Snapshot: snapshot}, nil
				},
				Prepare: func(context.Context, EnsureRequest, WorkflowProduction, Document, bool) error {
					t.Fatal("no repagination expected")
					return nil
				},
				Render: func(ctx context.Context, _ EnsureRequest, _ Document, _ Page) error {
					renderCtx = ctx
					repo.pages[0].ActiveArtifactID = "saved"
					if cancelDuringRender {
						cancel()
					}
					return ctx.Err()
				},
				VerifyFile: func(context.Context, PageArtifact, string) error { return nil },
				Validate: func(ctx context.Context, _ EnsureRequest, _ Document) error {
					validated = true
					if renderCtx == ctx || !errors.Is(renderCtx.Err(), context.Canceled) || ctx.Err() != nil {
						t.Fatal("validation inherited spent render context")
					}
					return nil
				},
				Publish: func(ctx context.Context, _ EnsureRequest, _ Collection) (bool, error) {
					published = true
					return ctx.Err() == nil, ctx.Err()
				},
			}
			result, err := w.Ensure(parent, EnsureRequest{Scope: Scope{"owner", "batch", "book"}, Target: "published", PublishSettingsKey: "mapping"})
			if cancelDuringRender {
				if !errors.Is(err, context.Canceled) || validated || published {
					t.Fatalf("parent cancellation ignored: %+v %v", result, err)
				}
				return
			}
			if err != nil || !result.Published || !validated || !published || parent.Err() != nil {
				t.Fatalf("%+v %v", result, err)
			}
		})
	}
}

type pinnedBudgetRepo struct{ *workflowRepo }

func (p pinnedBudgetRepo) GetVersion(context.Context, Scope, string) (Version, error) {
	return Version{ID: "v"}, nil
}

func TestTextWorkflowPinnedUploadNeverRegenerates(t *testing.T) {
	repo := pinnedBudgetRepo{&workflowRepo{collections: []Collection{{ID: "saved", VersionID: "v", PaginationVersion: 1, Status: "ready", Items: []CollectionItem{{PageID: "p", ArtifactID: "image", Ordinal: 1}}}}, pages: []Page{{ID: "p", ActiveArtifactID: "image", RenderVersion: 1}}}}
	phases := []string{}
	w := Workflow{Repo: repo, StageTimeout: func(s string) time.Duration { phases = append(phases, s); return time.Minute },
		Track: func(ctx context.Context, _ Scope, _ StageCheckpoint, _ bool, run func() error) error {
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("upload has no independent deadline")
			}
			return run()
		},
		Process: func(context.Context, EnsureRequest, bool) (WorkflowProduction, error) {
			t.Fatal("pinned upload reprocessed source")
			return WorkflowProduction{}, nil
		},
		Prepare: func(context.Context, EnsureRequest, WorkflowProduction, Document, bool) error {
			t.Fatal("pinned upload repaginated")
			return nil
		},
		Render: func(context.Context, EnsureRequest, Document, Page) error {
			t.Fatal("pinned upload regenerated images")
			return nil
		},
		VerifyFile: func(context.Context, PageArtifact, string) error { return nil }, Validate: func(context.Context, EnsureRequest, Document) error { return nil },
		Publish: func(context.Context, EnsureRequest, Collection) (bool, error) { return true, nil },
	}
	result, err := w.Ensure(context.Background(), EnsureRequest{Scope: Scope{"owner", "batch", "book"}, Target: "published", CollectionID: "saved", PublishSettingsKey: "mapping"})
	if err != nil || !result.Published || len(phases) != 1 || phases[0] != "text_publish" {
		t.Fatalf("phases=%v %+v %v", phases, result, err)
	}
}
