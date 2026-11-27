const activeStatuses = new Set(['queued', 'running']);

export function shouldPollScriptEditorJobs(jobs = []) {
  return jobs.some(job => activeStatuses.has(job.status));
}

export function jobProgressView(job = {}) {
  const result = job.result || {};
  const completed = result.completedEpisodes?.length ?? job.progressCurrent ?? 0;
  const failed = result.failedEpisodes?.length ?? 0;
  if (job.status === 'partial') return { tone: 'warning', label: `已保存${completed}集，${failed}集失败`, retryable: true };
  if (job.status === 'failed') return { tone: 'error', label: job.errorMessage || '任务失败', retryable: true };
  if (job.status === 'cancelled') return { tone: 'default', label: '已取消', retryable: true };
  if (job.status === 'succeeded') return { tone: 'success', label: job.type === 'script-generation' ? `已保存${completed}集` : '处理完成', retryable: false };
  if (job.status === 'running') return { tone: 'processing', label: `正在处理 ${job.progressCurrent || 0}/${job.progressTotal || 1}`, retryable: false };
  return { tone: 'default', label: '等待处理', retryable: false };
}

export function scriptEditorJobTitle(type) {
  return ({ 'script-generation': 'AI 出剧本', review: 'AI 审稿', polish: 'AI 优化', 'viral-opening': '爆款开头' })[type] || 'AI 任务';
}
