import { normalizeComicOutputStyles } from './batchFactoryComicStyle.js';

export const comicStyleMediaKey = (batchId, bookId, styleId = '') => JSON.stringify([String(batchId),String(bookId),String(styleId)]);
export const isCurrentComicStyleResponse = (capturedKey,currentKey) => capturedKey === currentKey;
export function comicBookOutputStyles(settings = {}) {
  try { return normalizeComicOutputStyles(settings.aiPromptConfig?.comic || {}); }
  catch { return []; }
}
export function comicMediaStyleSelection(styles, selected) {
  return styles.some(style => style.presetId === selected) ? selected : (styles[0]?.presetId || '');
}
