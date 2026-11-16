import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { revealClipRadius, revealRadius } from './themeReveal';

describe('主题圆形覆盖范围', () => {
  it('从右上方开关展开后覆盖最远的左下角', () => {
    const radius=revealRadius(350,110,390,780);
    expect(radius).toBeGreaterThanOrEqual(Math.hypot(350,670));
    expect(radius).toBeLessThan(Math.hypot(350,670)+3);
  });
  it('居中时覆盖所有四角，反向收回使用同一半径', () => {
    expect(revealRadius(195,390,390,780)).toBe(Math.ceil(Math.hypot(195,390))+1);
  });
  it('等待首帧后再向两种目标主题展开', () => {
    expect(revealClipRadius('dark', false, 640)).toBe(0);
    expect(revealClipRadius('dark', true, 640)).toBe(640);
    expect(revealClipRadius('light', false, 640)).toBe(0);
    expect(revealClipRadius('light', true, 640)).toBe(640);
  });
  it('预览层使用目标主题且底层保持当前主题', () => {
    const source = readFileSync(new URL('../theme.tsx', import.meta.url), 'utf8');
    expect(source).toContain('const baseTheme=value.theme');
    expect(source).toContain('theme:reveal?.next ?? value.theme');
    expect(source).toContain('themeClassName(reveal.next)');
    expect(source).toContain('uiVariables(reveal.next,variant)');
    expect(source).toContain('<LiquidGlassMaterial theme={reveal.next}');
    expect(source).toContain('<DynamicGlassCanvas theme={reveal.next}');
    expect(source).not.toContain("theme:'dark' as const");
  });
});
