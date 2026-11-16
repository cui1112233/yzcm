import { expect, test } from "vitest";
import { resolveCreatorLevel } from "./creatorLevel";
test("uses server level before usage and never invents a level without data", () => {
  expect(resolveCreatorLevel(null)).toBeNull();
  expect(resolveCreatorLevel({ usage: { month: { totalTokens: 50000 } } })).toMatchObject({ level: 2, percent: 0, source: "usage" });
  expect(resolveCreatorLevel({ level: { level: 7, name: "服务端等级", current: 3, next: 10 } })).toMatchObject({ level: 7, percent: 30, source: "backend" });
});
