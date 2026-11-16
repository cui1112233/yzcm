import { beforeEach, describe, expect, test, vi } from "vitest";

const h = vi.hoisted(() => ({
  slots: [] as any[],
  cursor: 0,
  effects: [] as Array<() => void | (() => void)>,
  read: vi.fn(),
  hasSession: false,
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
vi.mock("@tarojs/components", () => ({ Button: "Button", Text: "Text", View: "View" }));
vi.mock("../services/executors", () => ({ readExecutorSummary: h.read }));
vi.mock("../services/session", () => ({ hasAccountSession: () => h.hasSession }));

import ExecutorStatusCard from "./ExecutorStatusCard";

function render() { h.cursor = 0; return ExecutorStatusCard(); }
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
  h.slots = []; h.effects = [];
  vi.resetAllMocks();
});

describe("执行器状态卡片", () => {
  test("无登录会话时保持未读取，不发请求", () => {
    render();
    h.effects.forEach((effect) => effect());
    expect(h.read).not.toHaveBeenCalled();
    expect(textContent(render())).toContain("服务端未返回");
  });

  test("展示在线与配对数量", async () => {
    h.hasSession = true;
    h.read.mockResolvedValue({ paired: 2, onlineCount: 1 });
    render();
    h.effects.forEach((effect) => effect());
    await flush();
    expect(textContent(render())).toContain("在线");
    expect(textContent(render())).toContain("在线 1 台 / 共 2 台");
  });

  test("全部离线与未配对状态区分展示", async () => {
    h.hasSession = true;
    h.read.mockResolvedValue({ paired: 1, onlineCount: 0 });
    render();
    h.effects.forEach((effect) => effect());
    await flush();
    expect(textContent(render())).toContain("离线");
  });

  test("读取失败展示错误并允许刷新", async () => {
    h.hasSession = true;
    h.read.mockRejectedValueOnce(new Error("执行器状态读取失败")).mockResolvedValueOnce({ paired: 1, onlineCount: 1 });
    render();
    h.effects.forEach((effect) => effect());
    await flush();
    expect(textContent(render())).toContain("执行器状态读取失败");
    findByClass(render(), "mp-secondary").props.onClick();
    await flush();
    expect(h.read).toHaveBeenCalledTimes(2);
    expect(textContent(render())).toContain("在线 1 台 / 共 1 台");
  });
});
