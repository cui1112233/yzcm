import { beforeEach, describe, expect, test, vi } from "vitest";

const api = vi.hoisted(() => ({ apiRequest: vi.fn() }));
vi.mock("./api", () => api);

import { readExecutorSummary, summarizeExecutors } from "./executors";

describe("执行器在线状态", () => {
  beforeEach(() => { vi.clearAllMocks(); });

  test("按 online === true 统计在线数量", () => {
    expect(summarizeExecutors({ executors: [{ online: true }, { online: false }, { online: "true" }] })).toEqual({ paired: 3, onlineCount: 1 });
  });

  test("兼容 items 字段", () => {
    expect(summarizeExecutors({ items: [{ online: true }, { online: true }] })).toEqual({ paired: 2, onlineCount: 2 });
  });

  test("空列表返回零值而不是报错", () => {
    expect(summarizeExecutors({ executors: [] })).toEqual({ paired: 0, onlineCount: 0 });
  });

  test("缺少列表字段时明确报错，不猜状态", () => {
    expect(() => summarizeExecutors({})).toThrow("服务端未返回执行器列表");
    expect(() => summarizeExecutors(null)).toThrow("服务端未返回执行器列表");
  });

  test("读取时复用统一请求层", async () => {
    api.apiRequest.mockResolvedValue({ executors: [{ online: true }] });
    await expect(readExecutorSummary()).resolves.toEqual({ paired: 1, onlineCount: 1 });
    expect(api.apiRequest).toHaveBeenCalledWith("/api/shuihuo-production/local-executors", { timeout: 12000 });
  });
});
