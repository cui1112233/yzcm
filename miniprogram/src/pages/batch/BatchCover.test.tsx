// @vitest-environment jsdom
import { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, beforeEach, expect, test, vi } from "vitest";
const media = vi.hoisted(() => ({ loadAuthenticatedMedia: vi.fn(), forgetAuthenticatedMedia: vi.fn() }));
const recovery = vi.hoisted(() => ({ recoverBatchCoverVideo: vi.fn(), observe: undefined as any }));
vi.mock("../../services/mediaCache", () => media);
vi.mock("../../services/batchCoverMedia", () => recovery);
vi.mock("@tarojs/taro", () => ({ default: { getCurrentInstance: () => ({ page: {} }), createIntersectionObserver: () => ({ relativeToViewport() { return this; }, observe(_id: string, cb: any) { recovery.observe = cb; }, disconnect() {} }) } }));
vi.mock("@tarojs/components", () => ({ View: 'div', Button: 'button', Video: ({ src }: any) => <video src={src} />, Image: ({ src, onError, onClick }: any) => <img src={src} onError={onError} onClick={onClick} /> }));
import BatchCover from "./BatchCover";
let container: HTMLDivElement, root: Root;
beforeEach(() => { vi.clearAllMocks(); vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true); container = document.createElement("div"); root = createRoot(container); });
afterEach(async () => { await act(async () => root.unmount()); vi.unstubAllGlobals(); });
const render = async (url: string) => { await act(async () => root.render(<BatchCover url={url} />)); };
test("loads a private merged cover through the authenticated media client", async () => {
  media.loadAuthenticatedMedia.mockResolvedValue({ path: "/tmp/cover.jpg" });
  await act(async () => root.render(<BatchCover batchId="b" url="/api/batch-factory/v11/batches/b/merge-cover/m" />));
  await act(async () => recovery.observe({ intersectionRatio: 1 }));
  expect(media.loadAuthenticatedMedia).toHaveBeenCalledWith("/api/batch-factory/v11/batches/b/merge-cover/m", "jpg");
  expect(recovery.recoverBatchCoverVideo).not.toHaveBeenCalled();
  expect(container.querySelector("img")?.getAttribute("src")).toBe("/tmp/cover.jpg");
});
test("late cover responses cannot replace a newly selected work", async () => {
  let finish!: (value: any) => void;
  media.loadAuthenticatedMedia.mockImplementation((url: string) => url === "/old" ? new Promise(resolve => { finish = resolve; }) : Promise.resolve({ path: "/tmp/new.png" }));
  await render("/old"); await render("/new");
  await act(async () => finish({ path: "/tmp/old.png" }));
  expect(container.querySelector("img")?.getAttribute("src")).toBe("/tmp/new.png");
});
test("failed covers can retry without navigating into the work", async () => {
  media.loadAuthenticatedMedia.mockRejectedValueOnce(new Error("not available")).mockResolvedValueOnce({ path: "/tmp/retry.png" });
  await render("/cover");
  expect(container.querySelector("img")?.getAttribute("src")).not.toBe("/cover");
  await act(async () => container.querySelector("img")!.click());
  expect(media.forgetAuthenticatedMedia).toHaveBeenCalledWith("/cover");
  expect(container.querySelector("img")?.getAttribute("src")).toBe("/tmp/retry.png");
});
test('does not load offscreen covers; a visible broken image downloads its recovered video before previewing it', async () => {
  media.loadAuthenticatedMedia.mockRejectedValueOnce(new Error('404')).mockResolvedValueOnce({ path: '/tmp/ready.mp4' });
  recovery.recoverBatchCoverVideo.mockResolvedValue('https://cdn.example/ready.mp4');
  await act(async () => root.render(<BatchCover url="/missing.jpg" batchId="b" />));
  expect(media.loadAuthenticatedMedia).not.toHaveBeenCalled();
  await act(async () => recovery.observe({ intersectionRatio: 1 }));
  expect(container.querySelector('video')).toBeNull();
  expect(container.textContent).toContain('预览');
  await act(async () => container.querySelector('button')!.click());
  expect(media.loadAuthenticatedMedia).toHaveBeenLastCalledWith('https://cdn.example/ready.mp4', 'mp4');
  expect(container.querySelector('video')?.getAttribute('src')).toBe('/tmp/ready.mp4');
});
