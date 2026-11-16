import { describe, expect, test } from "vitest";
import { stageCanEnter, stageIndex, stageNext, stageTitle } from "./stageState";

describe("private comic stage state", () => {
  test("maps stages in production order", () => {
    expect(stageIndex("source")).toBe(0);
    expect(stageIndex("assets")).toBe(1);
    expect(stageIndex("result")).toBe(4);
    expect(stageNext("storyboard")).toBe("render");
    expect(stageNext("result")).toBeNull();
  });

  test("does not enter assets without a created book", () => {
    expect(stageCanEnter("assets", { sourceLength: 120, batchId: "", assetsCount: 0, pagesCount: 0, finished: false })).toBe(false);
    expect(stageCanEnter("assets", { sourceLength: 120, batchId: "b1", assetsCount: 0, pagesCount: 0, finished: false })).toBe(true);
  });

  test("uses user-facing Chinese titles", () => {
    expect(["source", "assets", "storyboard", "render", "result"].map((stage) => stageTitle(stage as any))).toEqual(["故事设置", "资产工作台", "生成配置确认", "分页出图", "成果导出"]);
  });
});
