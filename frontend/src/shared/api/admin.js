import { apiRequest } from './client.js';

const base = '/api/admin';

export function listAdminErrorLogs(limit = 100) {
  return apiRequest(`${base}/error-logs?limit=${encodeURIComponent(limit)}`);
}

export function listAdminPresets(module) {
  return apiRequest(`${base}/presets?module=${encodeURIComponent(module)}`);
}

export function listAdminPresetSlots(module) {
  return apiRequest(`${base}/preset-slots?module=${encodeURIComponent(module)}`);
}

export function getAdminPreset(id, version) {
  return apiRequest(`${base}/presets/${encodeURIComponent(id)}/${encodeURIComponent(version)}`);
}

export function createPresetDraft(input) {
  return apiRequest(`${base}/presets/draft`, {
    method: 'POST',
    body: JSON.stringify(input)
  });
}

export function updatePresetDraft(id, version, input) {
  return apiRequest(`${base}/presets/${encodeURIComponent(id)}/${encodeURIComponent(version)}/draft`, {
    method: 'PUT',
    body: JSON.stringify(input)
  });
}

export function publishPreset(id, version) {
  return apiRequest(`${base}/presets/${encodeURIComponent(id)}/publish`, {
    method: 'POST',
    body: JSON.stringify({ version })
  });
}

export function rollbackPreset(id, version) {
  return apiRequest(`${base}/presets/${encodeURIComponent(id)}/rollback`, {
    method: 'POST',
    body: JSON.stringify({ version })
  });
}

export function archivePreset(id, version) {
  return apiRequest(`${base}/presets/${encodeURIComponent(id)}/archive`, {
    method: 'POST',
    body: JSON.stringify({ version })
  });
}

export function deletePreset(id, version) {
  return apiRequest(`${base}/presets/${encodeURIComponent(id)}/${encodeURIComponent(version)}`, {
    method: 'DELETE'
  });
}

export function uploadAdminComicMaterial(dataUrl) {
  return apiRequest(`${base}/comic-materials/upload`, {
    method: 'POST',
    body: JSON.stringify({ dataUrl })
  });
}

export function loadAdminComicMaterialPreview(preset) {
  const id = encodeURIComponent(String(preset?.id || ''));
  const slot = preset?.protocolLock?.slot;
  const version = encodeURIComponent(String(preset?.version || ''));
  const asset = encodeURIComponent(String(preset?.previewAssetId || ''));
  const path = preset?.previewAssetId
    ? `/api/presets/comic-material/${id}/preview?asset=${asset}&version=${version}`
    : slot === 'batch.comic-style'
      ? `/api/presets/comic-style/${id}/preview?version=${version}`
      : `/api/presets/comic-reference-scene/${id}/preview?version=${version}`;
  return apiRequest(path, { responseType: 'blob', silent: true });
}

export function listAdminAgentSkills() {
  return apiRequest(`${base}/agent-skills`);
}

export function getAdminAgentSkill(id, version) {
  return apiRequest(`${base}/agent-skills/${encodeURIComponent(id)}/${encodeURIComponent(version)}`);
}

export function createAdminAgentSkillDraft(input) {
  return apiRequest(`${base}/agent-skills/draft`, { method: 'POST', body: JSON.stringify(input) });
}

export function publishAdminAgentSkill(id, version) {
  return apiRequest(`${base}/agent-skills/${encodeURIComponent(id)}/publish`, { method: 'POST', body: JSON.stringify({ version }) });
}

export function archiveAdminAgentSkill(id, version) {
  return apiRequest(`${base}/agent-skills/${encodeURIComponent(id)}/archive`, { method: 'POST', body: JSON.stringify({ version }) });
}
