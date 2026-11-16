import { afterEach, expect, test, vi } from 'vitest';
import { latestFrame } from './latestFrame';

afterEach(() => vi.useRealTimers());

test('120Hz input sends at most 30 updates per second and keeps the final position', () => {
  vi.useFakeTimers();
  const positions: number[] = [];
  const frame = latestFrame<number>(position => positions.push(position));
  for (let i = 0; i < 120; i++) {
    frame.push(i);
    vi.advanceTimersByTime(8);
  }
  frame.flush();
  expect(positions.length).toBeLessThanOrEqual(30);
  expect(positions[positions.length - 1]).toBe(119);
});

test('release flushes the final drag immediately with no delayed update afterwards', () => {
  vi.useFakeTimers();
  const update = vi.fn();
  const frame = latestFrame<number>(update);
  frame.push(10); frame.push(80); frame.flush();
  expect(update).toHaveBeenCalledExactlyOnceWith(80);
  vi.runAllTimers();
  expect(update).toHaveBeenCalledTimes(1);
});

test('leaving the page cancels pending work', () => {
  vi.useFakeTimers();
  const update = vi.fn();
  const frame = latestFrame<number>(update);
  frame.push(10); frame.cancel(); vi.runAllTimers();
  expect(update).not.toHaveBeenCalled();
});
