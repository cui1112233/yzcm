import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(path.join(here, 'ApiPresetSelectionModal.jsx'), 'utf8');
const restricted = source.slice(
  source.indexOf('function RestrictedYZCMPresetModal'),
  source.indexOf('// 「自定义预设」弹窗')
);

test('restricted modal does not search before open and discards stale responses', () => {
  assert.match(restricted, /if \(!open \|\| !onSearch\) return undefined/);
  assert.match(restricted, /requestRef\.current !== requestId/);
  assert.match(restricted, /setTimeout\([\s\S]+250\)/);
});

test('restricted modal supports all five YZCM kinds without rendering addresses', () => {
  for (const kind of ['text', 'image', 'video', 'audio', 'asr']) assert.match(source, new RegExp(`value: '${kind}'`));
  assert.doesNotMatch(restricted, /baseUrl|endpoint|provider|API 格式/);
  assert.match(restricted, /model\.modelId/);
});

test('restricted modal keeps selected rows independently from each search page', () => {
  assert.match(restricted, /setKnown\(current => mergeYZCMSelectionModels\(current, safe\.models\)\)/);
  assert.match(restricted, /const selectedRows = pending\.map/);
  assert.match(restricted, /onConfirm\(selectedRows\)/);
});
