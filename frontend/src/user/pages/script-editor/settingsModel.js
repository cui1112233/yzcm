export function layoutStorageKey(account, workId) { return `script-editor:layout:${account}:${workId}`; }
export function readStudioLayout(storage, account, workId) { try { return JSON.parse(storage.getItem(layoutStorageKey(account,workId))||'{}'); } catch { return {}; } }
export function writeStudioLayout(storage, account, workId, value) { storage.setItem(layoutStorageKey(account,workId),JSON.stringify(value)); }
