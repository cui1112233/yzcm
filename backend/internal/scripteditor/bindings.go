package scripteditor

import (
	"strings"
	"time"
)

type BindingTargetType string

const (
	BindingCMCanvas     BindingTargetType = "cm_canvas"
	BindingComicProject BindingTargetType = "comic_project"
	BindingAICutProject BindingTargetType = "aicut_project"
)

func (v BindingTargetType) Valid() bool {
	return v == BindingCMCanvas || v == BindingComicProject || v == BindingAICutProject
}

type SyncState string

const (
	SyncPending SyncState = "pending"
	SyncSynced  SyncState = "synced"
	SyncFailed  SyncState = "failed"
)

func (v SyncState) Valid() bool { return v == SyncPending || v == SyncSynced || v == SyncFailed }

type ConflictState string

const (
	ConflictNone     ConflictState = "none"
	ConflictDetected ConflictState = "detected"
	ConflictResolved ConflictState = "resolved"
)

func (v ConflictState) Valid() bool {
	return v == ConflictNone || v == ConflictDetected || v == ConflictResolved
}

type WorkBinding struct {
	ID             string            `json:"id"`
	OwnerUsername  string            `json:"-"`
	WorkID         string            `json:"workId"`
	TargetType     BindingTargetType `json:"targetType"`
	TargetID       string            `json:"targetId"`
	SourceRevision int64             `json:"sourceRevision"`
	SyncState      SyncState         `json:"syncState"`
	ConflictState  ConflictState     `json:"conflictState"`
	CreatedAt      time.Time         `json:"createdAt"`
	UpdatedAt      time.Time         `json:"updatedAt"`
}

func (row WorkBinding) Validate() error {
	if strings.TrimSpace(row.OwnerUsername) == "" || strings.TrimSpace(row.WorkID) == "" || strings.TrimSpace(row.TargetID) == "" || !row.TargetType.Valid() || !row.SyncState.Valid() || !row.ConflictState.Valid() || row.SourceRevision < 0 {
		return ErrValidation
	}
	return nil
}

type SourceLink struct {
	ID             string        `json:"id"`
	OwnerUsername  string        `json:"-"`
	WorkID         string        `json:"workId"`
	SourceType     string        `json:"sourceType"`
	SourceID       string        `json:"sourceId"`
	TargetType     string        `json:"targetType"`
	TargetID       string        `json:"targetId"`
	SourceRevision int64         `json:"sourceRevision"`
	SyncState      SyncState     `json:"syncState"`
	ConflictState  ConflictState `json:"conflictState"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}

type AssetLink struct {
	ID             string        `json:"id"`
	OwnerUsername  string        `json:"-"`
	WorkID         string        `json:"workId"`
	AssetID        string        `json:"assetId"`
	TargetType     string        `json:"targetType"`
	TargetID       string        `json:"targetId"`
	SourceRevision int64         `json:"sourceRevision"`
	SyncState      SyncState     `json:"syncState"`
	ConflictState  ConflictState `json:"conflictState"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}

type JobLink struct {
	ID             string        `json:"id"`
	OwnerUsername  string        `json:"-"`
	WorkID         string        `json:"workId"`
	JobID          string        `json:"jobId"`
	TargetType     string        `json:"targetType"`
	TargetID       string        `json:"targetId"`
	SourceRevision int64         `json:"sourceRevision"`
	SyncState      SyncState     `json:"syncState"`
	ConflictState  ConflictState `json:"conflictState"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}
