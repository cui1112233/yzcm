import assert from 'node:assert/strict';
import test from 'node:test';

import { formatMergeJobSpeed, mergeJobDurationSeconds } from './batchFactoryMergePresentation.js';

test('合成倍率保留三位小数，不把 1.036552 显示成 1.0', () => {
  assert.equal(formatMergeJobSpeed({ speed: 1.036552 }), '1.037x');
  assert.equal(formatMergeJobSpeed({ speed: 1 }), '1.000x');
});

test('旧合成记录缺少成片时长时，用片段真实时长除以倍率还原', () => {
  assert.equal(mergeJobDurationSeconds({
    speed: 1.036552,
    sources: [{ actualDurationSeconds: 8.1 }, { actualDurationSeconds: 7.1 }],
  }).toFixed(2), '14.66');
});

test('新合成记录优先使用保存的配音目标时长', () => {
  assert.equal(mergeJobDurationSeconds({
    audioDurationSeconds: 15.14,
    speed: 1.004,
    sources: [{ actualDurationSeconds: 8.1 }, { actualDurationSeconds: 7.1 }],
  }), 15.14);
});
