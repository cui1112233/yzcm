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
export function updateScriptEditorScript(workId, scriptId, input) { return apiRequest(`${root}/works/${id(workId)}/scripts/${id(scriptId)}`, { method: 'PATCH', body: JSON.stringify(input) }); }
export function deleteScriptEditorScript(workId, scriptId, revision) { return apiRequest(`${root}/works/${id(workId)}/scripts/${id(scriptId)}?expectedRevision=${Number(revision)}`, { method: 'DELETE' }); }
export function copyScriptEditorScript(workId, scriptId) { return apiRequest(`${root}/works/${id(workId)}/scripts/${id(scriptId)}/copy`, { method: 'POST' }); }
export function preferScriptEditorScript(workId, scriptId, expectedRevision) { return apiRequest(`${root}/works/${id(workId)}/scripts/${id(scriptId)}/prefer`, { method: 'POST', body: JSON.stringify({ expectedRevision }) }); }
export function generateScriptEditorScript(workId, input) { return apiRequest(`${root}/works/${id(workId)}/generate`, { method: 'POST', body: JSON.stringify(input), timeoutMs: 10 * 60 * 1000, suppressGlobalError: true }); }
export function exportScriptEditorWork(workId, format) { return apiRequest(`${root}/works/${id(workId)}/export?format=${encodeURIComponent(format)}`, { responseType: 'blob', suppressGlobalError: true }); }
