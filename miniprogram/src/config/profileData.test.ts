import { monthlyTokenUsage, profileFacts, profileScopes, formatTokenAmount } from "./profileData";
import { describe, expect, it } from "vitest";

describe("public profile data mapping", () => {
  it("preserves the profile fields used by the public personal information page", () => {
    expect(profileFacts({ member: { bio: "小说创作者", phone: "13800000000", email: "author@example.test", teamTitle: "编剧" } })).toMatchObject({
      bio: "小说创作者", phone: "13800000000", email: "author@example.test", teamTitle: "编剧",
    });
    expect(profileFacts({ member: {} })).toMatchObject({ bio: undefined, phone: undefined, email: undefined, teamTitle: undefined });
  });
  it("distinguishes public unlimited quota from absent quota and zero quota", () => {
    expect(formatTokenAmount(profileFacts({ member: { monthlyTokenLimit: null } }).monthlyTokenLimit)).toBe("不限额");
    expect(formatTokenAmount(profileFacts({ member: {} }).monthlyTokenLimit)).toBe("服务端未返回");
    expect(formatTokenAmount(profileFacts({ member: { monthlyTokenLimit: 0 } }).monthlyTokenLimit)).toBe("0");
  });
  it("formats token amounts when the mini-program runtime has no Intl API", () => {
    const originalIntl = globalThis.Intl;
    try {
      Object.defineProperty(globalThis, "Intl", { configurable: true, value: undefined });
      expect(formatTokenAmount(1234567)).toBe("1,234,567");
    } finally {
      Object.defineProperty(globalThis, "Intl", { configurable: true, value: originalIntl });
    }
  });
  it("keeps absent account facts absent instead of inventing usage or authorization", () => {
    const facts = profileFacts({ member: { role: "member" } });

    expect(facts.displayName).toBeUndefined();
    expect(facts.username).toBeUndefined();
    expect(facts.monthlyTokenUsage).toBeUndefined();
    expect(facts.monthlyTokenLimit).toBeUndefined();
    expect(facts.apiEnabled).toBeUndefined();
    expect(facts.scopes).toBeUndefined();
    expect(monthlyTokenUsage({ member: { role: "member" } })).toBeUndefined();
  });

  it("projects literal member, team, and monthly usage facts from the server response", () => {
    const center = {
      member: { username: "cui", role: "member", apiEnabled: false, apiScopes: ["text"], monthlyTokenLimit: 10000 },
      team: { name: "创作团队" },
      usage: { month: { totalTokens: 1234 } },
    };
    const facts = profileFacts(center);

    expect(facts).toMatchObject({
      username: "cui",
      role: "member",
      teamName: "创作团队",
      apiEnabled: false,
      scopes: ["text"],
      monthlyTokenUsage: 1234,
      monthlyTokenLimit: 10000,
    });
    expect(profileScopes(center)).toEqual(["text"]);
    expect(monthlyTokenUsage(center)).toBe(1234);
  });
});
