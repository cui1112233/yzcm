package screenplay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Called only with a server-owned receipt: either the just-returned provider
// response or the journal record located from the persisted run's binding.
// Database failures leave the receipt available to a fresh process.
func (s AuthoringService) applyScriptBatchReceipt(ctx context.Context, o, p, chapter string, r ScriptBatchReceipt) (AuthoringState, error) {
	var out AuthoringState
	if !r.Ready || r.Key.Owner != o || r.Key.Project != p || r.Key.Chapter != chapter {
		return out, ErrInvalidArgument
	}
	if err := validateReceipt(r); err != nil {
		return out, err
	}
	for retry := 0; retry < 3; retry++ {
		latest, err := s.Repository.Load(ctx, o, p)
		if err != nil {
			return out, err
		}
		i, ok := findChapter(latest.Chapters, chapter)
		if !ok {
			return out, ErrRevisionConflict
		}
		c := &latest.Chapters[i]
		g := c.ScriptGeneration
		historical := false
		if g == nil {
			return out, ErrRevisionConflict
		}
		if g.ID != r.Key.Generation {
			g = nil
			for _, old := range c.ScriptGeneration.History {
				if old.ID == r.Key.Generation {
					g = old
					historical = true
					break
				}
			}
			if g == nil {
				return out, ErrRevisionConflict
			}
		}
		if r.Key.Batch <= len(g.Batches) {
			batch := g.Batches[r.Key.Batch-1]
			digest := batch.RawDigest
			if digest == "" {
				digest = scriptDigest(batch.RawResult)
			}
			if batch.InputDigest != r.Key.InputDigest || digest != r.RawDigest {
				return out, ErrRevisionConflict
			}
			if !historical && g.Status == "completed" && ScriptCollectionReady(latest.Chapters) {
				return s.publishScriptBatchCollection(ctx, o, p, latest)
			}
			return latest, nil
		}
		if r.Key.Batch != len(g.Batches)+1 || g.InputDigest != r.Key.InputDigest || scriptDigest(g.Rollover) != r.Key.InputDigest {
			return out, ErrRevisionConflict
		}
		batch := ScriptBatch{InputDigest: r.Key.InputDigest, RawResult: r.RawResult, RawDigest: r.RawDigest, RawBytes: r.RawBytes, Preview: r.Preview}
		g.Error = ""
		switch {
		case r.Oversized:
			g.Status = "invalid"
			g.Error = fmt.Sprintf("响应超过%d字节上限，原响应%d字节；仅保存摘要和有限诊断", maxScriptBatchBytes, r.RawBytes)
			batch.Error = g.Error
		case r.Error != "":
			g.Status = "unknown"
			g.Error = r.Error
			batch.Error = r.Error
		case g.PromptID == ScriptBatchPromptID:
			parsed, e := validateScriptBatch(g, g.Rollover, r.RawResult)
			if e != nil {
				g.Status = "invalid"
				g.Error = e.Error()
				batch.Error = e.Error()
			} else {
				batch.Valid = true
				g.Rollover = *parsed.Rollover
				if *parsed.Info.Complete {
					g.Status = "completed"
				} else {
					g.Status = "awaiting_continue"
				}
			}
		default:
			text, e := selectedScreenplayOutput(r.RawResult, g.PromptID)
			if e == nil {
				e = rejectPartialScriptBatch(r.RawResult)
			}
			if e != nil {
				g.Status = "invalid"
				g.Error = e.Error()
				batch.Error = e.Error()
			} else if text == "" {
				g.Status = "invalid"
				g.Error = ErrAIEmptyContent.Error()
				batch.Error = g.Error
			} else {
				g.Status = "completed"
				g.Rollover = ""
				batch.Valid = true
			}
		}
		g.Batches = append(g.Batches, batch)
		if historical {
			g.Status = "stale"
			g.Error = "较早请求的迟到回包已留存，不覆盖新任务或旧成功剧本"
		} else {
			project, e := s.Store.GetProject(ctx, o, p)
			if e != nil {
				return out, e
			}
			assets, e := s.Store.ListAssets(ctx, o, p, "")
			if e != nil {
				return out, e
			}
			library, _ := json.Marshal(map[string]any{"assets": assets})
			base, e := BuildChapterTextRequest(project, *c, g.Instruction, string(library))
			base.SingleAttempt = true
			if e != nil || scriptDigest(c.SourceText) != g.SourceDigest || scriptConfigDigest(project, base) != g.ConfigDigest || latest.Archived {
				g.Status = "stale"
				g.Error = "生成期间原文或配置已变更，回包已保留，请重新开始"
			}
			if g.Status == "completed" {
				if g.PromptID == ScriptBatchPromptID {
					raw, text, e := mergedScriptBatches(g)
					if e != nil {
						return out, e
					}
					c.RawResult = raw
					c.ScriptText = text
				} else {
					text, e := selectedScreenplayOutput(r.RawResult, g.PromptID)
					if e != nil {
						return out, e
					}
					c.RawResult = r.RawResult
					c.ScriptText = text
				}
			}
		}
		out, err = s.Repository.Save(ctx, latest, latest.Revision)
		if errors.Is(err, ErrRevisionConflict) {
			continue
		}
		if err != nil {
			return out, err
		}
		if !historical && g.Status == "completed" && ScriptCollectionReady(out.Chapters) {
			return s.publishScriptBatchCollection(ctx, o, p, out)
		}
		return out, nil
	}
	return out, ErrRevisionConflict
}
