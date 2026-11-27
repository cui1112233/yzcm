function progressTime(value) {
  const date = new Date(value || '');
  if (Number.isNaN(date.getTime())) return '--:--:--';
  return date.toLocaleTimeString('zh-CN', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
}

export function novelFetchLatestJob(data = {}) {
  const latest = data?.latest;
  return latest && typeof latest === 'object' && Object.keys(latest).length ? latest : null;
}

export function novelFetchJobState(job) {
  return String(job?.queue_state || job?.queueState || job?.status || '').trim().toLowerCase();
}

export function novelFetchJobFinished(job) {
  // Deferred work uses another dispatch; this job ID will no longer advance.
  return ['done', 'failed', 'stopped', 'cancelled', 'canceled', 'completed', 'waiting_retry', 'waiting_classification', 'waiting_configuration', 'waiting_login', 'pending_confirmation', 'manual_only'].includes(novelFetchJobState(job));
}

export function novelFetchActionNotice(result, fallbackText) {
  if (result?.queued === true) return { type: 'info', message: result.message || '已进入后台生成队列' };
  return { type: 'success', message: result?.message || fallbackText };
}

export function novelFetchProgressLines(jobOrJobs, taskRows = []) {
  const jobs = (Array.isArray(jobOrJobs) ? jobOrJobs : [jobOrJobs]).filter(Boolean);
  const latestJob = jobs[0];
  if (latestJob?.status_reason === 'queue_missing' || latestJob?.queue_check_error) {
    return [String(latestJob.queue_check_error || latestJob.error)];
  }
  const entries = jobs.flatMap(job => Array.isArray(job?.progress) ? job.progress : (Array.isArray(job?.steps) ? job.steps : []));
  if (Array.isArray(taskRows)) {
    for (const row of taskRows) {
      const bookId = String(row?.book_id || row?.bookId || '').trim();
      const bookName = String(row?.book_name || row?.bookName || '').trim();
      const logs = Array.isArray(row?.logs) ? row.logs : [];
      for (const log of logs) {
        entries.push({
          time: log?.time || log?.at,
          book_id: bookId,
          book_name: bookName,
          message: log?.detail || log?.message || log?.event || log?.type || ''
        });
      }
    }
  }
  const seenEntries = new Set();
  const uniqueEntries = entries.filter(entry => {
    const key = [entry?.time || entry?.at || '', entry?.book_id || entry?.bookId || '', entry?.message || entry?.detail || entry?.event || ''].join('|');
    if (seenEntries.has(key)) return false;
    seenEntries.add(key);
    return true;
  });
  uniqueEntries.sort((left, right) => {
    const leftTime = Date.parse(left?.time || left?.at || '');
    const rightTime = Date.parse(right?.time || right?.at || '');
    if (Number.isNaN(leftTime) || Number.isNaN(rightTime)) return 0;
    return leftTime - rightTime;
  });
  if (uniqueEntries.length) {
    const renderedLines = uniqueEntries.map(entry => {
      const bookId = String(entry?.book_id || entry?.bookId || '').trim();
      const bookName = String(entry?.book_name || entry?.bookName || '').trim();
      const target = [bookName, bookId ? `(${bookId})` : ''].filter(Boolean).join(' ');
      return [`[${progressTime(entry?.time)}]`, target, String(entry?.message || entry?.status || '').trim()].filter(Boolean).join(' ');
    });
    return [...new Set(renderedLines)];
  }
  const job = jobs[0] || {};
  const status = String(job?.status || '').toLowerCase();
  if (job?.error) return [String(job.error)];
  if (status === 'queued') return ['任务已进入队列，等待执行。'];
  if (status === 'running' || status === 'processing') return ['任务正在执行，等待最新进度。'];
  if (status === 'done' || status === 'completed') return ['完整流程已完成。'];
  return ['暂无正在处理的任务。'];
}
