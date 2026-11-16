import { beforeEach, expect, test, vi } from "vitest";
const h = vi.hoisted(() => ({ slots: [] as any[], cursor: 0, effects: [] as any[], hide: () => {}, login: vi.fn(), read: vi.fn(), navigate: vi.fn(), save: vi.fn(), storage: new Map<string, unknown>(), has: false, prefs: { username: "", remember: false, automatic: false } }));
vi.mock("react", () => ({
  useState: (initial: any) => { const i = h.cursor++; if (!(i in h.slots)) h.slots[i] = typeof initial === "function" ? initial() : initial; return [h.slots[i], (next: any) => { h.slots[i] = typeof next === "function" ? next(h.slots[i]) : next; }]; },
  useRef: (initial: any) => { const i = h.cursor++; if (!(i in h.slots)) h.slots[i] = { current: initial }; return h.slots[i]; },
  useEffect: (effect: any) => { h.effects.push(effect); },
}));
vi.mock("@tarojs/taro", () => ({ default: { switchTab: h.navigate, getStorageSync: (key: string) => h.storage.get(key) }, useDidShow: () => {}, useDidHide: (callback: any) => { h.hide = callback; } }));
vi.mock("../../components/BrandHeader", () => ({ default: "Header" }));
vi.mock("../../components/LoginScene", () => ({ default: "Scene" }));
vi.mock("../../theme", () => ({ ThemedPage: "Page" }));
vi.mock("../../services/session", () => ({ loginAccount: h.login, readAccountSession: h.read, hasAccountSession: () => h.has }));
vi.mock("../../services/loginPreferences", () => ({ readLoginPreferences: () => h.prefs, saveLoginPreferences: h.save }));
import BindPage from "./index";
function render() { h.cursor = 0; h.effects = []; return (BindPage().props.children as any[])[1].props; }
async function flush() { for (let i = 0; i < 12; i++) await Promise.resolve(); }
beforeEach(() => { h.slots = []; h.storage.clear(); h.has = false; h.prefs = { username: "", remember: false, automatic: false }; vi.clearAllMocks(); h.login.mockResolvedValue({ account: { username: "demo" } }); h.navigate.mockResolvedValue({}); });
test("explicit login carries persistence choice and prevents duplicate submission", async () => {
  let done!: () => void; h.login.mockReturnValue(new Promise<void>(resolve => { done = resolve; }));
  render().onUsername("demo"); render().onPassword("example"); render().onRemember();
  const p = render(); p.onSubmit(); p.onSubmit();
  expect(h.login).toHaveBeenCalledTimes(1);
  expect(h.login).toHaveBeenCalledWith("demo", "example", "", { remember: true });
  expect(h.navigate).not.toHaveBeenCalled(); done(); await flush();
  expect(h.navigate).toHaveBeenCalledWith({ url: "/pages/workspace/index" });
  expect(render().password).toBe("");
});
test("invalid credentials show error without navigation or saved preferences", async () => {
  h.login.mockRejectedValue(new Error("密码错误")); render().onSubmit(); await flush();
  expect(render().error).toBe("密码错误"); expect(h.navigate).not.toHaveBeenCalled(); expect(h.save).not.toHaveBeenCalled();
});
test("automatic login verifies existing session instead of submitting a password", async () => {
  h.has = true; h.prefs = { username: "demo", remember: true, automatic: true };
  h.read.mockResolvedValue({ account: { username: "demo" } }); render(); h.effects.forEach(effect => effect()); await flush();
  expect(h.read).toHaveBeenCalledTimes(1); expect(h.login).not.toHaveBeenCalled(); expect(h.navigate).toHaveBeenCalledTimes(1);
});
test("leaving the page suppresses navigation from a late login response", async () => {
  let done!: () => void; h.login.mockReturnValue(new Promise<void>(resolve => { done = resolve; }));
  render().onSubmit(); h.hide(); done(); await flush(); expect(h.navigate).not.toHaveBeenCalled();
});
test("automatic requires remembered login and disabling remember clears automatic", () => {
  render().onAutomatic(); expect(render().remember).toBe(true); expect(render().automatic).toBe(true);
  render().onRemember(); expect(render().remember).toBe(false); expect(render().automatic).toBe(false);
});

test("legacy reduce-motion storage cannot silently remove the branded login entrance", () => {
  h.storage.set("yzcm.mobile.reduceMotion", true);
  expect(render().reduced).toBe(false);
});
