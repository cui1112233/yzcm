// Produces transparent, card-attached right-edge refraction layers at build time.
import { createRequire } from 'node:module';
import { mkdir, writeFile } from 'node:fs/promises';
import { refractGlass } from '../src/pages/functions/glassOptics.ts';

const require = createRequire(import.meta.url);
const sharp = require('sharp');
const out = new URL('../src/assets/glass/', import.meta.url);
await mkdir(out, { recursive: true });

for (const dark of [true, false]) {
  const fill = dark ? '#164155' : '#8bbac8';
  const shine = dark ? '#c9fbff' : '#f8ffff';
  const shade = dark ? '#387c91' : '#5595ad';
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="220" height="220"><rect width="220" height="220" fill="${fill}"/><path d="M176 -30C130 35 202 82 150 250" fill="none" stroke="${shine}" stroke-width="20"/><path d="M205 -30C160 36 220 106 179 250" fill="none" stroke="${shade}" stroke-width="24"/><path d="M143 -20C105 60 174 96 129 246" fill="none" stroke="#ffffff" stroke-opacity=".35" stroke-width="7"/></svg>`;
  const raw = await sharp(Buffer.from(svg)).ensureAlpha().raw().toBuffer();
  const rim = refractGlass({ width: 220, height: 220, data: new Uint8ClampedArray(raw) }, { left: 20, top: 20, width: 180, height: 180 }, 1, true);
  const image = await sharp(Buffer.from(rim.data), { raw: { width: 180, height: 180, channels: 4 } }).extract({ left: 115, top: 0, width: 65, height: 180 }).png().toBuffer();
  await writeFile(new URL(`right-refraction-${dark ? 'dark' : 'light'}.png`, out), image);
}
