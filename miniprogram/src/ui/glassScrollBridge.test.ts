import { expect, test, vi } from 'vitest';

vi.mock('@tarojs/taro', () => ({ default: { eventCenter: { trigger: vi.fn() } } }));

import {
  createGlassFrameScheduler,
  normalizeGlassScroll,
  viewportRectsToDocument,
} from './glassScrollBridge';

test('coalesces repeated scroll notifications into one frame using the latest value', () => {
  let callback: (() => void) | undefined;
  const draw = vi.fn();
  const request = vi.fn((next: () => void) => { callback = next; return 7; });
  const cancel = vi.fn();
  const scheduler = createGlassFrameScheduler(draw, request, cancel);

  scheduler.update(20);
  scheduler.update(40);
  scheduler.update(80);

  expect(request).toHaveBeenCalledOnce();
  callback?.();
  expect(draw).toHaveBeenCalledExactlyOnceWith(80);
  scheduler.stop();
  expect(cancel).not.toHaveBeenCalled();
});

test('stops a pending frame when the page hides', () => {
  const scheduler = createGlassFrameScheduler(vi.fn(), () => 11, vi.fn());
  scheduler.update(20);
  const cancel = vi.fn();
  const cancellable = createGlassFrameScheduler(vi.fn(), () => 13, cancel);
  cancellable.update(30);
  cancellable.stop();
  expect(cancel).toHaveBeenCalledExactlyOnceWith(13);
  scheduler.stop();
});

test('a canceled callback cannot draw after the scheduler has stopped', () => {
  let callback: (() => void) | undefined;
  const draw = vi.fn();
  const cancel = vi.fn();
  const scheduler = createGlassFrameScheduler(draw, next => { callback = next; return 17; }, cancel);

  scheduler.update(60);
  scheduler.stop();
  callback?.();

  expect(cancel).toHaveBeenCalledExactlyOnceWith(17);
  expect(draw).not.toHaveBeenCalled();
});

test('normalizes invalid internal scroll values and caches document coordinates', () => {
  expect(normalizeGlassScroll(Number.NaN)).toBe(0);
  expect(normalizeGlassScroll(-12)).toBe(0);
  expect(normalizeGlassScroll(28.5)).toBe(28.5);
  expect(viewportRectsToDocument([{ left: 10, top: 40, width: 200, height: 80 }], 30))
    .toEqual([{ left: 10, top: 70, width: 200, height: 80 }]);
});
