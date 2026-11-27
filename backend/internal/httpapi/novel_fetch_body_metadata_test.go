package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"qiantie/backend/internal/novelfetchworkshop"
	"testing"
	"time"
)

func TestBodyEditRouteUpdatesTaskCountsAndReference(t *testing.T) {
	ctx := context.Background()
	s := novelfetchworkshop.NewMemoryStore()
	d := novelfetchworkshop.Document{BookID: "123", Original: "旧原文四", ProcessedChars: 4, OriginalRaw: "完整原始正文", OriginalRawChars: 7}
	if err := s.PutDocument(ctx, "tester", d); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1800000000, 0)
	handler := NewRouter(RouterOptions{BridgeSecret: "body-test", Now: func() time.Time { return now }, NovelFetchStore: s})
	r := doNovelFetchRequest(t, handler, http.MethodPut, "/api/novel-fetch-workshop/tasks/123/bodies/original", map[string]any{"content": "现在正文已经修改为更长内容"}, now, "body-test")
	if r.Code != http.StatusOK {
		t.Fatalf("PUT=%d %s", r.Code, r.Body.String())
	}
	body, err := s.GetBody(ctx, "tester", "123", "original")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetDocument(ctx, "tester", "123")
	if err != nil {
		t.Fatal(err)
	}
	if body.CharCount != 13 || got.ProcessedChars != 13 || got.BodyRefs["original"].CharCount != 13 || got.BodyRefs["original"].Revision != body.Revision || got.BodyRefs["original"].ContentHash != body.ContentHash {
		t.Fatalf("body/task mismatch: body=%+v task=%+v", body.BodyRef, got)
	}
	if got.OriginalRaw != d.OriginalRaw || got.OriginalRawChars != d.OriginalRawChars {
		t.Fatal("PUT processed body altered full raw")
	}
	detail := doNovelFetchRequest(t, handler, http.MethodGet, "/api/novel-fetch-workshop/tasks/123", nil, now, "body-test")
	if detail.Code != http.StatusOK {
		t.Fatalf("detail=%d %s", detail.Code, detail.Body.String())
	}
	var public novelfetchworkshop.Document
	if err := json.Unmarshal(detail.Body.Bytes(), &public); err != nil {
		t.Fatal(err)
	}
	if public.Original != body.Content || public.ProcessedText != body.Content || public.OriginalRaw != d.OriginalRaw || public.ProcessedChars != 13 {
		t.Fatalf("public task detail lost body content: %+v", public)
	}

}
