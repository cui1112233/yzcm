import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const componentPath = path.join(root, 'user/components/RoleEnergyIcon.jsx');

test('uses one compact brand-derived icon component for all four formal roles', () => {
  assert.equal(fs.existsSync(componentPath), true, 'RoleEnergyIcon.jsx must exist');
  const source = fs.readFileSync(componentPath, 'utf8');
  for (const role of ['开发创始', '开发者', '管理者', '组员']) {
    assert.match(source, new RegExp(role));
  }
  assert.match(source, /role-energy-icon/);
  assert.match(source, /size\s*=\s*26/);
  assert.match(source, /<img/);
  assert.doesNotMatch(source, /<svg|linearGradient|clipPath/);
  assert.doesNotMatch(source, /Crown|ShieldCheck|BadgeCheck/);
});
