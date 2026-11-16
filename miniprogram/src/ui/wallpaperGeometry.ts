export type WallpaperSourceRect = {
  sx: number;
  sy: number;
  sw: number;
  sh: number;
};

const zeroRect = (): WallpaperSourceRect => ({ sx: 0, sy: 0, sw: 0, sh: 0 });

export function aspectFillSourceRect(
  sourceWidth: number,
  sourceHeight: number,
  targetWidth: number,
  targetHeight: number,
): WallpaperSourceRect {
  if (![sourceWidth, sourceHeight, targetWidth, targetHeight].every(value => Number.isFinite(value) && value > 0)) {
    return zeroRect();
  }

  const sourceRatio = sourceWidth / sourceHeight;
  const targetRatio = targetWidth / targetHeight;
  if (Math.abs(sourceRatio - targetRatio) < Number.EPSILON) {
    return { sx: 0, sy: 0, sw: sourceWidth, sh: sourceHeight };
  }
  if (sourceRatio > targetRatio) {
    const sw = sourceHeight * targetRatio;
    return { sx: (sourceWidth - sw) / 2, sy: 0, sw, sh: sourceHeight };
  }
  const sh = sourceWidth / targetRatio;
  return { sx: 0, sy: (sourceHeight - sh) / 2, sw: sourceWidth, sh };
}
