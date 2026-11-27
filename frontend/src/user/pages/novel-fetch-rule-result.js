const count = value => Number.isFinite(Number(value)) ? Math.max(0, Math.floor(Number(value))) : 0;

export function describeRuleApplyResult(result, fallbackText) {
  const updated = count(result?.updated ?? result?.processed);
  const skipped = count(result?.skipped);
  const failedVersions = Array.isArray(result?.version_results)
    ? result.version_results.filter(item => item?.status === 'failed') : [];
  const failedBooks = new Set(failedVersions.map(item => item.book_id).filter(Boolean)).size;
  const failed = Math.max(count(result?.failed), failedBooks, failedVersions.length ? 1 : 0);
  if (failed > 0) {
    return {
      type: updated > 0 ? 'warning' : 'error',
      message: `规则处理：已保存 ${updated} 本，含失败 ${failed} 本，跳过 ${skipped} 本`
    };
  }
  if (updated > 0) return { type: 'success', message: fallbackText };
  return { type: 'warning', message: skipped > 0 ? `规则处理：跳过 ${skipped} 本，没有任务完成处理` : '未取得规则处理成功结果，请查看任务详情' };
}
