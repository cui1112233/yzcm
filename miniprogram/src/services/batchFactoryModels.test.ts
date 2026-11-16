import { describe, expect, it } from "vitest";
import { deriveBookStageSummary, normalizeBatchFactoryBook, statusTone } from "./batchFactoryModels";

describe("batch factory domain models", () => {
  it("reads task progress from the owning job when tasks carry only videoId", () => {
    const view = deriveBookStageSummary({ id: "factory-1", bookId: "10151" }, { jobs: [{ bookId: "factory-1", tasks: [{ videoId: "video-1", status: "running" }] }] });
    expect(view.stages.video.status).toBe("running");
    expect(view.stages.video.label).toBe("生成中");
    expect(view.stages.merge.status).toBe("");
  });
  it("keeps batch, book and task status separate", () => {
    const view = normalizeBatchFactoryBook({
      id: "book-1",
      title: "第一本",
      production: { status: "running", taskId: "task-1" },
      merge: { status: "queued" },
      publish: { status: "unavailable", reason: "没有上传回执" },
    });
    expect(view.stages.video.status).toBe("running");
    expect(view.stages.merge.status).toBe("queued");
    expect(view.stages.publish.reason).toBe("没有上传回执");
  });

  it("does not infer completion from an unknown status", () => {
    expect(statusTone("mystery-status")).toEqual({ tone: "unknown", label: "mystery-status" });
    expect(statusTone("")).toEqual({ tone: "unknown", label: "状态未返回" });
  });
});
