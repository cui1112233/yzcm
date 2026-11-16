import Taro from '@tarojs/taro';
import type { GlassSurfaceRect } from './glassRefraction';

export const GLASS_INTERNAL_SCROLL = 'yzcm:glass-internal-scroll';
export const GLASS_LAYOUT_CHANGE = 'yzcm:glass-layout-change';

export function normalizeGlassScroll(value: number) {
  return Number.isFinite(value) && value > 0 ? value : 0;
}

export function viewportRectsToDocument(rects: GlassSurfaceRect[], scrollTop: number) {
  const offset = normalizeGlassScroll(scrollTop);
  return rects.map(rect => ({ ...rect, top: rect.top + offset }));
}

export function publishGlassScroll(pageKey: string, scrollTop: number) {
  Taro.eventCenter?.trigger(GLASS_INTERNAL_SCROLL, { pageKey, scrollTop: normalizeGlassScroll(scrollTop) });
}

export function publishGlassLayout(pageKey: string) {
  Taro.eventCenter?.trigger(GLASS_LAYOUT_CHANGE, { pageKey });
}

export function createGlassFrameScheduler(
  draw: (scrollTop: number) => void,
  requestFrame: (callback: () => void) => number,
  cancelFrame: (frame: number) => void,
) {
  let frame: number | undefined;
  let latestScroll = 0;
  let generation = 0;
  const update = (scrollTop: number) => {
    latestScroll = normalizeGlassScroll(scrollTop);
    if (frame !== undefined) return;
    const expected = generation;
    frame = requestFrame(() => {
      if (expected !== generation) return;
      frame = undefined;
      draw(latestScroll);
    });
  };
  return {
    update,
    redraw: () => update(latestScroll),
    stop() {
      generation += 1;
      if (frame !== undefined) cancelFrame(frame);
      frame = undefined;
    },
  };
}
