package screenplay

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode"

	"qiantie/backend/internal/prompt"
)

const ScriptBatchPromptID = "luoshui_script_293"
const maxScriptBatchBytes = 2 << 20
const maxScriptBatchTotalBytes = 8 << 20
const maxScriptBatches = 64
const maxScriptBatchScenes = 2000

type ScriptBatch struct {
	InputDigest string `json:"input_digest"`
	RawDigest   string `json:"raw_digest,omitempty"`
	RawBytes    int    `json:"raw_bytes,omitempty"`
	Preview     string `json:"preview,omitempty"`
	RawResult   string `json:"raw_result"`
	Valid       bool   `json:"valid"`
	Error       string `json:"error,omitempty"`
}

type ScriptGeneration struct {
	ID                  string              `json:"id"`
	Status              string              `json:"status"`
	SourceDigest        string              `json:"source_digest"`
	PromptID            string              `json:"prompt_id"`
	PromptDigest        string              `json:"prompt_digest"`
	ConfigDigest        string              `json:"config_digest"`
	Model               string              `json:"model"`
	Episode             int                 `json:"episode"`
	InputDigest         string              `json:"input_digest"`
	RequestedBatch      int                 `json:"requested_batch"`
	Instruction         string              `json:"instruction"`
	Rollover            string              `json:"rollover"`
	Batches             []ScriptBatch       `json:"batches"`
	Error               string              `json:"error,omitempty"`
	PublicationError    string              `json:"publication_error,omitempty"`
	PublishedDocumentID string              `json:"published_document_id,omitempty"`
	Deadline            time.Time           `json:"deadline,omitempty"`
	History             []*ScriptGeneration `json:"history,omitempty"`
}

type scriptBatchDocument struct {
	Episode int               `json:"episode_index"`
	Title   string            `json:"title"`
	Scenes  []json.RawMessage `json:"scenes"`
	Info    struct {
		Complete *bool `json:"is_complete"`
		Current  int   `json:"current_batch"`
	} `json:"batch_info"`
	Rollover *string `json:"unprocessed_script_rollover"`
}

func scriptDigest(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }

func scriptConfigDigest(p Project, request TextRequest) string {
	// Include hidden snapshots and rendered system text: changes to asset libraries,
	// style, template text, model and generation settings invalidate continuation.
	raw, _ := json.Marshal(struct {
		Project   Project
		Request   TextRequest
		Snapshots map[string]CreativePromptSnapshot
	}{p, request, func() map[string]CreativePromptSnapshot {
		if p.CreativeConfig != nil {
			return p.CreativeConfig.PromptSnapshots
		}
		return nil
	}()})
	return scriptDigest(string(raw))
}

func decodeScriptBatch(raw string) (scriptBatchDocument, error) {
	var d scriptBatchDocument
	if len(raw) > maxScriptBatchBytes {
		return d, fmt.Errorf("%w: 单批响应超过上限", ErrAIInvalidJSON)
	}
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```json") {
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, "```json"))
	}
	decoder := json.NewDecoder(strings.NewReader(cleaned))
	var payload json.RawMessage
	if err := decoder.Decode(&payload); err != nil {
		return d, ErrAIInvalidJSON
	}
	tail := strings.TrimSpace(cleaned[decoder.InputOffset():])
	if strings.HasPrefix(strings.TrimSpace(raw), "```json") {
		if !strings.HasPrefix(tail, "```") {
			return d, ErrAIInvalidJSON
		}
		tail = strings.TrimSpace(strings.TrimPrefix(tail, "```"))
	}
	if tail != "" && !validScriptBatchReport(tail) {
		return d, ErrAIInvalidJSON
	}
	if json.Unmarshal(payload, &d) != nil || d.Info.Complete == nil || d.Rollover == nil || d.Episode < 1 || len(d.Scenes) == 0 {
		return d, ErrAIInvalidJSON
	}
	return d, nil
}

func validScriptBatchReport(tail string) bool {
	// Exact recovered headings and field labels; never permit a second JSON or
	// arbitrary prose to hide after a successful object.
	section := ""
	fields := 0
	allowed := map[string]bool{"原文对话总数": true, "action 元素总数": true, "action元素总数": true, "场景总数": true, "element_id 是否连续无重复": true, "原文总字数": true, "本批次已转化覆盖的原文字数（估算）": true, "结转至 rollover 的原文字数": true, "合计": true, "⚠️ 若三项相加 ≠ 原文总字数，说明有内容丢失，必须重新生成": true, "原文 R 级动作节拍数": true, "剧本中对应的隐喻 action 元素数": true, "节拍是否 1:1 对应": true, "原文临终/惨叫/痛苦台词数": true, "剧本中保留的 dialogue 元素数": true, "台词是否 100% 保留": true}
	for _, line := range strings.Split(tail, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.ContainsAny(line, "{}[]") || strings.Contains(line, "```") {
			return false
		}
		if line == "【转换自检】" || line == "【无损死锁双向核对】" || strings.HasPrefix(line, "【暴力脱敏核对】") {
			section = line
			continue
		}
		if section == "" {
			return false
		}
		line = strings.TrimPrefix(line, "- ")
		if line == "⚠️ 若三项相加 ≠ 原文总字数，说明有内容丢失，必须重新生成" {
			continue
		}
		label, value, ok := strings.Cut(line, "：")
		if !ok {
			label, value, ok = strings.Cut(line, ":")
		}
		if !ok || !allowed[strings.TrimSpace(label)] || strings.TrimSpace(value) == "" {
			return false
		}
		fields++
	}
	return fields > 0
}

func sentenceRollover(input, remaining string) bool {
	if remaining == "" || len(remaining) >= len(input) || !strings.HasSuffix(input, remaining) {
		return false
	}
	prefix := strings.TrimRightFunc(input[:len(input)-len(remaining)], unicode.IsSpace)
	// Permit closing dialogue quotes after terminal punctuation.
	prefix = strings.TrimRight(prefix, "\"'”’」』）)")
	if prefix == "" {
		return false
	}
	runes := []rune(prefix)
	if runes[len(runes)-1] == '.' && len(runes) > 1 && unicode.IsDigit(runes[len(runes)-2]) {
		remainingRunes := []rune(strings.TrimSpace(remaining))
		if len(remainingRunes) > 0 && unicode.IsDigit(remainingRunes[0]) {
			return false
		}
	}
	return strings.ContainsRune("。！？.!?\n", runes[len(runes)-1])
}

func validateScriptBatch(g *ScriptGeneration, input, raw string) (scriptBatchDocument, error) {
	d, err := decodeScriptBatch(raw)
	if err != nil {
		return d, err
	}
	if g.Episode > 0 && d.Episode != g.Episode {
		return d, fmt.Errorf("%w: 剧集编号与请求不一致", ErrAIInvalidJSON)
	}
	next := 1
	sceneCount := 0
	episode := d.Episode
	ids := map[string]bool{}
	for _, batch := range g.Batches {
		if !batch.Valid {
			continue
		}
		old, e := decodeScriptBatch(batch.RawResult)
		if e != nil {
			return d, ErrInvalidState
		}
		if old.Episode != episode {
			return d, fmt.Errorf("%w: 剧集编号变化", ErrAIInvalidJSON)
		}
		next++
		sceneCount += len(old.Scenes)
		for _, r := range old.Scenes {
			var scene struct {
				Content []struct {
					ID string `json:"element_id"`
				} `json:"script_content"`
			}
			_ = json.Unmarshal(r, &scene)
			for _, el := range scene.Content {
				ids[el.ID] = true
			}
		}
	}
	if d.Info.Current != next || sceneCount+len(d.Scenes) > maxScriptBatchScenes {
		return d, fmt.Errorf("%w: 批次错序或场景超限", ErrAIInvalidJSON)
	}
	for i, r := range d.Scenes {
		var scene struct {
			Number  string `json:"scene_number"`
			Content []struct {
				ID        string `json:"element_id"`
				Type      string `json:"element_type"`
				Action    string `json:"action_type"`
				Voiceover *bool  `json:"is_voiceover"`
			} `json:"script_content"`
		}
		if json.Unmarshal(r, &scene) != nil || scene.Number != fmt.Sprintf("%d-%d", episode, sceneCount+i+1) || len(scene.Content) == 0 || len(scene.Content) > 999 {
			return d, fmt.Errorf("%w: 场景号必须连续", ErrAIInvalidJSON)
		}
		for j, el := range scene.Content {
			if ids[el.ID] || el.ID != fmt.Sprintf("%s-%03d", scene.Number, j+1) {
				return d, fmt.Errorf("%w: 元素编号重复或不连续", ErrAIInvalidJSON)
			}
			ids[el.ID] = true
			if el.Type == "dialogue" && el.Voiceover == nil {
				return d, ErrAIInvalidJSON
			}
			if el.Action != "" && el.Action != "action" && el.Action != "atmosphere" && el.Action != "camera" {
				return d, ErrAIInvalidJSON
			}
			if el.Type == "action" && el.Action == "" {
				return d, ErrAIInvalidJSON
			}
		}
	}
	if *d.Info.Complete {
		if *d.Rollover != "" {
			return d, fmt.Errorf("%w: 完成批次仍有剩余原文", ErrAIInvalidJSON)
		}
	} else if !sentenceRollover(input, *d.Rollover) {
		return d, fmt.Errorf("%w: 剩余原文必须逐字后缀、严格缩短且从完整句子开始", ErrAIInvalidJSON)
	}
	// Also validate actionable scene content through the existing renderer.
	payload, _ := json.Marshal(d)
	if _, err = professionalScriptMarkdown(string(payload)); err != nil {
		return d, err
	}
	return d, nil
}

func mergedScriptBatches(g *ScriptGeneration) (string, string, error) {
	var merged map[string]json.RawMessage
	scenes := []json.RawMessage{}
	for _, batch := range g.Batches {
		if !batch.Valid {
			continue
		}
		d, err := decodeScriptBatch(batch.RawResult)
		if err != nil {
			return "", "", err
		}
		if merged == nil {
			var payload json.RawMessage
			decoder := json.NewDecoder(strings.NewReader(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(batch.RawResult), "```json"))))
			if decoder.Decode(&payload) != nil {
				return "", "", ErrAIInvalidJSON
			}
			json.Unmarshal(payload, &merged)
		}
		scenes = append(scenes, d.Scenes...)
	}
	merged["scenes"], _ = json.Marshal(scenes)
	merged["batch_info"] = json.RawMessage(`{"is_complete":true,"current_batch":` + fmt.Sprint(len(g.Batches)) + `}`)
	merged["unprocessed_script_rollover"] = json.RawMessage(`""`)
	raw, _ := json.Marshal(merged)
	text, err := professionalScriptMarkdown(string(raw))
	return string(raw), text, err
}

// PreserveScriptGeneration is called at untrusted edit boundaries, before any
// collection is published. Internal generation writes directly to Repository.
func PreserveScriptGeneration(chapters, old []Chapter) error {
	byID := map[string]Chapter{}
	for _, c := range old {
		byID[c.ID] = c
	}
	for i := range chapters {
		c := &chapters[i]
		previous, ok := byID[c.ID]
		c.ScriptGeneration = nil
		if !ok {
			continue
		}
		c.ScriptGeneration = previous.ScriptGeneration
		if c.ScriptGeneration != nil && (c.SourceText != previous.SourceText || c.ScriptText != previous.ScriptText || c.RawResult != previous.RawResult || c.Title != previous.Title) {
			if c.ScriptGeneration.Status == "running" && time.Now().Before(c.ScriptGeneration.Deadline) {
				return fmt.Errorf("%w: 章节正在生成，请等待结果后再编辑", ErrInvalidState)
			}
			clone := *c.ScriptGeneration
			clone.Status = "stale"
			clone.Error = "章节已修改，请明确重新开始"
			c.ScriptGeneration = &clone
		}
	}
	for _, c := range old {
		if _, ok := findChapter(chapters, c.ID); !ok && c.ScriptGeneration != nil && c.ScriptGeneration.Status == "running" && time.Now().Before(c.ScriptGeneration.Deadline) {
			return fmt.Errorf("%w: 正在生成的章节不能移除", ErrInvalidState)
		}
	}
	return nil
}

func findChapter(chapters []Chapter, id string) (int, bool) {
	for i, c := range chapters {
		if c.ID == id {
			return i, true
		}
	}
	return -1, false
}

// GenerateScriptBatch owns the same project lock used by ordinary authoring.
// No model retry occurs here or in the provider adapter (SingleAttempt).
func (s AuthoringService) GenerateScriptBatch(ctx context.Context, o, p, chapter, action, instruction string, revision int64, confirmed bool) (AuthoringState, error) {
	var out AuthoringState
	err := s.WithLock(ctx, o, p, func() error {
		st, err := s.Load(ctx, o, p)
		if err != nil {
			return err
		}
		if st.Archived {
			return ErrInvalidState
		}
		if st.Revision != revision {
			return ErrRevisionConflict
		}
		index, ok := findChapter(st.Chapters, chapter)
		if !ok {
			return ErrNotFound
		}
		c := &st.Chapters[index]
		if action == "recover" {
			if c.ScriptGeneration == nil || s.Receipts == nil {
				return fmt.Errorf("%w: 没有可恢复的回包收据", ErrInvalidState)
			}
			receipt, e := s.Receipts.Load(ctx, receiptKey(o, p, chapter, c.ScriptGeneration))
			if e != nil {
				log.Printf("script batch receipt recovery load failed generation=%s: %v", c.ScriptGeneration.ID, e)
				return fmt.Errorf("%w: 尚无可恢复的回包收据，或收据校验未通过", ErrInvalidState)
			}
			if !receipt.Ready {
				return fmt.Errorf("%w: 请求尚无已返回的回包，不能自动重发", ErrInvalidState)
			}
			out, e = s.applyScriptBatchReceipt(ctx, o, p, chapter, receipt)
			return e
		}
		project, err := s.Store.GetProject(ctx, o, p)
		if err != nil {
			return err
		}
		selected, err := ResolveProjectPrompt(project, selectedScriptPrompt(project), "script")
		if err != nil {
			return err
		}
		if selected.ID != ScriptBatchPromptID && c.ScriptGeneration == nil {
			return ErrInvalidArgument
		}
		assets, err := s.Store.ListAssets(ctx, o, p, "")
		if err != nil {
			return err
		}
		library, _ := json.Marshal(map[string]any{"assets": assets})
		base, err := BuildChapterTextRequest(project, *c, instruction, string(library))
		if err != nil {
			return err
		}
		base.SingleAttempt = true
		config := scriptConfigDigest(project, base)
		g := c.ScriptGeneration
		if action == "publish" {
			if g == nil || g.Status != "completed" || !ScriptCollectionReady(st.Chapters) {
				return ErrInvalidState
			}
			out, err = s.publishScriptBatchCollection(ctx, o, p, st)
			return err
		}
		if action == "" {
			action = "start"
		}
		if g != nil && g.Status == "running" && time.Now().Before(g.Deadline) {
			return fmt.Errorf("%w: 此章节正在生成，请勿重复提交", ErrInvalidState)
		}
		switch action {
		case "start":
			if g != nil {
				return fmt.Errorf("%w: 已有生成记录，请继续或明确重新开始", ErrInvalidState)
			}
		case "continue":
			if g == nil || g.Status != "awaiting_continue" {
				return fmt.Errorf("%w: 当前状态不能继续，未知结果不会自动重发", ErrInvalidState)
			}
			if g.SourceDigest != scriptDigest(c.SourceText) || g.ConfigDigest != config || g.Instruction != instruction {
				return fmt.Errorf("%w: 原文、模型或生成配置已变更，请重新开始", ErrInvalidState)
			}
		case "restart":
			if !confirmed {
				return fmt.Errorf("%w: 重新开始可能再次扣费，请明确确认", ErrInvalidArgument)
			}
		default:
			return ErrInvalidArgument
		}
		if action != "continue" {
			history := []*ScriptGeneration{}
			if g != nil {
				if len(g.History) >= 10 {
					return fmt.Errorf("%w: 历史任务超过上限", ErrInvalidState)
				}
				history = append(history, g.History...)
				previous := *g
				previous.History = nil
				history = append(history, &previous)
			}
			snap, err := ResolveProjectPrompt(project, selected.ID, "script")
			if err != nil {
				return err
			}
			g = &ScriptGeneration{ID: newScreenplayID("scriptbatch"), Status: "running", SourceDigest: scriptDigest(c.SourceText), PromptID: selected.ID, PromptDigest: snap.Digest, ConfigDigest: config, Model: base.Model, Episode: index + 1, Instruction: instruction, Rollover: c.SourceText, History: history}
		}
		total := len(g.Rollover)
		for _, b := range g.Batches {
			total += len(b.RawResult)
		}
		for _, old := range g.History {
			total += len(old.Rollover)
			for _, b := range old.Batches {
				total += len(b.RawResult)
			}
		}
		if len(g.Batches) >= maxScriptBatches || total+maxScriptBatchBytes > maxScriptBatchTotalBytes || len(g.Rollover) > maxScriptBatchBytes {
			return fmt.Errorf("%w: 分批任务超过数量或大小上限", ErrInvalidArgument)
		}
		input := g.Rollover
		inputChapter := *c
		inputChapter.SourceText = input
		request, err := BuildChapterTextRequest(project, inputChapter, instruction, string(library))
		if err != nil {
			return err
		}
		request.SingleAttempt = true
		nextScene := 1
		episode := g.Episode
		for _, b := range g.Batches {
			if b.Valid {
				d, e := decodeScriptBatch(b.RawResult)
				if e != nil {
					return e
				}
				episode = d.Episode
				nextScene += len(d.Scenes)
			}
		}
		if selected.ID == ScriptBatchPromptID {
			request.SystemPrompt += prompt.ScriptBatchContinuation
			request.UserPrompt += fmt.Sprintf("\n本次批号：%d\n指定剧集：%d\n下一场序号：%d", len(g.Batches)+1, episode, nextScene)
		}
		g.Status = "running"
		g.InputDigest = scriptDigest(input)
		g.RequestedBatch = len(g.Batches) + 1
		g.Error = ""
		g.Deadline = time.Now().Add(8 * time.Minute)
		c.ScriptGeneration = g
		if s.Generator == nil {
			return fmt.Errorf("%w: 模型服务未配置", ErrInvalidState)
		}
		if s.Receipts == nil {
			return fmt.Errorf("%w: 未配置持久化回包收据，不能启动计费请求", ErrInvalidState)
		}
		key := receiptKey(o, p, chapter, g)
		if err = s.Receipts.Save(ctx, ScriptBatchReceipt{Key: key}); err != nil {
			log.Printf("script batch receipt reserve failed generation=%s: %v", g.ID, err)
			return fmt.Errorf("%w: 回包收据目录不可写，未调用模型", ErrInvalidState)
		}
		st, err = s.Repository.Save(ctx, st, revision)
		if err != nil {
			return err
		}
		callCtx, cancel := context.WithTimeout(ctx, 7*time.Minute)
		defer cancel()
		response, callErr := s.Generator.Generate(callCtx, request)
		// A cancelled HTTP request must not discard a returned response or diagnostic.
		persistCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		receipt := returnedScriptReceipt(key, response.Content, callErr)
		receiptErr := s.Receipts.Save(persistCtx, receipt)
		out, err = s.applyScriptBatchReceipt(persistCtx, o, p, chapter, receipt)
		if err != nil {
			if receiptErr == nil {
				return fmt.Errorf("回包收据已持久保存，存储恢复后请恢复已返回批次，不会再次调用模型：%w", err)
			}
			log.Printf("script batch receipt and database save failed generation=%s receipt=%v database=%v", g.ID, receiptErr, err)
			return fmt.Errorf("%w: 回包收据与数据库均保存失败，请先恢复存储后检查记录，勿重复生成", ErrInvalidState)
		}
		return nil
	})
	return out, err
}

// Publication can fail independently after the model result is durable. Its
// explicit retry reuses the saved result and never calls the model. Check the
// active immutable content first so a crash after publication cannot append an
// identical version on recovery.
func (s AuthoringService) publishScriptBatchCollection(ctx context.Context, o, p string, st AuthoringState) (AuthoringState, error) {
	content, err := chapterCollectionText(st.Chapters)
	if err != nil {
		return st, err
	}
	doc, err := s.Store.ActiveDocument(ctx, o, p)
	if errors.Is(err, ErrNoActiveScript) || errors.Is(err, ErrNotFound) || err == nil && strings.TrimSpace(doc.Content) != strings.TrimSpace(content) {
		doc, err = PublishChapterCollection(ctx, s.Store, o, p, st.Chapters)
	}
	if err == nil {
		err = InvalidateAuthoringContent(ctx, s.Store, o, p, "chapter")
	}
	publicationError := ""
	if err != nil {
		publicationError = err.Error()
	}
	for retry := 0; retry < 3; retry++ {
		latest, e := s.Repository.Load(ctx, o, p)
		if e != nil {
			return st, e
		}
		latestContent, e := chapterCollectionText(latest.Chapters)
		if e != nil || !ScriptCollectionReady(latest.Chapters) || latestContent != content {
			return latest, ErrRevisionConflict
		}
		for i := range latest.Chapters {
			c := &latest.Chapters[i]
			if c.ScriptGeneration == nil || c.ScriptGeneration.Status != "completed" {
				continue
			}
			c.ScriptGeneration.PublicationError = publicationError
			if publicationError == "" {
				c.ScriptGeneration.PublishedDocumentID = doc.ID
			}
		}
		out, e := s.Repository.Save(ctx, latest, latest.Revision)
		if errors.Is(e, ErrRevisionConflict) {
			continue
		}
		return out, e
	}
	return st, ErrRevisionConflict
}

func ScriptCollectionReady(chapters []Chapter) bool {
	for _, c := range chapters {
		if c.ScriptGeneration != nil && c.ScriptGeneration.Status != "completed" {
			return false
		}
	}
	return true
}

// Detect envelopes at all legacy single-shot boundaries before they can replace
// a successful script. 293 itself must use GenerateScriptBatch even when complete.
func rejectPartialScriptBatch(raw string) error {
	if !strings.Contains(raw, `"batch_info"`) && !strings.Contains(raw, `"unprocessed_script_rollover"`) {
		return nil
	}
	d, err := decodeScriptBatch(raw)
	if err != nil {
		return err
	}
	if !*d.Info.Complete {
		return fmt.Errorf("%w: 分批结果尚未完成，请使用章节续接入口", ErrInvalidState)
	}
	return nil
}
