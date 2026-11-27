package novelfetchworkshop

import (
	"context"
	"errors"
	"testing"
)

type rewriteWorkflowConfigStore struct {
	*MemoryStore
	workflowErr         error
	strictReads, writes int
}

func (s *rewriteWorkflowConfigStore) GetConfig(context.Context, string) (map[string]any, error) {
	s.strictReads++
	return nil, errors.New("legacy video cleanup pending")
}
func (s *rewriteWorkflowConfigStore) GetWorkflowConfig(ctx context.Context, owner string) (map[string]any, error) {
	if s.workflowErr != nil {
		return nil, s.workflowErr
	}
	return s.MemoryStore.GetConfig(ctx, owner)
}
func (s *rewriteWorkflowConfigStore) PutConfig(ctx context.Context, owner string, cfg map[string]any) error {
	s.writes++
	return s.MemoryStore.PutConfig(ctx, owner, cfg)
}

type readIsolationRewriteProvider struct{ calls int }

func (p *readIsolationRewriteProvider) Complete(context.Context, RewriteRequest) (string, error) {
	p.calls++
	return "改后首行", nil
}
func TestRewriteWorkflowReadDoesNotRunLegacyCleanup(t *testing.T) {
	for _, dbError := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending cleanup", true: "database read failure"}[dbError], func(t *testing.T) {
			ctx := context.Background()
			s := &rewriteWorkflowConfigStore{MemoryStore: NewMemoryStore()}
			s.MemoryStore.PutConfig(ctx, "alice", map[string]any{"appConfig": map[string]any{"rewrite": map[string]any{"default_template_id": "plain", "process_line_count": 1}}, "knowledgeLibraries": map[string]any{"rewrite_templates": map[string]any{"profiles": []any{map[string]any{"id": "plain", "prompt": "保持原文", "response_mode": "plain_text"}}}}})
			s.PutDocument(ctx, "alice", Document{BookID: "123", Original: "首行原文"})
			if dbError {
				s.workflowErr = errors.New("database read unavailable")
			}
			p := &readIsolationRewriteProvider{}
			_, err := GenerateAIRewritesForVersions(ctx, s, "alice", "123", []string{"ai1"}, p)
			if dbError {
				if err == nil || p.calls != 0 {
					t.Fatalf("read failure called provider: calls=%d err=%v", p.calls, err)
				}
			} else {
				body, readErr := s.GetBody(ctx, "alice", "123", "ai1")
				if err != nil || readErr != nil || body.Content != "改后首行" || p.calls != 1 {
					t.Fatalf("pending cleanup blocked supplied verified provider: calls=%d body=%q err=%v readErr=%v", p.calls, body.Content, err, readErr)
				}
			}
			if s.strictReads != 0 || s.writes != 0 {
				t.Fatalf("rewrite entered migration/config write strict=%d writes=%d", s.strictReads, s.writes)
			}
		})
	}
}
