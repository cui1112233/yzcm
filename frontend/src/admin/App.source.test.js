import assert from 'node:assert/strict';
import test from 'node:test';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const appSource = fs.readFileSync(path.join(here, 'App.jsx'), 'utf8');
const layoutSource = fs.readFileSync(path.join(here, '../shared/layouts/AdminLayout.jsx'), 'utf8');

test('旧后台账号地址进入正式账号中心', () => {
  assert.doesNotMatch(appSource, /AccountGovernancePage/);
  assert.match(appSource, /pathname === '\/admin\/accounts'/);
  assert.match(appSource, /window\.location\.replace\('\/accounts'\)/);
  assert.doesNotMatch(layoutSource, /href: '\/admin\/accounts'/);
  assert.match(layoutSource, /capabilities\?\.includes\('platform_admin'\)/);
});
