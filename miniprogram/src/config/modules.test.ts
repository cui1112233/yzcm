import { describe, expect, it } from "vitest";
import { MODULES, moduleRoute } from "./modules";

describe("小程序功能目录", () => {
  it("与网页功能目录保持一致", () => {
    expect(MODULES.map((item) => item.key)).toEqual([
      "script",
      "fetch",
      "comic",
      "panel",
      "shuihuo",
      "agent",
      "batch",
      "toolkit",
    ]);
    expect(MODULES.map((item) => item.label)).toEqual([
      "剧本生成",
      "小说获取",
      "漫画生成",
      "小说面板",
      "水货生产",
      "Agent 工作区",
      "批量工厂",
      "会员与工具",
    ]);
  });

  it("漫画生成属于创作组，会员与工具不承载漫画", () => {
    expect(MODULES.find((item) => item.key === "comic")).toMatchObject({ group: "创作", label: "漫画生成" });
    expect(MODULES.find((item) => item.key === "toolkit")).toMatchObject({ group: "助手", label: "会员与工具" });
    expect(MODULES.find((item) => item.key === "toolkit")?.description).not.toContain("漫画");
  });

  it("使用网页对应的语义图标，而不是页面内的字符图标", () => {
    expect(MODULES.map((item) => item.icon)).toEqual([
      "file-pen-line",
      "book-open",
      "sparkles",
      "notebook-tabs",
      "clapperboard",
      "bot",
      "factory",
      "crown",
    ]);
  });

  it("每个一级功能都有唯一的小程序入口路由", () => {
    expect(moduleRoute("script")).toBe("/pages/module-detail/index?key=script");
    expect(moduleRoute("agent")).toBe("/pages/module-detail/index?key=agent");
    expect(moduleRoute("comic")).toBe("/comic/pages/comic-new/index");
    expect(moduleRoute("batch")).toBe("/pages/batch/index");
    expect(moduleRoute("toolkit")).toBe("/comic/pages/index/index");
  });
});
