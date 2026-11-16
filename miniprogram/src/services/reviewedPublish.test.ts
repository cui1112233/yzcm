import { expect, test } from "vitest";
import { invalidateSessionResources } from "./sessionBoundary";
import { reviewedPublish } from "./reviewedPublish";

test("account change during review cannot confirm or submit the previous account's intent", async () => {
  const writes: string[] = [];
  await expect(reviewedPublish({
    create: async () => "intent-a",
    review: async () => { invalidateSessionResources(); return true; },
    confirm: async () => { writes.push("confirm"); },
    submit: async () => { writes.push("submit"); return "receipt"; },
    isCurrent: () => true,
  })).rejects.toThrow("账号或作品已切换");
  expect(writes).toEqual([]);
});
test("switching book while server confirms cannot submit the old book", async () => {
  let current = true; const writes: string[] = [];
  await expect(reviewedPublish({
    create: async () => "intent-a", review: async () => true,
    confirm: async () => { current = false; },
    submit: async () => { writes.push("submit"); }, isCurrent: () => current,
  })).rejects.toThrow("账号或作品已切换");
  expect(writes).toEqual([]);
});
test("cancelled review does not confirm or submit; accepted review returns receipt", async () => {
  const writes: string[] = [];
  const options = { create: async () => "intent-a", review: async () => false,
    confirm: async (id: string) => { writes.push(id); }, submit: async () => "receipt", isCurrent: () => true };
  expect(await reviewedPublish(options)).toBeNull(); expect(writes).toEqual([]);
  expect(await reviewedPublish({ ...options, review: async () => true })).toBe("receipt");
  expect(writes).toEqual(["intent-a"]);
});
