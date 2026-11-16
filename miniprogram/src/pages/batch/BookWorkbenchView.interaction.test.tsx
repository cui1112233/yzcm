import { beforeEach, expect, test, vi } from "vitest";
const h = vi.hoisted(() => ({ slots: [] as any[], cursor: 0, modal: vi.fn(), clipboard: vi.fn() }));
vi.mock("react", () => ({
  useState: (initial: any) => { const i = h.cursor++; if (!(i in h.slots)) h.slots[i] = initial; return [h.slots[i], (value: any) => { h.slots[i] = typeof value === "function" ? value(h.slots[i]) : value; }]; },
  useRef: (initial: any) => { const i = h.cursor++; if (!(i in h.slots)) h.slots[i] = { current: initial }; return h.slots[i]; },
  useEffect: () => {},
}));
vi.mock("@tarojs/components", () => Object.fromEntries(["Button", "Input", "Text", "Textarea", "View", "Image", "Video", "ScrollView"].map(v => [v, v])));
vi.mock("@tarojs/taro", () => ({ default: { showModal: h.modal, setClipboardData: h.clipboard } }));
vi.mock("../../components/AppIcon", () => ({ default: "Icon" }));
vi.mock("../../services/mediaCache", () => ({ loadAuthenticatedMedia: vi.fn() }));
import BookWorkbenchView, { AssetRow } from "./BookWorkbenchView";
import { invalidateSessionResources } from "../../services/sessionBoundary";
import type { BatchFactoryBookWorkspace } from "../../services/batchFactoryV11";
const workspace: BatchFactoryBookWorkspace = { readState: "ready", batchId: "batch-1", bookId: "book-1", book: { id: "book-1", title: "小说", videos: [{ id: "v1", videoPrompt: "第一个镜头", revision: 3 }, { id: "v2", revision: 4 }], sourceMetadata: { websiteSubmitProgress: { status: "waiting", message: "已接收，等待后台列表回读" } } }, sourceText: "完整原文只在原文页显示", stages: { prompt: "其他分镜的总提示词" }, assets: [{ id: "asset-1", name: "林晚", prompt: "旧资产提示词", revision: 2 }], assetImages: {}, production: { tasks: [{ videoId: "v1", prompt: "第一个镜头", status: "succeeded", mediaUrl: "/first.mp4" }, { videoId: "v2", status: "failed", error: "模型拒绝" }] }, mergeStatus: { jobs: [{ bookId: "book-1", outputUrl: "/final.mp4", status: "succeeded" }] } };
const actions = { runDirector: vi.fn(), runProduction: vi.fn(), runMerge: vi.fn(), runSubmit: vi.fn(), saveAssetPrompt: vi.fn().mockResolvedValue(undefined), saveVideoPrompt: vi.fn().mockResolvedValue(undefined), saveContentWindow: vi.fn().mockResolvedValue(undefined) };
const render = (data = workspace) => { h.cursor = 0; return BookWorkbenchView({ workspace: data, capabilities: { "director.run": { available: true }, "production.submit": { available: true }, "merge.run": { available: true }, "publish.121": { available: true } }, actions }); };
const nodes = (tree: any): any[] => !tree || typeof tree !== "object" ? [] : typeof tree.type === "function" ? nodes(tree.type(tree.props)) : [tree, ...[tree.props?.children].flat(Infinity).flatMap(nodes)];
const text = (tree: any): string => typeof tree === "string" || typeof tree === "number" ? String(tree) : typeof tree?.type === "function" ? text(tree.type(tree.props)) : tree?.props ? [tree.props.children].flat(Infinity).map(text).join("") : "";
const click = (label: string) => { const node = nodes(render()).find(n => n.props?.onClick && text(n) === label); expect(node, label).toBeTruthy(); return node.props.onClick(); };
const input = (label: string, value: string) => { const node = nodes(render()).find(n => n.type === "Input" && n.props?.["aria-label"] === label); expect(node, label).toBeTruthy(); node.props.onInput({ detail: { value } }); };
beforeEach(() => { h.slots = []; h.cursor = 0; vi.clearAllMocks(); h.modal.mockResolvedValue({ confirm: false }); });
test("shot selection changes prompt and status without leaking another shot's prompt", () => {
  expect(text(render())).toContain("第一个镜头");
  const shot = nodes(render()).find(n => n.props?.onClick && text(n).includes("分镜 2") && text(n).includes("失败"));
  expect(shot).toBeTruthy(); shot.props.onClick();
  expect(text(render())).not.toContain("第一个镜头");
  expect(text(render())).not.toContain("其他分镜的总提示词");
  expect(text(render())).toContain("模型拒绝");
});
test("source and delivery have reachable independent views", () => {
  expect(text(render())).not.toContain("完整原文只在原文页显示");
  click("原文"); expect(text(render())).toContain("完整原文只在原文页显示");
  click("合成与提交"); expect(text(render())).toContain("提交到视频管理系统");
  expect(text(render())).toContain("合成成片 1");
  expect(text(render())).toContain("已接收，等待后台列表回读");
});
test("reading and asset views free the viewport without losing video access", () => {
  const hasPlayer = () => nodes(render()).some(n => String(n.props?.className || "").includes("mp-batch-video-preview"));
  expect(hasPlayer()).toBe(true);
  click("资产");
  expect(hasPlayer()).toBe(false);
  expect(text(render())).not.toContain("分镜 1可预览");
  click("原文");
  expect(hasPlayer()).toBe(false);
  const original = nodes(render()).find(n => n.props.className === "mp-batch-source-text");
  expect(original.props.style.fontSize).toBe("14px");
  click("分镜");
  expect(hasPlayer()).toBe(true);
  expect(actions.runProduction).not.toHaveBeenCalled();
});
test("switching content sections and viewing the merged video preserves the selected shot", () => {
  const second = nodes(render()).find(n => n.props?.onClick && text(n).includes("分镜 2") && text(n).includes("失败"));
  second.props.onClick();
  click("资产"); click("原文"); click("分镜");
  expect(text(render())).toContain("模型拒绝");
  expect(text(render())).not.toContain("第一个镜头");
  click("合成与提交"); click("分镜");
  expect(text(render())).toContain("模型拒绝");
});
test("director remains reachable in tools and opening the tools never generates anything", async () => {
  click("制作工具");
  expect(text(render())).toContain("生成本书分镜");
  expect(actions.runDirector).not.toHaveBeenCalled();
  h.modal.mockResolvedValueOnce({ confirm: true });
  await click("生成本书分镜");
  expect(actions.runDirector).toHaveBeenCalledOnce();
});
test("a book without playable video uses the compact pending state", () => {
  const tree = render({ ...workspace, production: null, mergeStatus: null });
  expect(nodes(tree).some(n => n.props.className === "mp-batch-media-pending")).toBe(true);
  expect(nodes(tree).some(n => n.props.className === "mp-batch-video-preview")).toBe(false);
});
test("book-wide generation is explicit and cancellation does not execute it", async () => {
  await click("生成本书视频"); expect(h.modal).toHaveBeenCalledOnce(); expect(actions.runProduction).not.toHaveBeenCalled();
  h.modal.mockResolvedValueOnce({ confirm: true }); await click("生成本书视频"); expect(actions.runProduction).toHaveBeenCalledOnce();
});
test("account switch during confirmation cancels the action", async () => {
  let resolve!: (v: any) => void; h.modal.mockReturnValueOnce(new Promise(r => { resolve = r; }));
  const pending = click("生成本书视频"); invalidateSessionResources(); resolve({ confirm: true }); await pending;
  expect(actions.runProduction).not.toHaveBeenCalled();
});
test("rapid generation clicks open only one confirmation", async () => {
  let resolve!: (v: any) => void; h.modal.mockReturnValueOnce(new Promise(r => { resolve = r; }));
  const first = click("生成本书视频"), second = click("生成本书视频");
  expect(h.modal).toHaveBeenCalledOnce(); resolve({ confirm: false }); await Promise.all([first, second]);
  expect(actions.runProduction).not.toHaveBeenCalled();
});
test("copy uses the current shot and reading controls only change original text", () => {
  click("复制提示词");
  expect(h.clipboard).toHaveBeenLastCalledWith({ data: "第一个镜头" });
  const second = nodes(render()).find(n => n.props?.onClick && text(n).includes("分镜 2") && text(n).includes("失败"));
  second.props.onClick();
  expect(nodes(render()).some(n => n.props?.onClick && text(n) === "复制提示词")).toBe(false);
  click("原文"); click("20号字");
  const original = nodes(render()).find(n => n.props.className === "mp-batch-source-text");
  expect(original.props.style.fontSize).toBe("20px");
  click("复制原文");
  expect(h.clipboard).toHaveBeenLastCalledWith({ data: workspace.sourceText });
  expect(actions.runProduction).not.toHaveBeenCalled();
});
test("current video prompt uses the common prompt edit actions and saves only the selected video", async () => {
  click("编辑提示词");
  const input = nodes(render()).find(n => n.type === "Textarea");
  expect(input).toBeTruthy();
  input.props.onInput({ detail: { value: "已编辑镜头提示词" } });
  await click("保存提示词");
  expect(actions.saveVideoPrompt).toHaveBeenCalledWith("v1", "已编辑镜头提示词", 3);
  expect(actions.saveAssetPrompt).not.toHaveBeenCalled();
});
test("asset prompt exposes direct editing without requiring a separate view action", async () => {
  const renderAsset = () => { h.cursor = 0; return AssetRow({ asset: workspace.assets[0], onSavePrompt: actions.saveAssetPrompt }); };
  const clickAsset = (label: string) => { const node = nodes(renderAsset()).find(n => n.props?.onClick && text(n) === label); expect(node, label).toBeTruthy(); return node.props.onClick(); };
  expect(text(renderAsset())).toContain("旧资产提示词");
  expect(nodes(renderAsset()).some(n => n.props?.onClick && text(n) === "查看资产提示词")).toBe(false);
  clickAsset("编辑提示词");
  const input = nodes(renderAsset()).find(n => n.type === "Textarea");
  expect(input).toBeTruthy();
  input.props.onInput({ detail: { value: "已编辑资产提示词" } });
  await clickAsset("保存提示词");
  expect(actions.saveAssetPrompt).toHaveBeenCalledWith(expect.objectContaining({ id: "asset-1" }), "已编辑资产提示词");
  expect(actions.saveVideoPrompt).not.toHaveBeenCalled();
});
test("canceling a prompt edit restores browse mode without saving", () => {
  click("编辑提示词");
  expect(nodes(render()).some(n => n.type === "Textarea")).toBe(true);
  click("取消");
  expect(nodes(render()).some(n => n.type === "Textarea")).toBe(false);
  expect(text(render())).toContain("第一个镜头");
  expect(actions.saveVideoPrompt).not.toHaveBeenCalled();
});
test("source tab previews logical lines and saves only the selected book window", async () => {
  click("原文");
  expect(text(render())).toContain("AI 使用内容");
  expect(text(render())).toContain("当前 AI 使用前 5 个非空行");
  input("内容范围", "8");
  input("获取上限", "8000");
  await click("保存范围");
  expect(actions.saveContentWindow).toHaveBeenCalledWith({ contentRangeLines: 8, contentCaptureCharacters: 8000 });
});
test("invalid source window stays local and never invokes save", () => {
  click("原文");
  input("内容范围", "0");
  expect(text(render())).toContain("内容范围需为 1 至 500 的整数");
  expect(actions.saveContentWindow).not.toHaveBeenCalled();
});
