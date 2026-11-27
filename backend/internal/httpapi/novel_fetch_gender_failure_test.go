package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"qiantie/backend/internal/novelfetchworkshop"
	"strings"
	"testing"
	"time"
)

func TestManualFetchPreservesGenderFailureReason(t *testing.T) {
	for _, tc := range []struct {
		name, expected string
		resolverErr    error
		answer         string
		judgeErr       error
	}{
		{name: "missing_key", expected: "重新填写 Key", resolverErr: errors.New("文本模型需要在 API 配置中重新填写 Key")},
		{name: "network", expected: "请求超时", judgeErr: context.DeadlineExceeded},
		{name: "invalid_answer", expected: "未返回有效男女频", answer: `{"gender":"未知"}`},
		{name: "invalid_json", expected: "未返回有效男女频", answer: `invalid DO_NOT_LEAK`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(1_800_000_000, 0)
			store := novelfetchworkshop.NewMemoryStore()
			if err := store.PutDocument(context.Background(), "tester", novelfetchworkshop.Document{BookID: "12345678906", Gender: "待确认"}); err != nil {
				t.Fatal(err)
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{"code":200,"data":"原文正文","bookinfo":{"category":"其他","genre":"其他"}}`))
			}))
			defer upstream.Close()
			fallbackCalls, judgeCalls := 0, 0
			handler := NewRouter(RouterOptions{BridgeSecret: "test-secret", Now: func() time.Time { return now }, NovelFetchStore: store,
				NovelFetchFetcher: &novelfetchworkshop.UpstreamFetcher{Client: upstream.Client(), Endpoint: upstream.URL},
				NovelFetchAIGenderJudge: func(context.Context, novelfetchworkshop.BookInfo) (string, error) {
					fallbackCalls++
					return `{"gender":"女频"}`, nil
				},
				NovelFetchAIGenderJudgeResolver: func(context.Context, string) (novelfetchworkshop.AIGenderJudge, error) {
					if tc.resolverErr != nil {
						return nil, tc.resolverErr
					}
					return func(context.Context, novelfetchworkshop.BookInfo) (string, error) {
						judgeCalls++
						return tc.answer, tc.judgeErr
					}, nil
				},
			})
			response := doNovelFetchRequest(t, handler, http.MethodPost, "/api/novel-fetch-workshop/tasks/12345678906/fetch", map[string]any{"maxTxt": 4000}, now, "test-secret")
			if response.Code != http.StatusOK {
				t.Fatalf("fetch status=%d body=%s", response.Code, response.Body.String())
			}
			var body struct {
				Task novelfetchworkshop.Document `json:"task"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			doc := body.Task
			if strings.Contains(response.Body.String(), "DO_NOT_LEAK") {
				t.Fatal("unsafe model response leaked")
			}
			if fallbackCalls != 0 || doc.Gender != "待确认" || !strings.Contains(doc.GenderReason, tc.expected) || !strings.Contains(firstText(doc.Meta["classifyError"]), tc.expected) {
				t.Fatalf("lost classification error or used fallback: fallback=%d calls=%d gender=%q reason=%q meta=%#v", fallbackCalls, judgeCalls, doc.Gender, doc.GenderReason, doc.Meta)
			}
			detail := doNovelFetchRequest(t, handler, http.MethodGet, "/api/novel-fetch-workshop/tasks/12345678906", nil, now, "test-secret")
			if detail.Code != http.StatusOK || !strings.Contains(detail.Body.String(), tc.expected) {
				t.Fatalf("saved detail lost visible classification error: status=%d body=%s", detail.Code, detail.Body.String())
			}
			wantCalls := 1
			if tc.resolverErr != nil {
				wantCalls = 0
			}
			if judgeCalls != wantCalls {
				t.Fatalf("judge calls=%d want=%d", judgeCalls, wantCalls)
			}
		})
	}
}

func TestClassificationMetadataKeepsSavedManualLabels(t *testing.T) {
	doc := novelfetchworkshop.Document{Gender: "男频", GenderSource: novelfetchworkshop.GenderManual, GenderReason: "用户手动指定", Meta: map[string]any{"style": "年代通用", "styleSource": "manual"}}
	syncNovelFetchClassificationMetadata(&doc)
	if doc.Gender != "男频" || doc.Meta["gender"] != "男频" || doc.Meta["genderReason"] != "用户手动指定" || doc.Meta["style"] != "年代通用" || doc.Meta["styleSource"] != "manual" {
		t.Fatalf("manual labels changed: %#v", doc)
	}
}

func TestGenderFailureMetadataClearsOnlyItsOwnErrorAfterResolution(t *testing.T) {
	for _, tc := range []struct {
		name       string
		source     novelfetchworkshop.GenderSource
		otherError string
	}{
		{name: "ai_success", source: novelfetchworkshop.GenderAI},
		{name: "manual_confirmation", source: novelfetchworkshop.GenderManual},
		{name: "unrelated_error", source: novelfetchworkshop.GenderManual, otherError: "风格判断失败，请检查风格配置"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := novelfetchworkshop.Document{Gender: "待确认", GenderSource: novelfetchworkshop.GenderUnknown, GenderReason: "AI 男女频判断失败：请求超时", Meta: map[string]any{"style": "年代通用", "styleSource": "manual"}}
			syncNovelFetchClassificationMetadata(&doc)
			if doc.Meta["classifyError"] != doc.GenderReason {
				t.Fatalf("failure not recorded: %#v", doc.Meta)
			}
			if tc.otherError != "" {
				doc.Meta["classifyError"] = tc.otherError
				// Detail reads may sync again before gender is resolved.
				syncNovelFetchClassificationMetadata(&doc)
				if doc.Meta["classifyError"] != tc.otherError {
					t.Fatalf("pending sync replaced unrelated error: %#v", doc.Meta)
				}
			}
			doc.Gender = "男频"
			doc.GenderSource = tc.source
			doc.GenderReason = "成功确认男频"
			syncNovelFetchClassificationMetadata(&doc)
			if firstText(doc.Meta["classifyError"]) != tc.otherError {
				t.Fatalf("stale or unrelated error changed: %#v", doc.Meta)
			}
			if doc.Meta["genderReason"] != "成功确认男频" || doc.Meta["gender"] != "男频" || doc.Meta["style"] != "年代通用" || doc.Meta["styleSource"] != "manual" {
				t.Fatalf("labels/reason changed: %#v", doc.Meta)
			}
			if firstText(doc.Meta["genderError"]) != "" {
				t.Fatalf("resolved gender error retained: %#v", doc.Meta)
			}
		})
	}
}
