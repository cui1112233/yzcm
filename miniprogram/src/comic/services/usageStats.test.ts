import { describe, expect, test, vi } from "vitest";

const profile = vi.hoisted(() => ({ readMemberProfile: vi.fn() }));
vi.mock("../../services/memberProfile", () => profile);

import { readUsageStats, toUsageStatsView } from "./usageStats";

describe("用量统计只读数据", () => {
  test("解析今日、本月与最近调用", () => {
    const view = toUsageStatsView({
      usage: {
        day: { totalTokens: 1200, calls: 3 },
        month: { totalTokens: 50000, calls: 40, outputTokens: 8000, estimatedCost: 1.5, costCurrency: "CNY" },
        recent: [{ id: "a", feature: "script", model: "text-large", totalTokens: 300, createdAt: "2026-11-04" }],
      },
    });
    expect(view.day).toMatchObject({ totalTokens: 1200, calls: 3 });
    expect(view.month).toMatchObject({ totalTokens: 50000, outputTokens: 8000, costCurrency: "CNY" });
    expect(view.recent[0]).toMatchObject({ id: "a", feature: "script", totalTokens: 300 });
  });

  test("缺失字段保持 undefined，不补默认值", () => {
    const view = toUsageStatsView({ usage: { day: {}, month: {}, recent: [] } });
    expect(view.day.totalTokens).toBeUndefined();
    expect(view.month.calls).toBeUndefined();
    expect(view.recent).toEqual([]);
  });

  test("服务端未返回用量对象时明确报错", () => {
    expect(() => toUsageStatsView({ member: {} })).toThrow("服务端未返回用量数据");
  });

  test("读取时复用账号资料请求", async () => {
    profile.readMemberProfile.mockResolvedValue({ usage: { day: { calls: 1 }, month: {}, recent: [] } });
    await expect(readUsageStats()).resolves.toMatchObject({ day: { calls: 1 } });
    expect(profile.readMemberProfile).toHaveBeenCalledTimes(1);
  });
});
