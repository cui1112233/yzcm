export function filterAndSortWorks(rows, filters = {}) {
  const query = String(filters.query || '').trim().toLocaleLowerCase();
  const result = (Array.isArray(rows) ? rows : []).filter(row => {
    if (filters.type && row.type !== filters.type) return false;
    if (filters.mark && row.markStatus !== filters.mark) return false;
    return !query || String(row.title || '').toLocaleLowerCase().includes(query);
  });
  const sort = filters.sort || 'updated-desc';
  return [...result].sort((a, b) => {
    if (sort === 'name-asc') return String(a.title).localeCompare(String(b.title), 'zh-CN');
    if (sort === 'name-desc') return String(b.title).localeCompare(String(a.title), 'zh-CN');
    const delta = new Date(a.updatedAt || 0) - new Date(b.updatedAt || 0);
    return sort === 'updated-asc' ? delta : -delta;
  });
}

export function workTypeLabel(type) { return type === 'script' ? '剧本' : '小说'; }
export function markLabel(mark) { return ({ key: '重点', pending: '待处理', done: '已完成' })[mark] || '未标记'; }
