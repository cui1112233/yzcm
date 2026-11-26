import assert from 'node:assert/strict';
import test from 'node:test';
import { filterAndSortWorks } from './scriptEditorState.js';

test('剧本编辑筛选排序不修改接口原数组', () => {
  const rows = [{ id: '2', title: '乙', type: 'script', updatedAt: '2026-01-02' }, { id: '1', title: '甲', type: 'novel', updatedAt: '2026-01-01' }];
  const snapshot = structuredClone(rows);
  assert.deepEqual(filterAndSortWorks(rows, { type: 'novel', sort: 'name-asc' }).map(x => x.id), ['1']);
  assert.deepEqual(rows, snapshot);
});

test('剧本编辑可按标题搜索并按更新时间倒序', () => {
  const rows = [{ id: '1', title: '雨夜', updatedAt: '2026-01-01' }, { id: '2', title: '雨夜归人', updatedAt: '2026-01-03' }, { id: '3', title: '白昼', updatedAt: '2026-01-04' }];
  assert.deepEqual(filterAndSortWorks(rows, { query: '雨夜', sort: 'updated-desc' }).map(x => x.id), ['2', '1']);
});
