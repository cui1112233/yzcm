export function novelFetchAIFormPatch(changed = {}, values = {}) {
  const patch = {};
  const temperature = changed.rewrite?.temperature ?? changed.ai?.temperature;
  if (temperature !== undefined) {
    patch.rewrite = { ...values.rewrite, ...changed.rewrite, temperature };
    patch.ai = { ...values.ai, ...changed.ai, temperature };
  }
  if (changed.ai?.enable_thinking === true || changed.ai?.disable_thinking === true) {
    patch.ai = { ...values.ai, ...patch.ai, ...changed.ai };
    if (changed.ai.disable_thinking === true) patch.ai.enable_thinking = false;
    else patch.ai.disable_thinking = false;
  }
  return patch;
}

export function novelFetchReadbackLabel(row = {}) {
  if (row.content_verified === true || row.contentVerified === true) return '正文已核对';
  if (row.verified === true && row.readback_status === 'confirmed') return '列表已确认';
  if (row.status === 'submitted' || (row.status === 'skipped' && row.reason === 'confirmed_result') || row.readback_status === 'pending') return '已接收，待核对';
  return '待核对';
}

export function novelFetchCanReadback(row = {}) {
  return row.status === 'pending_confirmation' ||
    (row.status === 'submitted' && row.verified !== true && row.readback_status === 'pending');
}
