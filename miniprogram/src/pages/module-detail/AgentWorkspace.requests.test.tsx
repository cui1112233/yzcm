import { beforeEach, expect, test, vi } from "vitest";
const h = vi.hoisted(() => ({ slots: [] as any[], cursor: 0, show: () => {}, api: vi.fn(), modal: vi.fn() }));
vi.mock("react", () => ({ useState: (v: any) => { const i = h.cursor++; if (!(i in h.slots)) h.slots[i] = typeof v === "function" ? v() : v; return [h.slots[i], (next: any) => { h.slots[i] = typeof next === "function" ? next(h.slots[i]) : next; }]; }, useRef: (v: any) => { const i = h.cursor++; if (!(i in h.slots)) h.slots[i] = { current: v }; return h.slots[i]; }, useEffect: () => {} }));
vi.mock("@tarojs/taro", () => ({ default: { showModal: h.modal }, useDidShow: (fn: any) => { h.show = fn; } }));
vi.mock("@tarojs/components", () => Object.fromEntries(["Button", "Input", "Text", "Textarea", "Picker", "Image", "Video", "View"].map(v => [v, v])));
vi.mock("../../services/api", () => ({ apiRequest: h.api }));
vi.mock("../../services/mediaCache", () => ({ loadAuthenticatedMedia: vi.fn() }));
vi.mock("../../theme", () => ({ withThemedPage: (p: any) => p }));
vi.mock("../../components/AppIcon", () => ({ default: "Icon" }));
vi.mock("../../components/BrandHeader", () => ({ default: "Header" }));
vi.mock("../../components/AmbientStarfield", () => ({ default: "Stars" }));
import AgentWorkspace from "./AgentWorkspace";
import { invalidateSessionResources } from "../../services/sessionBoundary";
const render = () => { h.cursor = 0; return AgentWorkspace({}); };
const nodes = (t: any): any[] => t && typeof t === "object" ? [t, ...[t.props?.children].flat(Infinity).flatMap(nodes)] : [];
const text = (t: any): string => typeof t === "string" ? t : t?.props ? [t.props.children].flat(Infinity).map(text).join("") : "";
const click = (label: string) => { const n = nodes(render()).find(n => n.type === "Button" && text(n) === label); expect(n, label).toBeTruthy(); return n.props.onClick(); };
const flush = async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); };
beforeEach(() => {
  invalidateSessionResources();
  h.slots = []; h.cursor = 0; h.api.mockReset(); h.modal.mockResolvedValue({ confirm: true });
  h.api.mockImplementation((path: string) => Promise.resolve(path.endsWith("/projects") ? { projects: [{ id: "project-1", name: "已有项目" }] } : path.endsWith("/project-1") ? { id: "project-1", name: "已有项目", revision: 2, document: { nodes: [], chats: [] } } : path.endsWith("/models") ? { generation_enabled: true, models: [{ id: 1, kind: "text", name: "文本" }, { id: 2, kind: "image", name: "图像" }] } : path.endsWith("/runs") ? { runs: [] } : path.endsWith("/assets") ? { assets: [] } : path.endsWith("/skills") ? { skills: [] } : { brand_kits: [] }));
});
test("Agent reads Studio projects and explicit node save preserves server revision", async () => {
  render(); h.show(); await flush(); await click("打开已有项目"); await flush();
  await click("添加文本节点"); await click("保存项目并读回");
  const write = h.api.mock.calls.find(([, o]) => o?.method === "PUT");
  expect(write?.[0]).toBe("/api/studio/agent/projects/project-1");
  expect(write?.[1].data.revision).toBe(2); expect(write?.[1].data.document.nodes[0].type).toBe("text");
  expect(h.api.mock.calls.every(([p]) => !p.startsWith("/api/agent/"))).toBe(true);
});
test("AI plan actions are displayed for review and do not automatically invoke media", async () => {
  render(); h.show(); await flush(); await click("打开已有项目"); await flush();
  nodes(render()).find(n => n.type === "Textarea" && n.props.placeholder === "输入创作要求").props.onInput({ detail: { value: "生成一张海报" } });
  h.api.mockImplementation((p: string) => Promise.resolve(p.endsWith("/plan") ? { reply: "待确认方案", actions: [{ tool: "generate_image" }] } : {}));
  await click("生成规划（不执行媒体）");
  expect(text(render())).toContain("待确认方案");
  expect(h.api.mock.calls.some(([p, o]) => p.endsWith("/runs") && o?.method === "POST")).toBe(false);
});
test("a media timeout never automatically replays or unlocks from an unrelated list refresh", async () => {
  render(); h.show(); await flush(); await click("打开已有项目"); await flush();
  nodes(render()).find(n => n.type === "Textarea" && n.props.placeholder === "输入创作要求").props.onInput({ detail: { value: "海报" } });
  const normal = h.api.getMockImplementation()!;
  h.api.mockImplementation((p: string, o: any) => p.endsWith("/runs") && o?.method === "POST" ? Promise.reject(new Error("timeout")) : normal(p, o));
  await click("确认生成图片"); await flush();
  await click("刷新 Studio"); await flush(); await click("确认生成图片");
  expect(h.api.mock.calls.filter(([p, o]) => p.endsWith("/runs") && o?.method === "POST")).toHaveLength(1);
  expect(text(render())).toContain("结果未知");
});
test("a delayed project response cannot replace the newer selected project", async () => {
  render(); h.show(); await flush();
  let resolve!: (v: any) => void;
  h.api.mockReturnValueOnce(new Promise(r => { resolve = r; }));
  const pending = click("打开已有项目");
  h.api.mockResolvedValueOnce({ id: "project-1", name: "新版项目", revision: 3, document: { nodes: [] } });
  await click("打开已有项目");
  resolve({ id: "project-1", name: "晚到旧版", revision: 2, document: { nodes: [] } }); await pending;
  expect(text(render())).toContain("新版项目"); expect(text(render())).not.toContain("晚到旧版");
});
test("changing account while a media confirmation is open cancels the old paid action", async () => {
  render(); h.show(); await flush(); await click("打开已有项目"); await flush();
  nodes(render()).find(n => n.type === "Textarea" && n.props.placeholder === "输入创作要求").props.onInput({ detail: { value: "账号A的创作要求" } });
  let confirm!: (v: any) => void; h.modal.mockReturnValueOnce(new Promise(r => { confirm = r; }));
  const pending = click("确认生成图片");
  invalidateSessionResources(); confirm({ confirm: true }); await pending;
  expect(h.api.mock.calls.some(([p, o]) => p.endsWith("/runs") && o?.method === "POST")).toBe(false);
});
test("media interaction locks before modal so rapid clicks cannot queue paid confirmations", async () => {
  render(); h.show(); await flush(); await click("打开已有项目"); await flush();
  let confirm!: (v: any) => void; h.modal.mockReturnValue(new Promise(r => { confirm = r; })); h.modal.mockClear();
  const one = click("确认生成图片"), two = click("确认生成图片");
  expect(h.modal).toHaveBeenCalledTimes(1); confirm({ confirm: false }); await Promise.all([one, two]);
});
test("local missing-model validation does not lock later project saving as an unknown submission", async () => {
  const normal = h.api.getMockImplementation()!;
  h.api.mockImplementation((p: string, o: any) => p.endsWith("/models") ? Promise.resolve({ generation_enabled: true, models: [] }) : normal(p, o));
  render(); h.show(); await flush(); await click("打开已有项目"); await flush();
  nodes(render()).find(n => n.type === "Textarea" && n.props.placeholder === "输入创作要求").props.onInput({ detail: { value: "要求" } });
  await click("生成规划（不执行媒体）");
  expect(text(render())).toContain("选择可用的文本模型");
  await click("保存项目并读回");
  expect(h.api.mock.calls.some(([, o]) => o?.method === "PUT")).toBe(true);
});
