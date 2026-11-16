import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

describe("Taro webpack configuration", () => {
  it("disables prebundle for the WeChat development build", () => {
    const source = readFileSync(fileURLToPath(new URL("./index.ts", import.meta.url)), "utf8");

    expect(source).toContain("prebundle: { enable: false }");
  });

  it("writes the WeChat project output to dist", () => {
    const source = readFileSync(fileURLToPath(new URL("./index.ts", import.meta.url)), "utf8");

    expect(source).toContain('outputRoot: "dist"');
  });

  it("emits local image assets as files for WeChat image components", () => {
    const source = readFileSync(fileURLToPath(new URL("./index.ts", import.meta.url)), "utf8");

    expect(source).toContain("imageUrlLoaderOption: { limit: true }");
  });

  it("injects the immutable build SHA and dirty state", () => {
    const source = readFileSync(fileURLToPath(new URL("./index.ts", import.meta.url)), "utf8");

    expect(source).toContain("__MINIPROGRAM_BUILD_SHA__");
    expect(source).toContain("__MINIPROGRAM_BUILD_DIRTY__");
    expect(source).toContain("process.env.MINIPROGRAM_BUILD_SHA");
  });
});
