package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
)

const assistantContentOld = "# 第1集：旧稿\n## 1-1：日 内 家\n人物：小明\n小明：旧稿"
const assistantContentNew = "# 第1集：新稿\n## 1-1：日 内 家\n人物：小明\n小明：新稿"

func TestAuthoringAssistantContentCannotBindToNewDocumentAfterModel(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := screenplay.NewMemoryStore()
	store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice", ModelID: "text"})
	repo := screenplay.NewMemoryAuthoringRepository()
	first, err := screenplay.PublishChapterScripts(ctx, store, "alice", "p", assistantContentOld)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	var next screenplay.Document
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, store, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		return screenplayProviderFunc(func(context.Context, batchfactoryv11.TextCompletionRequest) (string, error) {
			next, err = screenplay.PublishChapterScripts(ctx, store, "alice", "p", assistantContentNew)
			if err != nil || next.Revision != first.Revision || next.ID == first.ID {
				t.Fatalf("fixture: first=%+v next=%+v err=%v", first, next, err)
			}
			return `{"reply":"已改写","actions":[{"kind":"document","expected_revision":1,"content":"# 第1集：模型稿\n## 1-1：日 内 家\n小明：模型稿"}]}`, nil
		}), nil
	})
	res := signedJSONRequest(t, mux, now, "alice", "POST", "/api/comic-screenplays/projects/p/assistant/chat", map[string]any{"expected_revision": 0, "message": "改写剧本"})
	if res.Code != http.StatusConflict {
		t.Fatalf("old model response prepared against a different active document: %d %s", res.Code, res.Body.String())
	}
	if current, err := store.ActiveDocument(ctx, "alice", "p"); err != nil || current.ID != next.ID || !strings.Contains(current.Content, "新稿") {
		t.Fatal(current, err)
	}
}

func TestAuthoringAssistantContentUsesServerEvidence(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := screenplay.NewMemoryStore()
	store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice", ModelID: "text"})
	repo := screenplay.NewMemoryAuthoringRepository()
	active, err := screenplay.PublishChapterScripts(ctx, store, "alice", "p", assistantContentOld)
	if err != nil {
		t.Fatal(err)
	}
	var reply string
	mux := http.NewServeMux()
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, store, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		return screenplayProviderFunc(func(context.Context, batchfactoryv11.TextCompletionRequest) (string, error) {
			return reply, nil
		}), nil
	})
	base := "/api/comic-screenplays/projects/p/assistant/chat"
	send := func(action string) int {
		t.Helper()
		reply = `{"reply":"正文已替换","actions":[` + action + `]}`
		return signedJSONRequest(t, mux, now, "alice", "POST", base, map[string]any{"expected_revision": 0, "message": "准备改动"}).Code
	}
	for _, action := range []string{
		`{"kind":"document","content":"稿件","content_edit":{"document":{"id":"forged"}}}`,
		`{"kind":"document","content":"稿件","target_id":"forged"}`,
		`{"kind":"chapter","chapter":{"id":"c","script_text":"稿件","planning_status":"completed"}}`,
		`{"kind":"chapter","chapter":{"id":"c","source_text":"原文","script_generation":{"status":"completed"}}}`,
		`{"kind":"chapter","chapter":{"id":"c","Title":"别名"}}`,
		`{"kind":"chapter","chapter":{"id":"c","title":"一","t\u0069tle":"二"}}`,
	} {
		if status := send(action); status != 400 {
			t.Fatalf("forged or ambiguous action %s: %d", action, status)
		}
	}
	content, _ := json.Marshal(assistantContentNew)
	if status := send(`{"kind":"document","expected_revision":9999,"content":` + string(content) + `}`); status != 200 {
		t.Fatal(status)
	}
	st, err := repo.Load(ctx, "alice", "p")
	if err != nil || len(st.Proposals) != 1 || len(st.Messages) != 2 {
		t.Fatal(st, err)
	}
	action := st.Proposals[0].Actions[0]
	if action.ExpectedRevision != active.Revision || action.TargetID != active.ID || action.ContentEdit.Document.ID != active.ID || !strings.Contains(st.Messages[1].Content, "请核对后确认应用") {
		t.Fatal(st)
	}
}

func TestAuthoringAssistantContentRejectsLegacyDocumentWithEqualRevision(t *testing.T) {
	ctx := context.Background()
	store := screenplay.NewMemoryStore()
	store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
	repo := screenplay.NewMemoryAuthoringRepository()
	svc := screenplay.AuthoringService{Store: store, Repository: repo}
	first, err := screenplay.PublishChapterScripts(ctx, store, "alice", "p", assistantContentOld)
	if err != nil {
		t.Fatal(err)
	}
	old := screenplay.AssistantProposal{ID: "old", Revision: 1, Status: "pending", Actions: []screenplay.AuthoringAction{{Kind: "document", Content: assistantContentOld, ExpectedRevision: first.Revision}}}
	if _, err := repo.Save(ctx, screenplay.AuthoringState{ProjectID: "p", Owner: "alice", Proposals: []screenplay.AssistantProposal{old}}, 0); err != nil {
		t.Fatal(err)
	}
	second, err := screenplay.PublishChapterScripts(ctx, store, "alice", "p", assistantContentNew)
	if err != nil || second.Revision != first.Revision || second.ID == first.ID {
		t.Fatal(first, second, err)
	}
	if _, err := svc.Apply(ctx, "alice", "p", "old", 1, true); err != screenplay.ErrRevisionConflict {
		t.Fatalf("legacy action without server evidence: %v", err)
	}
	st, err := svc.Load(ctx, "alice", "p")
	if err != nil || len(st.Proposals) != 1 || st.Proposals[0].ID != old.ID || st.Proposals[0].Status != "stale" {
		t.Fatal(st, err)
	}
	if current, err := store.ActiveDocument(ctx, "alice", "p"); err != nil || current.ID != second.ID {
		t.Fatal(current, err)
	}
}

func TestAuthoringAssistantContentPrepareAndApplyWithoutModel(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := screenplay.NewMemoryStore()
	store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice"})
	store.CreateProject(ctx, screenplay.Project{ID: "other", OwnerUsername: "alice"})
	repo := screenplay.NewMemoryAuthoringRepository()
	svc := screenplay.AuthoringService{Store: store, Repository: repo}
	st, _ := svc.Load(ctx, "alice", "p")
	st.Chapters = []screenplay.Chapter{{ID: "c", Title: "旧章", SourceText: "真实原文", ScriptText: assistantContentOld, RawResult: "旧结果", PlanningPromptID: "138"}}
	st.Members = []screenplay.MemberGrant{{Username: "bob", Read: true}, {Username: "editor", Read: true, Edit: true}}
	if _, err := svc.Save(ctx, "alice", "p", st, 0); err != nil {
		t.Fatal(err)
	}
	first, err := screenplay.PublishChapterScripts(ctx, store, "alice", "p", assistantContentOld)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, store, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		t.Fatal("manual content preparation used a model")
		return nil, nil
	})
	base := "/api/comic-screenplays/projects/p/assistant"
	call := func(actor, path string, payload any, want int) screenplay.AuthoringState {
		t.Helper()
		res := signedJSONRequest(t, mux, now, actor, "POST", path, payload)
		if res.Code != want {
			t.Fatalf("%s %s: %d %s", actor, path, res.Code, res.Body.String())
		}
		var reply struct {
			State screenplay.AuthoringState `json:"state"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &reply); err != nil {
			t.Fatal(err)
		}
		return reply.State
	}
	prepare := base + "/prepare-content-edit"
	body := map[string]any{"expected_revision": 1, "kind": "document", "content": assistantContentNew}
	call("bob", prepare, body, 403)
	call("other", prepare, body, 403)
	call("alice", prepare, map[string]any{"kind": "document", "content": assistantContentNew}, 400)
	call("alice", prepare, map[string]any{"expected_revision": 0, "kind": "document", "content": assistantContentNew}, 409)
	call("alice", prepare, map[string]any{"expected_revision": 1, "kind": "chapter", "chapter": map[string]any{"id": "c", "script_text": "无场次的稿件"}}, 400)
	call("alice", prepare, map[string]any{"expected_revision": 1, "kind": "chapter", "chapter": map[string]any{"id": "c", "title": "伪造", "script_generation": map[string]any{"status": "completed"}}}, 400)
	call("alice", prepare, map[string]any{"expected_revision": 1, "kind": "chapter", "chapter": map[string]any{"id": "absent", "title": "不存在"}}, 404)
	st = call("editor", prepare, body, 200)
	docEdit := st.Proposals[0].Actions[0].ContentEdit
	if st.Revision != 2 || st.Proposals[0].Status != "pending" || docEdit == nil || docEdit.Document.ID != first.ID || docEdit.Document.SHA256 != first.ContentSHA256 {
		t.Fatal(st)
	}
	doc, _ := store.ActiveDocument(ctx, "alice", "p")
	if doc.ID != first.ID {
		t.Fatal("preparing changed content", doc)
	}
	apply := base + "/apply"
	confirmation := map[string]any{"expected_revision": 2, "proposal_id": st.Proposals[0].ID, "confirmed": false}
	call("editor", apply, confirmation, 400)
	confirmation["confirmed"] = true
	call("bob", apply, confirmation, 403)
	call("alice", strings.Replace(apply, "/p/", "/other/", 1), map[string]any{"expected_revision": 0, "proposal_id": st.Proposals[0].ID, "confirmed": true}, 404)
	second, err := screenplay.PublishChapterScripts(ctx, store, "alice", "p", assistantContentNew)
	if err != nil || second.Revision != first.Revision || second.ID == first.ID {
		t.Fatal(first, second, err)
	}
	call("editor", apply, confirmation, 409)
	read := signedJSONRequest(t, mux, now, "editor", "GET", base+"/chat", nil)
	if read.Code != 200 || !strings.Contains(read.Body.String(), `"status":"stale"`) {
		t.Fatal(read.Code, read.Body.String())
	}
	doc, _ = store.ActiveDocument(ctx, "alice", "p")
	if doc.ID != second.ID {
		t.Fatal("old proposal replaced newer document", doc)
	}
	st = call("editor", prepare, map[string]any{"expected_revision": 2, "kind": "chapter", "chapter": map[string]any{"id": "c", "title": "新章", "script_text": assistantContentNew}}, 200)
	last := st.Proposals[len(st.Proposals)-1]
	if st.Chapters[0].Title != "旧章" || last.Status != "pending" || last.Actions[0].ContentEdit.Document.ID != second.ID {
		t.Fatal(st)
	}
	confirmation = map[string]any{"expected_revision": 3, "proposal_id": last.ID, "confirmed": true}
	st = call("editor", apply, confirmation, 200)
	if st.Revision != 4 || st.Proposals[1].Status != "applied" || st.Proposals[0].Status != "stale" || st.Chapters[0].Title != "新章" || st.Chapters[0].SourceText != "真实原文" || st.Chapters[0].RawResult != "" || st.Chapters[0].PlanningPromptID != "138" {
		t.Fatal(st)
	}
	confirmation["expected_revision"] = 4
	call("editor", apply, confirmation, 409)
	docs, _ := store.ListDocuments(ctx, "alice", "p")
	savedOld, _ := store.GetDocument(ctx, "alice", "p", first.ID)
	if len(docs) != 3 || docs[0].ID == second.ID || savedOld.Content != assistantContentOld {
		t.Fatal(docs, savedOld)
	}
}
