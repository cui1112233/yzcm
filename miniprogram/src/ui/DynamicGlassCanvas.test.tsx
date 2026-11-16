import { readFileSync } from 'node:fs';
import { expect, test, vi } from 'vitest';

const harness = vi.hoisted(() => ({
  effects: [] as Array<() => void | (() => void)>,
  taro: {} as Record<string, any>,
}));

vi.mock('react', () => ({
  useEffect: (effect: () => void | (() => void)) => { harness.effects.push(effect); },
  useRef: (value: unknown) => ({ current: value }),
  useState: (value: unknown) => [value, vi.fn()],
}));
vi.mock('@tarojs/components', () => ({ Canvas: 'Canvas', View: 'View' }));
vi.mock('@tarojs/taro', () => ({
  default: harness.taro,
  useDidHide: vi.fn(),
  useDidShow: vi.fn(),
  usePageScroll: vi.fn(),
}));

import DynamicGlassCanvas from './DynamicGlassCanvas';

function collect(node: any, type: string): any[] {
  if (!node || typeof node !== 'object') return [];
  const children = [node.props?.children].flat(Infinity);
  return [node.type === type ? node : null, ...children.flatMap(child => collect(child, type))].filter(Boolean);
}

test('owns exactly one visible canvas and never creates an exported image layer', () => {
  const tree = DynamicGlassCanvas({ theme: 'light', reduced: false, pageKey: 'workspace' }) as any;
  expect(collect(tree, 'Canvas')).toHaveLength(1);
  expect(collect(tree, 'Image')).toHaveLength(0);
  expect(collect(tree, 'Canvas')[0].props.id).toBe('dynamic-glass-workspace');
});

test('marks the layer as decorative and noninteractive', () => {
  const tree = DynamicGlassCanvas({ theme: 'dark', reduced: true, pageKey: 'profile' }) as any;
  expect(tree.props.className).toContain('dynamic-glass-layer');
  expect(tree.props['aria-hidden']).toBe('true');
});

test('keeps the visible canvas transparent outside optical surfaces', () => {
  const source = readFileSync(new URL('./DynamicGlassCanvas.tsx', import.meta.url), 'utf8');
  expect(source).not.toContain('context.drawImage(source, 0, 0, width, height)');
});

test('samples the exact wallpaper image displayed behind the glass', () => {
  const source = readFileSync(new URL('./DynamicGlassCanvas.tsx', import.meta.url), 'utf8');
  expect(source).toContain("import darkWall from '../assets/glass/wallpaper-dark.png'");
  expect(source).toContain("import lightWall from '../assets/glass/wallpaper-light.png'");
  expect(source).not.toContain('wallpaper-dark.webp');
  expect(source).not.toContain('wallpaper-light.webp');
  expect(source).toContain('const wallpaperImage = node.createImage()');
  expect(source).toContain("theme === 'dark' ? darkWall : lightWall");
  expect(source).not.toContain('aspectFillSourceRect');
  expect(source).toContain('0, 0, sourceWidth, sourceHeight,');
  expect(source).toContain('0, 0, pixelWidth, pixelHeight');
  expect(source).not.toContain('paintGlassWallpaper(sourceContext');
  expect(source).toContain('refractRoundedSurface(sourcePixels');
  expect(source).toContain('context.putImageData(image');
});

test('measures explicit glass roles and gives the primary card the reference optics', () => {
  const source = readFileSync(new URL('./DynamicGlassCanvas.tsx', import.meta.url), 'utf8');
  expect(source).toContain("fields({ rect: true, dataset: true })");
  expect(source).toContain('dataset?.glassRole');
  expect(source).toContain('glassOpticsForRole(rect.role)');
  expect(source).not.toContain("rect.role === 'primary' ? 26 : 16");
  expect(source).not.toContain("rect.role === 'primary' ? 25 : 12");
  expect(source).toContain('}), cssHeight)');
  expect(source).not.toContain('}), cssHeight, 6)');
});

test('renders the optical rim at a bounded retina backing scale', () => {
  const source = readFileSync(new URL('./DynamicGlassCanvas.tsx', import.meta.url), 'utf8');
  expect(source).toContain('glassBackingScale(');
  expect(source).toContain('scaleGlassRect(rect, scale)');
  expect(source).toContain('radius: optics.radius * scale');
  expect(source).toContain('rim: optics.rim * scale');
  expect(source).toContain('bend: optics.bend * scale');
});

test('retains the visible frame until the replacement wallpaper has loaded', () => {
  harness.effects.length = 0;
  const visibleContext = {
    clearRect: vi.fn(),
    createImageData: vi.fn((width: number, height: number) => ({ data: new Uint8ClampedArray(width * height * 4) })),
    putImageData: vi.fn(),
  };
  const sourceContext = {
    clearRect: vi.fn(),
    drawImage: vi.fn(),
    getImageData: vi.fn((_x: number, _y: number, width: number, height: number) => ({
      data: new Uint8ClampedArray(width * height * 4),
    })),
  };
  const wallpaperImage: any = { width: 780, height: 1500 };
  const node: any = {
    width: 390,
    height: 844,
    getContext: vi.fn(() => visibleContext),
    createImage: vi.fn(() => wallpaperImage),
    requestAnimationFrame: vi.fn((callback: () => void) => { callback(); return 1; }),
    cancelAnimationFrame: vi.fn(),
  };
  const query = {
    select: vi.fn(() => ({ fields: vi.fn() })),
    selectAll: vi.fn(() => ({ fields: vi.fn() })),
    exec: vi.fn((callback: (results: any[]) => void) => callback([
      { node, width: 390, height: 844 },
      [{ left: 20, top: 120, width: 320, height: 100, dataset: { glassRole: 'primary' } }],
    ])),
  };
  Object.assign(harness.taro, {
    nextTick: (callback: () => void) => callback(),
    createSelectorQuery: () => query,
    createOffscreenCanvas: () => ({ getContext: () => sourceContext }),
    getWindowInfo: () => ({ pixelRatio: 1.5 }),
    eventCenter: { on: vi.fn(), off: vi.fn() },
    onWindowResize: vi.fn(),
    offWindowResize: vi.fn(),
  });

  DynamicGlassCanvas({ theme: 'dark', reduced: false, pageKey: 'workspace' });
  expect(harness.effects).toHaveLength(1);
  const cleanup = harness.effects[0]();

  expect(node.width).toBe(390);
  expect(node.height).toBe(844);
  expect(visibleContext.clearRect).not.toHaveBeenCalled();

  wallpaperImage.onload();

  expect(node.width).toBe(488);
  expect(node.height).toBe(1055);
  expect(visibleContext.clearRect).toHaveBeenCalled();
  expect(visibleContext.putImageData).toHaveBeenCalled();
  cleanup?.();
});

test('retries when the canvas node is temporarily unavailable and eventually draws the glass', () => {
  vi.useFakeTimers();
  harness.effects.length = 0;
  const visibleContext = {
    clearRect: vi.fn(),
    createImageData: vi.fn((width: number, height: number) => ({ data: new Uint8ClampedArray(width * height * 4) })),
    putImageData: vi.fn(),
  };
  const sourceContext = {
    clearRect: vi.fn(),
    drawImage: vi.fn(),
    getImageData: vi.fn((_x: number, _y: number, width: number, height: number) => ({
      data: new Uint8ClampedArray(width * height * 4),
    })),
  };
  const wallpaperImage: any = { width: 780, height: 1500 };
  const node: any = {
    width: 390,
    height: 844,
    getContext: vi.fn(() => visibleContext),
    createImage: vi.fn(() => wallpaperImage),
    requestAnimationFrame: vi.fn(() => 1),
    cancelAnimationFrame: vi.fn(),
  };
  let queryAttempt = 0;
  const createSelectorQuery = vi.fn(() => ({
    select: vi.fn(() => ({ fields: vi.fn() })),
    selectAll: vi.fn(() => ({ fields: vi.fn() })),
    exec: vi.fn((callback: (results: any[]) => void) => {
      queryAttempt += 1;
      callback(queryAttempt === 1
        ? [{ node: undefined, width: 0, height: 0 }, []]
        : queryAttempt === 2
          ? [{ node, width: 390, height: 844 }, []]
          : [
          { node, width: 390, height: 844 },
          [{ left: 20, top: 120, width: 320, height: 100, dataset: { glassRole: 'primary' } }],
        ]);
    }),
  }));
  Object.assign(harness.taro, {
    nextTick: (callback: () => void) => callback(),
    createSelectorQuery,
    createOffscreenCanvas: () => ({ getContext: () => sourceContext }),
    getWindowInfo: () => ({ pixelRatio: 1.5 }),
    eventCenter: { on: vi.fn(), off: vi.fn() },
    onWindowResize: vi.fn(),
    offWindowResize: vi.fn(),
  });

  DynamicGlassCanvas({ theme: 'light', reduced: false, pageKey: 'functions' });
  const cleanup = harness.effects.at(-1)?.();

  expect(createSelectorQuery).toHaveBeenCalledTimes(1);
  vi.runAllTimers();
  expect(createSelectorQuery).toHaveBeenCalledTimes(3);

  wallpaperImage.onload();
  expect(visibleContext.putImageData).toHaveBeenCalled();

  cleanup?.();
  vi.useRealTimers();
});
