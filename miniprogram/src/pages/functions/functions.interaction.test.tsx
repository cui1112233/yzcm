import { beforeEach, expect, test, vi } from "vitest";

const h = vi.hoisted(() => ({ value: "fetch", reduced: false, slots: [] as any[], cursor: 0, show: null as any, navigate: vi.fn(), save: vi.fn() }));
vi.mock("react", () => ({ useState: (initial: any) => {
  const i = h.cursor++;
  if (!(i in h.slots)) h.slots[i] = typeof initial === "function" ? initial() : initial;
  return [h.slots[i], (value: any) => { h.slots[i] = value; }];
} }));
vi.mock("@tarojs/taro", () => ({ default: { getStorageSync: (key: string) => key === "yzcm.mobile.glass.reduceTransparency" ? h.reduced : h.value, setStorageSync: h.save, navigateTo: h.navigate }, useDidShow: (fn: any) => { h.show = fn; } }));
vi.mock("@tarojs/components", () => ({ Button: "Button", Image: "Image", Switch: "Switch", Text: "Text", View: "View" }));
vi.mock("../../theme", () => ({ ThemedPage: "ThemedPage", applyMobileTheme: vi.fn(), readMobileTheme: () => "light" }));
vi.mock("../../components/BrandHeader", () => ({ default: () => null }));
vi.mock("../../components/AppIcon", () => ({ default: () => null }));
vi.mock("./NativeGlassMaterial", () => ({ default: () => null }));
import FunctionsPage from "./index";
import { MODULES, moduleRoute } from "../../config/modules";

const nodes = (tree: any): any[] => !tree || typeof tree !== "object" ? [] : [tree, ...[tree.props?.children].flat(Infinity).flatMap(nodes)];
const render = () => { h.cursor = 0; return nodes(FunctionsPage()); };
beforeEach(() => { h.slots = []; h.value = "fetch"; h.reduced = false; h.navigate.mockReset(); h.save.mockReset(); });

test("each grouped entry opens its actual module and remembers the choice", () => {
  expect(render().filter(n => n.type === "Button")).toHaveLength(8);
  for (const item of MODULES) {
    render().find(n => n.type === "Button" && n.props.className.split(" ").includes(`tools-entry-${item.key}`)).props.onClick();
    expect(h.navigate).toHaveBeenLastCalledWith({ url: moduleRoute(item.key) });
    expect(h.save).toHaveBeenLastCalledWith("yzcm.mobile.activeModule", item.key);
  }
});

test("returning to the page refreshes the last-used marker from other workspaces", () => {
  render(); h.value = "batch"; h.show();
  const selected = render().filter(n => n.type === "Button" && n.props.className.includes("is-recent"));
  expect(selected).toHaveLength(1);
  expect(selected[0].props.className).toContain("tools-entry-batch");
});

test("function entry keeps its routes without an appearance preference control", () => {
  expect(render().find(n => n.type === "Switch")).toBeUndefined();
  expect(render().find(n => n.props?.className?.includes("is-recent")).props.className).toContain("tools-entry-fetch");
  expect(h.navigate).not.toHaveBeenCalled();
});
