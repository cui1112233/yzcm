import test from 'node:test';
import assert from 'node:assert/strict';
import { novelFetchLatestJob, novelFetchProgressLines, novelFetchJobFinished, novelFetchActionNotice } from './novel-fetch-progress.js';

test('stopped and cancelled jobs end waiting without pretending completion', () => {
  for (const status of ['done', 'failed', 'stopped', 'cancelled', 'canceled', 'completed', ' STOPPED ', 'waiting_retry', 'waiting_classification', 'waiting_configuration', 'waiting_login', 'pending_confirmation', 'manual_only']) {
    assert.equal(novelFetchJobFinished({ status }), true, status);
  }
  for (const status of ['queued', 'running', 'processing', '']) {
    assert.equal(novelFetchJobFinished({ status }), false, status);
  }
});

test('background generation reports queued rather than generated', () => {
  assert.deepEqual(novelFetchActionNotice({ queued: true, job_id: 'job-1' }, '缺少的 AI 版本已生成'), {
    type: 'info', message: '已进入后台生成队列'
  });
  assert.deepEqual(novelFetchActionNotice({ queued: true, message: '已进入后台生成队列' }, '操作完成'), {
    type: 'info', message: '已进入后台生成队列'
  });
  assert.deepEqual(novelFetchActionNotice({ message: '所需版本已齐' }, '操作完成'), {
    type: 'success', message: '所需版本已齐'
  });
});

test('one deferred book does not finish a batch whose queue is still running', () => {
  assert.equal(novelFetchJobFinished({ status: 'waiting_retry', queue_state: 'running' }), false);
  assert.equal(novelFetchJobFinished({ status: 'failed', queue_state: 'running' }), false);
  assert.equal(novelFetchJobFinished({ status: 'done', queue_state: 'processing' }), false);
  assert.equal(novelFetchJobFinished({ status: 'running', queue_state: 'waiting_retry' }), true);
  assert.equal(novelFetchJobFinished({ status: 'waiting_retry', queue_state: 'waiting_retry' }), true);
});

test('formats live progress with time, book name, id and message', () => {
  const lines = novelFetchProgressLines({
    progress: [
      { time: '2026-10-18T03:04:05Z', book_id: '12345678901', book_name: '测试书名', message: '开始处理' },
      { time: '2026-10-18T03:04:06Z', book_id: '12345678901', book_name: '测试书名', message: '处理完成' }
    ]
  });
  assert.equal(lines.length, 2);
  assert.match(lines[0], /测试书名/);
  assert.match(lines[0], /12345678901/);
  assert.match(lines[0], /开始处理/);
  assert.match(lines[1], /处理完成/);
});

test('falls back to job state when no progress entries exist', () => {
  assert.deepEqual(novelFetchProgressLines({ status: 'queued' }), ['任务已进入队列，等待执行。']);
  assert.deepEqual(novelFetchProgressLines(null), ['暂无正在处理的任务。']);
});

test('shows queue verification failures instead of stale running progress', () => {
  const stale = { status: 'failed', status_reason: 'queue_missing', error: '原队列任务已不存在，请手动重试。', progress: [{ message: '正在处理旧任务' }] };
  assert.deepEqual(novelFetchProgressLines(stale), ['原队列任务已不存在，请手动重试。']);
  assert.deepEqual(novelFetchProgressLines({ status: 'queued', queue_check_error: '队列状态暂时无法核对，请稍后刷新。' }), ['队列状态暂时无法核对，请稍后刷新。']);
});

test('an empty latest response clears the previous live job', () => {
  assert.equal(novelFetchLatestJob({ latest: {}, jobs: [] }), null);
  assert.equal(novelFetchLatestJob({}), null);
  assert.deepEqual(novelFetchLatestJob({ latest: { id: 'live', status: 'running' } }), { id: 'live', status: 'running' });
});

test('older interrupted history never hides the latest live progress', () => {
  const lines = novelFetchProgressLines([
    { id: 'live', status: 'running', progress: [{ message: '当前书籍正在处理' }] },
    { id: 'old', status: 'failed', status_reason: 'queue_missing', error: '旧任务已中断' }
  ]);
  assert.equal(lines.length, 1);
  assert.match(lines[0], /当前书籍正在处理/);
});

test('aggregates live logs for every book in the current batch', () => {
  const lines = novelFetchProgressLines(
    { progress: [{ time: '2026-10-18T03:04:05Z', book_id: 'book-a', book_name: '第一本', message: '处理完成' }] },
    [
      { book_id: 'book-a', book_name: '第一本', logs: [{ time: '2026-10-18T03:04:05Z', detail: '处理完成' }] },
      { book_id: 'book-b', book_name: '第二本', logs: [{ time: '2026-10-18T03:04:06Z', detail: '开始处理' }] },
      { book_id: 'book-c', book_name: '第三本', logs: [{ time: '2026-10-18T03:04:07Z', detail: '处理完成' }] }
    ]
  );
  assert.equal(lines.some(line => line.includes('第一本')), true);
  assert.equal(lines.some(line => line.includes('第二本')), true);
  assert.equal(lines.some(line => line.includes('第三本')), true);
  assert.equal(lines.filter(line => line.includes('第一本') && line.includes('处理完成')).length, 1);
});

test('deduplicates logs that render identically across live and persisted shapes', () => {
  const lines = novelFetchProgressLines(
    { progress: [{ time: '2026-10-18T03:04:05Z', book_id: 'book-a', book_name: '第一本', message: '处理完成' }] },
    [{ book_id: 'book-a', book_name: '第一本', logs: [{ at: '2026-10-18T03:04:05Z', event: '处理完成' }] }]
  );
  assert.equal(lines.filter(line => line.includes('第一本') && line.includes('处理完成')).length, 1);
});

test('merges same-book persisted stage events with live progress in chronological order', () => {
  const lines = novelFetchProgressLines(
    { progress: [{ time: '2026-10-18T03:04:06Z', book_id: 'book-a', book_name: '第一本', message: '处理中' }] },
    [{ book_id: 'book-a', book_name: '第一本', logs: [
      { time: '2026-10-18T03:04:05Z', stage: 'fetch', status: 'started', event: 'fetch_started', detail: '开始获取原文' },
      { time: '2026-10-18T03:04:07Z', stage: 'sensitive_processing', status: 'completed', event: 'sensitive_processing_completed', detail: '敏感词处理结果已保存' }
    ] }]
  );
  assert.equal(lines.length, 3);
  assert.match(lines[0], /开始获取原文/);
  assert.match(lines[1], /处理中/);
  assert.match(lines[2], /敏感词处理结果已保存/);
});
