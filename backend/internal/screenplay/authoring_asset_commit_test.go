package screenplay

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
	"time"
)

func TestMySQLAuthoringAssetCommitRollsBackIfProposalSaveFails(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			db, m, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			store := NewMySQLStore(db)
			repo := store.AuthoringRepository()
			state := AuthoringState{Owner: "alice", ProjectID: "p", Revision: 5, Proposals: []AssistantProposal{{ID: "proposal", Status: "pending", Revision: 5, Actions: []AuthoringAction{{Kind: "asset", AssetPrepared: true, TargetID: "asset", ExpectedRevision: 2, Asset: &Asset{Type: AssetCharacter, Name: "甲", Description: "新描述", Prompt: "保留图词"}}}}}}
			body, _ := json.Marshal(state)
			m.ExpectBegin()
			m.ExpectQuery("SELECT id FROM screenplay_projects").WithArgs("p", "alice").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("p"))
			m.ExpectQuery("SELECT body,revision FROM screenplay_authoring").WithArgs("alice", "p").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(body, 5))
			m.ExpectQuery("SELECT degree, revision, created_at, sort_order, source_batch FROM screenplay_assets").WithArgs("asset", "p", "alice").WillReturnRows(sqlmock.NewRows([]string{"degree", "revision", "created_at", "sort_order", "source_batch"}).AddRow("standard", 2, time.Now(), 1, 0))
			m.ExpectQuery("SELECT COUNT\\(\\*\\) FROM screenplay_assets").WithArgs("p", "alice", "character", "甲", "asset").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
			m.ExpectExec("UPDATE screenplay_assets").WillReturnResult(sqlmock.NewResult(0, 1))
			if failed {
				m.ExpectExec("UPDATE screenplay_authoring").WillReturnError(fmt.Errorf("database write unavailable"))
				m.ExpectRollback()
			} else {
				m.ExpectExec("UPDATE screenplay_authoring").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectCommit()
			}
			out, e := store.CommitAuthoringAsset(context.Background(), repo, "alice", "p", "proposal", 5)
			if failed {
				if e == nil {
					t.Fatal("proposal failure reported success")
				}
			} else if e != nil || out.Revision != 6 || out.Proposals[0].Status != "applied" {
				t.Fatal(out, e)
			}
			if e = m.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestMySQLAuthoringAssetCommitRejectsMixedRepositoryBeforeWriting(t *testing.T) {
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := NewMySQLStore(db)
	if _, e = s.CommitAuthoringAsset(context.Background(), NewMemoryAuthoringRepository(), "alice", "p", "proposal", 1); e == nil {
		t.Fatal("mixed repository allowed non-atomic write")
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

type rejectedAtomicAssetStore struct {
	*MemoryStore
	Calls int
}

func (s *rejectedAtomicAssetStore) CommitAuthoringAsset(context.Context, AuthoringRepository, string, string, string, int64) (AuthoringState, error) {
	s.Calls++
	return AuthoringState{}, fmt.Errorf("atomic commit rejected")
}

func TestAuthoringAssetAtomicFailureNeverFallsBackToSeparateWrites(t *testing.T) {
	ctx := context.Background()
	s := &rejectedAtomicAssetStore{MemoryStore: NewMemoryStore()}
	_, _ = s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	asset, _ := s.CreateAsset(ctx, Asset{ID: "a", ProjectID: "p", OwnerUsername: "alice", Type: AssetCharacter, Name: "甲", Description: "旧"})
	desired := asset
	desired.Description = "新"
	repo := NewMemoryAuthoringRepository()
	svc := AuthoringService{Store: s, Repository: repo}
	state, _ := svc.Load(ctx, "alice", "p")
	state.Proposals = []AssistantProposal{{ID: "proposal", Status: "pending", Revision: 1, Actions: []AuthoringAction{{Kind: "asset", AssetPrepared: true, TargetID: "a", Asset: &desired, ExpectedRevision: asset.Revision}}}}
	state, e := svc.Save(ctx, "alice", "p", state, 0)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = svc.Apply(ctx, "alice", "p", "proposal", state.Revision, true); e == nil || s.Calls != 1 {
		t.Fatal("atomic capability bypassed", e, s.Calls)
	}
	assets, _ := s.ListAssets(ctx, "alice", "p", "")
	if assets[0].Description != "旧" || assets[0].Revision != asset.Revision {
		t.Fatal("fell back to a partial write", assets)
	}
	current, _ := repo.Load(ctx, "alice", "p")
	if current.Revision != state.Revision || current.Proposals[0].Status != "pending" {
		t.Fatal(current)
	}
}

func TestMySQLAuthoringAssetCommitRejectsStaleStateBeforeAssetWrite(t *testing.T) {
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := NewMySQLStore(db)
	m.ExpectBegin()
	m.ExpectQuery("SELECT id FROM screenplay_projects").WithArgs("p", "alice").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("p"))
	m.ExpectQuery("SELECT body,revision FROM screenplay_authoring").WithArgs("alice", "p").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(`{}`, 6))
	m.ExpectRollback()
	if _, e = s.CommitAuthoringAsset(context.Background(), s.AuthoringRepository(), "alice", "p", "proposal", 5); e != ErrRevisionConflict {
		t.Fatal(e)
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

func TestMySQLUpdateAssetStandaloneStillCommits(t *testing.T) {
	db, m, e := sqlmock.New()
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := NewMySQLStore(db)
	created := time.Now().UTC()
	m.ExpectBegin()
	m.ExpectQuery("SELECT degree, revision, created_at, sort_order, source_batch FROM screenplay_assets").WithArgs("a", "p", "alice").WillReturnRows(sqlmock.NewRows([]string{"degree", "revision", "created_at", "sort_order", "source_batch"}).AddRow("standard", 2, created, 4, 1))
	m.ExpectQuery("SELECT COUNT\\(\\*\\) FROM screenplay_assets").WithArgs("p", "alice", "character", "甲", "a").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	m.ExpectExec("UPDATE screenplay_assets").WillReturnResult(sqlmock.NewResult(0, 1))
	m.ExpectCommit()
	asset, e := s.UpdateAsset(context.Background(), "alice", "p", "a", Asset{Type: AssetCharacter, Name: "甲", Description: "新", Prompt: "保留"}, 2)
	if e != nil || asset.Revision != 3 || asset.SortOrder != 4 || asset.SourceBatch != 1 || asset.Degree != AssetDegree("standard") || asset.Prompt != "保留" {
		t.Fatal(asset, e)
	}
	if e = m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
