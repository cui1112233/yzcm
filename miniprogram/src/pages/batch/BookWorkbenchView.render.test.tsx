import { expect, test, vi } from "vitest";
import { renderToStaticMarkup } from "react-dom/server";
vi.mock("@tarojs/components", () => ({ View: "div", Text: ({ selectable, ...props }: any) => <span {...props} />, Button: "button", Image: "img", Video: "video", ScrollView: ({ children }: any) => <div>{children}</div> }));
vi.mock("@tarojs/taro", () => ({ default: {} }));
vi.mock("../../services/mediaCache", () => ({ loadAuthenticatedMedia: vi.fn() }));
import BookWorkbenchView, { AssetImagePreview } from "./BookWorkbenchView";
import type { BatchFactoryBookWorkspace } from "../../services/batchFactoryV11";

const empty: BatchFactoryBookWorkspace = { readState: "loading", batchId: "batch-1", bookId: "book-1", book: null, stages: null, assets: [], assetImages: {}, production: null, mergeStatus: null };
const actions = { runDirector: vi.fn(), runProduction: vi.fn(), runMerge: vi.fn() };
test("asset download error visibly exposes retry instead of empty image state", () => {
  const html = renderToStaticMarkup(<AssetImagePreview imagePath="" error="图片读取失败：download denied" loading={false} onRetry={() => {}} />);
  expect(html).toContain("download denied");
  expect(html).toContain("重新读取图片");
  expect(html).not.toContain("暂无可用图片");
});
test("section failures retain their own rendered labels", () => {
  const html = renderToStaticMarkup(<BookWorkbenchView workspace={{ ...empty, readState: "ready", book: { id: "book-1" }, sectionErrors: { stages: "stage offline", assets: "asset offline", "images:asset-1": "image offline", production: "production offline", merge: "merge offline" } }} capabilities={{}} actions={actions} />);
  for (const text of ["分镜读取失败", "资产读取失败", "资产图片读取失败", "生产读取失败", "合成读取失败"]) expect(html).toContain(text);
});
test("null book while loading and after errors renders without crashing", () => {
  expect(renderToStaticMarkup(<BookWorkbenchView workspace={empty} capabilities={{}} actions={actions} />)).toContain("正在读取当前小说");
  expect(renderToStaticMarkup(<BookWorkbenchView workspace={{ ...empty, readState: "error", error: "读取失败" }} capabilities={{}} actions={actions} />)).toContain("读取失败");
});
test("ready book focuses the selected shot and keeps other sections reachable", () => {
  const workspace: BatchFactoryBookWorkspace = { ...empty, readState: "ready", book: { id: "book-1" }, sourceText: "真实正文", production: { tasks: [{ mediaUrl: "/shot.mp4", prompt: "真实分镜提示词" }, { status: "failed" }] }, mergeStatus: { jobs: [{ bookId: "book-1", status: "completed", outputUrl: "/merged.mp4" }] } };
  const html = renderToStaticMarkup(<BookWorkbenchView workspace={workspace} capabilities={{}} actions={actions} />);
  for (const text of ["真实分镜提示词", "资产", "原文", "合成与提交", "分镜 2", "生成本书视频"]) expect(html).toContain(text);
  expect(html).not.toContain("真实正文");
  expect(html).not.toContain("合成成片 1");
});
