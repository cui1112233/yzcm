import { beforeEach, expect, test, vi } from "vitest";
const request = vi.hoisted(() => vi.fn());
vi.mock("./api", () => ({ apiRequest: request }));
import { readPanelProject, savePanelProject, panelOutlineRequest, panelEntities, updatePanelEntity, panelImageUrl, selectedPanelProject, rememberPanelProject } from "./novelPanel";
import { invalidateSessionResources } from "./sessionBoundary";
beforeEach(() => request.mockReset());
test("小说面板使用自己的项目而不是剧本历史，保存保留全部未知字段并核对读回", async () => {
  const project = { id: "p1", name: "小说", data: { novel_text: "原文", characters: [], extension: { keep: true } } };
  request.mockResolvedValueOnce({ project }).mockResolvedValueOnce({ project }).mockResolvedValueOnce({ project });
  expect(await readPanelProject("p1")).toEqual(project);
  expect(await savePanelProject(project)).toEqual(project);
  expect(request.mock.calls.map(([path]) => path)).toEqual(["/api/novel-panel/projects/p1", "/api/novel-panel/projects", "/api/novel-panel/projects/p1"]);
  expect(request.mock.calls[1][1].data.data.extension).toEqual({ keep: true });
});
test("错项目或空保存回执不能显示成功", async () => {
  request.mockResolvedValueOnce({ project: { id: "wrong", name: "x", data: {} } });
  await expect(readPanelProject("p1")).rejects.toThrow(/项目/);
  request.mockResolvedValueOnce({ ok: true });
  await expect(savePanelProject({ id: "p1", name: "x", data: {} })).rejects.toThrow(/回执/);
});
test("编辑人物和分镜请求保留公网原字段和人物范围", () => {
  const data = { novel_text: "原文", style: { genre: "都市", camera: "手持" }, characters: [{ id: "c", name: "林晚", appearance: "黑衣", slot_id: "s", image_urls: ["/a"] }], outline_shots: [{ id: "shot", prompt: "镜头" }] };
  expect(panelEntities(data, "characters")[0].description).toContain("黑衣");
  expect(updatePanelEntity(data, "characters", 0, { appearance: "白衣" }).characters[0]).toMatchObject({ appearance: "白衣", slot_id: "s", image_urls: ["/a"] });
  expect(panelOutlineRequest(data)).toMatchObject({ novel_text: "原文", genre: "都市", camera: "手持", characters: data.characters });
});
test("参考资产使用公网版本化文件地址和旧媒体代理", () => {
  expect(panelImageUrl({ type: "character", asset_id: "a1", has_main_image: true, main_revision: "r2" })).toBe("/api/novel-panel/reference-assets/file/character/a1/main?v=r2");
  expect(panelImageUrl({ main_image_url: "https://legacy.tos-cn.volces.com/image.png" })).toContain("/api/novel-panel/reference-assets/legacy?url=");
});
test("最终分镜显示编辑后的提示词，页面重建记住所选项目但退出不复用", () => {
  expect(panelEntities({ final_segments: [{ text: "旧提示词", current_text: "修改后的提示词" }] }, "final_segments")[0].description).toBe("修改后的提示词");
  rememberPanelProject("p1");
  expect(selectedPanelProject()).toBe("p1");
  invalidateSessionResources();
  expect(selectedPanelProject()).toBe("");
});
