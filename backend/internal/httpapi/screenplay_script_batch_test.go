package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"qiantie/backend/internal/prompt"
	"strings"
	"testing"
	"time"

	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/screenplay"
)

func TestChapter293HTTPDurableContinueAccessAndForgery(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := screenplay.NewMemoryStore()
	store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice", ModelID: "configured", PromptID: screenplay.ScriptBatchPromptID})
	repo := screenplay.NewMemoryAuthoringRepository()
	svc := screenplay.AuthoringService{Store: store, Repository: repo}
	st, e := svc.Save(ctx, "alice", "p", screenplay.AuthoringState{Chapters: []screenplay.Chapter{{ID: "c", Title: "章", SourceText: "第一句。第二句。"}}}, 0)
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	mux := http.NewServeMux()
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, store, repo, func(ctx context.Context, o, model string) (batchfactoryv11.DirectorProvider, error) {
		if !batchfactoryv11.SingleAttempt(ctx) || o != "alice" || model != "configured" {
			t.Fatal("provider identity/retry mismatch")
		}
		return screenplayProviderFunc(func(ctx context.Context, r batchfactoryv11.TextCompletionRequest) (string, error) {
			calls++
			if !batchfactoryv11.SingleAttempt(ctx) {
				t.Fatal("retry context lost")
			}
			if calls == 2 && (!strings.Contains(r.UserPrompt, "章节原文：\n第二句。") || strings.Contains(r.UserPrompt, "第一句。")) {
				t.Fatal("wrong rollover")
			}
			complete := calls == 2
			rollover := "第二句。"
			if complete {
				rollover = ""
			}
			return fmt.Sprintf(`{"episode_index":1,"title":"章","scenes":[{"scene_number":"1-%d","location":"家","script_content":[{"element_type":"action","action_type":"action","element_id":"1-%d-001","description":"动作。"}]}],"batch_info":{"is_complete":%t,"current_batch":%d},"unprocessed_script_rollover":%q}`, calls, calls, complete, calls, rollover), nil
		}), nil
	})
	path := "/api/comic-screenplays/projects/p/chapters/c/script"
	for _, actor := range []string{"bob", "charlie"} {
		res := signedJSONRequest(t, mux, now, actor, "POST", path, map[string]any{"expected_revision": st.Revision})
		if res.Code != 403 || calls != 0 {
			t.Fatal("cross-user paid", res.Code, calls)
		}
	}
	res := signedJSONRequest(t, mux, now, "alice", "POST", path, map[string]any{"expected_revision": st.Revision, "action": "start"})
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	var payload struct {
		State screenplay.AuthoringState `json:"state"`
	}
	json.Unmarshal(res.Body.Bytes(), &payload)
	st = payload.State
	if st.Chapters[0].ScriptGeneration.Status != "awaiting_continue" || st.Chapters[0].ScriptText != "" || calls != 1 {
		t.Fatal("partial response", res.Body.String())
	}
	res = signedJSONRequest(t, mux, now, "alice", "POST", path, map[string]any{"expected_revision": st.Revision, "action": "start"})
	if res.Code != 422 || calls != 1 {
		t.Fatal("duplicate paid", res.Code, calls)
	}
	// Forged status in an ordinary PUT must be stripped before publication.
	st.Chapters[0].ScriptGeneration = &screenplay.ScriptGeneration{Status: "completed"}
	res = signedJSONRequest(t, mux, now, "alice", "PUT", "/api/comic-screenplays/projects/p/chapters", map[string]any{"expected_revision": st.Revision, "chapters": st.Chapters})
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	json.Unmarshal(res.Body.Bytes(), &payload)
	st = payload.State
	if st.Chapters[0].ScriptGeneration.Status != "awaiting_continue" {
		t.Fatal("forged status trusted")
	}
	// A fresh GET reloads the persisted rollover and advances only on explicit continue.
	res = signedJSONRequest(t, mux, now, "alice", "GET", "/api/comic-screenplays/projects/p/chapters", nil)
	json.Unmarshal(res.Body.Bytes(), &payload)
	st = payload.State
	res = signedJSONRequest(t, mux, now, "alice", "POST", path, map[string]any{"expected_revision": st.Revision, "action": "continue"})
	if res.Code != 200 || calls != 2 {
		t.Fatal(res.Code, res.Body.String(), calls)
	}
	json.Unmarshal(res.Body.Bytes(), &payload)
	if payload.State.Chapters[0].ScriptGeneration.Status != "completed" {
		t.Fatal("not completed")
	}
	doc, e := store.ActiveDocument(ctx, "alice", "p")
	if e != nil || !strings.Contains(doc.Content, "1-2") {
		t.Fatal("completed collection not published", e, doc)
	}
}

func TestChapter293HTTPTemplateSwitchCannotBypassRestart(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	store := screenplay.NewMemoryStore()
	store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice", ModelID: "configured", PromptID: screenplay.ScriptBatchPromptID})
	repo := screenplay.NewMemoryAuthoringRepository()
	svc := screenplay.AuthoringService{Store: store, Repository: repo}
	st, e := svc.Save(ctx, "alice", "p", screenplay.AuthoringState{Chapters: []screenplay.Chapter{{ID: "c", Title: "章", SourceText: "第一句。第二句。"}}}, 0)
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	mux := http.NewServeMux()
	RegisterScreenplayAuthoringRoutes(mux, BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}, store, repo, func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		return screenplayProviderFunc(func(_ context.Context, r batchfactoryv11.TextCompletionRequest) (string, error) {
			calls++
			complete := calls > 1
			rollover := "第二句。"
			if complete {
				rollover = ""
				if strings.Contains(r.SystemPrompt, "服务器分批续接合同") {
					t.Fatal("new144 received old293 continuation prompt")
				}
			}
			return fmt.Sprintf(`{"episode_index":1,"title":"章","scenes":[{"scene_number":"1-1","location":"家","script_content":[{"element_type":"action","action_type":"action","element_id":"1-1-001","description":"动作。"}]}],"batch_info":{"is_complete":%t,"current_batch":1},"unprocessed_script_rollover":%q}`, complete, rollover), nil
		}), nil
	})
	path := "/api/comic-screenplays/projects/p/chapters/c/script"
	res := signedJSONRequest(t, mux, now, "alice", "POST", path, map[string]any{"expected_revision": st.Revision})
	if res.Code != 200 {
		t.Fatal(res.Code, res.Body.String())
	}
	st, _ = repo.Load(ctx, "alice", "p")
	p, _ := store.GetProject(ctx, "alice", "p")
	p.PromptID = prompt.CreativeScript144
	if _, e = store.UpdateProject(ctx, "alice", "p", p, p.Revision); e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"start", "continue", "restart"} {
		res = signedJSONRequest(t, mux, now, "alice", "POST", path, map[string]any{"expected_revision": st.Revision, "action": action})
		if res.Code == 200 || calls != 1 {
			t.Fatal("template switch silently paid", action, res.Code, calls)
		}
	}
	res = signedJSONRequest(t, mux, now, "alice", "POST", path, map[string]any{"expected_revision": st.Revision, "action": "restart", "confirmed": true})
	if res.Code != 200 || calls != 2 {
		t.Fatal("explicit restart failed", res.Code, res.Body.String(), calls)
	}
	st, _ = repo.Load(ctx, "alice", "p")
	g := st.Chapters[0].ScriptGeneration
	if g.Status != "completed" || g.PromptID != prompt.CreativeScript144 || len(g.History) != 1 || g.History[0].PromptID != screenplay.ScriptBatchPromptID {
		t.Fatal("switched run not completed or prior state lost")
	}
}

type scriptReceiptHTTPRepository struct {
	screenplay.AuthoringRepository
	fail bool
}

func (r *scriptReceiptHTTPRepository) Save(ctx context.Context, s screenplay.AuthoringState, v int64) (screenplay.AuthoringState, error) {
	if r.fail {
		return screenplay.AuthoringState{}, errors.New("injected result-save database failure")
	}
	return r.AuthoringRepository.Save(ctx, s, v)
}

type scriptReceiptHTTPStore struct {
	*screenplay.MemoryStore
	repo screenplay.AuthoringRepository
}

func (s *scriptReceiptHTTPStore) AuthoringRepository() screenplay.AuthoringRepository { return s.repo }

func TestChapter293ProductionRouterFileReceiptSurvivesRouterRecreation(t *testing.T) {
	ctx := context.Background()
	now := time.Now()
	root := t.TempDir()
	repo := &scriptReceiptHTTPRepository{AuthoringRepository: screenplay.NewMemoryAuthoringRepository()}
	store := &scriptReceiptHTTPStore{MemoryStore: screenplay.NewMemoryStore(), repo: repo}
	store.CreateProject(ctx, screenplay.Project{ID: "p", OwnerUsername: "alice", PromptID: screenplay.ScriptBatchPromptID, ModelID: "model"})
	svc := screenplay.AuthoringService{Store: store, Repository: repo}
	st, e := svc.Save(ctx, "alice", "p", screenplay.AuthoringState{Chapters: []screenplay.Chapter{{ID: "c", Title: "章", SourceText: "第一句。第二句。"}}}, 0)
	if e != nil {
		t.Fatal(e)
	}
	calls := 0
	resolver := func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		return screenplayProviderFunc(func(context.Context, batchfactoryv11.TextCompletionRequest) (string, error) {
			calls++
			repo.fail = true
			return `{"episode_index":1,"title":"章","scenes":[{"scene_number":"1-1","location":"家","script_content":[{"element_type":"action","action_type":"action","element_id":"1-1-001","description":"动作。"}]}],"batch_info":{"is_complete":false,"current_batch":1},"unprocessed_script_rollover":"第二句。"}`, nil
		}), nil
	}
	auth := BridgeAuth{Secret: "secret", Now: func() time.Time { return now }}
	mux := http.NewServeMux()
	registerScreenplayWorkflow(mux, auth, RouterOptions{Screenplay: store, UserDataDir: root}, resolver, nil)
	path := "/api/comic-screenplays/projects/p/chapters/c/script"
	res := signedJSONRequest(t, mux, now, "alice", "POST", path, map[string]any{"expected_revision": st.Revision})
	if res.Code == 200 || calls != 1 {
		t.Fatal("database failure not injected", res.Code, calls)
	}
	repo.fail = false
	second := http.NewServeMux()
	registerScreenplayWorkflow(second, auth, RouterOptions{Screenplay: store, UserDataDir: root}, nil, nil)
	res = signedJSONRequest(t, second, now, "alice", "GET", "/api/comic-screenplays/projects/p/chapters", nil)
	if res.Code != 200 || strings.Contains(res.Body.String(), root) {
		t.Fatal("GET leaked server receipt path", res.Body.String())
	}
	st, _ = repo.Load(ctx, "alice", "p")
	res = signedJSONRequest(t, second, now, "alice", "POST", path, map[string]any{"expected_revision": st.Revision, "action": "recover"})
	if res.Code != 200 || calls != 1 {
		t.Fatal("fresh production router could not recover", res.Code, res.Body.String(), calls)
	}
	st, _ = repo.Load(ctx, "alice", "p")
	if st.Chapters[0].ScriptGeneration.Status != "awaiting_continue" || len(st.Chapters[0].ScriptGeneration.Batches) != 1 {
		t.Fatal("recovery did not save returned batch")
	}
	found := false
	filepath.WalkDir(filepath.Join(root, "screenplay", "script-batch-receipts"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".json") {
			found = true
		}
		return err
	})
	if !found {
		t.Fatal("production did not persist a file receipt under configured data root")
	}
	// Browser callers cannot supply paths or raw to the recovery action.
	for _, field := range []string{"raw_result", "receipt_path"} {
		res = signedJSONRequest(t, second, now, "alice", "POST", path, map[string]any{"expected_revision": st.Revision, "action": "recover", field: "forged"})
		if res.Code != 400 || calls != 1 {
			t.Fatal("untrusted receipt input accepted", field, res.Code)
		}
	}
	store.CreateProject(ctx, screenplay.Project{ID: "unconfigured", OwnerUsername: "alice", PromptID: screenplay.ScriptBatchPromptID, ModelID: "model"})
	_, e = svc.Save(ctx, "alice", "unconfigured", screenplay.AuthoringState{Chapters: []screenplay.Chapter{{ID: "c", Title: "章", SourceText: "原文。"}}}, 0)
	if e != nil {
		t.Fatal(e)
	}
	unconfigured := http.NewServeMux()
	registerScreenplayWorkflow(unconfigured, auth, RouterOptions{Screenplay: store}, resolver, nil)
	res = signedJSONRequest(t, unconfigured, now, "alice", "POST", "/api/comic-screenplays/projects/unconfigured/chapters/c/script", map[string]any{"expected_revision": 1})
	if res.Code == 200 || calls != 1 {
		t.Fatal("production router without data root billed model", res.Code, calls)
	}
}
