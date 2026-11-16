import { describe, expect, test } from "vitest";
import { buildAgentPlanRequest, buildAgentProject, textModelsFromPayload, createStudioClient, buildStudioRun, settleStudioRuns } from "./agentStudio";
import { invalidateSessionResources } from "./sessionBoundary";
import { vi } from "vitest";

describe("Agent Studio 小程序契约", () => {
  test("an uncertain paid run remains blocked across client recreation until matching readback", async () => {
    invalidateSessionResources();
    const request = vi.fn().mockRejectedValue(new Error("timeout"));
    await expect(createStudioClient(request).submitRun({ confirmed: true, request_key: "pending-key" })).rejects.toThrow("timeout");
    await expect(createStudioClient(request).submitRun({ confirmed: true, request_key: "new-key" })).rejects.toThrow("结果未知");
    expect(request).toHaveBeenCalledTimes(1);
    settleStudioRuns([{ id: "run-receipt", request: { request_key: "unrelated" } }]);
    await expect(createStudioClient(request).submitRun({ confirmed: true, request_key: "new-key" })).rejects.toThrow("结果未知");
    settleStudioRuns([{ id: "run-receipt", request: { request_key: "pending-key" } }]);
    request.mockResolvedValue({ id: "new-run" });
    await expect(createStudioClient(request).submitRun({ confirmed: true, request_key: "intentional-new-key" })).resolves.toEqual({ id: "new-run" });
  });
  test("project save uses real Studio revision contract then reads same project", async () => {
    const request = vi.fn().mockResolvedValue({ id: "project-1", revision: 3, name: "项目", document: { nodes: [] } });
    const client = createStudioClient(request);
    await client.saveProject({ id: "project-1", revision: 2, name: "项目", document: { nodes: [] }, created_at: "2026-10-16", updated_at: "2026-10-16" } as any);
    expect(request.mock.calls).toEqual([["/api/studio/agent/projects/project-1", { method: "PUT", data: { id: "project-1", revision: 2, name: "项目", document: { nodes: [] } } }], ["/api/studio/agent/projects/project-1"]]);
  });
  test("invalid project collections do not silently become an empty workspace", async () => {
    await expect(createStudioClient(async () => ({ tasks: [] })).projects()).rejects.toThrow(/项目/);
  });
  test("a failed readback after an accepted project save remains explicitly unknown", async () => {
    const request = vi.fn().mockResolvedValueOnce({ id: "project-1", revision: 3 }).mockRejectedValueOnce(Object.assign(new Error("forbidden read"), { statusCode: 403 }));
    await expect(createStudioClient(request).saveProject({ id: "project-1", name: "项目", revision: 2, document: { nodes: [] } })).rejects.toThrow("结果未知");
  });
  test("unconfirmed generation is rejected before network and image-to-video keeps source asset", async () => {
    expect(() => buildStudioRun({ mode: "video", instruction: "镜头", videoModelId: "5", assetId: "image-1", requestKey: "stable-key", confirmed: false })).toThrow(/确认/);
    expect(buildStudioRun({ mode: "video", instruction: "镜头", videoModelId: "5", assetId: "image-1", requestKey: "stable-key", confirmed: true })).toEqual({ request_key: "stable-key", mode: "video", instruction: "镜头", image_model_id: 0, video_model_id: 5, input_asset_id: "image-1", size: "", duration: 0, confirmed: true });
  });
  test("仅展示可用的文本模型，并保留服务端数值 ID", () => {
    expect(textModelsFromPayload({
      models: [
        { id: 12, name: "规划模型", kind: "text", enabled: true },
        { id: 13, name: "停用文本", kind: "text", enabled: false },
        { id: 14, name: "图像模型", kind: "image", enabled: true },
        { id: "not-a-number", name: "错误模型", kind: "text", enabled: true },
      ],
    })).toEqual([{ id: 12, name: "规划模型" }]);
  });

  test("新建项目写入服务端可持久化的空文档", () => {
    expect(buildAgentProject("  短剧分镜  ")).toEqual({
      name: "短剧分镜",
      document: { nodes: [], edges: [], viewport: { x: 0, y: 0, zoom: 1 } },
    });
  });

  test("规划请求只提交文本模型与用户任务，不携带自动执行动作", () => {
    expect(buildAgentPlanRequest("12", "  为第一集列出分镜计划  ", "短剧分镜")).toEqual({
      text_model_id: 12,
      message: "为第一集列出分镜计划",
      context: "当前项目：短剧分镜",
    });
  });
});
