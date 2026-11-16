import { expect, it } from "vitest";
import { previewContentWindow, readContentWindow, validateContentWindow } from "./contentWindow";

it("uses safe defaults and only counts non-empty logical lines", () => {
  expect(readContentWindow({})).toEqual({ contentRangeLines: 5, contentCaptureCharacters: 4000 });
  expect(readContentWindow({ contentRangeLines: 8, contentCaptureCharacters: 8000 })).toEqual({ contentRangeLines: 8, contentCaptureCharacters: 8000 });
  expect(previewContentWindow("一\n\n 二 \n三\n四", 2)).toEqual({ availableLines: 4, selectedLines: ["一", "二"] });
});

it("rejects fractional or out-of-range window values", () => {
  expect(validateContentWindow({ contentRangeLines: "0", contentCaptureCharacters: "4000" })).toEqual({ ok: false, error: "内容范围需为 1 至 500 的整数" });
  expect(validateContentWindow({ contentRangeLines: "5", contentCaptureCharacters: "99" })).toEqual({ ok: false, error: "获取上限需为 100 至 100000 的整数" });
  expect(validateContentWindow({ contentRangeLines: "5.5", contentCaptureCharacters: "4000" })).toEqual({ ok: false, error: "内容范围需为 1 至 500 的整数" });
});
