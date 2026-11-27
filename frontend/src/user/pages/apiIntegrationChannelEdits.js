function editableChannel(channel) {
  return {
    ...(channel.id ? { id: channel.id } : {}),
    name: channel.name,
    group: channel.group || '',
    baseUrl: channel.baseUrl,
    model: channel.model,
    concurrency: channel.concurrency ?? null,
    apiFormat: channel.apiFormat || 'openai_compatible',
    fullUrl: channel.fullUrl === true,
    ...(channel.apiKey ? { apiKey: channel.apiKey } : {}),
    // 显式清空 Key 才发这个标记，避免部分保存（例如只改名字）误删已存 Key。
    ...(channel.clearApiKey ? { clearApiKey: true } : {})
  };
}

export function channelEdits(previous, next) {
  const byId = new Map(previous.map(channel => [channel.id, editableChannel(channel)]));
  const edits = [];
  for (const channel of next) {
    const current = editableChannel(channel);
    const before = byId.get(channel.id);
    if (!before) {
      edits.push(current);
      continue;
    }
    const changed = Object.entries(current).filter(([key, value]) => key !== 'id' && value !== before[key]);
    if (changed.length) edits.push({ id: channel.id, ...Object.fromEntries(changed) });
  }
  return edits;
}

export function mergeChannelEdit(previous, edit) {
  const merged = { ...previous, ...edit };
  if (edit.clearApiKey) delete merged.apiKey;
  if (edit.apiKey) delete merged.clearApiKey;
  return merged;
}

export function applyPreviewChannelEdits(previous, payload) {
  const byId = new Map(previous.map(channel => [channel.id, channel]));
  for (const patch of payload.customChannels || []) {
    const id = patch.id || `ch_preview_${crypto.randomUUID()}`;
    const row = { ...(byId.get(id) || { id, hasApiKey: false }) };
    for (const key of ['name', 'baseUrl', 'model', 'concurrency', 'apiFormat', 'fullUrl', 'enabled', 'group']) {
      if (Object.hasOwn(patch, key)) {
        if (patch[key] === null) delete row[key];
        else row[key] = patch[key];
      }
    }
    if (patch.params && typeof patch.params === 'object' && !Array.isArray(patch.params)) {
      if (Object.keys(patch.params).length) row.params = { ...row.params, ...patch.params };
      else delete row.params;
    }
    if (patch.clearApiKey === true) row.hasApiKey = false;
    else if (typeof patch.apiKey === 'string' && patch.apiKey.trim()) row.hasApiKey = true;
    byId.set(id, row);
  }
  for (const id of payload.deletedChannelIds || []) byId.delete(id);
  return [...byId.values()];
}
