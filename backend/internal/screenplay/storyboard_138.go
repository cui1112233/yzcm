package screenplay

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"qiantie/backend/internal/prompt"
	"qiantie/backend/internal/prompt/luoshui"
	"strings"
	"time"
)

const max138Batches = 64
const max138Calls = 512
const max138Bytes = 2 << 20
const max138RunBytes = 8 << 20

type scene138Document struct {
	Elements []json.RawMessage `json:"script_content"`
}

func source138(unit storyboardSceneInput) ([]string, error) {
	if !unit.Structured {
		return nil, fmt.Errorf("%w: 138需要与已保存剧本一致、带element_id的专业剧本场景，请先保存专业剧本", ErrInvalidArgument)
	}
	var doc scene138Document
	if len(unit.Content) > max138Bytes || json.Unmarshal([]byte(unit.Content), &doc) != nil || len(doc.Elements) == 0 || len(doc.Elements) > 2000 {
		return nil, ErrInvalidArgument
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, el := range doc.Elements {
		var row struct {
			ID string `json:"element_id"`
		}
		if json.Unmarshal(el, &row) != nil || row.ID == "" || strings.TrimSpace(row.ID) != row.ID || len(row.ID) > 256 || seen[row.ID] {
			return nil, fmt.Errorf("%w: element_id缺失或重复，不能核验分镜覆盖", ErrInvalidArgument)
		}
		seen[row.ID] = true
		ids = append(ids, row.ID)
	}
	return ids, nil
}
func validate138Sources(units []storyboardSceneInput) error {
	seen := map[string]bool{}
	for _, unit := range units {
		ids, e := source138(unit)
		if e != nil {
			return e
		}
		for _, id := range ids {
			if seen[id] {
				return fmt.Errorf("%w: 跨场景element_id重复", ErrInvalidArgument)
			}
			seen[id] = true
		}
	}
	if len(seen) > 2000 {
		return ErrInvalidArgument
	}
	return nil
}
func coverage138(ids []string, covered int, shots []StoryboardShot) (int, error) {
	positions := map[string]int{}
	for i, id := range ids {
		positions[id] = i
	}
	next := covered
	for _, shot := range shots {
		var fields struct {
			IDs []string `json:"source_elements"`
		}
		if json.Unmarshal(shot.ProtocolFields, &fields) != nil || len(fields.IDs) == 0 {
			return covered, fmt.Errorf("%w: source_elements不能为空", ErrAIInvalidStoryboard)
		}
		for _, id := range fields.IDs {
			i, ok := positions[id]
			if !ok || i > next {
				return covered, fmt.Errorf("%w: source_elements未知编号或跳过来源", ErrAIInvalidStoryboard)
			}
			if i == next {
				next++
			}
		}
	}
	if next == covered {
		return covered, fmt.Errorf("%w: 本批未推进来源覆盖", ErrAIInvalidStoryboard)
	}
	return next, nil
}
func remaining138(unit storyboardSceneInput, covered int) (string, error) {
	var doc map[string]json.RawMessage
	if e := json.Unmarshal([]byte(unit.Content), &doc); e != nil {
		return "", e
	}
	var elements []json.RawMessage
	_ = json.Unmarshal(doc["script_content"], &elements)
	doc["script_content"], _ = json.Marshal(elements[covered:])
	raw, e := json.Marshal(doc)
	return string(raw), e
}
func hasAwaiting138(a *StoryboardGenerationAttempt) bool {
	for _, ep := range a.Episodes {
		if ep.Status == "awaiting_continue" {
			return true
		}
	}
	return false
}
func phase138(scene, batch int) string {
	return fmt.Sprintf("storyboard_scene_%d_batch_%d", scene, batch)
}
func parseScenePhase(phase string) (int, bool) {
	if !strings.HasPrefix(phase, "storyboard_scene_") {
		return 0, false
	}
	var scene, batch int
	if strings.Contains(phase, "_batch_") {
		if _, e := fmt.Sscanf(phase, "storyboard_scene_%d_batch_%d", &scene, &batch); e != nil || phase != phase138(scene, batch) || batch < 1 || batch > max138Batches {
			return 0, false
		}
	} else {
		if _, e := fmt.Sscanf(phase, "storyboard_scene_%d", &scene); e != nil || phase != fmt.Sprintf("storyboard_scene_%d", scene) {
			return 0, false
		}
	}
	return scene, scene > 0
}

func (s *StoryboardService) generateScenes138(ctx context.Context, req StoryboardRequest, episode episodeInput, units []storyboardSceneInput, selected prompt.CreativePromptSnapshot, roles, scenes, props, prefix, extra, planning, model string, useHistory bool) ([]StoryboardShot, []int, error) {
	if !prompt.Storyboard138SupportsBatch(selected) {
		return nil, nil, ErrInvalidArgument
	}
	session := req.generationSession
	all := []StoryboardShot{}
	failed := []int{}
	awaiting := false
	var firstErr error
	for index, unit := range units {
		if req.sceneOnlyIndex != nil && index != *req.sceneOnlyIndex {
			continue
		}
		ids, e := source138(unit)
		if e != nil {
			return nil, nil, e
		}
		covered := 0
		local := []StoryboardShot{}
		for batch := 1; covered < len(ids); batch++ {
			if batch > max138Batches {
				return nil, nil, fmt.Errorf("%w: 每场景最多64批", ErrInvalidState)
			}
			phase := phase138(index+1, batch)
			cached, exists := session.attempt.Episodes[episode.No].Calls[phase]
			if exists && cached.Status != "received" {
				return nil, []int{index + 1}, fmt.Errorf("%w: 该批次%s，须先核对原请求", ErrInvalidState, cached.Status)
			}
			var response TextResponse
			if exists {
				response.Content = cached.RawResponse
			} else {
				if req.recoveryOnly {
					awaiting = true
					break
				}
				remaining, e := remaining138(unit, covered)
				if e != nil {
					return nil, nil, e
				}
				user, e := prompt.StoryboardSceneUserMessage(episode.OriginalContent, remaining)
				if e != nil {
					return nil, nil, e
				}
				if useHistory {
					history := session.history(episode.No)
					if len(all) > 0 {
						recent := all
						if len(recent) > 5 {
							recent = recent[len(recent)-5:]
						}
						body, _ := json.Marshal(storyboardSemanticRows(recent))
						history += "\n" + string(body)
					}
					if strings.TrimSpace(history) != "" {
						user += "\n【此前场景分镜，仅供衔接，不重复输出】\n" + history
					}
				}
				if len(local) > 0 {
					recent := local
					if len(recent) > 5 {
						recent = recent[len(recent)-5:]
					}
					raw, _ := json.Marshal(storyboardSemanticRows(recent))
					user += "\n【此前已生成分镜，仅供衔接】\n" + string(raw)
				}
				if planning != "" {
					user += "\n【本次分镜规划】\n" + planning
				}
				if extra != "" {
					user += "\n【额外内容】\n" + extra
				}
				system := luoshui.Render(selected.Body, map[string]string{"ROLES_PLACEHOLDER": roles, "SCENES_PLACEHOLDER": scenes, "PROPS_PLACEHOLDER": props}) + "\n\n" + prompt.Storyboard138Continuation
				if prefix != "" {
					system = prefix + "\n\n" + system
				}
				response, e = s.generationCall(ctx, req, episode.No, phase, TextRequest{Model: model, SystemPrompt: system, UserPrompt: user, SingleAttempt: true})
				if e != nil {
					return nil, []int{index + 1}, e
				}
			}
			if session.verifyCurrent != nil {
				if e := session.verifyCurrent(ctx); e != nil {
					return nil, nil, e
				}
			}
			shots, e := parseStoryboardJSON(response.Content)
			if e == nil {
				e = validateSelectedStoryboardProtocol(selected, response.Content, shots)
			}
			next := covered
			if e == nil {
				next, e = coverage138(ids, covered, shots)
			}
			if e != nil {
				saveErr := session.mutate(context.WithoutCancel(ctx), func(a *StoryboardGenerationAttempt) error {
					c := a.Episodes[episode.No].Calls[phase]
					c.Status = "invalid"
					c.Error = boundedScriptDiagnostic(e.Error(), 4096)
					a.Episodes[episode.No].Calls[phase] = c
					return nil
				})
				if saveErr != nil {
					return nil, nil, saveErr
				}
				failed = append(failed, index+1)
				if firstErr == nil {
					firstErr = e
				}
				break
			}
			for _, shot := range shots {
				shot.ID = newScreenplayID("sb_shot")
				shot.SourceSceneIndex = index
				shot.SourceSceneLocalOrder = len(local) + 1
				var fields map[string]any
				_ = json.Unmarshal(shot.ProtocolFields, &fields)
				fields["_cm_contract"] = map[string]any{"prompt_id": selected.ID, "prompt_digest": selected.Digest, "output_protocol": selected.OutputProtocol, "parser_version": selected.ParserVersion}
				shot.ProtocolFields, _ = json.Marshal(fields)
				local = append(local, shot)
			}
			covered = next
			if e := session.mutate(context.WithoutCancel(ctx), func(a *StoryboardGenerationAttempt) error {
				ep := a.Episodes[episode.No]
				if ep.SceneProgress == nil {
					ep.SceneProgress = map[int]StoryboardGenerationSceneSummary{}
				}
				status := "awaiting_continue"
				if covered == len(ids) {
					status = "received"
				}
				ep.SceneProgress[index+1] = StoryboardGenerationSceneSummary{Index: index + 1, Status: status, BatchCount: batch, Covered: covered, Total: len(ids)}
				return nil
			}); e != nil {
				return nil, nil, e
			}
			if !exists && covered < len(ids) {
				awaiting = true
				break
			}
		}
		for _, shot := range local {
			shot.Seq = len(all) + 1
			all = append(all, shot)
		}
	}
	if firstErr != nil {
		return nil, failed, firstErr
	}
	if awaiting {
		e := session.mutate(context.WithoutCancel(ctx), func(a *StoryboardGenerationAttempt) error {
			a.Episodes[episode.No].Status = "awaiting_continue"
			a.Episodes[episode.No].Error = "本批已保存，待继续生成"
			return nil
		})
		if e != nil {
			return nil, nil, e
		}
		return nil, nil, fmt.Errorf("%w: 本批已保存，待继续生成", ErrPartialFailure)
	}
	if e := s.verify138SourceCurrent(ctx, req, units, episode.No); e != nil {
		return nil, nil, e
	}
	// Batch-local model ids may restart at one. Publish one stable sequence;
	// the untouched original ids remain in each durable raw response.
	for i := range all {
		var fields map[string]any
		_ = json.Unmarshal(all[i].ProtocolFields, &fields)
		fields["id"] = i + 1
		all[i].ProtocolFields, _ = json.Marshal(fields)
	}
	contract := selected
	contract.ResponseMode = "json_object" // reports were checked for every raw batch above
	if e := validateSelectedStoryboardProtocol(contract, "", all); e != nil {
		return nil, nil, e
	}
	return all, nil, nil
}

func (s *StoryboardService) verify138SourceCurrent(ctx context.Context, req StoryboardRequest, units []storyboardSceneInput, episode int) error {
	p, e := s.Store.GetProject(ctx, req.Owner, req.ProjectID)
	if e != nil {
		return e
	}
	original, e := ResolveProjectPrompt(p, req.generationSession.attempt.PromptID, "storyboard")
	if e != nil || !prompt.Storyboard138SupportsBatch(original) {
		return ErrRevisionConflict
	}
	state, e := s.storyboardAuthoringState(ctx, p)
	if e != nil {
		return e
	}
	freshReq, e := s.storyboardChapterInput(req, state)
	if e != nil {
		return e
	}
	episodes, e := s.resolveEpisodes(ctx, freshReq)
	if e != nil {
		return e
	}
	for _, ep := range episodes {
		if ep.No != episode {
			continue
		}
		current, e := storyboard138ScenesForEpisode(freshReq, state, ep)
		if e != nil {
			return e
		}
		a, _ := json.Marshal(current)
		b, _ := json.Marshal(units)
		if string(a) == string(b) {
			return nil
		}
	}
	return fmt.Errorf("%w: 剧本来源已变化，已返回批次保留但不发布", ErrRevisionConflict)
}

// RecoverStoryboard138 never invokes the model. It restores file receipts into
// the existing attempt, after which the normal confirmed retry validates them.
func (s *StoryboardService) RecoverStoryboard138(ctx context.Context, owner, project, key string) (StoryboardGenerationAttempt, error) {
	store, ok := s.Store.(StoryboardGenerationStore)
	if !ok || s.Authoring == nil || s.Authoring.Receipts == nil {
		return StoryboardGenerationAttempt{}, ErrInvalidState
	}
	state, e := store.MutateStoryboardGenerations(ctx, owner, project, func(v *StoryboardGenerationState) error {
		a := v.Attempts[key]
		if a == nil {
			return ErrNotFound
		}
		if a.PromptID != "luoshui_storyboard_138" {
			return ErrInvalidState
		}
		recovered := false
		for _, ep := range a.Episodes {
			for phase, c := range ep.Calls {
				if c.ReceiptKey == nil || c.Status == "received" || c.Status == "invalid" {
					continue
				}
				r, e := storyboard138Receipts(s.Authoring).Load(ctx, *c.ReceiptKey)
				if e != nil {
					return e
				}
				if !r.Ready {
					return ErrInvalidState
				}
				if r.Error != "" || r.Oversized {
					return ErrInvalidState
				}
				c.RawResponse = r.RawResult
				c.Status = "received"
				c.Error = ""
				ep.Calls[phase] = c
				ep.Status = "received"
				ep.Error = ""
				recovered = true
			}
		}
		if !recovered && a.Status == "running" {
			return ErrInvalidState
		}
		if recovered {
			a.Status = "failed"
			a.Error = "已恢复原始回包，请确认续跑以核验和保存；不会重复调用已返回批次"
			a.UpdatedAt = time.Now().UTC()
		}
		return nil
	})
	if e != nil {
		return StoryboardGenerationAttempt{}, e
	}
	return *state.Attempts[key], nil
}
func guard138Value(p Project, state *AuthoringState, assets []Asset, prefix string, prior *StoryboardGenerationAttempt) string {
	excluded := map[string]bool{}
	for _, ep := range prior.Episodes {
		for _, id := range ep.AddedAssetIDs {
			excluded[id] = true
		}
		for _, a := range ep.PendingAssets {
			excluded[a.ID] = true
		}
	}
	filtered := []Asset{}
	for _, a := range assets {
		if !excluded[a.ID] {
			filtered = append(filtered, a)
		}
	}
	assets = filtered
	raw, _ := json.Marshal([]any{p.CreativeConfig, p.ModelID, p.StyleDesc, p.ArtStyle, p.Synopsis, state, assets, prefix})
	return contentDigest(string(raw))
}
func (s *StoryboardService) guard138(ctx context.Context, req StoryboardRequest, expected string) error {
	p, e := s.Store.GetProject(ctx, req.Owner, req.ProjectID)
	if e != nil {
		return e
	}
	state, e := s.storyboardAuthoringState(ctx, p)
	if e != nil {
		return e
	}
	assets, e := s.Store.ListAssets(ctx, req.Owner, req.ProjectID, "")
	if e != nil {
		return e
	}
	prefix := ""
	if req.Prefix != nil {
		prefix = strings.TrimSpace(*req.Prefix)
	} else if ps, ok := s.Store.(StoryboardPreferencesStore); ok {
		v, e := ps.LoadStoryboardPreferences(ctx, req.Owner)
		if e != nil {
			return e
		}
		prefix = v.SelectedPrefix()
	}
	if guard138Value(p, state, assets, prefix, &req.generationSession.attempt) != expected {
		return fmt.Errorf("%w: 来源或配置已变化，回包已保存，不发布旧输入结果", ErrRevisionConflict)
	}
	return nil
}

func storyboard138Receipts(authors *AuthoringService) ScriptBatchReceipts {
	if authors == nil {
		return nil
	}
	if files, ok := authors.Receipts.(*FileScriptBatchReceipts); ok {
		return &FileScriptBatchReceipts{Root: filepath.Join(files.Root, "storyboard138")}
	}
	return authors.Receipts
}

func validate138Episode(req StoryboardRequest, state *AuthoringState, episode int) error {
	if state == nil {
		return ErrInvalidArgument
	}
	for i, c := range state.Chapters {
		if (req.ChapterID != "" && c.ID == req.ChapterID) || (req.ChapterID == "" && i+1 == episode) {
			var doc struct {
				Episode int `json:"episode_index"`
			}
			object, ok := extractJSONObject(c.RawResult)
			if !ok || json.Unmarshal([]byte(object), &doc) != nil || i+1 != episode || (doc.Episode > 0 && doc.Episode != episode) {
				return fmt.Errorf("%w: 所选章节与来源剧集编号不一致", ErrInvalidArgument)
			}
			return nil
		}
	}
	return ErrNotFound
}

// Caller text can select a saved chapter, but cannot replace its authoritative
// editable script or revive an outdated structured result. This restriction is
// specific to 138; other engines keep their existing free-text contract.
func storyboard138ScenesForEpisode(req StoryboardRequest, state *AuthoringState, episode episodeInput) ([]storyboardSceneInput, error) {
	if req.UseOriginalContentDirectly {
		return nil, fmt.Errorf("%w: 138需要已保存的专业剧本，不能直接使用原文", ErrInvalidArgument)
	}
	if e := validate138Episode(req, state, episode.No); e != nil {
		return nil, e
	}
	for i, c := range state.Chapters {
		if (req.ChapterID != "" && c.ID != req.ChapterID) || (req.ChapterID == "" && i+1 != episode.No) {
			continue
		}
		rendered, e := professionalScriptMarkdown(c.RawResult)
		if e != nil || strings.TrimSpace(rendered) != strings.TrimSpace(c.ScriptText) || strings.TrimSpace(episode.Content) != strings.TrimSpace(c.ScriptText) {
			return nil, fmt.Errorf("%w: 138来源必须与当前保存的专业剧本一致；旧RawResult或请求正文不能替代当前剧本", ErrInvalidArgument)
		}
		bound := req
		bound.ChapterID = c.ID
		units, e := storyboardScenesForEpisode(bound, state, episodeInput{No: episode.No, Content: c.ScriptText, OriginalContent: episode.OriginalContent})
		if e != nil {
			return nil, e
		}
		for n := range units {
			units[n].SourceChapterID = c.ID
		}
		return units, nil
	}
	return nil, ErrNotFound
}
