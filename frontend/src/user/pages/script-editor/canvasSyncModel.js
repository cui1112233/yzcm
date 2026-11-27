export function buildCanvasHref(taskId, workId) {
  const returnTo = `/script-editor?workId=${workId}&tab=canvas`;
  return `/agent/canvas?taskId=${encodeURIComponent(taskId)}&workId=${encodeURIComponent(workId)}&returnTo=${encodeURIComponent(returnTo)}`;
}

export function summarizeCanvasPreview(items = []) {
  return items.reduce((out, item) => ({ ...out, [item.state]: (out[item.state] || 0) + 1 }), { create: 0, update: 0, conflict: 0 });
}
