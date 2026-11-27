export function parseStoryboardJson(text, lastValid = []) {
  try {
    const value = JSON.parse(text);
    if (!Array.isArray(value)) throw new Error('分镜 JSON 必须是数组');
    return { valid: true, value, error: '' };
  } catch (error) { return { valid: false, value: lastValid, error: error.message }; }
}

export function createStoryboardShot(index = 0) {
  return { id: globalThis.crypto?.randomUUID?.() || `shot-${Date.now()}-${index}`, scene: `场景 ${index + 1}`, picture: '', shotSize: '中景', camera: '平视', movement: '固定', bindings: [], dialogue: '', narration: '', duration: 3, imagePrompt: '', videoPrompt: '', audioPrompt: '', sourceRange: { start: 0, end: 0 } };
}

export function summarizeCoverage(coverage = {}) {
  const size = rows => (rows || []).reduce((sum, row) => sum + Math.max(0, Number(row.end) - Number(row.start)), 0);
  return { covered: size(coverage.covered), missing: size(coverage.missing), duplicate: size(coverage.duplicate) };
}
