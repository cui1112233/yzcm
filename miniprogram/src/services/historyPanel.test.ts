import { describe, expect, test } from "vitest";
import { historyPanelFromEntry } from "./historyPanel";

describe("小说面板历史数据", () => {
  test("从服务端历史条目还原原文、人物、场景和分镜", () => {
    expect(historyPanelFromEntry({
      novelText: "雨夜，林晚登上天台。",
      output: "### 分镜一\n林晚推开天台门。\n\n### 分镜二\n霓虹映在雨水上。",
      extractInfo: {
        characters: [{ name: "林晚", description: "记者" }],
        scenes: [{ name: "天台", description: "雨夜" }],
      },
    })).toEqual({
      novelText: "雨夜，林晚登上天台。",
      characters: [{ name: "林晚", description: "记者" }],
      scenes: [{ name: "天台", description: "雨夜" }],
      shots: ["### 分镜一\n林晚推开天台门。", "### 分镜二\n霓虹映在雨水上。"],
      output: "### 分镜一\n林晚推开天台门。\n\n### 分镜二\n霓虹映在雨水上。",
    });
  });

  test("缺失服务端字段时保留空状态而不填充示例内容", () => {
    expect(historyPanelFromEntry({})).toEqual({
      novelText: "",
      characters: [],
      scenes: [],
      shots: [],
      output: "",
    });
  });
});
