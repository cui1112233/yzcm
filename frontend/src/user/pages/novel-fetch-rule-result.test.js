import assert from 'node:assert/strict';
import test from 'node:test';
import { describeRuleApplyResult } from './novel-fetch-rule-result.js';

test('partial rule failures never report complete success', () => {
  assert.deepEqual(describeRuleApplyResult({ updated: 1, failed: 1, skipped: 2 }, '已完成'), {
    type: 'warning', message: '规则处理：已保存 1 本，含失败 1 本，跳过 2 本'
  });
});

test('a version failure overrides a misleading successful book count', () => {
  const result = describeRuleApplyResult({ updated: 1, failed: 0, version_results: [
    { book_id: '123', version_id: 'ai1', status: 'failed', error: '保存失败' }
  ] }, '已完成');
  assert.equal(result.type, 'warning');
  assert.match(result.message, /失败/);
});

test('no successful book and a failed version reports error', () => {
  assert.equal(describeRuleApplyResult({ updated: 0, failed: 1 }, '已完成').type, 'error');
});

test('complete success retains the requested UI copy', () => {
  assert.deepEqual(describeRuleApplyResult({ updated: 2, failed: 0 }, '已完成全部任务规则处理'), {
    type: 'success', message: '已完成全部任务规则处理'
  });
});

test('empty or malformed outcomes do not invent success', () => {
  assert.equal(describeRuleApplyResult(null, '已完成').type, 'warning');
  assert.equal(describeRuleApplyResult({ updated: 0, failed: 0 }, '已完成').type, 'warning');
});

test('several failed versions of the same book count as one affected book', () => {
  const result = describeRuleApplyResult({ updated: 1, version_results: [
    { book_id: '123', version_id: 'ai1', status: 'success' },
    { book_id: '123', version_id: 'ai2', status: 'failed' },
    { book_id: '123', version_id: 'ai3', status: 'failed' }
  ] }, '已完成');
  assert.equal(result.type, 'warning');
  assert.equal(result.message, '规则处理：已保存 1 本，含失败 1 本，跳过 0 本');
});

test('all skipped tasks are explained without claiming success', () => {
  assert.deepEqual(describeRuleApplyResult({ updated: 0, failed: 0, skipped: 2 }, '已完成'), {
    type: 'warning', message: '规则处理：跳过 2 本，没有任务完成处理'
  });
});
