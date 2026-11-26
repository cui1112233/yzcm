package screenplay

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func shotCommitFixture() (AuthoringState, AuthoringAction) {
	before := StoryboardShot{ID: "s", StoryboardID: "b", ProjectID: "p", OwnerUsername: "alice", Seq: 1, Duration: 4, VideoDesc: "video", ImagePrompt: "image", Line: "dialogue", SourceElements: json.RawMessage(`[1]`), ProtocolFields: json.RawMessage(`{"binding":"asset","media":"keep"}`)}
	after := before
	after.ImagePrompt = "new"
	a := AuthoringAction{Kind: "creative_shot", TargetID: "s", ExpectedRevision: 2, CreativeShot: &AssistantCreativeShotEdit{BoardID: "b", Episode: 1, Engine: EngineXiaoluoV7, Before: before, After: after, Changes: []AssistantShotChange{{"image_prompt", "image", "new"}}}}
	return AuthoringState{ProjectID: "p", Owner: "alice", Revision: 5, Proposals: []AssistantProposal{{ID: "q", Revision: 5, Status: "pending", Actions: []AuthoringAction{a}}}}, a
}

type preservedShotProtocol struct{}

func (preservedShotProtocol) Match(v driver.Value) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	var f map[string]json.RawMessage
	if json.Unmarshal([]byte(s), &f) != nil {
		return false
	}
	var image, line, unknown string
	json.Unmarshal(f["image_prompt"], &image)
	json.Unmarshal(f["line"], &line)
	json.Unmarshal(f["future_extension"], &unknown)
	return image == "new" && line == "dialogue" && unknown == "keep" && string(f["source_elements"]) == `[1]`
}

func TestMySQLAuthoringShotAtomicRollback(t *testing.T) {
	for _, failure := range []string{"", "shot", "board", "receipt", "commit", "stale-board", "stale-state", "changed-shot"} {
		t.Run(failure, func(t *testing.T) {
			db, m, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			s := NewMySQLStore(db)
			st, a := shotCommitFixture()
			body, _ := json.Marshal(st)
			protocol, _ := marshalStoryboardProtocol(a.CreativeShot.Before)
			var raw map[string]any
			json.Unmarshal([]byte(protocol), &raw)
			raw["future_extension"] = "keep"
			protocolBytes, _ := json.Marshal(raw)
			m.ExpectBegin()
			m.ExpectQuery("SELECT id FROM screenplay_projects").WithArgs("p", "alice").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("p"))
			rev := 5
			if failure == "stale-state" {
				rev = 6
			}
			m.ExpectQuery("SELECT body,revision FROM screenplay_authoring").WithArgs("alice", "p").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(body, rev))
			if failure != "stale-state" {
				boardRev := 2
				if failure == "stale-board" {
					boardRev = 3
				}
				m.ExpectQuery("SELECT id,revision FROM screenplay_storyboards").WithArgs("p", "alice", 1, string(EngineXiaoluoV7)).WillReturnRows(sqlmock.NewRows([]string{"id", "revision"}).AddRow("b", boardRev))
				if failure != "stale-board" {
					video := "video"
					if failure == "changed-shot" {
						video = "concurrent"
					}
					m.ExpectQuery("SELECT id,storyboard_id,project_id,owner_username,seq").WithArgs("s", "b", "p", "alice").WillReturnRows(sqlmock.NewRows([]string{"id", "storyboard_id", "project_id", "owner_username", "seq", "scene_id", "duration_sec", "characters_text", "scene_name", "props_text", "video_desc", "protocol_json"}).AddRow("s", "b", "p", "alice", 1, 0, 4, "", "", "", video, protocolBytes))
					if failure != "changed-shot" {
						q := m.ExpectExec("UPDATE screenplay_storyboard_shots").WithArgs("video", preservedShotProtocol{}, sqlmock.AnyArg(), "s", "b", "p", "alice")
						if failure == "shot" {
							q.WillReturnError(errors.New("shot failed"))
						} else {
							q.WillReturnResult(sqlmock.NewResult(0, 1))
						}
						if failure != "shot" {
							q = m.ExpectExec("UPDATE screenplay_storyboards SET revision").WithArgs(sqlmock.AnyArg(), "b", "p", "alice", int64(2))
							if failure == "board" {
								q.WillReturnError(errors.New("board failed"))
							} else {
								q.WillReturnResult(sqlmock.NewResult(0, 1))
							}
							if failure != "board" {
								q = m.ExpectExec("UPDATE screenplay_authoring").WithArgs(sqlmock.AnyArg(), int64(6), "p", "alice", int64(5))
								if failure == "receipt" {
									q.WillReturnError(errors.New("receipt failed"))
								} else {
									q.WillReturnResult(sqlmock.NewResult(0, 1))
								}
							}
						}
					}
				}
			}
			if failure == "" {
				m.ExpectCommit()
			} else if failure == "commit" {
				m.ExpectCommit().WillReturnError(errors.New("uncertain commit"))
			} else {
				m.ExpectRollback()
			}
			out, e := s.CommitAuthoringShot(context.Background(), s.AuthoringRepository(), "alice", "p", "q", 5)
			if failure == "" {
				if e != nil || out.Revision != 6 || out.Proposals[0].Status != "applied" {
					t.Fatal(out, e)
				}
			} else if e == nil {
				t.Fatal("failed transaction succeeded", failure)
			}
			if e = m.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestMySQLAuthoringShotRejectsMixedRepository(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	s := NewMySQLStore(db)
	if _, e := s.CommitAuthoringShot(context.Background(), NewMemoryAuthoringRepository(), "alice", "p", "q", 5); e == nil {
		t.Fatal("mixed storage")
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}

func TestAssistantShotRejectsChangedBoardBeforeConfirmation(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	s.SaveStoryboard(ctx, Storyboard{ID: "b", ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{ID: "s", Seq: 1, Duration: 4, VideoDesc: "old"}}})
	a, _, e := PrepareAssistantShotEdit(ctx, s, "alice", "p", "modify_video_prompt", json.RawMessage(`{"shot_number":1,"action":"replace","new_prompt":"new"}`))
	if e != nil {
		t.Fatal(e)
	}
	r := NewMemoryAuthoringRepository()
	svc := AuthoringService{Store: s, Repository: r}
	st, _ := svc.Load(ctx, "alice", "p")
	st.Proposals = []AssistantProposal{{ID: "q", Revision: 1, Status: "pending", Actions: []AuthoringAction{a}}}
	st, _ = svc.Save(ctx, "alice", "p", st, 0)
	b, _ := s.GetStoryboard(ctx, "alice", "p", 1, EngineXiaoluoV7)
	b.Shots[0].VideoDesc = "concurrent"
	b, _ = s.SaveStoryboardRevision(ctx, b, b.Revision)
	if _, e = svc.Apply(ctx, "alice", "p", "q", st.Revision, true); !errors.Is(e, ErrRevisionConflict) {
		t.Fatal(e)
	}
	current, _ := s.GetStoryboard(ctx, "alice", "p", 1, EngineXiaoluoV7)
	saved, _ := r.Load(ctx, "alice", "p")
	if current.Revision != b.Revision || current.Shots[0].VideoDesc != "concurrent" || saved.Proposals[0].Status != "pending" {
		t.Fatal(fmt.Sprint(current), saved)
	}
}

type rejectedShotCommitter struct{ *MemoryStore }

func (s *rejectedShotCommitter) CommitAuthoringShot(context.Context, AuthoringRepository, string, string, string, int64) (AuthoringState, error) {
	return AuthoringState{}, ErrInvalidState
}
func TestAuthoringShotAtomicFailureNeverFallsBack(t *testing.T) {
	ctx := context.Background()
	s := &rejectedShotCommitter{NewMemoryStore()}
	s.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	s.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{ID: "s", Seq: 1, Duration: 4, VideoDesc: "old"}}})
	a, _, _ := PrepareAssistantShotEdit(ctx, s, "alice", "p", "modify_video_prompt", json.RawMessage(`{"shot_id":"s","new_prompt":"new"}`))
	repo := NewMemoryAuthoringRepository()
	svc := AuthoringService{Store: s, Repository: repo}
	st, _ := svc.Load(ctx, "alice", "p")
	st.Proposals = []AssistantProposal{{ID: "q", Revision: 1, Status: "pending", Actions: []AuthoringAction{a}}}
	st, _ = svc.Save(ctx, "alice", "p", st, 0)
	if _, e := svc.Apply(ctx, "alice", "p", "q", st.Revision, true); !errors.Is(e, ErrInvalidState) {
		t.Fatal(e)
	}
	b, _ := s.GetStoryboard(ctx, "alice", "p", 1, EngineXiaoluoV7)
	st, _ = repo.Load(ctx, "alice", "p")
	if b.Revision != a.ExpectedRevision || b.Shots[0].VideoDesc != "old" || st.Proposals[0].Status != "pending" {
		t.Fatal(b, st)
	}
}
