import { execFile } from 'node:child_process';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { basename, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { promisify } from 'node:util';

const execFileAsync = promisify(execFile);
const width = 780;
const height = 1500;
const rotationDegrees = -21.772396;

const themes = {
  light: {
    gradient: [[0, '#e5f3ed'], [.42, '#c0e5e4'], [1, '#6eafc3']],
    curves: ['#6fa7bb', '#edfff1', '#86c7cf', '#386a9b'],
    glow: '#ffffff70',
  },
  dark: {
    gradient: [[0, '#173a4b'], [.4, '#29576a'], [1, '#132c48']],
    curves: ['#478b97', '#a0d6d2', '#2b667f', '#193d62'],
    glow: '#bfeeff14',
  },
};

export function materialStudySvg(dark) {
  const palette = dark ? themes.dark : themes.light;
  const gradientStops = palette.gradient
    .map(([offset, color]) => `<stop offset="${offset}" stop-color="${color}"/>`)
    .join('');
  const ellipses = palette.curves
    .map((color, index) => `<ellipse cx="${90 - index * 18}" cy="670" rx="${420 + index * 67}" ry="${1020 + index * 56}" fill="none" stroke="${color}" stroke-width="${62 - index * 5}"/>`)
    .join('');

  return `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}"><defs><linearGradient id="base" x1="0" y1="0" x2="0" y2="${height}" gradientUnits="userSpaceOnUse">${gradientStops}</linearGradient><radialGradient id="glow" cx="170" cy="160" r="700" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="${palette.glow}"/><stop offset="1" stop-color="#ffffff00"/></radialGradient></defs><rect width="${width}" height="${height}" fill="url(#base)"/><g transform="translate(150 0) rotate(${rotationDegrees} 0 0)">${ellipses}</g><rect width="${width}" height="${height}" fill="url(#glow)"/></svg>`;
}

async function writeWebp(svg, outputPath, temporaryDirectory) {
  const htmlPath = join(temporaryDirectory, `${basename(outputPath, '.webp')}.html`);
  const svgUrl = `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
  const html = `<!doctype html><meta charset="utf-8"><canvas width="${width}" height="${height}"></canvas><script>const image=new Image();image.onload=()=>{const canvas=document.querySelector('canvas');canvas.getContext('2d').drawImage(image,0,0);document.body.textContent=canvas.toDataURL('image/webp',.95)};image.src=${JSON.stringify(svgUrl)};</script>`;
  await writeFile(htmlPath, html);
  const { stdout } = await execFileAsync('/Applications/Google Chrome.app/Contents/MacOS/Google Chrome', [
    '--headless=new',
    '--disable-gpu',
    '--no-sandbox',
    '--virtual-time-budget=2000',
    '--dump-dom',
    pathToFileURL(htmlPath).href,
  ], { maxBuffer: 8 * 1024 * 1024 });
  const encoded = stdout.match(/data:image\/webp;base64,([A-Za-z0-9+/=]+)/)?.[1];
  if (!encoded) throw new Error('Chrome did not return a WebP canvas payload');
  await writeFile(outputPath, Buffer.from(encoded, 'base64'));
}

export async function writeMaterialStudyWallpapers() {
  const outputDirectory = fileURLToPath(new URL('../src/assets/glass/', import.meta.url));
  const temporaryDirectory = await mkdtemp(join(tmpdir(), 'material-study-wallpaper-'));
  try {
    await Promise.all([false, true].map(dark => writeWebp(
      materialStudySvg(dark),
      join(outputDirectory, `wallpaper-${dark ? 'dark' : 'light'}.webp`),
      temporaryDirectory,
    )));
  } finally {
    await rm(temporaryDirectory, { recursive: true, force: true });
  }
}

const isDirectRun = process.argv[1]
  && import.meta.url === pathToFileURL(process.argv[1]).href;

if (isDirectRun) await writeMaterialStudyWallpapers();
