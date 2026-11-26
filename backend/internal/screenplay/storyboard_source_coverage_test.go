package screenplay

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func coverageShot(seq int, ids string) StoryboardShot {
	return StoryboardShot{Seq: seq, SourceElements: json.RawMessage(ids)}
}
func TestSourceCoverageExplicitNumbers(t *testing.T) {
	chapter := Chapter{ID: "c", ScriptText: "1. 开门\n2、回头\n3．离开"}
	cases := []struct {
		name                      string
		shots                     []StoryboardShot
		status                    string
		missing, extra, duplicate []string
	}{
		{"tail omission", []StoryboardShot{coverageShot(1, `["1"]`), coverageShot(2, `["2"]`)}, "incomplete", []string{"3"}, nil, nil},
		{"out of range", []StoryboardShot{coverageShot(1, `["1","2","3","4"]`)}, "incomplete", nil, []string{"4"}, nil},
		{"legitimate split", []StoryboardShot{coverageShot(1, `["1","2"]`), coverageShot(2, `["2","3"]`)}, "passed", nil, nil, nil},
		{"same shot repeats identifier", []StoryboardShot{coverageShot(1, `["1","1","2","3"]`)}, "incomplete", nil, nil, []string{"1"}},
		{"no mapping", []StoryboardShot{{Seq: 1, SourceSceneIndex: 1}}, "unverified", nil, nil, nil},
		{"partial mapping", []StoryboardShot{coverageShot(1, `["1","2","3"]`), {Seq: 2}}, "unverified", nil, nil, nil},
		{"protocol mapping", []StoryboardShot{{Seq: 1, ProtocolFields: json.RawMessage(`{"source_elements":["1","2","3"]}`)}}, "passed", nil, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := CheckStoryboardSourceCoverage(chapter, false, tc.shots)
			if r.Status != tc.status || !reflect.DeepEqual(nonempty(r.Missing), nonempty(tc.missing)) || !reflect.DeepEqual(nonempty(r.Extra), nonempty(tc.extra)) || !reflect.DeepEqual(nonempty(r.Duplicates), nonempty(tc.duplicate)) {
				t.Fatalf("%+v", r)
			}
		})
	}
}
func nonempty(a []string) []string {
	if len(a) == 0 {
		return nil
	}
	return a
}
func TestSourceCoverageUnknownSourceNeverPasses(t *testing.T) {
	for _, c := range []Chapter{{}, {SourceText: "普通自然段"}, {ScriptText: "普通自然段"}, {ScriptText: "1. 一\n1. 二"}} {
		r := CheckStoryboardSourceCoverage(c, false, []StoryboardShot{coverageShot(1, `["1"]`)})
		if r.Status != "unverified" {
			t.Fatalf("%+v", r)
		}
	}
}
func TestSourceCoverageStructuredCurrentScriptOnly(t *testing.T) {
	raw := `{"title":"测试","scenes":[{"location":"屋内","script_content":[{"element_id":"A","element_type":"action","description":"开门"},{"element_id":"B","element_type":"action","description":"离开"}]}]}`
	script, err := professionalScriptMarkdown(raw)
	if err != nil {
		t.Fatal(err)
	}
	c := Chapter{ID: "c", RawResult: raw, ScriptText: script, SourceText: "小说无编号"}
	r := CheckStoryboardSourceCoverage(c, false, []StoryboardShot{coverageShot(1, `["A"]`)})
	if r.Status != "incomplete" || !reflect.DeepEqual(r.Missing, []string{"B"}) {
		t.Fatalf("%+v", r)
	}
	c.ScriptText += "编辑后"
	if r := CheckStoryboardSourceCoverage(c, false, []StoryboardShot{coverageShot(1, `["A","B"]`)}); r.Status != "unverified" {
		t.Fatalf("stale raw passed: %+v", r)
	}
}
func TestStoryboardCheckSourceCoverageSelectedEpisode(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	store.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice", SourceText: "1. 整部小说\n2. 其他集"})
	authors := &AuthoringService{Store: store, Repository: NewMemoryAuthoringRepository()}
	_, err := authors.Save(ctx, "alice", "p", AuthoringState{Chapters: []Chapter{{ID: "a", Title: "一", ScriptText: "1. 第一集"}, {ID: "b", Title: "二", ScriptText: "7. 本集\n8. 结尾"}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	store.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 2, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{coverageShot(1, `["7"]`)}})
	s := StoryboardService{Store: store, Authoring: authors}
	r, err := s.CheckStoryboard(ctx, "alice", "p", 2, EngineXiaoluoV7)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Passed || r.SourceCoverage.Status != "incomplete" || r.SourceCoverage.ChapterID != "b" || !reflect.DeepEqual(r.SourceCoverage.Missing, []string{"8"}) {
		t.Fatalf("%+v", r)
	}
	calls := 0
	_, _, err = FillMissingStoryboardShot(ctx, store, "alice", "p", 2, EngineXiaoluoV7, 8, func(context.Context, string) (string, error) { calls++; return "{}", nil })
	if err == nil || calls != 0 {
		t.Fatalf("source ID fed to seq fill: %v calls=%d", err, calls)
	}
}

func TestSourceCoverageSavedMappingsAndLimits(t *testing.T) {
	c := Chapter{ID: "c", ScriptText: "1. 一\n2. 二"}
	cases := []struct{ name, raw, protocol, status string }{
		{"merged nested arrays", `[["1"],["2"]]`, "", "passed"},
		{"merged legitimate split", `[["1","2"],["2"]]`, "", "passed"},
		{"null mapping", `null`, "", "unverified"},
		{"empty mapping", `[]`, "", "unverified"},
		{"object is not identifier", `{"1":true}`, "", "unverified"},
		{"conflicting persisted mapping", `["1","2"]`, `{"source_elements":["1"]}`, "unverified"},
		{"too deeply nested", `[[[[[[[[[["1"]]]]]]]]]]`, "", "unverified"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := CheckStoryboardSourceCoverage(c, false, []StoryboardShot{{Seq: 1, SourceElements: json.RawMessage(tc.raw), ProtocolFields: json.RawMessage(tc.protocol)}})
			if r.Status != tc.status {
				t.Fatalf("%+v", r)
			}
		})
	}
	c.ScriptText = strings.Repeat("1. 一\n", coverageLimit+1)
	if r := CheckStoryboardSourceCoverage(c, false, []StoryboardShot{coverageShot(1, `["1"]`)}); r.Status != "unverified" {
		t.Fatalf("%+v", r)
	}
	c.ScriptText = "1. 一"
	ids := strings.Repeat(`"1",`, coverageLimit) + `"1"`
	if r := CheckStoryboardSourceCoverage(c, false, []StoryboardShot{coverageShot(1, "["+ids+"]")}); r.Status != "unverified" {
		t.Fatalf("%+v", r)
	}
}

func TestStoryboardCheckSourceCoverageGenerationChapterBinding(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	store.CreateProject(ctx, Project{ID: "p", OwnerUsername: "alice"})
	authors := &AuthoringService{Store: store, Repository: NewMemoryAuthoringRepository()}
	_, err := authors.Save(ctx, "alice", "p", AuthoringState{Chapters: []Chapter{{ID: "a", Title: "一", ScriptText: "1. 其他", SourceText: "5. 本集\n6. 本集尾部"}, {ID: "b", Title: "二", ScriptText: "7. 不能拿这章"}}}, 0)
	if err != nil {
		t.Fatal(err)
	}
	board, err := store.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 2, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{coverageShot(1, `["5"]`)}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.MutateStoryboardGenerations(ctx, "alice", "p", func(state *StoryboardGenerationState) error {
		state.Attempts["run"] = &StoryboardGenerationAttempt{Engine: EngineXiaoluoV7, ChapterID: "a", UseOriginal: true, Episodes: map[int]*StoryboardGenerationEpisode{2: {Storyboard: &board}}}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	s := StoryboardService{Store: store, Authoring: authors}
	r, err := s.CheckStoryboard(ctx, "alice", "p", 2, EngineXiaoluoV7)
	if err != nil {
		t.Fatal(err)
	}
	if r.SourceCoverage.ChapterID != "a" || r.SourceCoverage.Status != "incomplete" || !reflect.DeepEqual(r.SourceCoverage.Missing, []string{"6"}) {
		t.Fatalf("%+v", r)
	}
}

func TestSourceCoverageInvalidOrEditedRawDoesNotGuessNumberMapping(t *testing.T) {
	for _, raw := range []string{`{"scenes":`, "旧版已保存原文"} {
		r := CheckStoryboardSourceCoverage(Chapter{ScriptText: "1. 新版正文", RawResult: raw}, false, []StoryboardShot{coverageShot(1, `["1"]`)})
		if r.Status != "unverified" {
			t.Fatalf("stale raw must not pass: %+v", r)
		}
	}
}

func TestSourceCoverageActualMergePreservesEvidence(t *testing.T) {
	for _, mergeKind := range []string{"duration", "next"} {
		for _, tc := range []struct{ name, second, source, status string }{
			{"all members mapped", `["2"]`, "1. 一\n2. 二", "passed"},
			{"empty member", `[]`, "1. 一", "unverified"},
			{"missing member", ``, "1. 一", "unverified"},
			{"split same source", `["1"]`, "1. 一", "passed"},
		} {
			t.Run(mergeKind+"/"+tc.name, func(t *testing.T) {
				ctx := context.Background()
				store := NewMemoryStore()
				first := coverageShot(1, `["1"]`)
				first.ID = "a"
				first.Duration = 5
				first.VideoDesc = "甲"
				first.ProtocolFields = json.RawMessage(`{"source_elements":["1"]}`)
				second := coverageShot(2, tc.second)
				second.ID = "b"
				second.Duration = 5
				second.VideoDesc = "乙"
				if tc.second != "" {
					second.ProtocolFields = json.RawMessage(`{"source_elements":` + tc.second + `}`)
				}
				_, err := store.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: []StoryboardShot{first, second}})
				if err != nil {
					t.Fatal(err)
				}
				if mergeKind == "duration" {
					_, _, err = MergeStoryboardShotsByDuration(ctx, store, "alice", "p", 1, EngineXiaoluoV7, 15)
				} else {
					_, err = MergeStoryboardShotWithNext(ctx, store, "alice", "p", 1, EngineXiaoluoV7, "a")
				}
				if err != nil {
					t.Fatal(err)
				}
				saved, err := store.GetStoryboard(ctx, "alice", "p", 1, EngineXiaoluoV7)
				if err != nil {
					t.Fatal(err)
				}
				r := CheckStoryboardSourceCoverage(Chapter{ScriptText: tc.source}, false, saved.Shots)
				if r.Status != tc.status {
					t.Fatalf("%+v source=%s protocol=%s", r, saved.Shots[0].SourceElements, saved.Shots[0].ProtocolFields)
				}
				if tc.status == "passed" {
					// Archival metadata must not bypass a real aggregate or per-member conflict.
					broken := saved.Shots[0]
					broken.SourceElements = json.RawMessage(`["999"]`)
					if got := CheckStoryboardSourceCoverage(Chapter{ScriptText: tc.source}, false, []StoryboardShot{broken}); got.Status != "unverified" {
						t.Fatalf("aggregate conflict passed %+v", got)
					}
					var fields map[string]json.RawMessage
					json.Unmarshal(saved.Shots[0].ProtocolFields, &fields)
					var members []StoryboardShot
					json.Unmarshal(fields["merged_shots"], &members)
					members[1].ProtocolFields = json.RawMessage(`{"source_elements":["999"]}`)
					fields["merged_shots"], _ = json.Marshal(members)
					broken = saved.Shots[0]
					broken.ProtocolFields, _ = json.Marshal(fields)
					if got := CheckStoryboardSourceCoverage(Chapter{ScriptText: tc.source}, false, []StoryboardShot{broken}); got.Status != "unverified" {
						t.Fatalf("member conflict passed %+v", got)
					}
				}
			})
		}
	}
}
func TestSourceCoverageEmptyNestedMappingIsUnknown(t *testing.T) {
	r := CheckStoryboardSourceCoverage(Chapter{ScriptText: "1. 一"}, false, []StoryboardShot{coverageShot(1, `[["1"],[]]`)})
	if r.Status != "unverified" {
		t.Fatalf("%+v", r)
	}
}

func TestSourceCoverageRepeatedActualMerge(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	shots := []StoryboardShot{}
	for i, id := range []string{"1", "2", "3"} {
		s := coverageShot(i+1, `["`+id+`"]`)
		s.ID = id
		s.Duration = 5
		s.VideoDesc = "画面"
		s.ProtocolFields = json.RawMessage(`{"source_elements":["` + id + `"]}`)
		shots = append(shots, s)
	}
	_, err := store.SaveStoryboard(ctx, Storyboard{ProjectID: "p", OwnerUsername: "alice", EpisodeNo: 1, Engine: EngineXiaoluoV7, Shots: shots})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = MergeStoryboardShotWithNext(ctx, store, "alice", "p", 1, EngineXiaoluoV7, "1"); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := store.GetStoryboard(ctx, "alice", "p", 1, EngineXiaoluoV7)
	if err != nil {
		t.Fatal(err)
	}
	r := CheckStoryboardSourceCoverage(Chapter{ScriptText: "1. 一\n2. 二\n3. 三"}, false, saved.Shots)
	if r.Status != "passed" || r.CoveredTotal != 3 {
		t.Fatalf("%+v", r)
	}
}
