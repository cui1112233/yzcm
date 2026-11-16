// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { beforeEach, afterEach, expect, test, vi } from 'vitest';
vi.mock('@tarojs/taro', () => ({ default: { getSystemInfoSync: () => ({ windowWidth: 375 }), getStorageSync: () => false }, useDidHide: () => {}, useDidShow: () => {} }));
vi.mock('@tarojs/components', () => ({
  View: ({ children, catchMove, ariaLabel, onTouchStart, onTouchMove, onTouchEnd, onTouchCancel, ...props }: any) => <div {...props} aria-label={ariaLabel} onTouchStart={onTouchStart} onTouchMove={onTouchMove} onTouchEnd={onTouchEnd} onTouchCancel={onTouchCancel}>{children}</div>,
  Image: ({ mode, ...props }: any) => <img {...props} />,
}));
import Companion from './Companion';
let container: HTMLDivElement, root: Root;
beforeEach(() => { vi.useFakeTimers(); vi.stubGlobal('IS_REACT_ACT_ENVIRONMENT', true); container = document.createElement('div'); root = createRoot(container); });
afterEach(async () => { await act(async () => root.unmount()); vi.useRealTimers(); vi.unstubAllGlobals(); });
const render = async (parked = false) => { await act(async () => root.render(<Companion parked={parked} />)); };
const advance = async (ms: number) => { await act(async () => vi.advanceTimersByTimeAsync(ms)); };
const touch = async (type: string, x: number, y: number) => {
  const event = new Event(type, { bubbles: true, cancelable: true });
  Object.defineProperty(event, 'touches', { value: [{ clientX: x, clientY: y }] });
  await act(async () => (container.querySelector('.home-companion') as HTMLElement).dispatchEvent(event));
};
test('autonomous walk reaches the screen edge and stops walking to peek on arrival', async () => {
  await render(); await advance(6500);
  expect(container.querySelector('.companion-walk')).not.toBeNull();
  const pet = container.querySelector('.home-companion') as HTMLElement;
  expect(pet.style.transform).toBe('translate3d(-38.5px,0px,0)');
  const duration = parseFloat(pet.style.transitionDuration);
  await advance(duration);
  expect(container.querySelector('.companion-peek')).not.toBeNull();
  expect(container.querySelector('.companion-walk')).toBeNull();
  await advance(6500);
  expect(container.querySelector('.companion-walk')).not.toBeNull();
  expect(pet.style.transform).toBe('translate3d(170px,0px,0)');
  await advance(duration);
  expect(container.querySelector('.companion-idle')).not.toBeNull();
});
test('collapsing the header stops stepping and does not resume a walk at its old destination', async () => {
  await render(); await advance(6500); await render(true);
  expect(container.querySelector('.companion-walk')).toBeNull();
  await render(false);
  expect(container.querySelector('.companion-walk')).toBeNull();
});
test('dragging horizontally uses the walking sprite instead of sliding in the idle pose', async () => {
  await render();
  await touch('touchstart', 210, 20);
  await touch('touchmove', 270, 20);
  expect(container.querySelector('.companion-walk')).not.toBeNull();
  expect(container.querySelector('.companion-idle')).toBeNull();
});
