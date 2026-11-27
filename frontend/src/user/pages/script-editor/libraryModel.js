const workMenuEntries = [
  ['open', '打开作品'], ['rename', '重命名'], ['copy', '复制作品'],
  ['export', '导出作品'], ['workspace', '打开关联工作区'],
  ['mark', '切换标记'], ['delete', '删除作品'],
];

export function buildWorkMenu() {
  return workMenuEntries.map(([key, label]) => ({ key, label, danger: key === 'delete' }));
}

export function summarizeWorkJobs(jobs = []) {
  return jobs.reduce((result, job) => {
    if (job.status === 'running') result.running += 1;
    if (job.status === 'failed') result.failed += 1;
    if (job.status === 'queued') result.queued += 1;
    return result;
  }, { running: 0, failed: 0, queued: 0 });
}
