package screenplay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

type assistantReadArgs struct {
	Episode       int              `json:"episode_no"`
	Engine        StoryboardEngine `json:"engine"`
	ShotID        string           `json:"shot_id"`
	ShotIDs       []string         `json:"shot_ids"`
	ShotNumbers   []int            `json:"shot_numbers"`
	AssetType     string           `json:"asset_type"`
	Name          string           `json:"name"`
	IncludePrompt *bool            `json:"include_prompt"`
	Focus         string           `json:"focus"`
}

func IsAssistantReadTool(name string) bool {
	switch name {
	case "get_current_screenplay", "analyze_screenplay", "get_editor_status", "get_shot_numbers", "get_shot_detail", "read_assets", "read_shot_video_prompts", "read_shot_image_prompts", "get_all_shots_voicetext":
		return true
	}
	return false
}

// ReadAssistantTool has no generator, filesystem path, owner override or write
// operation. Its identity always comes from the already-authorized HTTP scope.
func ReadAssistantTool(ctx context.Context, store Store, owner, project, name string, raw json.RawMessage) (AssistantToolRun, map[string]any, error) {
	run := AssistantToolRun{Name: name}
	invalid := func(reason string) (AssistantToolRun, map[string]any, error) {
		return run, nil, fmt.Errorf("%w: %s", ErrInvalidArgument, reason)
	}
	if !IsAssistantReadTool(name) {
		return invalid("不支持的只读工具")
	}
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > 8192 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return invalid("工具参数过大或为空")
	}
	var args assistantReadArgs
	dec := json.NewDecoder(bytes.NewReader(raw))
	opening, err := dec.Token()
	if err != nil || opening != json.Delim('{') {
		return invalid("工具参数必须是一个对象")
	}
	// Struct decoding alone accepts case aliases and duplicate keys. Validate
	// decoded key names first so neither can bypass selector presence checks.
	fields := map[string]json.RawMessage{}
	for dec.More() {
		token, e := dec.Token()
		if e != nil {
			return invalid("工具参数无效")
		}
		key, ok := token.(string)
		if !ok {
			return invalid("工具参数无效")
		}
		switch key {
		case "episode_no", "engine", "shot_id", "shot_ids", "shot_numbers", "asset_type", "name", "include_prompt", "focus":
		default:
			return invalid("工具参数字段必须使用规定的小写名称")
		}
		if _, duplicate := fields[key]; duplicate {
			return invalid("工具参数不能重复同名字段")
		}
		var value json.RawMessage
		if dec.Decode(&value) != nil {
			return invalid("工具参数无效")
		}
		fields[key] = value
	}
	if closing, e := dec.Token(); e != nil || closing != json.Delim('}') {
		return invalid("工具参数无效")
	}
	var tail any
	if dec.Decode(&tail) != io.EOF {
		return invalid("工具参数必须是一个对象")
	}
	if json.Unmarshal(raw, &args) != nil {
		return invalid("工具参数无效")
	}
	wantedIDs := map[string]bool{}
	if _, hasIDs := fields["shot_ids"]; hasIDs {
		switch name {
		case "get_shot_numbers", "read_shot_video_prompts", "read_shot_image_prompts", "get_all_shots_voicetext":
		default:
			return invalid("此工具不接受shot_ids；镜头详情请使用单个shot_id")
		}
		if _, mixed := fields["shot_numbers"]; mixed {
			return invalid("shot_ids不能与shot_numbers混用")
		}
		if _, mixed := fields["shot_id"]; mixed {
			return invalid("shot_ids不能与shot_id混用")
		}
		if len(args.ShotIDs) == 0 || len(args.ShotIDs) > 200 {
			return invalid("shot_ids必须是1至200个真实镜头ID的字符串数组")
		}
		for _, id := range args.ShotIDs {
			if id == "" || strings.TrimSpace(id) != id || len(id) > 256 {
				return invalid("镜头ID必须是1至256字节的真实字符串，不能含首尾空白")
			}
			wantedIDs[id] = true
		}
	}
	if args.Episode < 0 || len(args.ShotNumbers) > 200 || len(args.ShotID) > 256 || len(args.Name) > 256 || len(args.Focus) > 2048 || len(args.Engine) > 128 {
		return invalid("工具筛选条件超出范围")
	}
	wanted := map[int]bool{}
	for _, n := range args.ShotNumbers {
		if n < 1 {
			return invalid("镜头序号必须大于0")
		}
		wanted[n] = true
	}
	if args.AssetType != "" && args.AssetType != "all" && args.AssetType != string(AssetCharacter) && args.AssetType != string(AssetScene) && args.AssetType != string(AssetProp) {
		return invalid("资产类型无效")
	}
	p, err := store.GetProject(ctx, owner, project)
	if err != nil {
		return run, nil, err
	}
	data := map[string]any{"project_id": p.ID, "truncated": false}
	budget := 96 * 1024
	text := func(value string) string {
		if len(value) <= budget {
			budget -= len(value)
			return value
		}
		value = value[:budget]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
		budget = 0
		data["truncated"] = true
		return value
	}
	finish := func(summary string) (AssistantToolRun, map[string]any, error) {
		b, e := json.Marshal(data)
		if e != nil {
			return run, nil, e
		}
		if len(b) > 768*1024 {
			return invalid("工具结果超出范围，请缩小筛选")
		}
		run.OK = true
		run.Summary = summary
		if data["truncated"] == true {
			run.Summary += "（结果已截取，请缩小范围）"
		}
		return run, data, nil
	}
	if name == "read_assets" || name == "get_current_screenplay" || name == "get_editor_status" || name == "analyze_screenplay" {
		kind := AssetType(args.AssetType)
		if args.AssetType == "all" {
			kind = ""
		}
		assets, e := store.ListAssets(ctx, owner, project, kind)
		if e != nil {
			return run, nil, e
		}
		sort.SliceStable(assets, func(i, j int) bool { return assets[i].ID < assets[j].ID })
		rows := []map[string]any{}
		matched := 0
		for _, a := range assets {
			if args.Name != "" && !strings.Contains(a.Name, args.Name) {
				continue
			}
			matched++
			if len(a.ID) > 256 {
				return invalid("保存的资产编号超出范围")
			}
			if len(rows) >= 200 || budget <= 0 {
				data["truncated"] = true
				continue
			}
			rows = append(rows, map[string]any{"id": a.ID, "type": a.Type, "name": text(a.Name), "description": text(a.Description), "identity": text(a.Identity), "image_prompt": text(a.Prompt), "voice": text(a.Voice), "revision": a.Revision})
		}
		data["assets"] = rows
		data["asset_total"] = matched
		if name == "read_assets" {
			return finish(fmt.Sprintf("已读取%d项资产", len(rows)))
		}
		boards, e := store.ListStoryboards(ctx, owner, project, "")
		if e != nil {
			return run, nil, e
		}
		shotCount := 0
		for _, board := range boards {
			shotCount += len(board.Shots)
		}
		data["storyboard_total"] = len(boards)
		data["shot_total"] = shotCount
		doc, e := store.ActiveDocument(ctx, owner, project)
		if e != nil && !errors.Is(e, ErrNotFound) && !errors.Is(e, ErrNoActiveScript) {
			return run, nil, e
		}
		data["has_script"] = e == nil
		data["document_id"] = doc.ID
		data["document_revision"] = doc.Revision
		if name == "analyze_screenplay" {
			data["source_text"] = text(p.SourceText)
			data["script_text"] = text(doc.Content)
			data["focus"] = args.Focus
			data["scope"] = "已保存原文与当前剧本供对照；不表示已证明情节完整"
		}
		if name == "get_editor_status" {
			data["media_counts_verified"] = false
		}
		return finish(fmt.Sprintf("已读取作品：%d项资产、%d个分镜", matched, shotCount))
	}
	boards, err := store.ListStoryboards(ctx, owner, project, args.Engine)
	if err != nil {
		return run, nil, err
	}
	matches := []Storyboard{}
	for _, board := range boards {
		if args.Episode == 0 || board.EpisodeNo == args.Episode {
			matches = append(matches, board)
		}
	}
	if len(matches) > 1 {
		return invalid("存在多个剧集或引擎，请指定episode_no和engine")
	}
	if len(matches) == 0 {
		return run, nil, fmt.Errorf("%w: 没有符合条件的分镜", ErrNotFound)
	}
	board := matches[0]
	data["episode_no"] = board.EpisodeNo
	data["engine"] = board.Engine
	data["revision"] = board.Revision
	sort.SliceStable(board.Shots, func(i, j int) bool { return board.Shots[i].Seq < board.Shots[j].Seq })
	rows := []map[string]any{}
	matched := 0
	foundNumbers := map[int]bool{}
	foundIDs := map[string]bool{}
	if name == "get_shot_detail" && args.ShotID == "" {
		return invalid("请指定真实shot_id")
	}
	for _, shot := range board.Shots {
		if len(wantedIDs) > 0 && !wantedIDs[shot.ID] {
			continue
		}
		if args.ShotID != "" && shot.ID != args.ShotID {
			continue
		}
		if len(wanted) > 0 && !wanted[shot.Seq] {
			continue
		}
		matched++
		if len(shot.ID) > 256 {
			return invalid("保存的镜头编号超出范围")
		}
		foundNumbers[shot.Seq] = true
		foundIDs[shot.ID] = true
		if len(rows) >= 200 || budget <= 0 {
			data["truncated"] = true
			continue
		}
		row := map[string]any{"id": shot.ID, "seq": shot.Seq}
		switch name {
		case "get_shot_detail":
			row["characters"] = text(shot.Characters)
			row["scene"] = text(shot.Scene)
			row["props"] = text(shot.Props)
			row["video_prompt"] = text(shot.VideoDesc)
			row["image_prompt"] = text(shot.ImagePrompt)
			row["line"] = text(shot.Line)
			row["emotion"] = text(shot.Emotion)
			row["voice_actor"] = text(shot.VoiceActor)
			row["duration_sec"] = shot.Duration
		case "read_shot_video_prompts":
			row["characters"] = text(shot.Characters)
			row["scene"] = text(shot.Scene)
			row["props"] = text(shot.Props)
			if args.IncludePrompt == nil || *args.IncludePrompt {
				row["video_prompt"] = text(shot.VideoDesc)
			}
		case "read_shot_image_prompts":
			row["image_prompt"] = text(shot.ImagePrompt)
		case "get_all_shots_voicetext":
			row["line"] = text(shot.Line)
			row["voice_actor"] = text(shot.VoiceActor)
			row["emotion"] = text(shot.Emotion)
		}
		rows = append(rows, row)
	}
	if matched == 0 && (args.ShotID != "" || len(wanted) > 0) {
		return run, nil, fmt.Errorf("%w: 所选镜头不存在", ErrNotFound)
	}
	for n := range wanted {
		if !foundNumbers[n] {
			return run, nil, fmt.Errorf("%w: 请求的镜头序号%d不存在", ErrNotFound, n)
		}
	}
	for id := range wantedIDs {
		if !foundIDs[id] {
			return run, nil, fmt.Errorf("%w: 请求的镜头ID不在当前作品及所选剧集和引擎中", ErrNotFound)
		}
	}
	data["shots"] = rows
	data["matched_total"] = matched
	data["board_shot_total"] = len(board.Shots)
	return finish(fmt.Sprintf("已读取第%d集%d个镜头", board.EpisodeNo, len(rows)))
}
