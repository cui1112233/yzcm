import test from 'node:test';
import assert from 'node:assert/strict';
import { diffScriptLines, groupScriptsByEpisode } from './scriptCompare.js';

test('line diff keeps insert delete and equal groups', () => {
  assert.deepEqual(diffScriptLines('A\nB', 'A\nC').map(item => item.type), ['equal', 'delete', 'insert']);
});

test('scripts group by episode with adjacent versions', () => {
  const groups = groupScriptsByEpisode([{id:'b',episode:2,version:1},{id:'a2',episode:1,version:2},{id:'a1',episode:1,version:1}]);
  assert.deepEqual(groups.map(group => group.scripts.map(row => row.id)), [['a1','a2'],['b']]);
});
