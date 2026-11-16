import { expect, test } from 'vitest';
import { loadTaskSummary } from './taskSummary';
test('task center distinguishes fetch, real Agent tasks, batch summaries and per-project Shuihuo tasks', async () => {
  const payloads: Record<string, unknown> = {
    '/api/history': { entries: [] }, '/api/studio/runs': { runs: [{ id: 'a', title: 'Agent' }] },
    '/api/batch-rewrite/tasks': { tasks: [{ id: 'f', title: 'Fetch' }] },
    '/api/batch-factory/v12/batches/summary': { batches: [{ id: 'b', title: 'Batch' }] },
    '/api/shuihuo-production/projects': { projects: [{ id: 7, name: 'Water' }] },
    '/api/shuihuo-production/projects/7/tasks': { tasks: [{ id: 9, kind: 'video', status: 'queued' }] },
  };
  const result = await loadTaskSummary(async path => { if (!(path in payloads)) throw new Error('Unexpected route'); return payloads[path]; });
  expect(result.sources.every(source => source.state === 'ready')).toBe(true);
  expect(result.entries.map(entry => entry.source)).toEqual(['agent', 'fetch', 'batch', 'shuihuo']);
  expect(result.entries.find(entry => entry.source === 'shuihuo')).toMatchObject({ projectId: '7', status: 'running' });
});
