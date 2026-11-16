import { expect, test, vi } from "vitest";
import { startAutomationPolling } from "./batchFactoryPageState";

test("slow automation reads never overlap and stopped batch cannot publish late response", async () => {
  vi.useFakeTimers();
  let finish!: (value: string) => void;
  let reads = 0;
  const visible: string[] = [];
  const stop = startAutomationPolling(() => { reads++; return new Promise<string>(resolve => { finish = resolve; }); }, value => visible.push(value), () => {}, 10);
  await vi.advanceTimersByTimeAsync(50);
  expect(reads).toBe(1);
  stop();
  finish("old batch");
  await vi.advanceTimersByTimeAsync(50);
  expect(visible).toEqual([]);
  expect(reads).toBe(1);
  vi.useRealTimers();
});
