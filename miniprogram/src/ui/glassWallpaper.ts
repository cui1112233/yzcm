export function paintWallpaper(ctx: CanvasRenderingContext2D, width: number, height: number, dark: boolean) {
  const gradient = ctx.createLinearGradient(0, 0, width, height);
  (dark ? ['#193746', '#285668', '#122f46'] : ['#eaf4f0', '#bfdfdf', '#78aebf']).forEach((color, i) => gradient.addColorStop(i / 2, color));
  ctx.fillStyle = gradient; ctx.fillRect(0, 0, width, height);
  ctx.save(); ctx.translate(-width * .45, height * .18); ctx.rotate(-.3);
  const colors = dark ? ['rgba(49,88,111,.64)', 'rgba(136,187,191,.68)', 'rgba(66,115,135,.54)'] : ['rgba(119,165,184,.62)', 'rgba(239,255,243,.76)', 'rgba(150,203,209,.62)'];
  colors.forEach((color, i) => {
    ctx.beginPath(); ctx.ellipse(width * (.18 - i * .035), height * .72, width * (.72 + i * .1), height * (.78 + i * .04), 0, 0, Math.PI * 2);
    ctx.strokeStyle = color; ctx.lineWidth = width * (.062 - i * .005); ctx.stroke();
  });
  ctx.restore();
  const glow = ctx.createRadialGradient(width * .15, height * .12, 0, width * .15, height * .12, width * 1.4);
  glow.addColorStop(0, dark ? 'rgba(224,255,255,.08)' : 'rgba(255,255,255,.42)'); glow.addColorStop(1, 'rgba(255,255,255,0)');
  ctx.fillStyle = glow; ctx.fillRect(0, 0, width, height);
}
