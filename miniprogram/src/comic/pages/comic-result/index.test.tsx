import { beforeEach, describe, expect, test, vi } from "vitest";

const h = vi.hoisted(() => ({
  slots: [] as any[],
  cursor: 0,
  effects: [] as Array<() => void | (() => void)>,
  books: vi.fn(),
  pages: vi.fn(),
  images: vi.fn(),
  preview: vi.fn(),
  toast: vi.fn(),
  modal: vi.fn(),
  openSetting: vi.fn(),
  clipboard: vi.fn(),
  redirectTo: vi.fn(),
  createCollection: vi.fn(),
  downloadZip: vi.fn(),
  saveAlbum: vi.fn(),
  params: { batchId: "batch-1", bookId: "book-1" } as Record<string, string>,
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
vi.mock("@tarojs/taro", () => ({
  default: { previewImage: h.preview, showToast: h.toast, showModal: h.modal, openSetting: h.openSetting, setClipboardData: h.clipboard, redirectTo: h.redirectTo },
  useRouter: () => ({ params: h.params }),
}));
vi.mock("@tarojs/components", () => ({ Button: "Button", Image: "Image", ScrollView: "ScrollView", Text: "Text", View: "View" }));
vi.mock("../../../components/BrandHeader", () => ({ default: "Header" }));
vi.mock("../../../theme", () => ({ ThemedPage: "Page" }));
vi.mock("../../services/comicFlow", () => ({ loadComicBooks: h.books }));
vi.mock("../../services/comicResult", () => ({
  buildPreviewRequest: (paths: string[], index: number) => ({ urls: paths.slice(0, 9), current: paths[index] }),
  createComicCollection: h.createCollection,
  downloadComicCollectionZip: h.downloadZip,
  loadComicImagePaths: h.images,
  loadComicPages: h.pages,
  saveComicImageToAlbum: h.saveAlbum,
}));

import ComicResultPage from "./index";

function render() { h.cursor = 0; return ComicResultPage(); }
async function flush() { for (let index = 0; index < 40; index += 1) await Promise.resolve(); }
function runEffects() { for (const effect of h.effects.splice(0)) effect(); }
function textContent(node: any): string {
  if (Array.isArray(node)) return node.map(textContent).join("");
  if (node === null || node === undefined || typeof node === "boolean") return "";
  if (typeof node !== "object") return String(node);
  return textContent(node.props?.children);
}
function findAll(node: any, className: string, found: any[] = []): any[] {
  if (Array.isArray(node)) { node.forEach((child) => findAll(child, className, found)); return found; }
  if (!node || typeof node !== "object") return found;
  if (typeof node.props?.className === "string" && node.props.className.split(" ").includes(className)) found.push(node);
  findAll(node.props?.children, className, found);
  return found;
}
function findByText(node: any, label: string): any {
  if (Array.isArray(node)) return node.map((child) => findByText(child, label)).find(Boolean);
  if (!node || typeof node !== "object") return undefined;
  if (typeof node.props?.onClick === "function" && textContent(node.props?.children).includes(label)) return node;
  return findByText(node.props?.children, label);
}

const PAGES = [
  {
    ordinal: 1, pageId: "p1", videoId: "v1", status: "ready", aspectRatio: "9:16", comicPrompt: "x",
    images: [
      { id: "i1", url: "/a", isPrimary: true, mediaType: "image/png" },
      { id: "i2", url: "/b", isPrimary: false, mediaType: "image/png" },
    ],
    primaryImageId: "i1",
  },
  {
    ordinal: 2, pageId: "p2", videoId: "v2", status: "ready", aspectRatio: "9:16", comicPrompt: "y",
    images: [{ id: "i3", url: "/c", isPrimary: true, mediaType: "image/png" }],
    primaryImageId: "i3",
  },
];

beforeEach(() => {
  h.slots = [];
  h.effects = [];
  vi.resetAllMocks();
  h.params = { batchId: "batch-1", bookId: "book-1" };
  h.books.mockResolvedValue([{ id: "book-1", title: "第一本" }]);
  h.pages.mockResolvedValue(PAGES);
  h.images.mockResolvedValue({ i1: "/tmp/i1.png", i2: "/tmp/i2.png" });
  h.preview.mockResolvedValue({});
  h.toast.mockResolvedValue({});
  h.modal.mockResolvedValue({ confirm: false, cancel: false });
  h.openSetting.mockResolvedValue({});
  h.clipboard.mockResolvedValue({});
  h.redirectTo.mockResolvedValue({});
});

describe("漫画结果页", () => {
  test("一页一图 + 页码条，并标出当前采用的图", async () => {
    render();
    runEffects();
    await flush();
    const text = textContent(render());
    expect(text).toContain("共 2 页");
    expect(text).toContain("当前采用");
    expect(text).toContain("第 1 / 2 页");
    expect(findAll(render(), "comic-tile")).toHaveLength(2);
    expect(findAll(render(), "comic-candidate")).toHaveLength(2);
  });

  test("点击候选图按本地路径预览", async () => {
    render();
    runEffects();
    await flush();
    const candidates = findAll(render(), "comic-candidate");
    candidates[1].props.onClick();
    await flush();
    expect(h.preview).toHaveBeenCalledWith(expect.objectContaining({ urls: ["/tmp/i1.png", "/tmp/i2.png"], current: "/tmp/i2.png" }));
  });

  test("切页会更新页码与主图", async () => {
    render();
    runEffects();
    await flush();
    findAll(render(), "comic-tile")[1].props.onClick();
    await flush();
    expect(textContent(render())).toContain("第 2 / 2 页");
  });

  test("保存主图到相册，权限被拒时引导去设置", async () => {
    render();
    runEffects();
    await flush();
    h.saveAlbum.mockRejectedValue(new Error("未获得相册权限，请在设置中开启“保存到相册”后重试"));
    h.modal.mockResolvedValue({ confirm: true, cancel: false });
    findByText(render(), "保存这张图到相册").props.onClick();
    await flush();
    expect(h.saveAlbum).toHaveBeenCalledWith("/tmp/i1.png");
    expect(h.modal).toHaveBeenCalledWith(expect.objectContaining({ confirmText: "去设置" }));
    expect(h.openSetting).toHaveBeenCalled();
  });

  test("打包下载使用全部有主图的页", async () => {
    render();
    runEffects();
    await flush();
    h.createCollection.mockResolvedValue({ id: "c1", bookId: "book-1", status: "ready", items: [{ ordinal: 1, videoId: "v1", comicPageId: "p1", imageId: "i1", url: "/x" }, { ordinal: 2, videoId: "v2", comicPageId: "p2", imageId: "i3", url: "/y" }] });
    h.downloadZip.mockResolvedValue({ path: "/saved/comic-collection.zip", url: "https://example/api/zip" });
    findByText(render(), "打包下载全部").props.onClick();
    await flush();
    expect(h.createCollection).toHaveBeenCalledWith("batch-1", "book-1", ["v1", "v2"]);
    expect(h.downloadZip).toHaveBeenCalledWith("batch-1", "book-1", "c1");
    expect(h.modal).toHaveBeenCalledWith(expect.objectContaining({ title: "已打包下载" }));
  });

  test("没有入口参数时给出下一步动作", async () => {
    h.params = {};
    render();
    runEffects();
    await flush();
    const text = textContent(render());
    expect(text).toContain("还没有可看的漫画，先完成一次生成");
    expect(text).not.toContain("缺少批次或书目编号");
    findByText(render(), "去做漫画").props.onClick();
    await flush();
    expect(h.redirectTo).toHaveBeenCalledWith(expect.objectContaining({ url: "/comic/pages/comic-config/index" }));
  });
});
