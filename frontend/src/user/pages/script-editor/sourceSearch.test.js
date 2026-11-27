import test from 'node:test';
import assert from 'node:assert/strict';
import { buildSearchExcerpt, referencedChapterCounts } from './sourceSearch.js';

test('search excerpt keeps the matching text', () => {
  assert.equal(buildSearchExcerpt('他在深夜月台等待列车', 4, 6, 4).includes('月台'), true);
});

test('referenced counts use stable chapter ids', () => {
  assert.deepEqual(referencedChapterCounts([{ sourceChapterIds:['c1','c2'] }, { sourceChapterIds:['c1'] }]), { c1:2, c2:1 });
});
