package novelfetchworkshop

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRewriteFailurePersistsOnlySafeError(t *testing.T) {
	ctx := context.Background()
	s := plainRewriteTestStore("alice")
	_ = s.PutDocument(ctx, "alice", Document{BookID: "123", Original: "真实小说正文。"})
	sentinel := errors.New("api_key=fixture-persist-secret")
	_, err := GenerateAIRewritesForVersions(ctx, s, "alice", "123", []string{"ai1"}, rewriteProviderFunc(func(context.Context, RewriteRequest) (string, error) { return "", sentinel }))
	d, _ := s.GetDocument(ctx, "alice", "123")
	if err == nil || !errors.Is(err, sentinel) || strings.Contains(err.Error(), "fixture-persist-secret") || strings.Contains(firstTextForErrorTest(d.Meta["aiError"]), "fixture-persist-secret") {
		t.Fatal("rewrite error was not safely persisted with identity")
	}
}
func firstTextForErrorTest(v any) string { s, _ := v.(string); return s }
