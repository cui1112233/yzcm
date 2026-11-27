import test from 'node:test';
import assert from 'node:assert/strict';
import { parseStoryboardJson, summarizeCoverage } from './storyboardModel.js';

test('invalid JSON preserves the last valid storyboard', () => {
  const previous = [{ id: 'shot-1' }];
  const result = parseStoryboardJson('{bad', previous);
  assert.equal(result.valid, false);
  assert.deepEqual(result.value, previous);
});

test('coverage summary counts ranges', () => {
  assert.deepEqual(summarizeCoverage({ covered: [{ start: 0, end: 20 }], missing: [{ start: 20, end: 30 }], duplicate: [] }), { covered: 20, missing: 10, duplicate: 0 });
});
