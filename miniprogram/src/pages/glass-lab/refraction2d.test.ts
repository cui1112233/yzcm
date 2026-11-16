import { expect, test, vi } from 'vitest';
import { drawLens } from './refraction2d';

function context() {
  return Object.fromEntries(['save','restore','beginPath','moveTo','lineTo','quadraticCurveTo','closePath','clip','drawImage','fillRect','stroke'].map(key=>[key,vi.fn()])) as any;
}
test('samples only the bounded optical rim and keeps text outside the bitmap',()=>{
  const c=context(),source={};
  drawLens(c,source,{x:22,y:151,width:346,height:118},true);
  expect(c.drawImage).toHaveBeenCalledTimes(48);
  for(const call of c.drawImage.mock.calls){
    const [image,sx,sy,sw,sh,dx,dy,dw,dh]=call;
    expect(image).toBe(source);
    expect(sx).toBeGreaterThanOrEqual(22);expect(sy).toBeGreaterThanOrEqual(151);
    expect(sx+sw).toBeLessThanOrEqual(368);expect(sy+sh).toBeLessThanOrEqual(269);
    expect(dx+dw).toBeLessThanOrEqual(368);expect(dy+dh).toBeLessThanOrEqual(269);
  }
  expect(c.clip).toHaveBeenCalledOnce();expect(c.restore).toHaveBeenCalledOnce();
});
test('comparison mode keeps the surface but performs no displaced sampling',()=>{
  const c=context();drawLens(c,{}, {x:22,y:151,width:346,height:118},false);
  expect(c.drawImage).not.toHaveBeenCalled();expect(c.fillRect).toHaveBeenCalledOnce();
});
