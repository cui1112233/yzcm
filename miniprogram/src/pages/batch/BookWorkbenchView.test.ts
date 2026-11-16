import { describe, expect, it } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import { bookWorkbenchStageRows } from "./bookWorkbenchState";

describe("book workbench stage model", () => {
  it("keeps source, video, merge and publish status distinct", () => {
    const rows = bookWorkbenchStageRows({
      id: "book-1",
      sourceText: "正文",
      production: { status: "running" },
      merge: { status: "queued" },
      publish: { status: "blocked" },
    } as any);
    expect(rows.map((row) => row.state)).toEqual(["已读取", "状态未返回", "running", "queued", "blocked"]);
  });

  it("renders real media and the single-book production surfaces", () => {
    const source = readFileSync(resolve(__dirname, "BookWorkbenchView.tsx"), "utf8");
    expect(source).toContain("Image");
    expect(source).toContain("Video");
    expect(source).toContain("workspace.sourceText");
    expect(source).toContain("workspace.assetImages");
    expect(source).toContain("workspace.production?.tasks");
    expect(source).toContain("视频提示词");
    expect(source).toContain("资产提示词");
    expect(source).toContain("提交");
  });
});
