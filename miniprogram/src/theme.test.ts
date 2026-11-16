import { beforeEach, describe, expect, test, vi } from "vitest";
import { readFileSync } from "node:fs";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { uiPalette, uiVariables } from "./ui/tokens";

const taro = vi.hoisted(() => ({
  getStorageSync: vi.fn(),
  setStorageSync: vi.fn(),
  setNavigationBarColor: vi.fn(),
  setBackgroundColor: vi.fn(),
  setTabBarStyle: vi.fn(),
  setTabBarItem: vi.fn(),
  getCurrentPages: vi.fn(),
  getAppBaseInfo: vi.fn(),
  showTabBar: vi.fn(),
  hideTabBar: vi.fn(),
  didShowCallbacks: [] as Array<() => void>,
}));

vi.mock("@tarojs/taro", () => ({
  default: taro,
  useDidShow: (callback: () => void) => { taro.didShowCallbacks.push(callback); },
}));
vi.mock("@tarojs/components", () => ({ View: "view", Image: "image", Button: "button", Text: "text" }));
vi.mock("./ui/DynamicGlassCanvas", () => ({ default: "dynamic-glass-canvas" }));

import {
  MOBILE_THEME_STORAGE_KEY,
  readMobileTheme,
  saveMobileTheme,
  themeClassName,
  ThemedPage,
} from "./theme";

describe("mobile theme", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    taro.didShowCallbacks = [];
    taro.getCurrentPages.mockReturnValue([]);
  });

  test("mounts one dynamic optical layer on the profile liquid shell", () => {
    const html = renderToStaticMarkup(createElement(ThemedPage, { className: "mp-page mp-profile-page" }));
    expect(html).not.toContain('mp-profile-ordinary');
    expect(html).toContain('mp-liquid');
    expect(html).toContain('liquid-wall');
    taro.didShowCallbacks.forEach(callback => callback());
    expect(taro.showTabBar).toHaveBeenCalledWith({ animation: false });
    expect(taro.hideTabBar).not.toHaveBeenCalled();
    expect(html.match(/<dynamic-glass-canvas/g)).toHaveLength(1);
  });

  test("mounts the dynamic renderer on public pages but not business pages", () => {
    const publicHtml = renderToStaticMarkup(createElement(ThemedPage, { className: "mp-page mp-primary-page mp-functions-page" }));
    const businessHtml = renderToStaticMarkup(createElement(ThemedPage, { className: "mp-page mp-detail-page mp-studio" }));
    expect(publicHtml.match(/<dynamic-glass-canvas/g)).toHaveLength(1);
    expect(publicHtml).toContain("mp-liquid");
    expect(publicHtml).toContain("liquid-wall");
    expect(businessHtml).not.toContain("dynamic-glass-canvas");
    expect(businessHtml).not.toContain("mp-liquid");
  });

  test("omits dynamic optics when reduced transparency is enabled", () => {
    taro.getStorageSync.mockImplementation((key: string) => key.includes('reduceTransparency'));
    const html = renderToStaticMarkup(createElement(ThemedPage, { className: "mp-page mp-primary-page mp-functions-page" }));
    expect(html).toContain('liquid-reduced');
    expect(html).not.toContain('dynamic-glass-canvas');
  });

  test.each([
    "mp-page mp-primary-page mp-workspace-home",
    "mp-page mp-primary-page mp-tasks-page",
    "mp-page mp-primary-page mp-functions-page",
  ])("keeps the native TabBar available on primary page: %s", (className) => {
    renderToStaticMarkup(createElement(ThemedPage, { className }));
    taro.didShowCallbacks.forEach(callback => callback());
    expect(taro.showTabBar).toHaveBeenCalledWith({ animation: false });
    expect(taro.hideTabBar).not.toHaveBeenCalled();
  });

  test.each(['mp-page mp-detail-page', 'mp-page mp-primary-page'])("does not mark non-profile roots as ordinary profile: %s", className => {
    const html = renderToStaticMarkup(createElement(ThemedPage, { className }));
    expect(html).not.toContain('mp-profile-ordinary');
  });

  test("does not expose native TabBar when an ordinary detail page is shown", () => {
    renderToStaticMarkup(createElement(ThemedPage, { className: "mp-page mp-detail-page" }));
    taro.didShowCallbacks.forEach(callback => callback());
    expect(taro.showTabBar).not.toHaveBeenCalled();
    expect(taro.hideTabBar).not.toHaveBeenCalled();
  });

  test("uses dark when the saved theme is absent or invalid", () => {
    taro.getStorageSync.mockReturnValueOnce("");
    expect(readMobileTheme()).toBe("dark");

    taro.getStorageSync.mockReturnValueOnce("system");
    expect(readMobileTheme()).toBe("dark");
  });

  test("persists light theme and applies its native page chrome immediately", () => {
    taro.getCurrentPages.mockReturnValue([{ route: "pages/functions/index" }]);
    saveMobileTheme("light");

    expect(taro.setStorageSync).toHaveBeenCalledWith(MOBILE_THEME_STORAGE_KEY, "light");
    expect(taro.setNavigationBarColor).toHaveBeenCalledWith({
      frontColor: "#000000",
      backgroundColor: uiPalette("light").canvas,
    });
    expect(taro.setBackgroundColor).toHaveBeenCalledWith({
      backgroundColorTop: uiPalette("light").canvas,
      backgroundColorBottom: uiPalette("light").canvas,
    });
    expect(themeClassName("light")).toBe("mp-theme mp-theme-light");
    expect(taro.setTabBarItem).toHaveBeenCalledTimes(4);
    expect(taro.setTabBarItem).toHaveBeenCalledWith({ index: 0, iconPath: expect.stringContaining("normal-light.png"), selectedIconPath: expect.stringContaining("selected-light.png") });
  });
  test("system preference resolves the current native theme", () => {
    taro.getStorageSync.mockReturnValue("system");
    taro.getAppBaseInfo.mockReturnValue({ theme: "light" });
    expect(readMobileTheme()).toBe("light");
    saveMobileTheme("system");
    expect(taro.setStorageSync).toHaveBeenCalledWith(MOBILE_THEME_STORAGE_KEY, "system");
    expect(taro.setNavigationBarColor).toHaveBeenLastCalledWith({ frontColor: "#000000", backgroundColor: uiPalette("light").canvas });
  });

  test("does not call tab-bar APIs from the non-tab batch factory page", () => {
    taro.getCurrentPages.mockReturnValue([{ route: "pages/batch/index" }]);
    saveMobileTheme("light");
    expect(taro.setTabBarStyle).not.toHaveBeenCalled();
    expect(taro.setTabBarItem).not.toHaveBeenCalled();
  });

  test("keeps tab-bar updates on primary tab pages", () => {
    taro.getCurrentPages.mockReturnValue([{ route: "pages/workspace/index" }]);
    saveMobileTheme("light");
    expect(taro.setTabBarStyle).toHaveBeenCalledOnce();
    expect(taro.setTabBarItem).toHaveBeenCalledTimes(4);
  });

  test.each([{ pages: [] }, { pages: [{ route: "pages/bind/index" }] }])("skips tab-bar APIs before a tab page is ready: %j", ({ pages }) => {
    taro.getCurrentPages.mockReturnValue(pages);
    saveMobileTheme("light");
    expect(taro.setNavigationBarColor).toHaveBeenCalled();
    expect(taro.setTabBarStyle).not.toHaveBeenCalled();
    expect(taro.setTabBarItem).not.toHaveBeenCalled();
  });

  test("allows the light theme to style a page when the theme class is on that page root", () => {
    const stylesheet = readFileSync(new URL("./app.scss", import.meta.url), "utf8");
    expect(stylesheet).toContain(".mp-theme-light.mp-page");
    expect(uiVariables("light")["--ui-canvas"]).toBe(uiPalette("light").canvas);
    expect(uiVariables("light")["--ui-textMuted"]).toBe(uiPalette("light").textMuted);
  });

  test("keeps the wallpaper and dynamic layer ready for a theme preview", () => {
    const html = renderToStaticMarkup(createElement(ThemedPage, { className: "mp-page mp-primary-page mp-workspace-home", animateTheme: true }));
    expect(html).toContain('liquid-wall');
    expect(html.match(/<dynamic-glass-canvas/g)).toHaveLength(1);
  });

  test("uses the shared dynamic canvas without custom liquid navigation", () => {
    const source = readFileSync(new URL("./theme.tsx", import.meta.url), "utf8");
    expect(source).toContain("DynamicGlassCanvas");
    expect(source).toContain("liquidPageKey");
    expect(source).not.toContain("LiquidNavigation");
  });

  test("puts the reactive theme page root on the current detail page and each primary tab", () => {
    [
      "./pages/profile-detail/index.tsx",
      "./pages/workspace/index.tsx",
      "./pages/tasks/index.tsx",
      "./pages/functions/index.tsx",
      "./pages/profile/index.tsx",
      "./pages/bind/index.tsx",
      "./pages/batch/index.tsx",
      "./pages/module-detail/index.tsx",
      "./pages/module-detail/AgentWorkspace.tsx",
      "./pages/module-detail/FetchWorkspace.tsx",
      "./pages/module-detail/PanelWorkspace.tsx",
      "./pages/module-detail/ScriptWorkspace.tsx",
      "./pages/module-detail/ShuihuoWorkspace.tsx",
    ].forEach((file) => {
      const pageSource = readFileSync(new URL(file, import.meta.url), "utf8");
      expect(pageSource).toContain("ThemedPage");
    });
  });
});
