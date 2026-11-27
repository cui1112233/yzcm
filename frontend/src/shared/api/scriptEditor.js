import { apiRequest } from './client.js';

const root = '/api/script-editor';
const id = value => encodeURIComponent(value);

export function listScriptEditorWorks(filters = {}, signal) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) if (value) query.set(key === 'query' ? 'q' : key, value);
  return apiRequest(`${root}/works${query.size ? `?${query}` : ''}`, { signal, suppressGlobalError: true });
}
export function getScriptEditorWork(workId, signal) { return apiRequest(`${root}/works/${id(workId)}`, { signal, suppressGlobalError: true }); }
export function createScriptEditorWork(input) { return apiRequest(`${root}/works`, { method: 'POST', body: JSON.stringify(input) }); }
export function updateScriptEditorWork(workId, input) { return apiRequest(`${root}/works/${id(workId)}`, { method: 'PATCH', body: JSON.stringify(input) }); }
export function deleteScriptEditorWork(workId, revision) { return apiRequest(`${root}/works/${id(workId)}?expectedRevision=${Number(revision)}`, { method: 'DELETE' }); }
export function copyScriptEditorWork(workId, requestKey) { return apiRequest(`${root}/works/${id(workId)}/copy`, { method: 'POST', body: JSON.stringify({ requestKey }) }); }
export function getScriptEditorBinding(workId, targetType, signal) { return apiRequest(`${root}/works/${id(workId)}/bindings/${id(targetType)}`, { signal, suppressGlobalError: true }); }
export function putScriptEditorBinding(workId, targetType, input) { return apiRequest(`${root}/works/${id(workId)}/bindings/${id(targetType)}`, { method: 'PUT', body: JSON.stringify(input), suppressGlobalError: true }); }
export function searchScriptEditorWork(workId, query, signal) { return apiRequest(`${root}/works/${id(workId)}/search?q=${encodeURIComponent(query)}`, { signal, suppressGlobalError: true }); }
export function importIntoScriptEditorWork(workId, input) { return apiRequest(`${root}/works/${id(workId)}/import`, { method: 'POST', body: JSON.stringify(input), suppressGlobalError: true }); }
export function previewScriptEditorImport(file) {
  const body = new FormData(); body.append('file', file);
  return apiRequest(`${root}/import/preview`, { method: 'POST', body, suppressGlobalError: true });
}
export function commitScriptEditorImport(input) { return apiRequest(`${root}/import/commit`, { method: 'POST', body: JSON.stringify(input) }); }
export function createScriptEditorChapter(workId, input) { return apiRequest(`${root}/works/${id(workId)}/chapters`, { method: 'POST', body: JSON.stringify(input) }); }
export function updateScriptEditorChapter(workId, chapterId, input) { return apiRequest(`${root}/works/${id(workId)}/chapters/${id(chapterId)}`, { method: 'PATCH', body: JSON.stringify(input) }); }
export function deleteScriptEditorChapter(workId, chapterId, revision) { return apiRequest(`${root}/works/${id(workId)}/chapters/${id(chapterId)}?expectedRevision=${Number(revision)}`, { method: 'DELETE' }); }
export function reorderScriptEditorChapters(workId, chapterIds) { return apiRequest(`${root}/works/${id(workId)}/chapters/reorder`, { method: 'POST', body: JSON.stringify({ chapterIds }) }); }
export function createScriptEditorScript(workId, input) { return apiRequest(`${root}/works/${id(workId)}/scripts`, { method: 'POST', body: JSON.stringify(input) }); }
export function importScriptEditorScripts(workId, input) { return apiRequest(`${root}/works/${id(workId)}/scripts/import`, { method: 'POST', body: JSON.stringify(input), suppressGlobalError: true }); }
export function replaceScriptEditorScripts(workId, input) { return apiRequest(`${root}/works/${id(workId)}/scripts/replace`, { method: 'POST', body: JSON.stringify(input), suppressGlobalError: true }); }
export function updateScriptEditorScript(workId, scriptId, input) { return apiRequest(`${root}/works/${id(workId)}/scripts/${id(scriptId)}`, { method: 'PATCH', body: JSON.stringify(input) }); }
export function deleteScriptEditorScript(workId, scriptId, revision) { return apiRequest(`${root}/works/${id(workId)}/scripts/${id(scriptId)}?expectedRevision=${Number(revision)}`, { method: 'DELETE' }); }
export function copyScriptEditorScript(workId, scriptId) { return apiRequest(`${root}/works/${id(workId)}/scripts/${id(scriptId)}/copy`, { method: 'POST' }); }
export function preferScriptEditorScript(workId, scriptId, expectedRevision) { return apiRequest(`${root}/works/${id(workId)}/scripts/${id(scriptId)}/prefer`, { method: 'POST', body: JSON.stringify({ expectedRevision }) }); }
export function generateScriptEditorScript(workId, input) { return apiRequest(`${root}/works/${id(workId)}/generate`, { method: 'POST', body: JSON.stringify(input), timeoutMs: 10 * 60 * 1000, suppressGlobalError: true }); }
export function createScriptEditorGenerationJob(workId, input) { return apiRequest(`${root}/works/${id(workId)}/jobs/script-generation`, { method: 'POST', body: JSON.stringify(input), suppressGlobalError: true }); }
export function createScriptEditorReviewJob(workId, input) { return apiRequest(`${root}/works/${id(workId)}/jobs/review`, { method: 'POST', body: JSON.stringify(input), suppressGlobalError: true }); }
export function listScriptEditorJobs(workId, signal) { return apiRequest(`${root}/works/${id(workId)}/jobs`, { signal, suppressGlobalError: true }); }
export function cancelScriptEditorJob(workId, jobId) { return apiRequest(`${root}/works/${id(workId)}/jobs/${id(jobId)}/cancel`, { method: 'POST', suppressGlobalError: true }); }
export function retryScriptEditorJob(workId, jobId) { return apiRequest(`${root}/works/${id(workId)}/jobs/${id(jobId)}/retry`, { method: 'POST', suppressGlobalError: true }); }
export function applyScriptEditorJob(workId, jobId) { return apiRequest(`${root}/works/${id(workId)}/jobs/${id(jobId)}/apply`, { method: 'POST', suppressGlobalError: true }); }
export function exportScriptEditorWork(workId, format) { return apiRequest(`${root}/works/${id(workId)}/export?format=${encodeURIComponent(format)}`, { responseType: 'blob', suppressGlobalError: true }); }
export function handoffScriptEditorWork(workId, input) { return apiRequest(`${root}/works/${id(workId)}/handoffs/comic-screenplay`, { method: 'POST', body: JSON.stringify(input), suppressGlobalError: true }); }
