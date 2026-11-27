package screenplay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
)

type AssistantShotChange struct {
	Field  string `json:"field"`
	Before string `json:"before"`
	After  string `json:"after"`
}
type AssistantCreativeShotEdit struct {
	BoardID string                `json:"board_id"`
	Episode int                   `json:"episode_no"`
	Engine  StoryboardEngine      `json:"engine"`
	Before  StoryboardShot        `json:"before"`
	After   StoryboardShot        `json:"after"`
	Binding *AssistantShotBinding `json:"binding,omitempty"`
	Changes []AssistantShotChange `json:"changes"`
}

func IsAssistantShotEditTool(name string) bool {
	switch name {
	case "bind_shot_assets", "add_shot", "delete_shots", "modify_storyboard", "modify_script_text", "modify_image_prompt", "modify_video_prompt":
		return true
	}
	return false
}

// Strict object decoding prevents aliases, duplicate keys and null values from
// silently changing the selected target or the requested operation.
func assistantShotObject(raw json.RawMessage, allowed ...string) (map[string]json.RawMessage, error) {
	return assistantStrictEditObject(raw, 32768, "镜头", allowed...)
}

func assistantStrictEditObject(raw json.RawMessage, limit int, label string, allowed ...string) (map[string]json.RawMessage, error) {
	fail := func() (map[string]json.RawMessage, error) {
		return nil, fmt.Errorf("%w: %s参数字段无效、重复或为空", ErrInvalidArgument, label)
	}
	if len(raw) == 0 || len(raw) > limit {
		return fail()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return fail()
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		tok, e = d.Token()
		if e != nil {
			return fail()
		}
		key, ok := tok.(string)
		if !ok {
			return fail()
		}
		valid := false
		for _, k := range allowed {
			if key == k {
				valid = true
			}
		}
		if !valid || out[key] != nil {
			return fail()
		}
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return fail()
		}
		out[key] = v
	}
	if tok, e = d.Token(); e != nil || tok != json.Delim('}') {
		return fail()
	}
	var tail any
	if d.Decode(&tail) != io.EOF {
		return fail()
	}
	return out, nil
}
func shotText(s *StoryboardShot, field string) *string {
	switch field {
	case "video_desc":
		return &s.VideoDesc
	case "image_prompt":
		return &s.ImagePrompt
	case "line":
		return &s.Line
	case "voice_actor":
		return &s.VoiceActor
	case "emotion":
		return &s.Emotion
	}
	return nil
}
func PrepareAssistantShotEdit(ctx context.Context, store Store, owner, project, name string, raw json.RawMessage) (AuthoringAction, string, error) {
	if name == "bind_shot_assets" {
		return prepareAssistantShotBinding(ctx, store, owner, project, raw)
	}
	if isShotStructure(name, raw) {
		return PrepareAssistantShotStructure(ctx, store, owner, project, name, raw)
	}
	fail := func(reason string) (AuthoringAction, string, error) {
		return AuthoringAction{}, "", fmt.Errorf("%w: %s", ErrInvalidArgument, reason)
	}
	if name == "modify_script_text" {
		return fail("创作分镜没有独立画面描述字段，modify_script_text暂不支持；不能改写台词或视频词代替")
	}
	if !IsAssistantShotEditTool(name) {
		return fail("不支持的镜头修改工具")
	}
	allowed := []string{"episode_no", "engine", "shot_id", "shot_number"}
	if name == "modify_storyboard" {
		allowed = append(allowed, "action", "new_data")
	} else {
		allowed = append(allowed, "action", "mode", "new_prompt")
	}
	f, e := assistantShotObject(raw, allowed...)
	if e != nil {
		return AuthoringAction{}, "", e
	}
	var episode, number int
	var engine StoryboardEngine
	var id, mode, action string
	for key, dst := range map[string]any{"episode_no": &episode, "engine": &engine, "shot_id": &id, "shot_number": &number, "mode": &mode, "action": &action} {
		if f[key] != nil && json.Unmarshal(f[key], dst) != nil {
			return fail("筛选参数类型无效")
		}
	}
	if episode < 0 || number < 0 || (f["episode_no"] != nil && episode == 0) || (f["shot_number"] != nil && number == 0) || len(engine) > 128 || len(id) > 256 || strings.TrimSpace(id) != id || (id == "" && number == 0) {
		return fail("请提供真实shot_id或正整数shot_number")
	}
	if name != "modify_storyboard" && action != "" {
		if mode != "" && mode != action {
			return fail("action和mode冲突")
		}
		mode = action
	}
	if mode == "" {
		mode = "replace"
	}
	if mode != "replace" && mode != "append" {
		return fail("本层仅支持replace或append，不支持AI rewrite或fix_violations")
	}
	values := map[string]string{}
	if name == "modify_storyboard" {
		if action != "update" {
			return fail("modify_storyboard仅支持action update，不支持新增删除")
		}
		nested, err := assistantShotObject(f["new_data"], "video_desc", "image_prompt", "line", "voice_actor", "emotion")
		if err != nil {
			return AuthoringAction{}, "", err
		}
		for k, v := range nested {
			var value string
			if json.Unmarshal(v, &value) != nil {
				return fail("文字字段必须为字符串")
			}
			values[k] = value
		}
	} else {
		key, field := "new_prompt", "image_prompt"
		if name == "modify_video_prompt" {
			field = "video_desc"
		}
		if len(f[key]) == 0 {
			return fail("请提供工具对应的新文字字段")
		}
		var value string
		if json.Unmarshal(f[key], &value) != nil {
			return fail("新文字必须为字符串")
		}
		values[field] = value
	}
	b, found, e := assistantShotTarget(ctx, store, owner, project, episode, engine, id, number)
	if e != nil {
		return AuthoringAction{}, "", e
	}
	edit := AssistantCreativeShotEdit{BoardID: b.ID, Episode: b.EpisodeNo, Engine: b.Engine, Before: *found, After: *found}
	for _, field := range []string{"video_desc", "image_prompt", "line", "voice_actor", "emotion"} {
		v, ok := values[field]
		if !ok {
			continue
		}
		dst := shotText(&edit.After, field)
		if mode == "append" {
			v = *dst + v
		}
		if len(v) > 16384 {
			return fail("单字段文字超过上限")
		}
		if *dst != v {
			edit.Changes = append(edit.Changes, AssistantShotChange{field, *dst, v})
			*dst = v
		}
	}
	if len(edit.Changes) == 0 {
		return fail("没有需要修改的文字")
	}
	if e = validateStoryboardShotInput(edit.After); e != nil {
		return AuthoringAction{}, "", e
	}
	return AuthoringAction{Kind: "creative_shot", TargetID: found.ID, ExpectedRevision: b.Revision, CreativeShot: &edit}, fmt.Sprintf("修改第%d集镜头%d文字", b.EpisodeNo, found.Seq), nil
}

func validateCreativeShotAction(a AuthoringAction) error {
	d := a.CreativeShot
	if d == nil || a.TargetID == "" || d.BoardID == "" || d.Episode <= 0 || d.Engine == "" || a.ExpectedRevision <= 0 || a.TargetID != d.Before.ID || len(d.Changes) == 0 || len(d.Changes) > 5 {
		return ErrInvalidArgument
	}
	if d.Binding != nil {
		return validateBindingEdit(d)
	}
	rebuilt := d.Before
	seen := map[string]bool{}
	for _, c := range d.Changes {
		dst := shotText(&rebuilt, c.Field)
		if dst == nil || seen[c.Field] || *dst != c.Before || c.Before == c.After || len(c.After) > 16384 {
			return ErrInvalidArgument
		}
		seen[c.Field] = true
		*dst = c.After
	}
	if !reflect.DeepEqual(rebuilt, d.After) {
		return ErrInvalidArgument
	}
	return validateStoryboardShotInput(d.After)
}
func applyCreativeShot(board *Storyboard, a AuthoringAction) error {
	if e := validateCreativeShotAction(a); e != nil {
		return e
	}
	d := a.CreativeShot
	if board.ID != d.BoardID || board.Revision != a.ExpectedRevision || board.EpisodeNo != d.Episode || board.Engine != d.Engine {
		return ErrRevisionConflict
	}
	i := storyboardShotIndex(board.Shots, a.TargetID)
	if i < 0 {
		return ErrNotFound
	}
	current, err := json.Marshal(board.Shots[i])
	if err != nil {
		return err
	}
	original, err := json.Marshal(d.Before)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) && !(d.Binding != nil && bindingShotEqual(board.Shots[i], d.Before)) {
		return ErrRevisionConflict
	}
	board.Shots[i] = d.After
	return nil
}

func assistantShotTarget(ctx context.Context, store Store, owner, project string, episode int, engine StoryboardEngine, id string, number int) (Storyboard, *StoryboardShot, error) {
	invalid := func(reason string) (Storyboard, *StoryboardShot, error) {
		return Storyboard{}, nil, fmt.Errorf("%w: %s", ErrInvalidArgument, reason)
	}
	if _, e := store.GetProject(ctx, owner, project); e != nil {
		return Storyboard{}, nil, e
	}
	boards, e := store.ListStoryboards(ctx, owner, project, engine)
	if e != nil {
		return Storyboard{}, nil, e
	}
	matches := []Storyboard{}
	for _, b := range boards {
		if episode == 0 || episode == b.EpisodeNo {
			matches = append(matches, b)
		}
	}
	if len(matches) != 1 {
		return invalid("请明确选择唯一episode_no和engine")
	}
	b := matches[0]
	var found *StoryboardShot
	for _, sh := range b.Shots {
		if (id != "" && sh.ID != id) || (id == "" && sh.Seq != number) {
			continue
		}
		if number != 0 && sh.Seq != number {
			return invalid("镜头ID与序号冲突")
		}
		if found != nil {
			return invalid("镜头目标有歧义")
		}
		cp := sh
		found = &cp
	}
	if found == nil {
		return invalid("当前作品及所选剧集和引擎中没有该镜头")
	}
	return b, found, nil
}
