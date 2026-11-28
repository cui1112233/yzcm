package httpapi

import (
	"context"
	"errors"
	"net/http"
	"qiantie/backend/internal/novelfetchworkshop"
	"strings"
	"testing"
	"time"
)

func restoreFixture(t *testing.T) (*novelfetchworkshop.MemoryStore, time.Time, string) {
	t.Helper()
	ctx := context.Background()
	s := novelfetchworkshop.NewMemoryStore()
	cfg := precisionSensitiveSettings(0, false, map[string]any{"find": "禁词", "replace": "合规"})
	mapOrEmpty(cfg["sensitive_ai"])["enabled"] = false
	if err := s.PutConfig(ctx, "tester", cfg); err != nil {
		t.Fatal(err)
	}
	if err := s.PutDocument(ctx, "tester", novelfetchworkshop.Document{BookID: "12345678", MaxTxt: 4, Meta: map[string]any{"originalStatus": "done", "sensitiveStatus": "done", "sensitiveReplaced": 99}}); err != nil {
		t.Fatal(err)
	}
	for v, text := range map[string]string{"original_raw": "禁词正文完整备份", "original": "旧原文", "ai1": "旧AI正文"} {
		if _, err := s.PutBody(ctx, "tester", novelfetchworkshop.BodyRecord{BookID: "12345678", BodyRef: novelfetchworkshop.BodyRef{VersionID: v}, Content: text}); err != nil {
			t.Fatal(err)
		}
	}
	return s, time.Unix(1800000000, 0), "restore-secret"
}
func TestRestoreOriginalRunsCurrentRulesWithoutRewritingBackups(t *testing.T) {
	s, now, secret := restoreFixture(t)
	ctx := context.Background()
	raw, _ := s.GetBody(ctx, "tester", "12345678", "original_raw")
	ai, _ := s.GetBody(ctx, "tester", "12345678", "ai1")
	h := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: s})
	w := doNovelFetchRequest(t, h, http.MethodPost, "/api/novel-fetch-workshop/tasks/12345678/restore-original", map[string]any{}, now, secret)
	body, _ := s.GetBody(ctx, "tester", "12345678", "original")
	latest, _ := s.GetDocument(ctx, "tester", "12345678")
	if w.Code != 200 || body.Content != "合规正文" || latest.ProcessedChars != 4 || intValue(latest.Meta["sensitiveReplaced"]) != 1 {
		t.Fatalf("status=%d body=%q meta=%v response=%s", w.Code, body.Content, latest.Meta, w.Body.String())
	}
	for v, want := range map[string]novelfetchworkshop.BodyRecord{"original_raw": raw, "ai1": ai} {
		got, _ := s.GetBody(ctx, "tester", "12345678", v)
		if got.Content != want.Content || got.Revision != want.Revision || got.ContentHash != want.ContentHash {
			t.Fatalf("backup %s changed", v)
		}
	}
}
func TestRestoreOriginalSensitiveSwitchPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		explicit any
		snapshot any
		account  bool
		calls    int
	}{{"explicit_false", false, true, true, 0}, {"explicit_true", true, false, false, 1}, {"snapshot_false", nil, false, true, 0}, {"snapshot_true", nil, true, false, 1}, {"account_true", nil, nil, true, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			s, now, secret := restoreFixture(t)
			ctx := context.Background()
			cfg, _ := s.GetConfig(ctx, "tester")
			mapOrEmpty(cfg["sensitive_ai"])["enabled"] = tc.account
			_ = s.PutConfig(ctx, "tester", cfg)
			d, _ := s.GetDocument(ctx, "tester", "12345678")
			if tc.snapshot != nil {
				d.Meta["processSnapshot"] = map[string]any{"sensitive_ai_enabled": tc.snapshot}
				_ = s.PutDocument(ctx, "tester", d)
			}
			calls := 0
			h := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: s, NovelFetchRewrite: rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
				calls++
				return "安全正文", nil
			})})
			input := map[string]any{}
			if tc.explicit != nil {
				input["sensitive_ai_enabled"] = tc.explicit
			}
			w := doNovelFetchRequest(t, h, http.MethodPost, "/api/novel-fetch-workshop/tasks/12345678/restore-original", input, now, secret)
			if w.Code != 200 || calls != tc.calls {
				t.Fatalf("status=%d calls=%d want=%d response=%s", w.Code, calls, tc.calls, w.Body.String())
			}
		})
	}
}
func TestRestoreOriginalModelFailurePreservesBodyAndHoldsOriginalOnly(t *testing.T) {
	s, now, secret := restoreFixture(t)
	ctx := context.Background()
	old, _ := s.GetBody(ctx, "tester", "12345678", "original")
	h := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: s, NovelFetchRewriteResolver: func(context.Context, string) (novelfetchworkshop.RewriteProvider, error) {
		return nil, errors.New("failed https://example.invalid?api_key=secret-value")
	}})
	w := doNovelFetchRequest(t, h, http.MethodPost, "/api/novel-fetch-workshop/tasks/12345678/restore-original", map[string]any{"sensitive_ai_enabled": true}, now, secret)
	body, _ := s.GetBody(ctx, "tester", "12345678", "original")
	d, _ := s.GetDocument(ctx, "tester", "12345678")
	if w.Code == 200 || body.Revision != old.Revision || body.Content != old.Content || d.Meta["sensitiveManualRetryRequired"] != true || mapOrEmpty(d.Meta["sensitiveManualRetryErrors"])["original"] == nil {
		t.Fatalf("status=%d body=%q meta=%v", w.Code, body.Content, d.Meta)
	}
	if strings.Contains(w.Body.String(), "secret-value") || strings.Contains(firstText(d.Meta["sensitiveError"]), "secret-value") {
		t.Fatal("unsafe error")
	}
}

type restoreWriteHookStore struct {
	*novelfetchworkshop.MemoryStore
	afterWrite func()
	writeError error
	casError   error
}

func (s *restoreWriteHookStore) PutBodyIfDocumentUnchanged(ctx context.Context, owner string, body novelfetchworkshop.BodyRecord, expected novelfetchworkshop.Document) (novelfetchworkshop.BodyRef, error) {
	if s.writeError != nil {
		return novelfetchworkshop.BodyRef{}, s.writeError
	}
	ref, err := s.MemoryStore.PutBodyIfDocumentUnchanged(ctx, owner, body, expected)
	if err == nil && s.afterWrite != nil {
		s.afterWrite()
	}
	return ref, err
}
func (s *restoreWriteHookStore) CompareAndSwapDocument(ctx context.Context, owner string, d novelfetchworkshop.Document) (string, error) {
	if s.casError != nil {
		return "", s.casError
	}
	return s.MemoryStore.CompareAndSwapDocument(ctx, owner, d)
}
func TestRestoreOriginalConcurrentChangeDuringModelCannotReplaceBody(t *testing.T) {
	for _, change := range []string{"stop", "user_original", "raw"} {
		t.Run(change, func(t *testing.T) {
			s, now, secret := restoreFixture(t)
			ctx := context.Background()
			before, _ := s.GetBody(ctx, "tester", "12345678", "original")
			calls := 0
			provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
				calls++
				d, _ := s.GetDocument(ctx, "tester", "12345678")
				if change == "stop" {
					d.Meta["status"] = "stopped"
					d.Meta["manualOnly"] = true
					_ = s.PutDocument(ctx, "tester", d)
				} else {
					version := "original"
					if change == "raw" {
						version = "original_raw"
					}
					_, err := s.PutBody(ctx, "tester", novelfetchworkshop.BodyRecord{BookID: "12345678", BodyRef: novelfetchworkshop.BodyRef{VersionID: version}, Content: "用户新正文"})
					if err != nil {
						t.Fatal(err)
					}
					if change == "user_original" {
						before, _ = s.GetBody(ctx, "tester", "12345678", "original")
					}
				}
				return "模型安全正文", nil
			})
			h := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: s, NovelFetchRewrite: provider})
			w := doNovelFetchRequest(t, h, http.MethodPost, "/api/novel-fetch-workshop/tasks/12345678/restore-original", map[string]any{"sensitive_ai_enabled": true}, now, secret)
			after, _ := s.GetBody(ctx, "tester", "12345678", "original")
			d, _ := s.GetDocument(ctx, "tester", "12345678")
			if w.Code != 409 || calls != 1 || after.Revision != before.Revision || after.Content != before.Content || d.Meta["sensitiveManualRetryRequired"] != true {
				t.Fatalf("status=%d calls=%d before=%q after=%q meta=%v", w.Code, calls, before.Content, after.Content, d.Meta)
			}
			if change == "stop" && (d.Meta["status"] != "stopped" || d.Meta["manualOnly"] != true) {
				t.Fatal("stop erased")
			}
		})
	}
}
func TestRestoreOriginalLatestMetadataPreservesStopAndOtherVersionFailure(t *testing.T) {
	base, now, secret := restoreFixture(t)
	ctx := context.Background()
	d, _ := base.GetDocument(ctx, "tester", "12345678")
	d.Meta["sensitiveManualRetryErrors"] = map[string]any{"original": "旧恢复失败"}
	d.Meta["sensitiveManualRetryRequired"] = true
	_ = base.PutDocument(ctx, "tester", d)
	s := &restoreWriteHookStore{MemoryStore: base}
	s.afterWrite = func() {
		latest, _ := base.GetDocument(ctx, "tester", "12345678")
		latest.Meta["status"] = "stopped"
		latest.Meta["manualOnly"] = true
		latest.Meta["sensitiveManualRetryErrors"] = map[string]any{"original": "旧恢复失败", "ai1": "新AI失败"}
		_ = base.PutDocument(ctx, "tester", latest)
	}
	h := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: s})
	w := doNovelFetchRequest(t, h, http.MethodPost, "/api/novel-fetch-workshop/tasks/12345678/restore-original", map[string]any{"sensitive_ai_enabled": false}, now, secret)
	latest, _ := base.GetDocument(ctx, "tester", "12345678")
	held := mapOrEmpty(latest.Meta["sensitiveManualRetryErrors"])
	if w.Code != 200 || latest.Meta["status"] != "stopped" || latest.Meta["manualOnly"] != true || firstText(held["ai1"]) != "新AI失败" || held["original"] != nil || latest.Meta["sensitiveManualRetryRequired"] != true {
		t.Fatalf("status=%d meta=%v", w.Code, latest.Meta)
	}
}
func TestRestoreOriginalFailedItemsAndSaveErrorsNeverReportSuccess(t *testing.T) {
	for _, mode := range []string{"model_item", "write", "metadata"} {
		t.Run(mode, func(t *testing.T) {
			base, now, secret := restoreFixture(t)
			ctx := context.Background()
			before, _ := base.GetBody(ctx, "tester", "12345678", "original")
			s := &restoreWriteHookStore{MemoryStore: base}
			if mode == "write" {
				s.writeError = errors.New("fixture body save failure")
			}
			if mode == "metadata" {
				s.casError = errors.New("fixture metadata save failure")
			}
			calls := 0
			provider := rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
				calls++
				return "", errors.New("unexpected EOF")
			})
			h := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: s, NovelFetchRewrite: provider})
			w := doNovelFetchRequest(t, h, http.MethodPost, "/api/novel-fetch-workshop/tasks/12345678/restore-original", map[string]any{"sensitive_ai_enabled": mode == "model_item"}, now, secret)
			after, _ := base.GetBody(ctx, "tester", "12345678", "original")
			latest, _ := base.GetDocument(ctx, "tester", "12345678")
			if w.Code == 200 {
				t.Fatalf("failure reported success: %s", w.Body.String())
			}
			if mode != "metadata" && (after.Revision != before.Revision || after.Content != before.Content || latest.Meta["sensitiveManualRetryRequired"] != true) {
				t.Fatalf("failed processing/save changed body or lost hold: %+v", latest.Meta)
			}
			if mode == "model_item" && calls != 1 {
				t.Fatalf("model calls=%d", calls)
			}
			if mode == "metadata" && !strings.Contains(w.Body.String(), "保存失败状态失败") {
				t.Fatalf("hidden metadata failure: %s", w.Body.String())
			}
		})
	}
}

func TestRestoreOriginalUsesSavedTaskModelWithoutFallback(t *testing.T) {
	for _, mode := range []string{"selected", "unavailable", "empty_resolver"} {
		t.Run(mode, func(t *testing.T) {
			s, now, secret := restoreFixture(t)
			ctx := context.Background()
			d, _ := s.GetDocument(ctx, "tester", "12345678")
			d.Meta["processSnapshot"] = map[string]any{"sensitive_ai_enabled": true, "text_model_id": "task-saved-model"}
			_ = s.PutDocument(ctx, "tester", d)
			old, _ := s.GetBody(ctx, "tester", "12345678", "original")
			observed := ""
			paidCalls, fallbackCalls := 0, 0
			selected := rewriteProviderFunc(func(callCtx context.Context, _ novelfetchworkshop.RewriteRequest) (string, error) {
				paidCalls++
				if novelfetchworkshop.ProcessTextModel(callCtx) != "task-saved-model" {
					t.Error("model selection was lost before provider call")
				}
				return "安全", nil
			})
			h := NewRouter(RouterOptions{BridgeSecret: secret, Now: func() time.Time { return now }, NovelFetchStore: s,
				NovelFetchRewrite: rewriteProviderFunc(func(context.Context, novelfetchworkshop.RewriteRequest) (string, error) {
					fallbackCalls++
					return "账号默认输出", nil
				}),
				NovelFetchRewriteResolver: func(callCtx context.Context, owner string) (novelfetchworkshop.RewriteProvider, error) {
					observed = novelfetchworkshop.ProcessTextModel(callCtx)
					if mode == "unavailable" {
						return nil, errors.New("选中模型不可用")
					}
					if mode == "empty_resolver" {
						return nil, nil
					}
					return selected, nil
				}})
			w := doNovelFetchRequest(t, h, http.MethodPost, "/api/novel-fetch-workshop/tasks/12345678/restore-original", map[string]any{}, now, secret)
			body, _ := s.GetBody(ctx, "tester", "12345678", "original")
			if observed != "task-saved-model" || fallbackCalls != 0 {
				t.Fatalf("observed=%q fallback calls=%d response=%s", observed, fallbackCalls, w.Body.String())
			}
			if mode == "selected" {
				if w.Code != 200 || paidCalls != 1 || body.Content != "安全正文" {
					t.Fatalf("status=%d calls=%d body=%q", w.Code, paidCalls, body.Content)
				}
			} else {
				latest, _ := s.GetDocument(ctx, "tester", "12345678")
				if w.Code == 200 || paidCalls != 0 || body.Revision != old.Revision || body.Content != old.Content || latest.Meta["sensitiveManualRetryRequired"] != true {
					t.Fatalf("status=%d calls=%d body=%q meta=%v", w.Code, paidCalls, body.Content, latest.Meta)
				}
			}
		})
	}
}
