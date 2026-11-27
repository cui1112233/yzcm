import { manualBookIDsFromInput, manualFetchedSourceText } from './batchFactoryManualFetch.js';

// Existing supplied originals stay attached. Automatic production retrieves
// missing originals in its saved server job, after the workbench is open.
export async function prepareCreationSources({ groups, deferSources, productionMode, maxTxt }, fetchOriginals) {
  const sourcesByGroup = {};
  const sourceMetadataByGroup = {};
  const failedPlatforms = [];
  const failedReasons = [];
  let fetchedCount = 0;
  for (const group of groups) {
    const groupKey = String(group.platformId);
    const ids = manualBookIDsFromInput(group.inputText);
    const have = sourcesByGroup[groupKey] || group.sourceTextByBookId || {};
    sourcesByGroup[groupKey] = { ...have };
    sourceMetadataByGroup[groupKey] = { ...(group.sourceMetadataByBookId || {}) };
    fetchedCount += ids.filter(bookId => manualFetchedSourceText(have[bookId])).length;
    const pending = ids.filter(bookId => !manualFetchedSourceText(have[bookId]));
    if (!pending.length || deferSources) continue;
    try {
      const result = await fetchOriginals({ platform: group.platformId, bookIds: pending, productionMode, maxTxt });
      const groupSources = {};
      const groupMetadata = {};
      for (const item of Array.isArray(result?.results) ? result.results : []) {
        if (item?.status !== 'ok' && item?.status !== 'processing_error') continue;
        const bookId = String(item.bookId);
        groupSources[bookId] = { data: item.data, rawData: item.rawData };
        if (item.sourceMetadata && typeof item.sourceMetadata === 'object' && !Array.isArray(item.sourceMetadata)) groupMetadata[bookId] = item.sourceMetadata;
      }
      sourcesByGroup[groupKey] = { ...have, ...groupSources };
      sourceMetadataByGroup[groupKey] = { ...groupMetadata, ...sourceMetadataByGroup[groupKey] };
      for (const bookId of pending) {
        if (manualFetchedSourceText(groupSources[bookId])) fetchedCount += 1;
      }
    } catch (error) {
      const platformLabel = group.platformName || group.platformId;
      failedPlatforms.push(platformLabel);
      failedReasons.push(`${platformLabel}：${error?.message || '请求失败'}`);
    }
  }
  return { sourcesByGroup, sourceMetadataByGroup, failedPlatforms, failedReasons, fetchedCount };
}
