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
vi.mock("../../services/memberCenter", () => ({ readMemberCenterView: h.read }));

import MemberCenterPage from "./index";

function render() { h.cursor = 0; return MemberCenterPage(); }
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

describe("会员中心页", () => {
  test("展示身份、团队、额度与 AI 服务状态", async () => {
    h.read.mockResolvedValue({
      facts: { displayName: "小明", username: "ming", role: "member", teamName: "北极星", apiEnabled: true, scopes: ["text"], monthlyTokenUsage: 50000, monthlyTokenLimit: 200000 },
      quota: { used: 50000, limit: 200000, percent: 25 },
    });
    render();
    h.effects.forEach((effect) => effect());
    await flush();
    const text = textContent(render());
    expect(text).toContain("小明");
    expect(text).toContain("@ming");
    expect(text).toContain("MEMBER");
    expect(text).toContain("北极星");
    expect(text).toContain("50,000 Token");
    expect(text).toContain("额度占比：25%");
    expect(text).toContain("已启用");
  });

  test("读取失败时展示错误并可重试", async () => {
    h.read.mockRejectedValueOnce(new Error("网络连接失败")).mockResolvedValueOnce({ facts: { username: "ming" }, quota: {} });
    render();
    h.effects.forEach((effect) => effect());
    await flush();
    expect(textContent(render())).toContain("网络连接失败");
    findByClass(render(), "mp-primary").props.onClick();
    await flush();
    expect(h.read).toHaveBeenCalledTimes(2);
    expect(textContent(render())).toContain("@ming");
  });
});
