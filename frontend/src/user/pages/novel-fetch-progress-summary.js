import { novelFetchTaskStatus } from './novel-fetch-status.js';

const text = value => String(value ?? '').trim().toLowerCase();
const done = value => /^(done|success|completed|complete|ready|ai_done|uploaded|submitted|已提交|已完成|完成|成功|已上传)$/.test(text(value));
const aiVersion = value => /^ai[1-5]$/.test(text(value));
const aiFiles = row => Array.isArray(row.aiFiles ?? row.ai_files) ? (row.aiFiles ?? row.ai_files) : [];

function selectedAI(row) {
  const snapshot = row.meta?.processSnapshot ?? row.processSnapshot ?? {};
  for (const value of [snapshot.selected_versions, snapshot.selectedVersions, row.selectedVersions, row.selected_versions]) {
    if (Array.isArray(value)) return [...new Set(value.map(text).filter(aiVersion))];
  }
  return null; // Historical tasks did not store a round selection.
}

function ready(row, version) {
  const ref = row.bodyRefs?.[version] ?? row.body_refs?.[version];
  if (ref) return text(ref.state) === 'ready' && Number(ref.charCount ?? ref.char_count) > 0;
  const value = row.versions?.[version];
  if (typeof value === 'string') return Boolean(value.trim());
  if (value && typeof value === 'object') return text(value.state) === 'ready' && value.charCount !== 0;
  if (value === false) return false;
  return aiFiles(row).some(file => text(file) === version);
}

export function summarizeWorkshopProgress(rows, fallbackSubmissions = []) {
  const source = Array.isArray(rows) ? rows : [];
  const requested = source.map(row => selectedAI(row));
  const aiRequired = requested.filter(versions => versions === null || versions.length > 0).length;
  const aiDone = source.filter((row, index) => {
    const versions = requested[index];
    if (versions !== null) return versions.length > 0 && versions.every(version => ready(row, version));
    const historical = new Set([...Object.keys(row.versions ?? {}), ...Object.keys(row.bodyRefs ?? row.body_refs ?? {}), ...aiFiles(row)]);
    return done(row.aiStatus ?? row.ai_status) && [...historical].some(version => aiVersion(version) && ready(row, version));
  }).length;
  const failed = value => /fail|error|failed|失败|错误/.test(text(value));
  return {
    total: source.length,
    originalDone: source.filter(row => done(row.originalStatus ?? row.original_status) || Number(row.originalRawChars ?? row.original_raw_chars) > 0).length,
    aiDone, aiRequired,
    processing: source.filter(row => ['queued', 'processing'].includes(novelFetchTaskStatus(row).status)).length,
    waitingRetry: source.filter(row => novelFetchTaskStatus(row).status === 'waiting_retry').length,
    failed: source.filter((row, index) => {
      const versions = requested[index];
      if (versions === null) return [row.status, row.originalStatus ?? row.original_status, row.aiStatus ?? row.ai_status, row.siteSubmitStatus ?? row.site_submit_status].some(failed);
      const snapshot = row.meta?.processSnapshot ?? row.processSnapshot ?? {};
      if ([row.status, row.originalStatus ?? row.original_status].some(failed)) return true;
      if (versions.some(version => {
        const ref = row.bodyRefs?.[version] ?? row.body_refs?.[version];
        return failed(ref?.state ?? row.versions?.[version]?.state);
      })) return true;
      return snapshot.auto_submit === true && failed(row.siteSubmitStatus ?? row.site_submit_status);
    }).length,
    submissionDone: source.length ? source.filter(row => done(row.siteSubmitStatus ?? row.site_submit_status)).length : fallbackSubmissions.filter(row => done(row.status)).length
  };
}
