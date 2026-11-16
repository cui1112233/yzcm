import { afterEach, beforeEach, expect, test, vi } from "vitest";

const taro = vi.hoisted(() => ({ request: vi.fn(), getStorageSync: vi.fn(), login: vi.fn(), removeStorageSync: vi.fn(), reLaunch: vi.fn() }));
vi.mock("@tarojs/taro", () => ({ default: taro }));
vi.mock("../config/runtime", () => ({ requireApiOrigin: () => undefined, apiUrl: (path: string) => `https://api.test${path}` }));
import { readMemberProfile } from "./memberProfile";

beforeEach(() => { vi.resetAllMocks(); vi.useFakeTimers(); });
afterEach(() => { vi.useRealTimers(); });

test("a stalled native request stops loading with a timeout error", async () => {
  taro.request.mockImplementation(() => new Promise(() => {}));
  const result = expect(readMemberProfile()).rejects.toThrow(/超时/);
  await vi.advanceTimersByTimeAsync(15000);
  await result;
});

test("native failure retains its diagnostic message", async () => {
  taro.request.mockRejectedValue({ errMsg: "request:fail url not in domain list" });
  await expect(readMemberProfile()).rejects.toThrow("request:fail url not in domain list");
  expect(vi.getTimerCount()).toBe(0);
});

test("expired authentication clears the stale session and returns to account login", async () => {
  taro.request.mockResolvedValue({ statusCode: 401, data: { error: "Unauthorized" }, header: { "X-Qiantie-Auth-Failure": "session" } });
  await expect(readMemberProfile()).rejects.toThrow(/登录.*失效/);
  expect(taro.request).toHaveBeenCalledTimes(1);
  expect(taro.login).not.toHaveBeenCalled();
  expect(taro.removeStorageSync).toHaveBeenCalled();
  expect(taro.reLaunch).toHaveBeenCalledWith({ url: "/pages/bind/index" });
});

test("returns actual account data and clears the loading watchdog", async () => {
  taro.request.mockResolvedValue({ statusCode: 200, data: { member: { username: "real-account" } } });
  await expect(readMemberProfile()).resolves.toEqual({ member: { username: "real-account" } });
  expect(vi.getTimerCount()).toBe(0);
});
