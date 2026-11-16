import { describe, expect, test } from "vitest";
import { buildExtractRequest, buildGenerateRequest, buildHistoryEntry, getScriptStepState, normalizeScriptEntities, parseScriptOutput, restoreScriptHistory, scriptConstraintChoices, updateScriptConstraint } from "./scriptWorkspace";

const base = {
  novelText: "  原文内容  ",
  characters: [{ id: "c1", name: "林晚" }],
  scenes: [{ id: "s1", name: "客厅" }],
  opening: "分段开头",
  format: "分镜",
  duration: "15 秒",
  matchAudio: false,
  audioTotalSeconds: 36,
  textModelId: "text-1",
  extractionPreset: "preset-1",
};

describe("小程序剧本工作台请求契约", () => {
  test("恢复公网历史时保留每个分镜的视频任务和播放地址", () => {
    expect(restoreScriptHistory({ ...buildHistoryEntry(base, "分镜正文"), videoTasks: { 0: { taskId: "task-1", status: "completed", videoUrl: "/api/script-video/task-1/video" }, 1: { taskId: "task-2", status: "processing" } } })).toMatchObject({ videoTasks: { 0: { taskId: "task-1", status: "completed", videoUrl: "/api/script-video/task-1/video" }, 1: { taskId: "task-2", status: "processing" } } });
  });
  test("约束目录按公网 constraintCategory 选择且开启层时启用总开关", () => {
    const catalog = [{ id: "prefix-1", constraintCategory: "prefix" }, { id: "other", constraintCategory: "quality" }];
    expect(scriptConstraintChoices(catalog, "prefix")).toEqual([catalog[0]]);
    const updated = updateScriptConstraint({}, "prefix", { enabled: true, source: "system", presetId: "prefix-1" });
    expect(buildGenerateRequest({ ...base, constraints: updated }).constraints).toMatchObject({ enabled: true, prefix: { enabled: true, presetId: "prefix-1" } });
    expect(updateScriptConstraint(updated, "prefix", { enabled: false }).enabled).toBe(false);
  });
  test("人物场景提取请求与网页端使用同一 promptType", () => {
    expect(buildExtractRequest(base)).toMatchObject({
      promptType: "extract",
      novelText: "原文内容",
      extractionPreset: "preset-1",
      textModelId: "text-1",
    });
  });

  test("剧本生成请求携带人物、场景、模式和时长", () => {
    expect(buildGenerateRequest(base)).toMatchObject({
      promptType: "script",
      mode: "segmented",
      format: "shotlist",
      duration: "15s",
      characters: base.characters,
      scenes: base.scenes,
      max_tokens: 16000,
      audioTotalSeconds: null,
    });
    expect(buildGenerateRequest({ ...base, opening: "爆款开头", format: "剧本", duration: "10 秒" })).toMatchObject({
      mode: "hook",
      format: "shortdrama",
      duration: "10s",
    });
  });

  test("生成结果保存为可被历史面板读取的标准条目", () => {
    expect(buildHistoryEntry(base, "  生成结果  ")).toMatchObject({
      mode: "segmented",
      format: "shotlist",
      duration: "15s",
      output: "生成结果",
      novelText: "原文内容",
      extractInfo: { characters: base.characters, scenes: base.scenes },
      videoTasks: {},
    });
  });

  test("步骤状态按原文和人物场景进度变化", () => {
    expect(getScriptStepState({ novelText: "", characters: [], scenes: [] })).toMatchObject({ source: "active", entities: "idle" });
    expect(getScriptStepState(base)).toMatchObject({ source: "done", entities: "done" });
  });

  test("兼容公网返回的 extractInfo 和 people 字段", () => {
    expect(normalizeScriptEntities({ extractInfo: { characters: [{ id: "c" }], scenes: [{ id: "s" }] } })).toEqual({ characters: [{ id: "c" }], scenes: [{ id: "s" }] });
    expect(normalizeScriptEntities({ people: [{ id: "c" }] })).toEqual({ characters: [{ id: "c" }], scenes: [] });
  });

  test("透传并保存视觉风格、聚焦人物和分层约束，恢复时不丢失", () => {
    const input = { ...base, visualStyle: "电影光影", protagonists: ["c1"], constraints: { enabled: true, prefix: { enabled: true, source: "draft", body: "统一服装" } } };
    expect(buildGenerateRequest(input)).toMatchObject({ visualStyle: input.visualStyle, protagonists: [base.characters[0]], constraints: input.constraints });
    const saved = buildHistoryEntry(input, "剧本");
    expect(saved).toMatchObject({ textModelId: "text-1", constraints: input.constraints });
    expect(restoreScriptHistory(saved)).toMatchObject({ ...input, novelText: "原文内容", audioTotalSeconds: null, output: "剧本" });
  });

  test("提取解析 OpenAI 文本内的 JSON，结构错误不冒充结果", () => {
    expect(normalizeScriptEntities({ choices: [{ message: { content: '```json\n{"characters":[{"name":"林晚"}],"scenes":[]}\n```' } }] }).characters).toEqual([{ name: "林晚" }]);
    expect(parseScriptOutput({ choices: [{ message: { content: "实际分镜" } }] })).toBe("实际分镜");
    expect(() => parseScriptOutput({ status: "queued" })).toThrow(/内容/);
  });
});
