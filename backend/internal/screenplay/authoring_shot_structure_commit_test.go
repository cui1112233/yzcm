package screenplay

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/DATA-DOG/go-sqlmock"
	"testing"
)

func TestMySQLShotStructureTransaction(t *testing.T) {
	for _, op := range []string{"add", "delete"} {
		for _, failure := range []string{"", "mutation", "board", "receipt", "commit", "changed"} {
			t.Run(op+"/"+failure, func(t *testing.T) {
				ctx := context.Background()
				memory := NewMemoryStore()
				memory.CreateProject(ctx, Project{ID: "p", OwnerUsername: "a"})
				memory.SaveStoryboard(ctx, Storyboard{ID: "b", ProjectID: "p", OwnerUsername: "a", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{{ID: "s1", Seq: 1, Duration: 4, VideoDesc: "one"}, {ID: "s2", Seq: 2, Duration: 4, VideoDesc: "two", ProtocolFields: json.RawMessage(`{"media":"keep"}`)}}})
				name, raw := "add_shot", `{"insert_after_shot_id":"s1","new_data":{"line":"new"}}`
				if op == "delete" {
					name, raw = "delete_shots", `{"shot_ids":["s1"]}`
				}
				a, _, e := PrepareAssistantShotEdit(ctx, memory, "a", "p", name, json.RawMessage(raw))
				if e != nil {
					t.Fatal(e)
				}
				d := a.CreativeStructure
				st := AuthoringState{Owner: "a", ProjectID: "p", Revision: 5, Proposals: []AssistantProposal{{ID: "q", Revision: 5, Status: "pending", Actions: []AuthoringAction{a}}}}
				body, _ := json.Marshal(st)
				db, m, e := sqlmock.New()
				if e != nil {
					t.Fatal(e)
				}
				defer db.Close()
				m.ExpectBegin()
				m.ExpectQuery("SELECT id FROM screenplay_projects").WithArgs("p", "a").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("p"))
				m.ExpectQuery("SELECT body,revision FROM screenplay_authoring").WithArgs("a", "p").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(body, 5))
				m.ExpectQuery("SELECT id,revision FROM screenplay_storyboards").WithArgs("p", "a", 1, string(EngineXiaoluoV7)).WillReturnRows(sqlmock.NewRows([]string{"id", "revision"}).AddRow("b", a.ExpectedRevision))
				rows := sqlmock.NewRows([]string{"id", "storyboard_id", "project_id", "owner_username", "seq", "scene_id", "duration_sec", "characters_text", "scene_name", "props_text", "video_desc", "protocol_json"})
				for i, sh := range d.Before {
					protocol, _ := marshalStoryboardProtocol(sh)
					if failure == "changed" && i == 1 {
						sh.VideoDesc = "same version changed"
					}
					rows.AddRow(sh.ID, sh.StoryboardID, sh.ProjectID, sh.OwnerUsername, sh.Seq, sh.SceneID, sh.Duration, sh.Characters, sh.Scene, sh.Props, sh.VideoDesc, protocol)
				}
				m.ExpectQuery("SELECT id,storyboard_id,project_id,owner_username,seq").WithArgs("b", "p", "a").WillReturnRows(rows)
				if failure != "changed" {
					mutations := []string{}
					if op == "delete" {
						mutations = append(mutations, "DELETE FROM screenplay_storyboard_shots")
					}
					mutations = append(mutations, "UPDATE screenplay_storyboard_shots SET seq")
					if op == "add" {
						mutations = append(mutations, "INSERT INTO screenplay_storyboard_shots")
					}
					mutations = append(mutations, "UPDATE screenplay_storyboard_shots SET seq")
					for j, q := range mutations {
						expect := m.ExpectExec(q)
						if failure == "mutation" && j == 0 {
							expect.WillReturnError(errors.New("mutation failed"))
							break
						}
						expect.WillReturnResult(sqlmock.NewResult(1, 1))
					}
					if failure != "mutation" {
						q := m.ExpectExec("UPDATE screenplay_storyboards SET shot_count").WithArgs(len(d.After), sqlmock.AnyArg(), "b", "p", "a", a.ExpectedRevision)
						if failure == "board" {
							q.WillReturnError(errors.New("board failed"))
						} else {
							q.WillReturnResult(sqlmock.NewResult(0, 1))
							q = m.ExpectExec("UPDATE screenplay_authoring").WithArgs(sqlmock.AnyArg(), int64(6), "p", "a", int64(5))
							if failure == "receipt" {
								q.WillReturnError(errors.New("receipt failed"))
							} else {
								q.WillReturnResult(sqlmock.NewResult(0, 1))
							}
						}
					}
				}
				if failure == "" {
					m.ExpectCommit()
				} else if failure == "commit" {
					m.ExpectCommit().WillReturnError(errors.New("uncertain"))
				} else {
					m.ExpectRollback()
				}
				out, e := NewMySQLStore(db).CommitAuthoringShotStructure(ctx, &MySQLAuthoringRepository{db: db}, "a", "p", "q", 5)
				if failure == "" {
					if e != nil || out.Proposals[0].Status != "applied" {
						t.Fatal(e, out)
					}
				} else if e == nil {
					t.Fatal("failure accepted")
				}
				if e = m.ExpectationsWereMet(); e != nil {
					t.Fatal(e)
				}
			})
		}
	}
}
func TestMySQLShotStructureRejectsMixedDatabase(t *testing.T) {
	db, m, _ := sqlmock.New()
	defer db.Close()
	other, n, _ := sqlmock.New()
	defer other.Close()
	for _, r := range []AuthoringRepository{NewMemoryAuthoringRepository(), &MySQLAuthoringRepository{db: other}} {
		if _, e := NewMySQLStore(db).CommitAuthoringShotStructure(context.Background(), r, "a", "p", "q", 1); e == nil {
			t.Fatal("mixed database accepted")
		}
	}
	if e := m.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
	if e := n.ExpectationsWereMet(); e != nil {
		t.Fatal(e)
	}
}
