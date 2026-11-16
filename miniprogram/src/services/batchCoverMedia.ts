import { apiRequest } from './api';
import { sessionRevision } from './sessionBoundary';

let running = 0;
const waiting: Array<() => void> = [];

// Visible-card recovery only: never poll or read every batch detail.
export async function recoverBatchCoverVideo(batchId: string, current = () => true): Promise<string> {
  if (!batchId.trim()) return '';
  const revision = sessionRevision();
  await new Promise<void>(resolve => {
    const start = () => { running += 1; resolve(); };
    if (running < 2) start(); else waiting.push(start);
  });
  try {
    if (!current() || revision !== sessionRevision()) return '';
    const merged = await apiRequest<{ jobs?: Array<{ status?: string; outputUrl?: string; mediaUrl?: string }> }>(
      `/api/batch-factory/v12/batches/${encodeURIComponent(batchId)}/merge-status`, { recoverSession: false },
    ).catch(() => null);
    if (!current() || revision !== sessionRevision()) return '';
    for (const job of merged?.jobs || []) {
      if (["succeeded", "completed"].includes(String(job.status || "").toLowerCase())) {
        const outputUrl = String(job.outputUrl || job.mediaUrl || "").trim();
        if (outputUrl) return outputUrl;
      }
    }
    const result = await apiRequest<{ jobs?: Array<{ tasks?: Array<{ status?: string; mediaUrl?: string }> }> }>(
      `/api/batch-factory/v12/batches/${encodeURIComponent(batchId)}/status`, { recoverSession: false },
    );
    if (!current() || revision !== sessionRevision()) return '';
    for (const job of result.jobs || []) for (const task of job.tasks || []) {
      if (task.status === 'succeeded' && task.mediaUrl?.trim()) return task.mediaUrl.trim();
    }
    return '';
  } finally {
    running -= 1;
    waiting.shift()?.();
  }
}
