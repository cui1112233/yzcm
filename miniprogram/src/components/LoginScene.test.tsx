import { beforeEach, describe, expect, test, vi } from "vitest";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";

const harness = vi.hoisted(() => ({
  slots: [] as unknown[], cursor: 0, effects: [] as Array<() => void | (() => void)>,
}));
const media = vi.hoisted(() => ({ loadLoginIntroMedia: vi.fn() }));

vi.mock("react", () => ({
  useState: (initial: unknown) => {
    const index = harness.cursor++;
    if (!(index in harness.slots)) harness.slots[index] = typeof initial === "function" ? (initial as () => unknown)() : initial;
    return [harness.slots[index], (next: unknown) => { harness.slots[index] = typeof next === "function" ? (next as (value: unknown) => unknown)(harness.slots[index]) : next; }];
  },
  useEffect: (effect: () => void | (() => void)) => { harness.effects.push(effect); },
}));
vi.mock("@tarojs/components", () => ({ Button: "Button", Image: "Image", Input: "Input", Text: "Text", Video: "Video", View: "View" }));
vi.mock("../assets/brand-logo-black.png", () => ({ default: "brand" }));
vi.mock("../assets/login/box.png", () => ({ default: "box.png" }));
vi.mock("../assets/login/pair.png", () => ({ default: "pair.png" }));
vi.mock("../assets/login/tail.png", () => ({ default: "tail.png" }));
vi.mock("../assets/login/parts.png", () => ({ default: "parts.png" }));
vi.mock("../assets/login/wheel.png", () => ({ default: "wheel.png" }));
vi.mock("../assets/login/blink-girl.png", () => ({ default: "blink-girl.png" }));
vi.mock("../assets/login/blink-boy.png", () => ({ default: "blink-boy.png" }));
vi.mock("../assets/login/boy.png", () => ({ default: "boy.png" }));
vi.mock("../assets/login/girl.png", () => ({ default: "girl.png" }));
vi.mock("../assets/login/boy-arms.png", () => ({ default: "boy-arms.png" }));
vi.mock("../assets/login/girl-arms.png", () => ({ default: "girl-arms.png" }));
vi.mock("../assets/login/grips.png", () => ({ default: "grips.png" }));
vi.mock("../assets/login/charm-boy.png", () => ({ default: "charm-boy.png" }));
vi.mock("../assets/login/charm-girl.png", () => ({ default: "charm-girl.png" }));
vi.mock("../assets/login/smoke.png", () => ({ default: "smoke.png" }));
vi.mock("./AppIcon", () => ({ default: "AppIcon" }));
vi.mock("./LoginScene.scss", () => ({}));
vi.mock("../services/loginIntroMedia", () => media);
vi.mock("../config/runtime", () => ({ apiUrl: (path: string) => `https://api.test${path}` }));
vi.mock("../config/release", () => ({ MINIPROGRAM_VERSION_LABEL: "v1.1.0 · abc1234" }));

import LoginScene from "./LoginScene";

const props = {
  username: "", password: "", mfaCode: "", remember: false, automatic: false,
  busy: false, error: "", active: true, reduced: false, rememberedSession: false,
  onUsername: () => {}, onPassword: () => {}, onMfa: () => {}, onRemember: () => {}, onAutomatic: () => {}, onSubmit: () => {},
};

function render() {
  harness.cursor = 0;
  harness.effects = [];
  return LoginScene(props) as { props: { className: string } };
}

function findByClass(node: any, className: string): any {
  if (!node || typeof node !== "object") return undefined;
  if (String(node.props?.className || "").split(" ").includes(className)) return node;
  const children = node.props?.children;
  for (const child of Array.isArray(children) ? children : [children]) {
    const found = findByClass(child, className);
    if (found) return found;
  }
  return undefined;
}

describe("LoginScene", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    harness.slots = [];
    media.loadLoginIntroMedia.mockReset();
    media.loadLoginIntroMedia.mockResolvedValue("wxfile://login-intro.mp4");
  });

  test("starts the entrance when the safety timer fires after local video is ready", async () => {
    render();
    harness.effects.forEach(effect => effect());
    await Promise.resolve();
    render();
    harness.effects.forEach(effect => effect());
    vi.advanceTimersByTime(3000);
    expect(render().props.className).toContain("nl-entering");
  });

  test("a first touch on the login form skips the remaining entrance", () => {
    const form = findByClass(render(), "nl-form");
    expect(form.props.onTouchStart).toBeTypeOf("function");
    form.props.onTouchStart();
    expect(render().props.className).toContain("nl-ready");
  });

  test("uses PNG artwork that remains visible in the real-device mini-program runtime", () => {
    const scene = render();
    for (const className of ["nl-box", "nl-pair", "nl-tail", "nl-parts", "nl-smoke", "nl-grips"]) {
      expect(findByClass(scene, className)?.props.src).toMatch(/\.png$/);
    }
  });

  test("downloads the approved crash preview before giving its local path to the player", async () => {
    render();
    harness.effects.forEach(effect => effect());
    await Promise.resolve();
    render();
    harness.effects.forEach(effect => effect());
    vi.advanceTimersByTime(3000);
    const scene = render();
    const video = findByClass(scene, "nl-intro-video");
    expect(media.loadLoginIntroMedia).toHaveBeenCalledWith("https://api.test/assets/login-intro-88258223.mp4");
    expect(video?.props.src).toBe("wxfile://login-intro.mp4");
    expect(video?.props.autoplay).toBe(true);
    expect(video?.props.onEnded).toBeTypeOf("function");
    video.props.onEnded();
    expect(findByClass(render(), "nl-intro-video")).toBeUndefined();
    expect(render().props.className).toContain("nl-ready");
  });

  test("hands off to the real login form within five seconds", async () => {
    render();
    harness.effects.forEach(effect => effect());
    await Promise.resolve();
    render();
    harness.effects.forEach(effect => effect());
    vi.advanceTimersByTime(3000);
    expect(render().props.className).toContain("nl-entering");
    harness.effects.forEach(effect => effect());
    vi.advanceTimersByTime(5000);
    expect(render().props.className).toContain("nl-ready");
  });

  test("renders a pointer-inert wheel that loops behind the ready login card", () => {
    const scene = render();
    const track = findByClass(scene, "nl-wheel-track");
    const wheel = findByClass(scene, "nl-wheel");
    const styles = readFileSync(resolve(__dirname, "LoginScene.scss"), "utf8");

    expect(track).toBeDefined();
    expect(wheel?.props.src).toMatch(/wheel\.png$/);
    expect(styles).toMatch(/\.nl-wheel-track\{[^}]*z-index:2[^}]*pointer-events:none/);
    expect(styles).toMatch(/\.nl-card-assembly\{[^}]*z-index:3/);
    expect(styles).toMatch(/\.nl-wheel\{[^}]*width:60px[^}]*height:60px/);
    expect(styles).toContain(".nl-ready .nl-wheel{animation:nl-wheel-roll 7s linear infinite}");
    expect(styles).toContain("@keyframes nl-wheel-roll");
  });

  test("shows the uploaded mini-program version below the login form", () => {
    const version = findByClass(render(), "nl-version");
    expect(version).toBeDefined();
    expect(version.props.children).toBe("v1.1.0 · abc1234");
    expect(version.props.children).toMatch(/^v1\.1\.0 · [0-9a-f]{7}$/);
  });

  test("failed intro download still plays the CSS entrance and logs a warning", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    media.loadLoginIntroMedia.mockRejectedValue(new Error("404 Not Found"));

    render();
    harness.effects.forEach(effect => effect());
    await Promise.resolve();
    render();
    harness.effects.forEach(effect => effect());
    await vi.advanceTimersByTimeAsync(3000);
    const scene = render();
    expect(scene.props.className).toContain("nl-entering");
    expect(findByClass(scene, "nl-intro-video")).toBeUndefined();
    expect(warn).toHaveBeenCalledWith("登录入场视频加载失败，已降级为 CSS 入场动画", expect.anything());
    warn.mockRestore();
  });
});
