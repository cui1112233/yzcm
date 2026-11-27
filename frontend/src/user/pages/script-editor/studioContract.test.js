import test from 'node:test';
import assert from 'node:assert/strict';
import { SCRIPT_EDITOR_RAIL_ITEMS, resolveStudioLocation, buildStudioSearch } from './studioContract.js';

test('rail covers the complete Luo Shui studio with CM Canvas naming', () => {
  assert.deepEqual(
    SCRIPT_EDITOR_RAIL_ITEMS.map(item => item.key),
    ['source', 'scripts', 'agent', 'assets', 'storyboard', 'canvas', 'production', 'delivery', 'settings'],
  );
  assert.equal(SCRIPT_EDITOR_RAIL_ITEMS.find(item => item.key === 'canvas').label, 'CM 画布');
});

test('location restores the work tab and selected item', () => {
  const value = resolveStudioLocation('?workId=w1&tab=scripts&itemId=s2', { id: 'w1', type: 'novel' });
  assert.deepEqual(value, { workId: 'w1', tab: 'scripts', itemId: 's2' });
  assert.equal(buildStudioSearch(value), '?workId=w1&tab=scripts&itemId=s2');
});

test('location defaults to scripts for script works and rejects unknown tabs', () => {
  assert.equal(resolveStudioLocation('', { id: 'w1', type: 'script' }).tab, 'scripts');
  assert.equal(resolveStudioLocation('?tab=unknown', { id: 'w1', type: 'novel' }).tab, 'source');
});
