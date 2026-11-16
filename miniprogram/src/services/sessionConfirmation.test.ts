import { expect, test } from "vitest";
import { invalidateSessionResources } from "./sessionBoundary";
import { sessionConfirmation } from "./sessionConfirmation";

test("a confirmation accepted after account change is treated as cancelled", async () => {
  let finish!: (value: { confirm: boolean }) => void;
  const pending = sessionConfirmation(() => new Promise<{ confirm: boolean }>(resolve => { finish = resolve; }));
  invalidateSessionResources(); finish({ confirm: true });
  expect((await pending).confirm).toBe(false);
});
test("same-session confirmation and cancellation retain the user's choice", async () => {
  expect((await sessionConfirmation(async () => ({ confirm: true }))).confirm).toBe(true);
  expect((await sessionConfirmation(async () => ({ confirm: false }))).confirm).toBe(false);
});
