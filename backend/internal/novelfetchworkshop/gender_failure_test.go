package novelfetchworkshop

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestResolveGenderFailureReasonsAreSafe(t *testing.T) {
	for _, tc := range []struct {
		name, answer, want string
		err                error
	}{
		{name: "missing_key", err: errors.New("文本模型需要在 API 配置中重新填写 Key"), want: "重新填写 Key"},
		{name: "timeout", err: context.DeadlineExceeded, want: "请求超时"},
		{name: "network_url", err: &url.Error{Op: "Post", URL: "https://private.example/?api_key=DO_NOT_LEAK", Err: errors.New("connection reset")}, want: "网络连接失败"},
		{name: "unknown_error", err: errors.New("provider returned DO_NOT_LEAK"), want: "请检查配置"},
		{name: "invalid_json", answer: `{"error":"DO_NOT_LEAK"}`, want: "未返回有效男女频"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			decision := ResolveGender(context.Background(), "", BookInfo{}, func(context.Context, BookInfo) (string, error) { calls++; return tc.answer, tc.err })
			if calls != 1 || decision.Gender != "待确认" || !strings.Contains(decision.Reason, tc.want) || strings.Contains(decision.Reason, "DO_NOT_LEAK") {
				t.Fatalf("calls=%d decision=%#v", calls, decision)
			}
		})
	}
}
