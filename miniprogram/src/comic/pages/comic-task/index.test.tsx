import { beforeEach, describe, expect, test, vi } from "vitest";

const h = vi.hoisted(() => ({
  slots: [] as any[],
  cursor: 0,
  effects: [] as Array<() => void | (() => void)>,
  books: vi.fn(),
  read: vi.fn(),
  navigateTo: vi.fn(),
  navigateBack: vi.fn(),
  poll: null as any,
  params: {} as Record<string, string>,
}));

vi.mock("react", () => ({
  useState: (initial: any) => {
    const index = h.cursor++;
    if (!(index in h.slots)) h.slots[index] = typeof initial === "function" ? initial() : initial;
    return [h.slots[index], (next: any) => { h.slots[index] = typeof next === "function" ? next(h.slots[index]) : next; }];
  },
  useEffect: (effect: any) => { h.effects.push(effect); },
}));
vi.mock("@tarojs/taro", () => ({ default: { navigateTo: h.navigateTo, navigateBack: h.navigateBack }, useRouter: () => ({ params: h.params }) }));
vi.mock("@tarojs/components", () => ({ Button: "Button", Text: "Text", View: "View" }));
vi.mock("../../../components/BrandHeader", () => ({ default: "Header" }));
vi.mock("../../../theme", () => ({ ThemedPage: "Page" }));
vi.mock("../../services/comicFlow", () => ({
  loadComicBooks: h.books,
  readComicAutomation: h.read,
  comicProgress: (value: any) => {
    const counts = value?.counts || {};
    return {
      total: Number(counts.total) || 0,
      succeeded: (Number(counts.succeeded) || 0) + (Number(counts.completed) || 0),
      running: (Number(counts.running) || 0) + (Number(counts.queued) || 0),
      failed: Number(counts.failed) || 0,
      finished: ["completed", "failed", "cancelled", "canceled", "partial"].includes(String(value?.state || "")),
    };
  },
}));
vi.mock("./useComicPolling", () => ({
  useComicPolling: (enabled: boolean, batchId: string, read: any, publish: any, fail: any) => {
    h.poll = { enabled, batchId, read, publish, fail };
  },
}));

import ComicTaskPage from "./index";

function render() { h.cursor = 0; return ComicTaskPage(); }
async function flush() { for (let index = 0; index < 30; index += 1) await Promise.resolve(); }
function runEffects() { for (const effect of h.effects.splice(0)) effect(); }
function textContent(node: any): string {
  if (Array.isArray(node)) return node.map(textContent).join("");
  if (node === null || node === undefined || typeof node === "boolean") return "";
  if (typeof node !== "object") return String(node);
  return textContent(node.props?.children);
}
function findByText(node: any, label: string): any {
  if (Array.isArray(node)) return node.map((child) => findByText(child, label)).find(Boolean);
  if (!node || typeof node !== "object") return undefined;
  if (typeof node.props?.onClick === "function" && textContent(node.props?.children).includes(label)) return node;
  return findByText(node.props?.children, label);
}

beforeEach(() => {
  h.slots = [];
  h.effects = [];
  h.poll = null;
  vi.resetAllMocks();
  h.params = { batchId: "batch-1", bookIds: "book-1,book-2" };
  h.books.mockResolvedValue([{ id: "book-1", title: "第一本" }, { id: "book-2", title: "第二本" }]);
  h.read.mockResolvedValue({ state: "running", counts: { total: 2, running: 1, succeeded: 0, failed: 0 }, books: [{ bookId: "book-1", status: "running", message: "" }] });
  h.navigateTo.mockResolvedValue({});
  h.navigateBack.mockResolvedValue({});
});

describe("漫画任务页", () => {
  test("首次读取前只说正在准备，不出现内部字段名", async () => {
    render();
    runEffects();
    await flush();
    const text = textContent(render());
    expect(text).toContain("正在准备…");
    ["批次", "未提供", "未返回", "服务端", "运行模式", "状态"].forEach((word) => {
      expect(text).not.toContain(word);
    });
    expect(h.poll.batchId).toBe("batch-1");
  });

  test("用书名说进度，不再显示编号", async () => {
    render();
    runEffects();
    await flush();
    h.poll.publish({ state: "running", counts: { total: 2, running: 1, succeeded: 1, failed: 0 }, books: [
      { bookId: "book-1", status: "succeeded" },
      { bookId: "book-2", status: "running" },
    ] });
    await flush();
    const text = textContent(render());
    expect(text).toContain("2 本已完成");
    expect(text).toContain("正在生成第二本");
    expect(text).toContain("每本书的进度");
    expect(text).not.toContain("book-2");
  });

  test("完成后主按钮进入结果页", async () => {
    render();
    runEffects();
    await flush();
    h.poll.publish({ state: "completed", counts: { total: 2, succeeded: 2, failed: 0 }, books: [
      { bookId: "book-1", status: "succeeded" },
      { bookId: "book-2", status: "succeeded" },
    ] });
    await flush();
    findByText(render(), "看漫画").props.onClick();
    await flush();
    expect(h.navigateTo).toHaveBeenCalledWith(expect.objectContaining({
      url: expect.stringContaining("/comic/pages/comic-result/index?batchId=batch-1&bookId=book-1"),
    }));
  });

  test("失败时说清哪一本、为什么、怎么办", async () => {
    render();
    runEffects();
    await flush();
    h.poll.publish({ state: "failed", counts: { total: 2, succeeded: 1, failed: 1 }, books: [
      { bookId: "book-1", status: "succeeded" },
      { bookId: "book-2", status: "failed", error: "提示词为空" },
    ] });
    await flush();
    const text = textContent(render());
    expect(text).toContain("第二本");
    expect(text).toContain("提示词为空");
    expect(text).toContain("可以回到上一步重新生成");
  });
});
