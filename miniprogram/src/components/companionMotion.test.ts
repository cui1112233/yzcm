import { describe, expect, it } from 'vitest';
import { greetingAt, boundPosition, releasePose, edgePeekPosition } from './companionMotion';

describe('首页宠物与状态卡', () => {
  it('按设备本地时间问候，凌晨不提示开工', () => {
    expect(greetingAt(3)).toContain('休息');
    expect(greetingAt(9)).toContain('早上');
    expect(greetingAt(15)).toContain('下午');
    expect(greetingAt(21)).toContain('晚上');
  });
  it('窄屏拖动不会把宠物完全拖出屏幕', () => {
    expect(boundPosition(-500, 340)).toBe(-38.5);
    expect(boundPosition(900, 340)).toBe(306.5);
  });
  it.each([320, 375, 390, 430])('在 %ipx 屏幕上按探头身体裁切边吸附，而不是按图片外框定位', viewport => {
    const scale = viewport / 750;
    const width = 680 * scale;
    const origin = 35 * scale; // 状态卡外边距和边框
    expect(origin + edgePeekPosition(-10, width) + 42 * scale).toBeCloseTo(0);
    expect(origin + edgePeekPosition(width - 60, width) + 102 * scale).toBeCloseTo(viewport);
  });
  it('边缘放手探头，中间放手恢复站立', () => {
    expect(releasePose(-10, 280)).toBe('peek');
    expect(releasePose(230, 280)).toBe('peek');
    expect(releasePose(120, 280)).toBe('idle');
  });
});
