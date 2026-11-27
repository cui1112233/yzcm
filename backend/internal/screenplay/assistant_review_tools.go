package screenplay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

func IsAssistantReviewReadTool(name string) bool {
	switch name {
	case "compare_original_vs_voicetext", "review_voice_emotions", "review_image_prompts", "review_video_prompts", "review_asset_bindings":
		return true
	}
	return false
}

func validateAssistantReviewArgs(name string, args assistantReadArgs, fields map[string]json.RawMessage) error {
	for key, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("审核参数 %s 不能为空", key)
		}
		switch key {
		case "episode_no":
			if args.Episode < 1 {
				return fmt.Errorf("剧集序号必须大于0")
			}
		case "engine":
			if args.Engine == "" || strings.TrimSpace(string(args.Engine)) != string(args.Engine) {
				return fmt.Errorf("引擎必须是实际保存的引擎标识")
			}
		case "shot_ids", "shot_numbers":
			if (key == "shot_ids" && len(args.ShotIDs) == 0) || (key == "shot_numbers" && len(args.ShotNumbers) == 0) {
				return fmt.Errorf("镜头筛选不能为空数组")
			}
		case "focus":
			if name != "review_image_prompts" {
				return fmt.Errorf("仅画面提示词检查支持focus")
			}
			switch args.Focus {
			case "quality", "consistency", "style", "all":
			default:
				return fmt.Errorf("画面提示词检查的focus必须是quality、consistency、style或all")
			}
		default:
			return fmt.Errorf("此审核工具不接受参数 %s", key)
		}
	}
	return nil
}

func assistantTextPrefix(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	for limit > 0 && !utf8.RuneStart(value[limit]) {
		limit--
	}
	return value[:limit]
}

func (s StoryboardService) addAssistantReviewOriginal(ctx context.Context, project Project, board Storyboard, data map[string]any, text func(string) string) error {
	chapter, boundID, _, err := s.storyboardSourceChapter(ctx, project, board)
	if err != nil {
		return err
	}
	source := chapter.SourceText
	data["source_status"] = "unverified"
	data["source_text"] = ""
	// Storyboard generation hashes its trimmed input. Return the saved original
	// intact once that digest ties it to the requested board.
	if boundID == "" && strings.TrimSpace(source) == "" && board.SourceSHA256 != "" && board.SourceSHA256 == contentDigest(strings.TrimSpace(project.SourceText)) {
		source = project.SourceText
		data["source_basis"] = "project_source_digest_match"
	}
	if strings.TrimSpace(source) == "" {
		data["source_reason"] = "没有能归到所选分镜的已保存原文"
		return nil
	}
	if data["source_basis"] == nil && chapter.ID != "" {
		data["chapter_id"] = text(chapter.ID)
		data["source_basis"] = "episode_chapter_order"
		if boundID != "" {
			data["source_basis"] = "saved_generation_chapter"
		}
	}
	const sourceBudget = 32 * 1024
	prefix := text(assistantTextPrefix(source, sourceBudget))
	data["source_text"] = prefix
	data["source_status"] = "available"
	if len(prefix) != len(source) {
		data["truncated"] = true
		data["source_status"] = "truncated"
	}
	return nil
}

type assistantReviewAssets struct {
	byID      map[string]Asset
	byName    map[AssetType]map[string][]Asset
	remaining int
}

func indexAssistantReviewAssets(assets []Asset) assistantReviewAssets {
	result := assistantReviewAssets{
		byID:      make(map[string]Asset, len(assets)),
		byName:    make(map[AssetType]map[string][]Asset),
		remaining: 600,
	}
	for _, asset := range assets {
		result.byID[asset.ID] = asset
		if result.byName[asset.Type] == nil {
			result.byName[asset.Type] = map[string][]Asset{}
		}
		names := result.byName[asset.Type]
		names[asset.Name] = append(names[asset.Name], asset)
	}
	return result
}

func (assets *assistantReviewAssets) bindings(shot StoryboardShot, text func(string) string, markTruncated func()) map[AssetType]map[string]any {
	result := make(map[AssetType]map[string]any, len(bindingKinds))
	assetSummary := func(a Asset) map[string]any {
		return map[string]any{"id": text(a.ID), "name": text(a.Name), "type": a.Type, "revision": a.Revision}
	}
	for _, kind := range bindingKinds {
		names := *bindingShotField(&shot, kind)
		binding := map[string]any{"saved_names": text(names), "id_source": "name_only", "id_status": "no_saved_ids"}
		result[kind] = binding
		ids, explicit, err := ShotAssetBindingIDs(shot.ProtocolFields, kind, false)
		if explicit {
			binding["id_source"] = "explicit"
		} else if err == nil {
			var legacy bool
			ids, legacy, err = ShotAssetBindingIDs(shot.ProtocolFields, kind, true)
			if legacy {
				binding["id_source"] = "legacy"
			}
		} else {
			binding["id_source"] = "unknown"
		}
		switch {
		case err != nil:
			binding["id_status"] = "invalid"
		case explicit && len(ids) == 0:
			binding["id_status"] = "unbound"
		case len(ids) > 0:
			binding["id_status"] = "saved"
		}
		idFacts := []map[string]any{}
		for _, id := range ids {
			if assets.remaining == 0 || len(idFacts) == 20 {
				markTruncated()
				break
			}
			assets.remaining--
			match := map[string]any{"id": text(id), "status": "missing"}
			if asset, ok := assets.byID[id]; ok {
				match["asset"] = assetSummary(asset)
				match["status"] = "resolved"
				if asset.Type != kind {
					match["status"] = "wrong_type"
				}
			}
			idFacts = append(idFacts, match)
		}
		binding["ids"], binding["ids_total"] = idFacts, len(ids)
		nameFacts := []map[string]any{}
		nameTotal := 0
		for _, name := range splitAssetNames(names) {
			if name == "none" || name == "无道具" || name == "-" {
				continue
			}
			nameTotal++
			if assets.remaining == 0 || len(nameFacts) == 20 {
				markTruncated()
				continue
			}
			assets.remaining--
			match := map[string]any{"name": text(name), "status": "unresolved"}
			switch candidates := assets.byName[kind][name]; len(candidates) {
			case 0:
			case 1:
				match["status"] = "resolved"
				match["asset"] = assetSummary(candidates[0])
			default:
				match["status"] = "ambiguous"
				match["candidate_count"] = len(candidates)
			}
			nameFacts = append(nameFacts, match)
		}
		binding["name_matches"], binding["names_total"] = nameFacts, nameTotal
	}
	return result
}

func addAssistantReviewShot(row map[string]any, shot StoryboardShot, name string, text func(string) string) {
	missing := []string{}
	field := func(key, value string) {
		row[key] = text(value)
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	switch name {
	case "compare_original_vs_voicetext", "review_voice_emotions":
		field("line", shot.Line)
		field("voice_actor", shot.VoiceActor)
		if name == "review_voice_emotions" {
			field("emotion", shot.Emotion)
		}
	case "review_image_prompts", "review_video_prompts", "review_asset_bindings":
		row["line"] = text(shot.Line)
		if name == "review_asset_bindings" {
			row["video_prompt"] = text(shot.VideoDesc)
			row["image_prompt"] = text(shot.ImagePrompt)
			break
		}
		row["characters"] = text(shot.Characters)
		row["scene"] = text(shot.Scene)
		row["props"] = text(shot.Props)
		if name == "review_image_prompts" {
			field("image_prompt", shot.ImagePrompt)
			row["video_prompt"] = text(shot.VideoDesc)
		} else {
			field("video_prompt", shot.VideoDesc)
			row["duration_sec"] = shot.Duration
		}
	}
	row["missing_fields"] = missing
}
