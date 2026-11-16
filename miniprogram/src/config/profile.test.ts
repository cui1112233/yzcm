import { describe, expect, it } from "vitest";
import * as profileConfig from "./profile";

const { PROFILE_SECTIONS } = profileConfig;

describe("profile parity model", () => {
  it("keeps only verified data on detail pages and exposes unavailable routes honestly", () => {
    expect(PROFILE_SECTIONS.map((section) => section.title)).toEqual([
      "个人资料",
      "服务",
    ]);
    expect(PROFILE_SECTIONS.flatMap((section) => section.items.map((item) => item.title))).toEqual([
      "个人资料",
      "会员中心",
      "账号安全",
      "API 配置",
      "用量与制作",
    ]);
    const titles = PROFILE_SECTIONS.flatMap((section) => section.items.map((item) => item.title));
    expect(titles).not.toContain("联合治理");
    expect(titles).not.toContain("组员管理");
    expect(titles).not.toContain("账号与角色");
    expect(titles).not.toContain("管理后台");
  });

  it("uses the same semantic icons as the web personal center", () => {
    expect(PROFILE_SECTIONS.flatMap((section) => section.items.map((item) => item.icon))).toEqual([
      "user-round",
      "crown",
      "shield-check",
      "key-round",
      "chart",
    ]);
  });

  it("uses the web role boundaries for governance and developer tools", () => {
    const buildProfileSections = (profileConfig as unknown as Record<string, unknown>).buildProfileSections;

    expect(buildProfileSections).toEqual(expect.any(Function));
    if (typeof buildProfileSections !== "function") return;

    const managerItems = buildProfileSections({ role: "manager" }).flatMap((section: any) => section.items.map((item: any) => item.key));
    const developerItems = buildProfileSections({ role: "dev" }).flatMap((section: any) => section.items.map((item: any) => item.key));

    expect(managerItems).toEqual(expect.arrayContaining(["governance", "team"]));
    expect(managerItems).not.toEqual(expect.arrayContaining(["accounts", "adminConsole"]));
    expect(developerItems).toEqual(expect.arrayContaining(["governance", "team", "accounts", "adminConsole"]));
  });
});
