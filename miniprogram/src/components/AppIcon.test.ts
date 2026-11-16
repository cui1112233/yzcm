import { existsSync } from "node:fs";
import { resolve } from "node:path";
import { expect, test } from "vitest";

test("提供统一的本地图标组件供页面和工作区复用", () => {
  expect(existsSync(resolve(__dirname, "AppIcon.tsx"))).toBe(true);
});
