import { readFileSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { expect, test } from "vitest";

test("walking sprite keeps its frame geometry within the mini-program package budget", () => {
  const path = fileURLToPath(new URL("../assets/companion/walk.png", import.meta.url));
  const png = readFileSync(path);

  expect(png.subarray(1, 4).toString()).toBe("PNG");
  expect(png.readUInt32BE(16)).toBe(1152);
  expect(png.readUInt32BE(20)).toBe(288);
  expect(statSync(path).size).toBeLessThanOrEqual(25_000);
});
