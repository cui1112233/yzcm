package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/aicut"
	"qiantie/backend/internal/novelfetchworkshop"
)

type asrTeamFixture struct {
	accountcenter.Store
	manager string
}

func (f *asrTeamFixture) ResolveIdentity(_ context.Context, username string) (accountcenter.Identity, error) {
	if username == "member" {
		return accountcenter.Identity{Username: username, Role: accountcenter.RoleMember, TeamID: "team", Active: true}, nil
	}
	return accountcenter.Identity{Username: username, Role: accountcenter.RoleManager, Active: true}, nil
}

func (f *asrTeamFixture) ListTeams(context.Context, accountcenter.TeamFilter) ([]accountcenter.TeamSummary, error) {
	if f.manager == "" {
		return nil, nil
	}
	return []accountcenter.TeamSummary{{Team: accountcenter.Team{ID: "team", ManagerUsername: f.manager}}}, nil
}

func TestASRListAndQueuedWorkerUseCurrentTeamCatalog(t *testing.T) {
	ctx := context.Background()
	var currentCalls, staleCalls atomic.Int32
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		staleCalls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer old.Close()
	current := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer current-fixture" {
			t.Error("ASR worker did not use the current model's fixture key")
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"segments":[{"start":0,"end":1,"text":"本地字幕"}]}`)
	}))
	defer current.Close()

	catalog := novelfetchworkshop.NewMemoryStore()
	for _, seed := range []struct{ owner, url, name, key string }{
		{"old-manager", old.URL, "old-manager-model", "old-fixture"},
		{"member", old.URL, "obsolete-personal-model", "personal-fixture"},
		{"current-manager", current.URL, "current-manager-model", "current-fixture"},
	} {
		if err := catalog.PutConfig(ctx, seed.owner, map[string]any{"modelCatalog": []any{map[string]any{
			"id": "asr-main", "kind": "asr", "modelId": seed.name, "endpoint": seed.url, "credential": seed.key, "enabled": true,
		}}}); err != nil {
			t.Fatal(err)
		}
	}
	center := &asrTeamFixture{manager: "old-manager"}
	load := func(ctx context.Context, subject string) (map[string]any, error) {
		return loadSubjectModelConfig(ctx, center, catalog, subject)
	}
	list := func(want string) {
		t.Helper()
		settings, err := load(ctx, "member")
		if available := aicut.AgentASRModelsFromCatalog(settings); err != nil || len(available) != 1 || available[0].Name != want {
			t.Fatalf("formal ASR list did not match current team: %v", err)
		}
	}
	list("old-manager-model")
	store := aicut.NewMemoryStore()
	now := time.Now()
	if err := store.CreateProject(ctx, "member", aicut.Project{ID: "cut", Title: "本地测试", AspectRatio: "9:16", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateAsset(ctx, "member", aicut.Asset{ID: "audio", ProjectID: "cut", Kind: "audio", Name: "local.mp3", ContentType: "audio/mpeg", ObjectKey: "local.mp3", ByteSize: 3, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	service := aicut.NewASRService(aicut.ASRServiceOptions{Store: store, OpenObject: func(context.Context, aicut.Asset) (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader("mp3")), nil
	}, ResolveProvider: func(ctx context.Context, subject, id string) (aicut.ASRProvider, error) {
		settings, err := load(ctx, subject)
		if err != nil {
			return nil, err
		}
		return aicut.ASRProviderFromCatalog(settings, id, current.Client())
	}})
	job, err := service.Submit(ctx, "member", aicut.ASRSubmit{ProjectID: "cut", AssetID: "audio", ModelID: "asr-main"})
	if err != nil {
		t.Fatal(err)
	}
	center.manager = "current-manager"
	list("current-manager-model")
	if err := service.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	job, err = store.ASRJob(ctx, "member", "cut", job.ID)
	if err != nil || job.Owner != "member" || job.State != aicut.ASRSucceeded || currentCalls.Load() != 1 || staleCalls.Load() != 0 {
		t.Fatalf("worker did not preserve member job and fresh provider: %s, %v", job.State, err)
	}
	center.manager = ""
	if _, err := load(ctx, "member"); err == nil {
		t.Fatal("unverified team must block ASR lookup")
	}
}
