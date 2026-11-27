package yzcmmodels

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestYZCMModelCatalogQueryBounds(t *testing.T) {
	normalized, err := (Query{Kind: " TEXT ", Q: " 大模型 "}).Normalize()
	if err != nil || normalized.Kind != "text" || normalized.Q != "大模型" || normalized.Limit != defaultLimit {
		t.Fatalf("query normalization: %+v %v", normalized, err)
	}
	for _, query := range []Query{{Kind: "unknown"}, {Limit: -1}, {Limit: 101}, {Offset: -1}, {Offset: 10001},
		{Q: strings.Repeat("文", 192)}, {Q: "名\x00字"}, {Q: "\xff"}} {
		if _, err := query.Normalize(); !errors.Is(err, ErrInvalidQuery) {
			t.Fatalf("expected invalid query: %+v %v", query, err)
		}
	}
}

func TestYZCMModelCatalogSourceNotConfigured(t *testing.T) {
	store := &Store{}
	if run, err := store.Sync(context.Background(), nil); !errors.Is(err, ErrSourceNotConfigured) || run.ID != 0 {
		t.Fatalf("missing source must not start a sync: %+v %v", run, err)
	}
}

func TestYZCMModelCatalogSnapshotRequiresValidCompleteRecords(t *testing.T) {
	row := Model{ModelID: "local-fixture", DisplayName: "测试模型", Kind: "text", Adapted: true, Allowed: false}
	now := time.Now()
	accept := Snapshot{SourceUpdatedAt: now, Complete: true, Models: []Model{row}}
	if err := validateSnapshot(accept, now); err != nil || availabilityReason(row) != "not_allowed" {
		t.Fatalf("complete disabled record can be saved: %v", err)
	}
	for _, model := range []Model{{ModelID: "", DisplayName: "模型", Kind: "text"},
		{ModelID: "test", DisplayName: " ", Kind: "text"}, {ModelID: "test", DisplayName: "模型\r\n", Kind: "text"},
		{ModelID: "test ", DisplayName: "模型", Kind: "text"}, {ModelID: strings.Repeat("字", 192), DisplayName: "模型", Kind: "text"}} {
		snapshot := Snapshot{SourceUpdatedAt: now, Complete: true, Models: []Model{model}}
		if err := validateSnapshot(snapshot, now); err == nil {
			t.Fatalf("accepted invalid model: %+v", model)
		}
	}
	accept.SourceUpdatedAt = time.Time{}
	if err := validateSnapshot(accept, now); err == nil {
		t.Fatal("accepted a catalog without valid source time")
	}
}

func TestYZCMModelCatalogValidatesUnroundedSourceTime(t *testing.T) {
	base := time.Date(2026, 10, 1, 5, 4, 37, 0, time.UTC)
	snapshot := Snapshot{Complete: true, Models: []Model{}, SourceUpdatedAt: base.Add(900 * time.Nanosecond)}
	if err := validateSnapshot(snapshot, base.Add(100*time.Nanosecond)); err == nil {
		t.Fatal("a future instant in the same microsecond was accepted")
	}
	if err := validateSnapshot(snapshot, snapshot.SourceUpdatedAt); err != nil {
		t.Fatalf("a source timestamp equal to observation time was rejected: %v", err)
	}
}
