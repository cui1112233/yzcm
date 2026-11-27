import test from 'node:test';
import assert from 'node:assert/strict';
import { jobProgressView, shouldPollScriptEditorJobs } from './scriptEditorJobs.js';

test('partial generation clearly reports saved and failed episodes', () => {
  assert.deepEqual(jobProgressView({
    type: 'script-generation', status: 'partial', progressCurrent: 2, progressTotal: 3,
    result: { completedEpisodes: [1, 2], failedEpisodes: [{ episode: 3, error: 'timeout' }] },
  }), { tone: 'warning', label: '已保存2集，1集失败', retryable: true });
});

test('only active jobs keep polling', () => {
  assert.equal(shouldPollScriptEditorJobs([{ status: 'succeeded' }, { status: 'failed' }]), false);
  assert.equal(shouldPollScriptEditorJobs([{ status: 'queued' }]), true);
  assert.equal(shouldPollScriptEditorJobs([{ status: 'running' }]), true);
});
