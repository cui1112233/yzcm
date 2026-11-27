// 「自定义预设」只决定哪些预置渠道显示在文本 / 视频 / 图片组里。
// 是否可用仍然只看 Key 是否填写，不引入启用开关。
// 勾选结果存放在服务端 apiIntegrations.channelSelection，形如：
// { text: { enabled: ['deepseek', 'doubao'] } }
// 没有该分组记录 = 默认展示全部预置；记录为空数组 = 用户显式全部取消。

export function explicitPresetIds(channelSelection, groupId) {
  const raw = channelSelection?.[groupId];
  if (!raw || typeof raw !== 'object' || !Array.isArray(raw.enabled)) return null;
  return raw.enabled.map(value => String(value ?? '').trim()).filter(Boolean);
}

export function selectedPresetIds(channelSelection, groupId, allIds) {
  const ids = (Array.isArray(allIds) ? allIds : []).map(value => String(value ?? '').trim()).filter(Boolean);
  const explicit = explicitPresetIds(channelSelection, groupId);
  if (explicit === null) return ids;
  const explicitSet = new Set(explicit);
  // 结果按预置清单顺序返回，避免卡片顺序随勾选顺序变化。
  return ids.filter(id => explicitSet.has(id));
}

export function filterPresetsBySelection(presets, channelSelection, groupId) {
  const list = Array.isArray(presets) ? presets : [];
  const visible = new Set(selectedPresetIds(channelSelection, groupId, list.map(preset => preset.id)));
  return list.filter(preset => visible.has(preset.id));
}

export function buildPresetSelectionPayload(channelSelection, groupId, ids) {
  return {
    ...(channelSelection || {}),
    [groupId]: { enabled: Array.from(new Set((Array.isArray(ids) ? ids : []).map(id => String(id ?? '').trim()).filter(Boolean))) }
  };
}

const YZCM_MODEL_KINDS = new Set(['text', 'image', 'video', 'audio', 'asr']);

export function yzcmModelSelectionId(model) {
  const kind = String(model?.kind ?? '').trim().toLowerCase();
  const modelId = String(model?.modelId ?? '').trim();
  if (!YZCM_MODEL_KINDS.has(kind) || !modelId) return '';
  const canonical = `yzcm:${kind}:${modelId}`;
  const explicit = String(model?.id ?? '').trim();
  return !explicit || explicit === canonical ? canonical : '';
}

function yzcmServerRowId(model) {
  const canonical = yzcmModelSelectionId(model);
  return canonical && String(model?.id ?? '').trim() === canonical ? canonical : '';
}

export function mergeYZCMSelectionModels(current, searchRows) {
  const byId = new Map();
  for (const model of Array.isArray(current) ? current : []) {
    const id = yzcmServerRowId(model);
    if (id) byId.set(id, { ...model, id });
  }
  for (const row of Array.isArray(searchRows) ? searchRows : []) {
    if (row?.adapted !== true || row?.allowed !== true) continue;
    if (row?.provider && row.provider !== 'yzcm') continue;
    if (row?.catalogSource && row.catalogSource !== 'yzcm_saved') continue;
    const id = yzcmModelSelectionId(row);
    if (!id) continue;
    byId.set(id, { ...(byId.get(id) || {}), ...row, id });
  }
  return [...byId.values()];
}

export function buildYZCMSelectionOperations(existingModels, selectedModels) {
  const existing = new Map();
  for (const model of Array.isArray(existingModels) ? existingModels : []) {
    const id = yzcmServerRowId(model);
    if (id) existing.set(id, model);
  }
  const selected = new Map();
  for (const model of Array.isArray(selectedModels) ? selectedModels : []) {
    const id = yzcmModelSelectionId(model);
    if (id) selected.set(id, model);
  }
  const operations = [];
  for (const [id, model] of existing) {
    const before = model.selectedForConfigPage !== false;
    const after = selected.has(id);
    if (before !== after) operations.push({ type: 'update', id, patch: { selectedForConfigPage: after } });
  }
  for (const [id, model] of selected) {
    if (existing.has(id)) continue;
    operations.push({ type: 'create', payload: { kind: String(model.kind), modelId: String(model.modelId) } });
  }
  return operations;
}

export function applyYZCMSelectionOperation(models, operation) {
  const rows = Array.isArray(models) ? models : [];
  if (operation?.type === 'create') {
    const id = yzcmModelSelectionId(operation.payload);
    return id ? [...rows, { ...operation.payload, id, selectedForConfigPage: true }] : rows;
  }
  if (operation?.type === 'update') {
    return rows.map(model => model.id === operation.id ? { ...model, ...operation.patch } : model);
  }
  return rows;
}

export async function executeYZCMSelectionOperations({
  existingModels, selectedModels, create, update, refresh, onModels
}) {
  let localModels = Array.isArray(existingModels) ? existingModels : [];
  let operationError = null;
  try {
    for (const operation of buildYZCMSelectionOperations(localModels, selectedModels)) {
      if (operation.type === 'create') await create(operation.payload);
      else await update(operation.id, operation.patch);
      localModels = applyYZCMSelectionOperation(localModels, operation);
      onModels?.(localModels);
    }
  } catch (error) {
    operationError = error;
  }
  let refreshError = null;
  try {
    const refreshed = await refresh();
    if (Array.isArray(refreshed)) {
      localModels = refreshed;
      onModels?.(localModels);
    }
  } catch (error) {
    refreshError = error;
  }
  if (operationError) throw operationError;
  if (refreshError) throw refreshError;
  return localModels;
}

export function restrictedCredentialPatch(value, clear) {
  if (clear) return { clearCredential: true };
  const credential = String(value ?? '').trim();
  if (!credential) throw new TypeError('请输入 API Key');
  return { credential };
}

export function settleCatalogKeyDraft(drafts, modelId, submitted, focused) {
  const current = drafts || {};
  if (focused || String(current[modelId] ?? '').trim() !== String(submitted ?? '').trim()) return current;
  return { ...current, [modelId]: '' };
}

export function setCatalogKeyRequestState(states, modelId, patch) {
  const current = states || {};
  return { ...current, [modelId]: { ...(current[modelId] || {}), ...(patch || {}) } };
}
