import { beforeEach, expect, test, vi } from "vitest";

const harness = vi.hoisted(() => ({ slots: [] as any[], cursor: 0, cleanups: [] as (() => void)[], api: vi.fn(), media: vi.fn() }));
vi.mock("react", () => ({
  useState: (initial: any) => { const i = harness.cursor++; if (!(i in harness.slots)) harness.slots[i] = typeof initial === "function" ? initial() : initial; return [harness.slots[i], (next: any) => { harness.slots[i] = typeof next === "function" ? next(harness.slots[i]) : next; }]; },
  useRef: (initial: any) => { const i = harness.cursor++; if (!(i in harness.slots)) harness.slots[i] = { current: initial }; return harness.slots[i]; },
  useEffect: (effect: () => (() => void) | void) => { const cleanup = effect(); if (cleanup) harness.cleanups.push(cleanup); },
}));
vi.mock("@tarojs/components", () => Object.fromEntries(["Button", "Text", "Video", "View"].map((name) => [name, name])));
vi.mock("../../services/api", () => ({ apiRequest: harness.api }));
vi.mock("../../services/mediaCache", () => ({ loadAuthenticatedMedia: harness.media }));
import ScriptHistoryVideos from "./ScriptHistoryVideos";

const tasks = { 0: { taskId: "existing", status: "completed", videoUrl: "/api/script-video/existing/video" }, 1: { taskId: "pending", status: "processing" } };
function render() { harness.cursor = 0; return ScriptHistoryVideos({ tasks }); }
function nodes(tree: any): any[] { return !tree || typeof tree !== "object" ? [] : [tree, ...[tree.props?.children].flat(Infinity).flatMap(nodes)]; }
function text(tree: any): string { return typeof tree === "string" || typeof tree === "number" ? String(tree) : tree?.props ? [tree.props.children].flat(Infinity).map(text).join("") : ""; }
function button(label: string, index = 0) { return nodes(render()).filter((item) => item.type === "Button" && text(item) === label)[index]; }
beforeEach(() => { harness.slots = []; harness.cursor = 0; harness.cleanups = []; harness.api.mockReset(); harness.media.mockReset(); });

test("existing history video is downloaded through authenticated media and exposed to the native player", async () => {
  harness.media.mockResolvedValue({ path: "wxfile://video.mp4" });
  expect(text(render())).toContain("分镜 1");
  await button("查看视频").props.onClick();
  expect(harness.media).toHaveBeenCalledWith("/api/script-video/existing/video", "mp4");
  expect(nodes(render()).find((item) => item.type === "Video")?.props).toMatchObject({ src: "wxfile://video.mp4", controls: true });
  expect(harness.api).not.toHaveBeenCalled();
});

test("refreshing an existing pending task reveals the returned video without creating another job", async () => {
  harness.api.mockResolvedValue({ taskId: "pending", status: "completed", videoUrl: "/api/script-video/pending/video" });
  await button("刷新状态", 1).props.onClick();
  expect(harness.api.mock.calls).toEqual([["/api/script-video/pending"]]);
  expect(nodes(render()).filter((item) => item.type === "Button" && text(item) === "查看视频")).toHaveLength(2);
});

test("download failures remain visible and can be retried without fake playback", async () => {
  harness.media.mockRejectedValue(new Error("视频下载失败：403"));
  await button("查看视频").props.onClick();
  expect(text(render())).toContain("视频下载失败：403");
  expect(nodes(render()).some((item) => item.type === "Video")).toBe(false);
  harness.media.mockResolvedValue({ path: "wxfile://retry.mp4" });
  await button("查看视频").props.onClick();
  expect(nodes(render()).find((item) => item.type === "Video")?.props.src).toBe("wxfile://retry.mp4");
});

test("double tapping does not duplicate downloads and leaving the history discards the late player", async () => {
  let resolve!: (value: any) => void;
  harness.media.mockReturnValue(new Promise((done) => { resolve = done; }));
  const first = button("查看视频").props.onClick();
  await button("查看视频").props.onClick();
  expect(harness.media).toHaveBeenCalledTimes(1);
  harness.cleanups[0]();
  resolve({ path: "wxfile://old-history.mp4" }); await first;
  expect(nodes(render()).some((item) => item.type === "Video")).toBe(false);
});

test("a mismatched task response cannot replace the selected history video", async () => {
  harness.api.mockResolvedValue({ taskId: "other", status: "completed", videoUrl: "/wrong.mp4" });
  await button("刷新状态", 1).props.onClick();
  expect(text(render())).toContain("视频任务响应无效");
  expect(nodes(render()).filter((item) => item.type === "Button" && text(item) === "查看视频")).toHaveLength(1);
});
