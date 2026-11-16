import { expect, test } from "vitest";
import { uiPalette } from "./tokens";

function luminance(hex: string) {
  const rgb = hex.slice(1).match(/../g)!.map(value => parseInt(value, 16) / 255)
    .map(value => value <= .04045 ? value / 12.92 : ((value + .055) / 1.055) ** 2.4);
  return rgb[0] * .2126 + rgb[1] * .7152 + rgb[2] * .0722;
}
function contrast(a: string, b: string) {
  const values = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (values[0] + .05) / (values[1] + .05);
}

for (const theme of ["light", "dark"] as const) {
  test(`${theme}: actual UI foreground pairs remain readable on both page and surface`, () => {
    const palette = uiPalette(theme);
    for (const foreground of [palette.text, palette.textMuted, palette.success, palette.warning, palette.error, palette.info]) {
      for (const background of [palette.canvas, palette.surface]) expect(contrast(foreground, background)).toBeGreaterThanOrEqual(4.5);
    }
    expect(contrast(palette.onAccent, palette.accent)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(palette.accent, palette.accentSoft)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(palette.textMuted, palette.surfaceRaised)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(palette.disabledText, palette.disabledSurface)).toBeGreaterThanOrEqual(4.5);
    for (const background of [palette.canvas, palette.surface]) expect(contrast(palette.controlBorder, background)).toBeGreaterThanOrEqual(3);
  });
}
