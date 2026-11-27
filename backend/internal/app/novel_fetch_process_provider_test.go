package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"qiantie/backend/internal/accountcenter"
	"qiantie/backend/internal/batchfactoryv11"
	"qiantie/backend/internal/httpapi"
	"qiantie/backend/internal/novelfetchworkshop"
)

type novelFetchProviderAccounts struct{ accountcenter.Store }

func (novelFetchProviderAccounts) ResolveIdentity(_ context.Context, name string) (accountcenter.Identity, error) {
	if name == "member" {
		return accountcenter.Identity{Username: name, Role: accountcenter.RoleMember, Active: true, TeamID: "team"}, nil
	}
	if name == "manager" {
		return accountcenter.Identity{Username: name, Role: accountcenter.RoleManager, Active: true}, nil
	}
	return accountcenter.Identity{}, accountcenter.ErrForbidden
}
func (novelFetchProviderAccounts) ListTeams(context.Context, accountcenter.TeamFilter) ([]accountcenter.TeamSummary, error) {
	return []accountcenter.TeamSummary{{Team: accountcenter.Team{ID: "team", ManagerUsername: "manager"}}}, nil
}

func TestNovelFetchProcessCustomProvider(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var request map[string]any
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request["model"] != "baidu/ernie-5.5-flash-preview:free" {
					t.Errorf("wrong model: %v", request["model"])
				}
				if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer fake-local-key" {
					t.Errorf("wrong selected channel target: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"gender\":\"男频\"}"}}]}`))
			}))
			defer server.Close()
			store := httpapi.NewUserConfigStore(t.TempDir())
			path, err := httpapi.UserScopedPath(store.BaseDir, "manager", "api-config.json")
			if err != nil {
				t.Fatal(err)
			}
			if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]any{"apiIntegrations": map[string]any{
				"values": map[string]any{}, "customChannels": []any{map[string]any{
					"id": "ch_local_ofm001", "name": "local test", "group": "text", "baseUrl": server.URL + "/v1",
					"model": "baidu/ernie-5.5-flash-preview:free", "apiKey": "fake-local-key", "enabled": true,
				}}, "channelSelection": map[string]any{"text": map[string]any{"enabled": []any{"ch_local_ofm001"}}},
			}})
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			selected := func(ctx context.Context, owner, model string) (batchfactoryv11.DirectorProvider, error) {
				if owner != "manager" {
					t.Errorf("member config leaked: owner=%s", owner)
				}
				provider, matched, err := httpapi.ResolveIntegrationTextProvider(ctx, store, owner, model)
				if !matched && err == nil {
					err = errors.New("no matching channel")
				}
				return provider, err
			}
			catalog := func(context.Context, string, string) (*batchfactoryv11.OpenAICompatibleProvider, error) {
				return nil, nil
			}
			provider, err := resolveNovelFetchProcessTextProvider(context.Background(), novelFetchProviderAccounts{}, "member", "custom:ch_local_ofm001", selected, catalog)
			if err != nil {
				t.Fatal(err)
			}
			if provider == nil {
				t.Fatal("explicit custom channel resolved to nil instead of saved provider")
			}
			judge := textGenderJudge(provider)
			if judge == nil {
				t.Fatal("custom channel gender judge missing")
			}
			_, judgeErr := judge(context.Background(), novelfetchworkshop.BookInfo{Text: "测试小说正文"})
			adapter := novelFetchRewriteAdapter{provider: provider}
			if got := adapter.ModelIdentity(); got != "baidu/ernie-5.5-flash-preview:free" {
				t.Fatalf("model identity=%q", got)
			}
			_, rewriteErr := adapter.Complete(context.Background(), novelfetchworkshop.RewriteRequest{UserPrompt: "rewrite"})
			if status == http.StatusOK && (judgeErr != nil || rewriteErr != nil) {
				t.Fatalf("judge=%v rewrite=%v", judgeErr, rewriteErr)
			}
			if status != http.StatusOK && (judgeErr == nil || rewriteErr == nil) {
				t.Fatalf("500 should fail both calls: judge=%v rewrite=%v", judgeErr, rewriteErr)
			}
			if got := calls.Load(); got != 2 {
				t.Fatalf("classification and rewrite must call once each, calls=%d", got)
			}
		})
	}
}

func TestNovelFetchProcessProviderSelectionSafety(t *testing.T) {
	calls := 0
	selected := func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
		calls++
		return nil, errors.New("selected unavailable")
	}
	defaultProvider := &batchfactoryv11.OpenAICompatibleProvider{Model: "default"}
	catalogCalls := 0
	catalog := func(context.Context, string, string) (*batchfactoryv11.OpenAICompatibleProvider, error) {
		catalogCalls++
		return defaultProvider, nil
	}
	provider, err := resolveNovelFetchProcessTextProvider(context.Background(), novelFetchProviderAccounts{}, "member", "custom:missing", selected, catalog)
	if err == nil || provider != nil || calls != 1 || catalogCalls != 0 {
		t.Fatalf("explicit unavailable must not fallback: provider=%v err=%v selected=%d catalog=%d", provider, err, calls, catalogCalls)
	}
	provider, err = resolveNovelFetchProcessTextProvider(context.Background(), novelFetchProviderAccounts{}, "forbidden", "custom:missing", selected, catalog)
	if err == nil || provider != nil || calls != 1 {
		t.Fatalf("unauthorized owner reached provider: %v %v calls=%d", provider, err, calls)
	}
	provider, err = resolveNovelFetchProcessTextProvider(context.Background(), novelFetchProviderAccounts{}, "member", "", selected, catalog)
	if err != nil || provider != defaultProvider || calls != 1 {
		t.Fatalf("default selection changed: %v %v", provider, err)
	}
	catalog = func(context.Context, string, string) (*batchfactoryv11.OpenAICompatibleProvider, error) {
		return nil, nil
	}
	provider, err = resolveNovelFetchProcessTextProvider(context.Background(), novelFetchProviderAccounts{}, "member", "", selected, catalog)
	if err != nil || provider != nil {
		t.Fatalf("empty default returned typed nil: %T %v", provider, err)
	}
	var typedNil *batchfactoryv11.OpenAICompatibleProvider
	if judge := textGenderJudge(typedNil); judge != nil {
		t.Fatal("typed nil default must not produce a callable judge")
	}
	if got := (novelFetchRewriteAdapter{provider: typedNil}).ModelIdentity(); got != "" {
		t.Fatalf("typed nil model identity=%q", got)
	}
	if got := (novelFetchRewriteAdapter{provider: defaultProvider}).ModelIdentity(); got != "default" {
		t.Fatalf("legacy model identity=%q", got)
	}
}

func TestNovelFetchProcessExplicitNilCannotFallback(t *testing.T) {
	for _, typed := range []bool{false, true} {
		selected := func(context.Context, string, string) (batchfactoryv11.DirectorProvider, error) {
			if typed {
				var provider *batchfactoryv11.OpenAICompatibleProvider
				return provider, nil
			}
			return nil, nil
		}
		catalog := func(context.Context, string, string) (*batchfactoryv11.OpenAICompatibleProvider, error) {
			t.Fatal("explicit missing provider must not call default catalog")
			return nil, nil
		}
		provider, err := resolveNovelFetchProcessTextProvider(context.Background(), novelFetchProviderAccounts{}, "member", "custom:missing", selected, catalog)
		if provider != nil || !errors.Is(err, batchfactoryv11.ErrConfigRequired) {
			t.Fatalf("typed=%v provider=%T err=%v", typed, provider, err)
		}
	}
}

func TestNovelFetchGenderJudgeBoundsBackgroundModelCall(t *testing.T) {
	provider := fixedTextProvider(func(ctx context.Context, _ batchfactoryv11.TextCompletionRequest) (string, error) {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("gender model call must have the classification time limit")
		}
		if !batchfactoryv11.SingleAttempt(ctx) {
			t.Error("gender model call must remain single attempt")
		}
		return `{"gender":"女频"}`, nil
	})
	if _, err := textGenderJudge(provider)(context.Background(), novelfetchworkshop.BookInfo{Text: "故事正文"}); err != nil {
		t.Fatal(err)
	}
}
