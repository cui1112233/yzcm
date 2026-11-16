import { describe, expect, it } from "vitest";
import { entriesFromHistory, batchesFromPayload, latestHistoryEntry } from "./normalizers";

describe("Git 接口数据适配", () => {
  it("读取剧本历史接口返回的 entries", () => {
    expect(entriesFromHistory({ entries: [{ id: "h1", preview: "第一集" }] })).toEqual([
      { id: "h1", preview: "第一集" },
    ]);
  });

  it("only returns a real latest history entry and leaves absent history empty", () => {
    expect(latestHistoryEntry({ entries: [{ id: "h2", preview: "第 2 集" }] })).toMatchObject({ id: "h2", preview: "第 2 集" });
    expect(latestHistoryEntry({ entries: [] })).toBeUndefined();
    expect(latestHistoryEntry({ entries: [null, "bad"] })).toBeUndefined();
  });

  it("把小说获取任务兼容成批量工厂列表", () => {
    expect(batchesFromPayload({ tasks: [{ book_id: "b1", book_name: "第一本", ai_status: "完成" }] })).toEqual([
      { id: "b1", title: "第一本", status: "完成" },
    ]);
  });
});
