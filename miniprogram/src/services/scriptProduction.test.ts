import { expect, test, vi } from "vitest";
vi.mock("./api", () => ({ apiRequest: vi.fn() }));
import { apiRequest } from "./api";
import { splitScriptShots, scriptVideoPayload, createShotVideo, savePersonalPrompt, persistScriptVideoTasks } from "./scriptProduction";
test("splits outer shots without splitting internal headings and applies H3 duration", () => {
  const shots = splitScriptShots("### 分镜一\n总时长：12s\n#### 画面\n动作\n### 分镜二\n总时长：10s");
  expect(shots).toHaveLength(2);
  expect(scriptVideoPayload(shots[0], "minimax-h3-video", "15 秒", [])).toEqual({ prompt: shots[0], modelKey: "minimax-h3-video", duration: 12, resolution: "480p竖", imageUrls: [] });
  expect(() => scriptVideoPayload("总时长：16s", "minimax-h3-video", "15 秒", [])).toThrow(/15/);
});
test("existing live task cannot be resubmitted and new task is written to its own history", async () => {
  vi.mocked(apiRequest).mockReset();
  await expect(createShotVideo("h", "0", "shot", "m", "15 秒", { 0: { taskId: "existing", status: "processing" } })).rejects.toThrow(/已有/);
  expect(apiRequest).not.toHaveBeenCalled();
  vi.mocked(apiRequest).mockResolvedValueOnce({ entry: { videoTasks: {} } }).mockResolvedValueOnce({ taskId: "new" }).mockResolvedValueOnce({ entry: { videoTasks: {} } }).mockResolvedValueOnce({ ok: true }).mockResolvedValueOnce({ entry: { videoTasks: { 0: { taskId: "new", status: "processing", prompt: "shot" } } } });
  await expect(createShotVideo("h", "0", "shot", "m", "15 秒", {})).resolves.toMatchObject({ tasks: { 0: { taskId: "new" } }, warning: "" });
  expect(apiRequest).toHaveBeenNthCalledWith(4, "/api/history/h", { method: "PATCH", data: { videoTasks: { 0: { taskId: "new", status: "processing", prompt: "shot" } } } });
});
test("a provider task survives history persistence failure so it is not resubmitted", async () => {
  vi.mocked(apiRequest).mockReset().mockResolvedValueOnce({ entry: {} }).mockResolvedValueOnce({ taskId: "receipt" }).mockRejectedValueOnce(new Error("offline"));
  const result = await createShotVideo("failed-sync", "0", "shot", "m", "15 秒", {});
  expect(result.tasks[0].taskId).toBe("receipt");
  expect(result.warning).toContain("历史");
  vi.mocked(apiRequest).mockReset().mockResolvedValueOnce({ entry: {} }).mockResolvedValueOnce({ ok: true }).mockResolvedValueOnce({ entry: { videoTasks: result.tasks } });
  const restored = await createShotVideo("failed-sync", "0", "shot", "m", "15 秒", {});
  expect(restored.tasks[0].taskId).toBe("receipt");
  expect(apiRequest).not.toHaveBeenCalledWith("/api/script-video", expect.anything());
});
test("personal prompt save is verified against actual list after PUT", async () => {
  vi.mocked(apiRequest).mockReset().mockResolvedValueOnce({ ok: true }).mockResolvedValueOnce({ prompts: [{ id: "p", category: "quality", name: "画质", body: "清晰" }] });
  await expect(savePersonalPrompt({ id: "p", category: "quality", name: "画质", body: "清晰" })).resolves.toHaveLength(1);
  expect(apiRequest).toHaveBeenNthCalledWith(1, "/api/script-constraint-prompts/p", { method: "PUT", data: { category: "quality", name: "画质", body: "清晰" } });
});
test("unknown video submission is fenced across repeated attempts", async () => {
  vi.mocked(apiRequest).mockReset().mockResolvedValueOnce({ entry: {} }).mockRejectedValueOnce(new Error("network timeout"));
  await expect(createShotVideo("unknown-history", "0", "shot", "m", "15 秒", {})).rejects.toThrow(/timeout/);
  await expect(createShotVideo("unknown-history", "0", "shot", "m", "15 秒", {})).rejects.toThrow(/阻止重复/);
  expect(apiRequest).toHaveBeenCalledTimes(2);
});
test("reopened history recovers a server task without creating a paid task", async () => {
  vi.mocked(apiRequest).mockReset().mockResolvedValueOnce({ entry: { videoTasks: { 0: { taskId: "server-existing", status: "processing" } } } });
  expect((await createShotVideo("reopened", "0", "shot", "m", "15 秒", {})).tasks[0].taskId).toBe("server-existing");
  expect(apiRequest).toHaveBeenCalledTimes(1);
});
test("history saves serialize and merge distinct slots from stale component snapshots", async () => {
  let saved: any = { 2: { taskId: "original", status: "completed" } };
  vi.mocked(apiRequest).mockReset().mockImplementation(async (_path, options: any) => {
    if (options?.method === "PATCH") { saved = options.data.videoTasks; return { ok: true }; }
    return { entry: { videoTasks: { ...saved } } };
  });
  await Promise.all([persistScriptVideoTasks("parallel", { 0: { taskId: "first", status: "processing" } }), persistScriptVideoTasks("parallel", { 1: { taskId: "second", status: "processing" } })]);
  expect(Object.values(saved).map((value: any) => value.taskId)).toEqual(["first", "second", "original"]);
});
