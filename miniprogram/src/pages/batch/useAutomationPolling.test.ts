import { afterEach, expect, test, vi } from "vitest";
const h = vi.hoisted(() => ({ show: () => {}, hide: () => {}, cleanup: () => {} }));
vi.mock("@tarojs/taro", () => ({ useDidShow: (fn: () => void) => { h.show = fn; }, useDidHide: (fn: () => void) => { h.hide = fn; } }));
vi.mock("react", () => ({ useRef: (value: any) => ({ current: value }), useEffect: (fn: () => (() => void)) => { h.cleanup = fn(); } }));
import { useAutomationPolling } from "./useAutomationPolling";
afterEach(() => { h.cleanup?.(); vi.useRealTimers(); });
test("hidden batch stops polling and ignores the in-flight response, show resumes", async () => {
  vi.useFakeTimers();
  let resolve!: (value: string) => void;
  const read = vi.fn(() => new Promise<string>(r => { resolve = r; }));
  const update = vi.fn();
  useAutomationPolling(true, "batch-a", read, update, () => {});
  await vi.advanceTimersByTimeAsync(5000);
  expect(read).toHaveBeenCalledTimes(1);
  h.hide(); resolve("old");
  await vi.advanceTimersByTimeAsync(20000);
  expect(update).not.toHaveBeenCalled(); expect(read).toHaveBeenCalledTimes(1);
  h.show(); await vi.advanceTimersByTimeAsync(5000);
  expect(read).toHaveBeenCalledTimes(2);
});
test("works list cannot poll a previously selected batch", async () => {
  vi.useFakeTimers(); const read = vi.fn();
  useAutomationPolling(false, "old-batch", read, () => {}, () => {});
  h.show(); await vi.advanceTimersByTimeAsync(20000);
  expect(read).not.toHaveBeenCalled();
});
