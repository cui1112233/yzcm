import { drawBackgroundRefraction, roundedGlassPath } from '../../ui/glassRefraction';

export const roundedRect = roundedGlassPath;

// Direct background sampling; no PNG export or per-frame React updates.
export function drawLens(c: any, source: any, box: {x:number;y:number;width:number;height:number}, enabled: boolean) {
  drawBackgroundRefraction(c, source, { left: box.x, top: box.y, width: box.width, height: box.height }, { radius: 28, rim: 12, reduced: !enabled });
}
