import { afterEach, beforeEach, expect, test, vi } from 'vitest';

const h = vi.hoisted(() => ({ effects: [] as (() => unknown)[], scroll: null as any, exporter: vi.fn(), unlink: vi.fn() }));
vi.mock('react', () => ({ useRef: (current: unknown) => ({ current }), useState: (value: unknown) => [value, vi.fn()], useEffect: (effect: () => unknown) => h.effects.push(effect) }));
vi.mock('../../theme', () => ({ useMobileTheme: () => ({ theme: 'light' }) }));
vi.mock('@tarojs/components', () => ({ Canvas: 'Canvas', Image: 'Image', View: 'View' }));
vi.mock('@tarojs/taro', () => {
  const gradient = { addColorStop: vi.fn() };
  const ctx = {
    createLinearGradient: () => gradient, createRadialGradient: () => gradient,
    fillRect() {}, save() {}, translate() {}, rotate() {}, beginPath() {}, ellipse() {}, stroke() {}, restore() {},
    getImageData: () => ({ width: 200, height: 200, data: new Uint8ClampedArray(200 * 200 * 4) }),
    createImageData: (width: number, height: number) => ({ data: new Uint8ClampedArray(width * height * 4) }), putImageData() {},
  };
  const canvas = { width: 200, height: 200, getContext: () => ctx };
  const query = { select: () => query, fields: () => query, boundingClientRect: () => query, exec: (cb: any) => cb([
    { node: canvas, width: 200, height: 200 }, { node: canvas }, { left: 20, top: 40, width: 100, height: 60 },
  ]) };
  return { default: {
    nextTick: (fn: () => void) => fn(), createSelectorQuery: () => query,
    getWindowInfo: () => ({ pixelRatio: 1 }), canvasToTempFilePath: h.exporter,
    getFileSystemManager: () => ({ unlink: h.unlink }), onWindowResize() {}, offWindowResize() {},
  }, usePageScroll: (fn: any) => { h.scroll = fn; }, useDidHide() {}, useDidShow() {} };
});
import NativeGlassMaterial from './NativeGlassMaterial';

beforeEach(() => {
  vi.useFakeTimers(); h.effects = []; h.unlink.mockReset(); h.exporter.mockReset();
  h.exporter.mockResolvedValueOnce({ tempFilePath: 'wall' }).mockResolvedValueOnce({ tempFilePath: 'initial-lens' });
});
afterEach(() => vi.useRealTimers());
async function mount() {
  const publish = vi.fn(); NativeGlassMaterial({ reduced: false, onLensChange: publish });
  h.effects.forEach(effect => effect()); await vi.advanceTimersByTimeAsync(0);
  expect(publish).toHaveBeenLastCalledWith('initial-lens'); return publish;
}
test('hides the previous lens immediately when scrolling starts', async () => {
  const publish = await mount(); h.scroll({ scrollTop: 15 });
  expect(publish).toHaveBeenLastCalledWith('');
});
test('does not publish an in-flight lens sampled before the latest scroll', async () => {
  const publish = await mount(); let finish!: (value: { tempFilePath: string }) => void;
  h.exporter.mockImplementationOnce(() => new Promise(resolve => { finish = resolve; }));
  h.scroll({ scrollTop: 15 }); await vi.advanceTimersByTimeAsync(140);
  h.scroll({ scrollTop: 35 }); finish({ tempFilePath: 'stale-lens' }); await vi.advanceTimersByTimeAsync(0);
  expect(publish).not.toHaveBeenCalledWith('stale-lens');
  expect(h.unlink).toHaveBeenCalledWith(expect.objectContaining({ filePath: 'stale-lens' }));
});
