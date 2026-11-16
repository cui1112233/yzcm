import { expect, test, vi } from "vitest";
vi.mock("./api", () => ({ apiRequest: vi.fn() }));
import { apiRequest } from "./api";
import { entityImagePayload, entityImageUrls, generateEntityImage } from "./scriptEntityMedia";
test("entity media uses real reference asset contract and nested image aliases", async () => {
  expect(entityImageUrls({ data: { images: [{ url: "/a" }], mainImageUrl: "/b" } })).toEqual(["/a", "/b"]);
  const payload = entityImagePayload("characters", { id: "c1", data: { 角色名称: "小明", 外观描述: "黑衣" } }, "原文", "preset", "model");
  expect(payload).toMatchObject({ asset_type: "character", asset_id: "c1", context: "原文", imageModelId: "model", character: { name: "小明" } });
  vi.mocked(apiRequest).mockResolvedValueOnce({ url: "/image" });
  expect(await generateEntityImage(payload)).toBe("/image");
  expect(await generateEntityImage(payload)).toBe("/image");
  expect(apiRequest).toHaveBeenCalledTimes(1);
});
test("unknown image submission is never automatically repeated", async () => {
  vi.mocked(apiRequest).mockReset().mockRejectedValueOnce(new Error("timeout"));
  const payload = entityImagePayload("scenes", { id: "unknown", 场景名称: "大厅" }, "原文", "", "model");
  await expect(generateEntityImage(payload)).rejects.toThrow("timeout");
  await expect(generateEntityImage(payload)).rejects.toThrow(/阻止重复/);
  expect(apiRequest).toHaveBeenCalledTimes(1);
});
