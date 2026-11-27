package screenplay

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

type bindingProtocolArgument struct{}

func (bindingProtocolArgument) Match(value driver.Value) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	var f map[string]json.RawMessage
	if json.Unmarshal([]byte(s), &f) != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(f["protocol_fields"], &fields) != nil {
		return false
	}
	return string(f["future_root"]) == "99" && string(f["source_elements"]) == "[4]" && string(fields["extension"]) == `{"data":1}` && string(fields["asset_bindings"]) == `{"character_ids":["b"]}` && string(fields["scene_id"]) == "4"
}
func TestMySQLBindingLocksAssetsAndRollsBack(t *testing.T) {
	for _, tc := range []string{"success", "revision", "rename", "removed", "receipt"} {
		t.Run(tc, func(t *testing.T) {
			mem, svc := bindingFixture(t)
			a, st := proposeBinding(t, mem, svc, `{"shot_id":"s","mode":"replace","character_ids":["b"]}`)
			sh := a.CreativeShot.Before
			body, _ := json.Marshal(st)
			protocol, _ := marshalStoryboardProtocol(sh)
			var fields map[string]json.RawMessage
			json.Unmarshal([]byte(protocol), &fields)
			fields["future_root"] = json.RawMessage("99")
			saved, _ := json.Marshal(fields)
			db, m, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			s := NewMySQLStore(db)
			m.ExpectBegin()
			m.ExpectQuery("SELECT id FROM screenplay_projects.*FOR UPDATE").WithArgs("p", "alice").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("p"))
			m.ExpectQuery("SELECT body,revision FROM screenplay_authoring.*FOR UPDATE").WithArgs("alice", "p").WillReturnRows(sqlmock.NewRows([]string{"body", "revision"}).AddRow(body, st.Revision))
			m.ExpectQuery("SELECT id,revision FROM screenplay_storyboards.*FOR UPDATE").WithArgs("p", "alice", 1, string(EngineXiaoluoV7)).WillReturnRows(sqlmock.NewRows([]string{"id", "revision"}).AddRow(sh.StoryboardID, a.ExpectedRevision))
			m.ExpectQuery("SELECT id,storyboard_id,project_id,owner_username,seq.*FOR UPDATE").WithArgs("s", sh.StoryboardID, "p", "alice").WillReturnRows(sqlmock.NewRows([]string{"id", "board", "project", "owner", "seq", "sceneid", "duration", "characters", "scene", "props", "video", "protocol"}).AddRow("s", sh.StoryboardID, "p", "alice", 1, 4, 4, "阿青", "旧街", "书", "", saved))
			for _, asset := range a.CreativeShot.Binding.Assets {
				rows := sqlmock.NewRows([]string{"asset_type", "name", "revision"})
				rev, name := asset.Revision, asset.Name
				if asset.ID == "b" {
					if tc == "revision" {
						rev++
					}
					if tc == "rename" {
						name = "别人修改"
					}
				}
				if !(tc == "removed" && asset.ID == "b") {
					rows.AddRow(string(asset.Type), name, rev)
				}
				m.ExpectQuery("SELECT asset_type,name,revision FROM screenplay_assets.*FOR UPDATE").WithArgs(asset.ID, "p", "alice").WillReturnRows(rows)
			}
			if tc == "success" || tc == "receipt" {
				m.ExpectExec("UPDATE screenplay_storyboard_shots SET characters_text=").WithArgs("阿蓝", "旧街", "书", bindingProtocolArgument{}, sqlmock.AnyArg(), "s", sh.StoryboardID, "p", "alice").WillReturnResult(sqlmock.NewResult(0, 1))
				m.ExpectExec("UPDATE screenplay_storyboards SET revision").WithArgs(sqlmock.AnyArg(), sh.StoryboardID, "p", "alice", a.ExpectedRevision).WillReturnResult(sqlmock.NewResult(0, 1))
				update := m.ExpectExec("UPDATE screenplay_authoring SET body").WithArgs(sqlmock.AnyArg(), st.Revision+1, "p", "alice", st.Revision)
				if tc == "success" {
					update.WillReturnResult(sqlmock.NewResult(0, 1))
				} else {
					update.WillReturnError(errors.New("receipt failed"))
				}
			}
			if tc == "success" {
				m.ExpectCommit()
			} else {
				m.ExpectRollback()
			}
			out, e := s.CommitAuthoringShot(context.Background(), s.AuthoringRepository(), "alice", "p", "q", st.Revision)
			if tc == "success" {
				if e != nil || out.Proposals[0].Status != "applied" {
					t.Fatal(out, e)
				}
			} else if e == nil {
				t.Fatal("unsafe commit", tc)
			}
			if e = m.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestBindingLegacyProtectsBothDeletePhases(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		kind AssetType
		raw  string
	}{{AssetCharacter, `{"character_ids":["a"]}`}, {AssetScene, `{"scene_id":"a"}`}, {AssetProp, `{"prop_ids":["a"]}`}} {
		t.Run(string(tc.kind), func(t *testing.T) {
			mem := NewMemoryStore()
			mem.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
			asset, _ := mem.CreateAsset(ctx, Asset{ID: "a", Name: "旧资产", Type: tc.kind, OwnerUsername: "alice", ProjectID: "p"})
			a, _, e := PrepareAssistantAssetEdit(ctx, mem, "alice", "p", "delete_"+string(tc.kind), json.RawMessage(`{"asset_id":"a"}`))
			if e != nil {
				t.Fatal(e)
			}
			sh := StoryboardShot{ID: "legacy", Duration: 4, Seq: 1, ProtocolFields: json.RawMessage(tc.raw)}
			mem.SaveStoryboard(ctx, Storyboard{ID: "board", OwnerUsername: "alice", ProjectID: "p", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{sh}})
			if _, _, e := PrepareAssistantAssetEdit(ctx, mem, "alice", "p", "delete_"+string(tc.kind), json.RawMessage(`{"asset_id":"a"}`)); e == nil || !strings.Contains(e.Error(), "引用") {
				t.Fatal("prepare", e)
			}
			db, m, e := sqlmock.New()
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			m.ExpectBegin()
			tx, e := db.BeginTx(ctx, nil)
			if e != nil {
				t.Fatal(e)
			}
			m.ExpectQuery("SELECT asset_type,name,revision FROM screenplay_assets.*FOR UPDATE").WithArgs("a", "p", "alice").WillReturnRows(sqlmock.NewRows([]string{"type", "name", "revision"}).AddRow(string(tc.kind), asset.Name, asset.Revision))
			m.ExpectQuery("SELECT id,character_names,scene_name,prop_names.*FOR UPDATE").WithArgs("p", "alice").WillReturnRows(sqlmock.NewRows([]string{"id", "characters", "scene", "props"}))
			protocol, _ := marshalStoryboardProtocol(sh)
			m.ExpectQuery("SELECT id,characters_text,scene_name,props_text,protocol_json.*FOR UPDATE").WithArgs("p", "alice").WillReturnRows(sqlmock.NewRows([]string{"id", "characters", "scene", "props", "protocol"}).AddRow("legacy", "", "", "", protocol))
			if e := deleteAssistantAssetInTx(ctx, tx, "alice", "p", a); e == nil || !strings.Contains(e.Error(), "legacy") {
				t.Fatal("confirm", e)
			}
			m.ExpectRollback()
			tx.Rollback()
			if e := m.ExpectationsWereMet(); e != nil {
				t.Fatal(e)
			}
		})
	}
}
