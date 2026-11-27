const railEntries = [
  ['source', '小说'],
  ['scripts', '剧本'],
  ['agent', 'Agent'],
  ['assets', '资产'],
  ['storyboard', '分镜'],
  ['canvas', 'CM 画布'],
  ['production', '生产'],
  ['delivery', '交付'],
  ['settings', '设置'],
];

export const SCRIPT_EDITOR_RAIL_ITEMS = railEntries.map(([key, label]) => ({ key, label }));

const validTabs = new Set(SCRIPT_EDITOR_RAIL_ITEMS.map(item => item.key));

export function resolveStudioLocation(search, work) {
  const query = new URLSearchParams(search || '');
  const fallbackTab = work?.type === 'script' ? 'scripts' : 'source';
  const requestedTab = query.get('tab') || fallbackTab;
  return {
    workId: query.get('workId') || work?.id || '',
    tab: validTabs.has(requestedTab) ? requestedTab : fallbackTab,
    itemId: query.get('itemId') || '',
  };
}

export function buildStudioSearch(value) {
  const query = new URLSearchParams();
  if (value?.workId) query.set('workId', value.workId);
  if (value?.tab) query.set('tab', value.tab);
  if (value?.itemId) query.set('itemId', value.itemId);
  const encoded = query.toString();
  return encoded ? `?${encoded}` : '';
}
