import { describe, expect, it } from 'vitest';
import { cardDrop } from './companionCard';

describe('状态卡躲藏落点', () => {
  it('横向越过两侧时扒住对应边缘', () => {
    expect(cardDrop(-15, 0, 320)).toBe('left');
    expect(cardDrop(260, 0, 320)).toBe('right');
  });
  it('向下拖到卡片时藏在上沿，向上拉走就脱离', () => {
    expect(cardDrop(120, 35, 320)).toBe('top');
    expect(cardDrop(120, -45, 320)).toBe(null);
    expect(cardDrop(120, 180, 320)).toBe(null);
  });
  it('坐标无效时不进入无法拖出的躲藏状态', () => {
    expect(cardDrop(Number.NaN, 35, 320)).toBe(null);
    expect(cardDrop(120, 35, 0)).toBe(null);
  });
});
