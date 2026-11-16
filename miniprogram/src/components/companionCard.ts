export type CardSide = 'top' | 'left' | 'right';

/** Coordinates are relative to the status card, in logical pixels. */
export function cardDrop(x: number, downward: number, width: number): CardSide | null {
  if (![x, downward, width].every(Number.isFinite) || width <= 0) return null;
  if (downward < -25 || downward > 110) return null;
  if (x < 8) return 'left';
  if (x > width - 80) return 'right';
  return downward > 20 ? 'top' : null;
}
