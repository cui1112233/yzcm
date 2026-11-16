import { beforeEach, describe, expect, test, vi } from "vitest";

const h = vi.hoisted(() => ({ navigateTo: vi.fn() }));

vi.mock("@tarojs/taro", () => ({ default: { navigateTo: h.navigateTo } }));
vi.mock("@tarojs/components", () => ({ Button: "Button", Text: "Text", View: "View" }));
vi.mock("../../../components/BrandHeader", () => ({ default: "Header" }));
vi.mock("../../../components/AppIcon", () => ({ default: "Icon" }));
vi.mock("../../../theme", () => ({ ThemedPage: "Page" }));

import ComicToolkitPage from "./index";

const nodes = (tree: any): any[] => (!tree || typeof tree !== "object" ? [] : [tree, ...[tree.props?.children].flat(Infinity).flatMap(nodes)]);
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

beforeEach(() => { vi.resetAllMocks(); h.navigateTo.mockResolvedValue({}); });

describe("会员与工具页", () => {
  test("只保留会员、用量与配音三个工具入口，不再承载漫画", () => {
    const tree = ComicToolkitPage();
    const header = nodes(tree).find((node) => node.type === "Header");
    expect(header?.props.title).toBe("会员与工具");
    expect(header?.props.eyebrow).toBe("账号与工具");

    const text = textContent(tree);
    ["会员中心", "用量统计", "独立配音"].forEach((label) => expect(text).toContain(label));
    ["做一本漫画", "开始做漫画", "漫画生成"].forEach((label) => expect(text).not.toContain(label));
    ["批次", "runMode", "presetId"].forEach((word) => expect(text).not.toContain(word));
  });

  test("点击会员中心进入会员页", () => {
    findByText(ComicToolkitPage(), "会员中心").props.onClick();
    expect(h.navigateTo).toHaveBeenCalledWith({ url: "/comic/pages/member-center/index" });
  });
});
