import test from 'node:test';
import assert from 'node:assert/strict';
import { buildWorkMenu, summarizeWorkJobs } from './libraryModel.js';

test('work menu exposes all Luo Shui actions', () => {
  assert.deepEqual(buildWorkMenu({ id: 'w1' }).map(item => item.key), ['open', 'rename', 'copy', 'export', 'workspace', 'mark', 'delete']);
});

test('running and failed jobs remain visible on work cards', () => {
  assert.deepEqual(summarizeWorkJobs([{ status: 'running' }, { status: 'failed' }]), { running: 1, failed: 1, queued: 0 });
});
