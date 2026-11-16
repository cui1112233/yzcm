import { beforeEach, expect, test, vi } from 'vitest';
const request = vi.hoisted(() => vi.fn());
vi.mock('./api', () => ({ apiRequest: request }));
import { recoverBatchCoverVideo } from './batchCoverMedia';
beforeEach(() => { request.mockReset(); });
test('recovers an existing successful provider video, ignoring unfinished tasks', async () => {
  request.mockResolvedValue({ jobs: [{ tasks: [{ status: 'running', mediaUrl: '/unfinished.mp4' }, { status: 'succeeded', mediaUrl: 'https://cdn.example/ready.mp4' }] }] });
  expect(await recoverBatchCoverVideo('batch/a')).toBe('https://cdn.example/ready.mp4');
  expect(request).toHaveBeenCalledWith('/api/batch-factory/v12/batches/batch%2Fa/status', { recoverSession: false });
});
test('uses the completed merged output as the work preview before an individual shot', async () => {
  request.mockImplementation(async (path: string) => path.endsWith('/merge-status')
    ? { jobs: [{ bookId: 'book-1', status: 'completed', outputUrl: '/merged.mp4' }] }
    : { jobs: [{ tasks: [{ status: 'succeeded', mediaUrl: '/shot.mp4' }] }] });
  expect(await recoverBatchCoverVideo('batch-1')).toBe('/merged.mp4');
  expect(request).toHaveBeenCalledWith('/api/batch-factory/v12/batches/batch-1/merge-status', { recoverSession: false });
});
test('does not turn a missing successful video into a made-up cover', async () => {
  request.mockResolvedValue({ jobs: [{ tasks: [{ status: 'failed', mediaUrl: '/failed.mp4' }] }] });
  expect(await recoverBatchCoverVideo('b')).toBe('');
});
test('limits recovery reads to two and skips obsolete queued work', async () => {
  const finish: Array<(v: unknown) => void> = [];
  request.mockImplementation((path: string) => path.endsWith('/merge-status')
    ? Promise.resolve({ jobs: [] })
    : new Promise(r => finish.push(r)));
  const first = recoverBatchCoverVideo('one'), second = recoverBatchCoverVideo('two');
  const obsolete = recoverBatchCoverVideo('old', () => false);
  await vi.waitFor(() => expect(request).toHaveBeenCalledTimes(4));
  finish.forEach(done => done({ jobs: [] }));
  await Promise.all([first, second, obsolete]);
  expect(request).toHaveBeenCalledTimes(4);
});
