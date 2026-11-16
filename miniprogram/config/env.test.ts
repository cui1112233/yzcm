import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { resolveApiOrigin } from "./env";

describe("Taro API environment", () => {
  it("reads the selected mode file when the shell has no API variable", () => {
    const root = mkdtempSync(join(tmpdir(), "yzcm-taro-env-"));
    writeFileSync(join(root, ".env.development"), "TARO_APP_API_ORIGIN=https://yzcm.site\n");

    expect(resolveApiOrigin({ root, mode: "development", environment: {} })).toBe(
      "https://yzcm.site",
    );
  });
});
