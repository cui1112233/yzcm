package novelfetchworkshop

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRewritePlanReferenceOff(t *testing.T) {
	for _, method := range []string{"opening_instruction", "high_imitation"} {
		settings := map[string]any{"rewrite": map[string]any{"strategy": method, "method_sequence": []any{method}, "opening_phrase_mode": "off", "high_imitation_mode": "off"}, "knowledgeLibraries": map[string]any{"opening_phrases": map[string]any{"items": []any{map[string]any{"content": "FORBIDDEN_OPENING"}}}, "high_imitation": map[string]any{"references": []any{map[string]any{"content": "FORBIDDEN_REFERENCE"}}}}}
		plan := rewritePlan(settings, "ai1", 1)
		prompt, _, _, _ := buildRewritePrompt(Document{Meta: map[string]any{}}, "真实小说正文。", 1, plan)
		if strings.Contains(prompt, "FORBIDDEN_") {
			t.Fatalf("%s off still injects reference: %s", method, prompt)
		}
	}
}

func TestRewritePlanStrategyFallback(t *testing.T) {
	if plan := rewritePlan(map[string]any{"rewrite": map[string]any{"strategy": "high_imitation"}}, "ai1", 1); plan.Method != "high_imitation" {
		t.Fatalf("strategy ignored: %q", plan.Method)
	}
}

func TestRewritePlanMatchesStyleThenGeneric(t *testing.T) {
	for _, method := range []string{"opening_instruction", "high_imitation"} {
		library := map[string]any{"items": []any{
			map[string]any{"name": "wrong", "style": "玄幻", "content": "WRONG_GENRE"},
			map[string]any{"name": "generic", "content": "GENERIC_REFERENCE"},
			map[string]any{"name": "disabled", "enabled": false, "style": "现代甜文", "content": "DISABLED_REFERENCE"},
			map[string]any{"name": "match", "style": "现代甜文", "gender": "女频", "content": "MATCHING_REFERENCE"},
		}}
		settings := map[string]any{"rewrite": map[string]any{"strategy": method}, "knowledgeLibraries": map[string]any{"opening_phrases": library, "high_imitation": library}}
		for _, tc := range []struct{ style, want string }{{"现代甜文", "MATCHING_REFERENCE"}, {"末世", "GENERIC_REFERENCE"}} {
			doc := Document{Gender: "女频", Meta: map[string]any{"style": tc.style}}
			plan := rewritePlan(settings, "ai1", 1, doc)
			prompt, _, _, _ := buildRewritePrompt(doc, "真实正文。", 1, plan)
			if !strings.Contains(prompt, tc.want) || strings.Contains(prompt, "WRONG_GENRE") || strings.Contains(prompt, "DISABLED_REFERENCE") {
				t.Fatalf("%s/%s reference mismatch: %s", method, tc.style, prompt)
			}
		}
		doc := Document{Gender: "女频", Meta: map[string]any{"style": "现代甜文", "opening_phrase_name": "wrong", "high_imitation_reference_name": "wrong"}}
		plan := rewritePlan(settings, "ai1", 1, doc)
		if !strings.Contains(strings.Join(plan.Instructions, "\n"), "WRONG_GENRE") {
			t.Fatal("explicit user reference choice was discarded")
		}
	}
}

func TestRewritePlanNoMatchingReferenceDoesNotBorrowUnrelatedGenre(t *testing.T) {
	s := map[string]any{"rewrite": map[string]any{"strategy": "opening_instruction"}, "knowledgeLibraries": map[string]any{"opening_phrases": map[string]any{"items": []any{map[string]any{"style": "玄幻", "content": "UNRELATED_REFERENCE"}}}}}
	plan := rewritePlan(s, "ai1", 1, Document{Meta: map[string]any{"style": "现代甜文"}})
	if strings.Contains(strings.Join(plan.Instructions, "\n"), "UNRELATED_REFERENCE") {
		t.Fatal("borrowed unrelated genre")
	}
}

func TestRewritePlanUsesSavedHighReferenceTextPayload(t *testing.T) {
	for _, tc := range []struct {
		name, mode, style string
		want              bool
	}{
		{"matching", "auto", "现代甜文", true},
		{"off", "off", "现代甜文", false},
		{"wrong-style", "auto", "玄幻", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := map[string]any{"rewrite": map[string]any{"strategy": "high_imitation", "high_imitation_mode": tc.mode}}
			// Same field names and collection as the real knowledge editor save.
			_, err := SaveKnowledge(settings, "high_imitation", map[string]any{
				"id": "saved-reference", "name": "页面高仿参考", "collection": "references",
				"reference_text": "REAL_REFERENCE_BODY", "analysis": "参考拆解", "style": tc.style,
				"gender": "女频", "tags": []any{"情感"}, "enabled": true,
			})
			if err != nil {
				t.Fatal(err)
			}
			doc := Document{Gender: "女频", Meta: map[string]any{"style": "现代甜文"}}
			plan := rewritePlan(settings, "ai1", 1, doc)
			prompt, _, _, _ := buildRewritePrompt(doc, "真实原文。", 1, plan)
			if strings.Contains(prompt, "REAL_REFERENCE_BODY") != tc.want {
				t.Fatalf("saved reference_text selected=%v want=%v prompt=%s", strings.Contains(prompt, "REAL_REFERENCE_BODY"), tc.want, prompt)
			}
			if tc.want && plan.HighImitationReferenceName != "页面高仿参考" {
				t.Fatalf("selected evidence missing: %+v", plan)
			}
		})
	}
}

func TestHighImitationSavedSystemPromptReachesRequestOnlyForItsMethod(t *testing.T) {
	for _, tc := range []struct{ method, mode string }{{"high_imitation", "auto"}, {"high_imitation", "off"}, {"opening_instruction", "auto"}, {"instruction", "auto"}} {
		t.Run(tc.method+"/"+tc.mode, func(t *testing.T) {
			ctx := context.Background()
			store := NewMemoryStore()
			settings := map[string]any{"appConfig": map[string]any{"rewrite": map[string]any{"ai_slot_methods": map[string]any{"ai1": tc.method}, "high_imitation_mode": tc.mode, "process_line_count": 1, "default_template_id": "contract"}}, "knowledgeLibraries": map[string]any{"rewrite_templates": map[string]any{"profiles": []any{map[string]any{"id": "contract", "system_prompt": "EXISTING_OUTPUT_CONTRACT", "user_prompt_template": "{book_id}|{target_lines_text}|{anchor_lines_text}", "response_mode": "plain_text"}}}}}
			SaveKnowledge(settings, "high_imitation", map[string]any{"id": "disabled-high", "collection": "prompts", "enabled": false, "system_prompt": "DISABLED_HIGH_PROMPT"})
			SaveKnowledge(settings, "high_imitation", map[string]any{"id": "saved-high", "name": "高仿专用指令", "collection": "prompts", "enabled": true, "system_prompt": "SAVED_HIGH_SYSTEM_PROMPT", "user_prompt_template": "DO_NOT_REPLACE_EXISTING_USER_TEMPLATE"})
			if err := store.PutConfig(ctx, "alice", settings); err != nil {
				t.Fatal(err)
			}
			store.PutDocument(ctx, "alice", Document{BookID: "123", Original: "原先的第一句。\n继续的第二句。"})
			calls := 0
			_, err := GenerateAIRewritesForVersions(ctx, store, "alice", "123", []string{"ai1"}, rewriteProviderFunc(func(_ context.Context, req RewriteRequest) (string, error) {
				calls++
				if strings.Contains(req.SystemPrompt, "SAVED_HIGH_SYSTEM_PROMPT") != (tc.method == "high_imitation") {
					t.Errorf("method=%s high system prompt mismatch: %s", tc.method, req.SystemPrompt)
				}
				if !strings.Contains(req.SystemPrompt, "EXISTING_OUTPUT_CONTRACT") || strings.Contains(req.SystemPrompt, "DISABLED_HIGH_PROMPT") {
					t.Errorf("existing contract/disabled policy changed: %s", req.SystemPrompt)
				}
				if !strings.HasPrefix(req.UserPrompt, "123|") || strings.Contains(req.UserPrompt, "DO_NOT_REPLACE") {
					t.Errorf("existing user template changed: %s", req.UserPrompt)
				}
				return "改后的第一句。", nil
			}))
			if err != nil || calls != 1 {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestHighImitationInstructionOnlyNeverBecomesReference(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	settings := map[string]any{"appConfig": map[string]any{"rewrite": map[string]any{"ai_slot_methods": map[string]any{"ai1": "high_imitation"}, "default_template_id": "contract", "process_line_count": 1}}, "knowledgeLibraries": map[string]any{"rewrite_templates": map[string]any{"profiles": []any{map[string]any{"id": "contract", "system_prompt": "EXISTING_OUTPUT_CONTRACT", "response_mode": "plain_text"}}}}}
	if _, err := SaveKnowledge(settings, "high_imitation", map[string]any{"id": "instruction-only", "name": "高仿处理指令", "collection": "prompts", "enabled": true, "content": "SYSTEM_ONLY_HIGH_INSTRUCTION"}); err != nil {
		t.Fatal(err)
	}
	doc := Document{BookID: "123", Original: "原先的第一句。", Meta: map[string]any{}}
	plan := rewritePlan(settings, "ai1", 1, doc)
	if plan.HighImitationReferenceName != "" {
		t.Errorf("system instruction falsely selected as reference: %q", plan.HighImitationReferenceName)
	}
	if err := store.PutConfig(ctx, "alice", settings); err != nil {
		t.Fatal(err)
	}
	store.PutDocument(ctx, "alice", doc)
	calls := 0
	_, err := GenerateAIRewritesForVersions(ctx, store, "alice", "123", []string{"ai1"}, rewriteProviderFunc(func(_ context.Context, req RewriteRequest) (string, error) {
		calls++
		if !strings.Contains(req.SystemPrompt, "SYSTEM_ONLY_HIGH_INSTRUCTION") {
			t.Error("high system instruction missing")
		}
		if strings.Contains(req.UserPrompt, "SYSTEM_ONLY_HIGH_INSTRUCTION") || strings.Contains(req.UserPrompt, "高仿参考（") {
			t.Errorf("system instructions were injected as a reference: %s", req.UserPrompt)
		}
		return "改后的第一句。", nil
	}))
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestAIReuseInvalidAndConfirmedHistory(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryStore()
	invalid := "```json\n{}\n```"
	for _, version := range []string{"ai1", "ai2"} {
		_, err := s.PutBody(ctx, "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: version, State: "ready"}, Content: invalid})
		if err != nil {
			t.Fatal(err)
		}
	}
	doc := Document{BookID: "123", Meta: map[string]any{"siteSubmitDoneVersions": []string{"ai2"}, "siteSubmitStatus": "submitted"}}
	ready, err := AIRewriteVersionReady(ctx, s, "alice", doc, "ai1")
	if err != nil || ready {
		t.Fatalf("unconfirmed invalid AI ready=%v err=%v", ready, err)
	}
	ready, err = AIRewriteVersionReady(ctx, s, "alice", doc, "ai2")
	if err != nil || !ready {
		t.Fatalf("confirmed historical version not protected: %v %v", ready, err)
	}
	doc.Meta = map[string]any{"siteSubmitStatus": "submitted"}
	ready, err = AIRewriteVersionReady(ctx, s, "alice", doc, "ai1")
	if err != nil || ready {
		t.Fatal("task-level submitted label falsely confirms this version")
	}
	doc.Meta = map[string]any{}
	doc.Logs = []any{map[string]any{"event": "site_submit", "version": "ai1", "status": "submitted", "remote_receipt": map[string]any{"verified": true, "submission_id": "receipt"}}}
	ready, err = AIRewriteVersionReady(ctx, s, "alice", doc, "ai1")
	if err != nil || !ready {
		t.Fatal("verified historical receipt lost")
	}
	doc.Logs[0].(map[string]any)["remote_receipt"].(map[string]any)["verified"] = false
	ready, err = AIRewriteVersionReady(ctx, s, "alice", doc, "ai1")
	if err != nil || ready {
		t.Fatal("unverified receipt protected invalid content")
	}
}

func TestAIReuseStoredAuthorityErrorsAndLegacy(t *testing.T) {
	ctx := context.Background()
	doc := Document{BookID: "123", Meta: map[string]any{}, Versions: map[string]any{"ai1": "有效兼容正文。", "ai2": map[string]any{"text": "有效对象正文。"}, "ai3": map[string]any{"state": "ready"}}}
	for _, version := range []string{"ai1", "ai2"} {
		ready, err := AIRewriteVersionReady(ctx, NewMemoryStore(), "alice", doc, version)
		if err != nil || !ready {
			t.Fatalf("legacy %s rejected %v", version, err)
		}
	}
	ready, err := AIRewriteVersionReady(ctx, NewMemoryStore(), "alice", doc, "ai3")
	if err != nil || ready {
		t.Fatal("metadata alone became ready")
	}
	s := &bodyReaderFaultStore{MemoryStore: NewMemoryStore(), failure: errors.New("TOS unavailable")}
	if _, err = AIRewriteVersionReady(ctx, s, "alice", doc, "ai1"); err == nil {
		t.Fatal("storage failure fell back to legacy body")
	}
}

func TestRewriteFailedReplacementPreservesInvalidOldBody(t *testing.T) {
	ctx := context.Background()
	s := plainRewriteTestStore("alice")
	old := "```json\n{}\n```"
	s.PutDocument(ctx, "alice", Document{BookID: "123", Original: "真实原文。"})
	s.PutBody(ctx, "alice", BodyRecord{BookID: "123", BodyRef: BodyRef{VersionID: "ai1", State: "ready"}, Content: old})
	_, err := GenerateAIRewritesForVersions(ctx, s, "alice", "123", []string{"ai1"}, rewriteProviderFunc(func(context.Context, RewriteRequest) (string, error) { return "", errors.New("provider unavailable") }))
	if err == nil {
		t.Fatal("expected replacement failure")
	}
	body, _ := s.GetBody(ctx, "alice", "123", "ai1")
	if body.Content != old {
		t.Fatalf("old body lost: %q", body.Content)
	}
}
