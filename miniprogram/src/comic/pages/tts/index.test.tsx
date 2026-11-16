import { beforeEach, describe, expect, test, vi } from "vitest";

const h = vi.hoisted(() => ({
  slots: [] as any[],
  cursor: 0,
  effects: [] as Array<() => void | (() => void)>,
  read: vi.fn(),
  synth: vi.fn(),
  createAudio: vi.fn(),
  toast: vi.fn(),
}));

vi.mock("react", () => ({
  useState: (initial: any) => {
    const index = h.cursor++;
    if (!(index in h.slots)) h.slots[index] = typeof initial === "function" ? initial() : initial;
    return [h.slots[index], (next: any) => { h.slots[index] = typeof next === "function" ? next(h.slots[index]) : next; }];
  },
  useEffect: (effect: any) => { h.effects.push(effect); },
  useRef: (initial: any) => {
    const index = h.cursor++;
    if (!(index in h.slots)) h.slots[index] = { current: initial };
    return h.slots[index];
  },
}));
vi.mock("@tarojs/taro", () => ({
  default: { createInnerAudioContext: h.createAudio, showToast: h.toast },
}));
vi.mock("@tarojs/components", () => ({ Button: "Button", Picker: "Picker", Slider: "Slider", Text: "Text", Textarea: "Textarea", View: "View" }));
vi.mock("../../../components/BrandHeader", () => ({ default: "Header" }));
vi.mock("../../../theme", () => ({ ThemedPage: "Page" }));
vi.mock("../../../services/tts", () => ({
  DEFAULT_TTS_OPTIONS: { voice: "zh-CN-XiaoxiaoNeural", style: "general", speed: 1.8, pitch: 10 },
  TTS_VOICES: [{ label: "晓晓", value: "zh-CN-XiaoxiaoNeural" }],
  TTS_STYLES: [{ label: "通用", value: "general" }],
  readTtsOptions: h.read,
  synthesizeSpeech: h.synth,
}));

import TtsPage from "./index";

function render() { h.cursor = 0; return TtsPage(); }
async function flush() { for (let index = 0; index < 12; index += 1) await Promise.resolve(); }
function textContent(node: any): string {
  if (Array.isArray(node)) return node.map(textContent).join("");
  if (node === null || node === undefined || typeof node === "boolean") return "";
  if (typeof node !== "object") return String(node);
  return textContent(node.props?.children);
}
function findByClass(node: any, className: string): any {
  if (Array.isArray(node)) return node.map((child) => findByClass(child, className)).find(Boolean);
  if (!node || typeof node !== "object") return undefined;
  if (node.props?.className?.split(" ").includes(className)) return node;
  return findByClass(node.props?.children, className);
}

beforeEach(() => {
  h.slots = []; h.effects = [];
  vi.resetAllMocks();
  h.read.mockResolvedValue({});
  h.createAudio.mockReturnValue({
    play: vi.fn(), pause: vi.fn(), stop: vi.fn(), destroy: vi.fn(),
    onEnded: vi.fn(), onPause: vi.fn(), onError: vi.fn(), src: "",
  });
});

describe("独立配音页", () => {
  test("空文本不发请求并提示", () => {
    render();
    findByClass(render(), "mp-primary").props.onClick();
    expect(textContent(render())).toContain("请输入要配音的文字");
    expect(h.synth).not.toHaveBeenCalled();
  });

  test("读取服务端默认参数并用于生成", async () => {
    h.read.mockResolvedValue({ voice: "zh-CN-YunxiNeural", speed: 1.2 });
    h.synth.mockResolvedValue("/user-data/a.mp3");
    render();
    h.effects.forEach((effect) => effect());
    await flush();
    findByClass(render(), "mp-textarea").props.onInput({ detail: { value: "你好" } });
    await findByClass(render(), "mp-primary").props.onClick();
    await flush();
    expect(h.synth).toHaveBeenCalledWith("你好", { voice: "zh-CN-YunxiNeural", style: "general", speed: 1.2, pitch: 10 });
    expect(textContent(render())).toContain("已生成到当前设备缓存");
  });

  test("生成失败展示错误", async () => {
    h.synth.mockRejectedValue(new Error("配音接口未返回有效音频"));
    render();
    findByClass(render(), "mp-textarea").props.onInput({ detail: { value: "你好" } });
    await findByClass(render(), "mp-primary").props.onClick();
    await flush();
    expect(textContent(render())).toContain("配音接口未返回有效音频");
  });

  test("生成后可播放和暂停", async () => {
    h.synth.mockResolvedValue("/user-data/a.mp3");
    render();
    findByClass(render(), "mp-textarea").props.onInput({ detail: { value: "你好" } });
    await findByClass(render(), "mp-primary").props.onClick();
    await flush();
    const playButton = findByClass(render(), "mp-secondary");
    playButton.props.onClick();
    expect(h.createAudio).toHaveBeenCalledTimes(1);
    expect(textContent(render())).toContain("暂停");
    findByClass(render(), "mp-secondary").props.onClick();
    expect(textContent(render())).toContain("播放配音");
  });
});
