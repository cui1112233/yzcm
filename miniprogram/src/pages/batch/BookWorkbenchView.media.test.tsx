// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, test, vi } from "vitest";

const media = vi.hoisted(() => ({ loadAuthenticatedMedia: vi.fn(), forgetAuthenticatedMedia: vi.fn() }));
vi.mock("../../services/mediaCache", () => media);
vi.mock("@tarojs/taro", () => ({ default: {} }));
vi.mock("../../components/AppIcon", () => ({ default: () => null }));
vi.mock("@tarojs/components", () => ({
  View: "div", Text: "span", Button: "button", Image: "img",
  ScrollView: ({ children }: any) => <div>{children}</div>,
  Video: ({ src, onError }: any) => <video src={src} onError={() => onError({ detail: { errMsg: "MEDIA_ERR_DECODE", errCode: 3 } })} />,
}));
import BookWorkbenchView from "./BookWorkbenchView";
import type { BatchFactoryBookWorkspace } from "../../services/batchFactoryV11";

const workspace: BatchFactoryBookWorkspace = { readState: "ready", batchId: "batch-1", bookId: "book-1", book: { id: "book-1", title: "已有作品" }, stages: null, assets: [], assetImages: {}, production: { tasks: [{ mediaUrl: "/clip.mp4", prompt: "真实分镜", status: "succeeded" }] }, mergeStatus: null };
let container: HTMLDivElement;
let root: Root;
beforeEach(() => {
  vi.clearAllMocks();
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  container = document.createElement("div");
  document.body.appendChild(container);
  root = createRoot(container);
});
afterEach(async () => { await act(async () => root.unmount()); container.remove(); vi.unstubAllGlobals(); });
const render = async (data = workspace) => {
  await act(async () => root.render(<BookWorkbenchView workspace={data} capabilities={{}} actions={{ runDirector() {}, runProduction() {}, runMerge() {} }} />));
};

test("offers a completed merged work from a visible preview action", async () => {
  media.loadAuthenticatedMedia.mockResolvedValue({ path: "/tmp/merged.mp4" });
  await render({ ...workspace, production: null, mergeStatus: { jobs: [{ bookId: "book-1", status: "completed", outputUrl: "/merged.mp4" }] } });
  const preview = Array.from(container.querySelectorAll("button")).find((button) => button.textContent === "预览成片");
  expect(preview).toBeTruthy();
  await act(async () => preview!.click());
  expect(media.loadAuthenticatedMedia).toHaveBeenCalledWith("/merged.mp4", "mp4");
  expect(container.querySelector("video")?.getAttribute("src")).toBe("/tmp/merged.mp4");
});

test("failed native download displays its cause and hides signed URLs", async () => {
  media.loadAuthenticatedMedia.mockRejectedValueOnce(new Error("downloadFile:fail timeout https://media.example/clip.mp4?token=private"));
  await render();
  expect(container.textContent).toContain("timeout");
  expect(container.textContent).not.toContain("token=private");
  expect(container.textContent).toContain("重新读取视频");
});

test("playback failure can retry with a fresh download instead of the failed cached file", async () => {
  const steps: string[] = [];
  media.loadAuthenticatedMedia.mockImplementation(async () => { steps.push("download"); return { path: steps.length === 1 ? "/tmp/old.mp4" : "/tmp/fresh.mp4" }; });
  media.forgetAuthenticatedMedia.mockImplementation(() => steps.push("forget"));
  await render();
  await act(async () => container.querySelector("video")!.dispatchEvent(new Event("error")));
  expect(container.textContent).toContain("MEDIA_ERR_DECODE");
  const retry = Array.from(container.querySelectorAll("button")).find(button => button.textContent === "重新读取视频")!;
  await act(async () => retry.click());
  expect(steps).toEqual(["download", "forget", "download"]);
  expect(container.querySelector("video")?.getAttribute("src")).toBe("/tmp/fresh.mp4");
  expect(container.textContent).not.toContain("MEDIA_ERR_DECODE");
});
