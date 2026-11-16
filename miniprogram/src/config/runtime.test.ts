import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { API_ORIGIN, requireApiOrigin } from "./runtime";

describe("runtime configuration", () => {
  it("does not reference Node's process global", () => {
    const source = readFileSync(fileURLToPath(new URL("./runtime.ts", import.meta.url)), "utf8");

    expect(source).not.toContain("process.env");
  });

  it("rejects the deliberately invalid fallback instead of treating it as a server", () => {
    expect(API_ORIGIN).toBe("https://api.example.invalid");
    expect(() => requireApiOrigin()).toThrow(
      "服务端地址未配置，请使用已配置微信合法域名的 HTTPS 构建",
    );
  });
});
