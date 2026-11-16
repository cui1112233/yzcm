import { beforeEach, describe, expect, test, vi } from "vitest";

const h = vi.hoisted(() => ({
  slots: [] as any[],
  cursor: 0,
  effects: [] as Array<() => void | (() => void)>,
  shows: [] as Array<() => void>,
  read: vi.fn(),
}));

vi.mock("react", () => ({
  useState: (initial: any) => {
    const index = h.cursor++;
    if (!(index in h.slots)) h.slots[index] = typeof initial === "function" ? initial() : initial;
    return [h.slots[index], (next: any) => { h.slots[index] = typeof next === "function" ? next(h.slots[index]) : next; }];
  },
  useRef: (initial: any) => {
    const index = h.cursor++;
    if (!(index in h.slots)) h.slots[index] = { current: initial };
    return h.slots[index];
  },
  useEffect: (effect: any) => { h.effects.push(effect); },
}));
vi.mock("@tarojs/taro", () => ({ default: {}, useDidShow: (callback: () => void) => { h.shows.push(callback); } }));
vi.mock("@tarojs/components", () => ({ Button: "Button", Text: "Text", View: "View" }));
vi.mock("../../../components/BrandHeader", () => ({ default: "Header" }));
vi.mock("../../../theme", () => ({ ThemedPage: "Page" }));
vi.mock("../../services/usageStats", () => ({ readUsageStats: h.read }));

import UsageStatsPage from "./index";

function render() { h.cursor = 0; return UsageStatsPage(); }
async function flush() { for (let index = 0; index < 12; index += 1) await Promise.resolve(); }
function textContent(node: any): string {
  if (Array.isArray(node)) return node.map(textContent).join("");
  if (node === null || node === undefined || typeof node === "boolean") return "";
  if (typeof node !== "object") return String(node);
  return textContent(node.props?.children);
}
function findByClass(node: any, className: string): any {
  if (Array.isArray(node)) return node.map((child) => findByClass(child, className)).find(Boolean);
  if (!node || typeof node !== "object") return undefined;
  if (node.props?.className?.split(" ").includes(className)) return node;
  return findByClass(node.props?.children, className);
}

beforeEach(() => {
  h.slots = []; h.effects = []; h.shows = [];
  vi.resetAllMocks();
});

describe("用量统计页", () => {
  test("展示今日与本月数字和最近调用", async () => {
    h.read.mockResolvedValue({
      day: { totalTokens: 1200, calls: 3 },
      month: { totalTokens: 50000, calls: 40, outputTokens: 8000, estimatedCost: 1.5, costCurrency: "CNY" },
      recent: [{ id: "a", feature: "script", model: "text-large", totalTokens: 300, createdAt: "2026-11-04" }],
    });
    render();
    h.effects.forEach((effect) => effect());
    await flush();
    const text = textContent(render());
    expect(text).toContain("1,200 Token");
    expect(text).toContain("3 次");
    expect(text).toContain("50,000 Token");
    expect(text).toContain("8,000 Token");
    expect(text).toContain("1.5 CNY");
    expect(text).toContain("script · text-large");
    expect(text).toContain("2026-11-04");
  });

  test("最近调用为空时不伪造记录", async () => {
    h.read.mockResolvedValue({ day: {}, month: {}, recent: [] });
    render();
    h.effects.forEach((effect) => effect());
    await flush();
    expect(textContent(render())).toContain("暂无最近调用");
    expect(textContent(render())).toContain("服务端未返回");
  });

  test("读取失败展示错误并允许重试", async () => {
    h.read.mockRejectedValueOnce(new Error("服务端未返回用量数据")).mockResolvedValueOnce({ day: {}, month: {}, recent: [] });
    render();
    h.effects.forEach((effect) => effect());
    await flush();
    expect(textContent(render())).toContain("服务端未返回用量数据");
    findByClass(render(), "mp-primary").props.onClick();
    await flush();
    expect(h.read).toHaveBeenCalledTimes(2);
  });
});
