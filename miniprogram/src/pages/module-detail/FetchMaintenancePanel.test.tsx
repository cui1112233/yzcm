import { beforeEach, expect, test, vi } from "vitest";
const h = vi.hoisted(() => ({ slots: [] as any[], cursor: 0, effects: [] as any[], api: vi.fn() }));
vi.mock("react", () => ({ useState: (initial: any) => { const i = h.cursor++; if (!(i in h.slots)) h.slots[i] = typeof initial === "function" ? initial() : initial; return [h.slots[i], (v: any) => { h.slots[i] = typeof v === "function" ? v(h.slots[i]) : v; }]; }, useRef: (v: any) => { const i = h.cursor++; if (!(i in h.slots)) h.slots[i] = { current: v }; return h.slots[i]; }, useEffect: (fn: any) => { const i = h.cursor++; if (!(i in h.slots)) { h.slots[i] = true; h.effects.push(fn); } } }));
vi.mock("@tarojs/components", () => Object.fromEntries(["View", "Text", "Button", "Input", "Textarea", "Picker", "Switch"].map(v => [v, v])));
vi.mock("../../services/api", () => ({ apiRequest: h.api }));
import FetchMaintenancePanel from "./FetchMaintenancePanel";
const render = (panel = "config") => { h.cursor = 0; return FetchMaintenancePanel({ panel }); };
const nodes = (t: any): any[] => t && typeof t === "object" ? [t, ...[t.props?.children].flat(Infinity).flatMap(nodes)] : [];
const text = (t: any): string => typeof t === "string" ? t : t?.props ? [t.props.children].flat(Infinity).map(text).join("") : "";
const flush = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };
beforeEach(() => { h.slots = []; h.cursor = 0; h.effects = []; h.api.mockReset(); });
test("workflow save preserves unseen fields and reads persisted configuration", async () => {
  h.api.mockResolvedValue({ app_config: { workflow: { auto_fetch_original: true, extra: "keep" }, fetch: { retries: 0 } }, knowledge: {} });
  render(); h.effects.forEach(fn => fn()); await flush();
  nodes(render()).find(n => n.type === "Switch").props.onChange({ detail: { value: false } });
  await nodes(render()).find(n => n.type === "Button" && text(n) === "保存配置并读回").props.onClick();
  expect(h.api.mock.calls.find(([, o]) => o?.method === "POST")?.[1].data.app_config.workflow).toEqual({ auto_fetch_original: true, auto_classify_missing: false, extra: "keep" });
  expect(h.api.mock.calls[h.api.mock.calls.length - 1]?.[0]).toBe("/api/batch-rewrite/config");
});
test("website editor never shows or resends saved password, blank means preserve", async () => {
  h.api.mockResolvedValue({ app_config: {}, web_submit: { username: "u", password: "saved-secret", selected_profile: "p", upload_profiles: [] } });
  render("web"); h.effects.forEach(fn => fn()); await flush();
  expect(nodes(render("web")).filter(n => n.type === "Input").map(n => n.props.value)).not.toContain("saved-secret");
  await nodes(render("web")).find(n => n.type === "Button" && text(n) === "保存提交配置并读回").props.onClick();
  expect(h.api.mock.calls.find(([, o]) => o?.method === "POST")?.[1].data.settings).not.toHaveProperty("password");
});
