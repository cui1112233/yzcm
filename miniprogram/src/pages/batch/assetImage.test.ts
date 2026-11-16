import { expect, test, vi } from "vitest";
import { readAssetImage } from "./bookMedia";

test("failed asset download stays an error and explicit retry can recover", async () => {
  let attempts = 0;
  const download = async () => { if (++attempts === 1) throw new Error("download denied"); return { path: "/local/image.png" }; };
  expect(await readAssetImage("/image.png", download)).toEqual({ path: "", error: "图片读取失败：download denied" });
  expect(await readAssetImage("/image.png", download)).toEqual({ path: "/local/image.png", error: "" });
});

test("absent asset image is distinct from failed download", async () => {
  const download = vi.fn();
  expect(await readAssetImage("", download)).toEqual({ path: "", error: "" });
  expect(download).not.toHaveBeenCalled();
});
