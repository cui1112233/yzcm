import { describe, expect, test, vi } from "vitest";

const profile = vi.hoisted(() => ({ readMemberProfile: vi.fn() }));
vi.mock("../../services/memberProfile", () => profile);

import { readMemberCenterView, toMemberCenterView } from "./memberCenter";

describe("会员中心只读数据", () => {
  test("把账号中心返回字段映射为只读视图", () => {
    const view = toMemberCenterView({
      member: {
        displayName: "小明", username: "ming", role: "member", team: { name: "北极星" },
        apiEnabled: true, apiScopes: ["text", "image"], monthlyTokenLimit: 200000,
      },
      team: { name: "北极星" },
      usage: { month: { totalTokens: 50000 } },
    });
    expect(view.facts.displayName).toBe("小明");
    expect(view.facts.teamName).toBe("北极星");
    expect(view.facts.apiEnabled).toBe(true);
    expect(view.quota).toEqual({ used: 50000, limit: 200000, percent: 25 });
  });

  test("额度不限额或缺失时不计算百分比", () => {
    const view = toMemberCenterView({ member: { monthlyTokenLimit: null }, usage: { month: { totalTokens: 12 } } });
    expect(view.quota.limit).toBeNull();
    expect(view.quota.percent).toBeUndefined();
    const missing = toMemberCenterView({ member: {} });
    expect(missing.quota.used).toBeUndefined();
    expect(missing.quota.percent).toBeUndefined();
  });

  test("服务端未返回会员对象时明确报错而不是用假数据占位", async () => {
    profile.readMemberProfile.mockResolvedValue({});
    await expect(readMemberCenterView()).rejects.toThrow("服务端未返回有效的会员资料");
  });

  test("读取成功后返回同一份视图", async () => {
    profile.readMemberProfile.mockResolvedValue({ member: { username: "ming", role: "dev" }, usage: { month: { totalTokens: 1 } } });
    await expect(readMemberCenterView()).resolves.toMatchObject({ facts: { username: "ming", role: "dev" }, quota: { used: 1 } });
  });
});
