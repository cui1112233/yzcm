export function buildSearchExcerpt(text, start, end, radius = 28) {
  const from = Math.max(0, Number(start) - radius);
  const to = Math.min(text.length, Number(end) + radius);
  return `${from > 0 ? '…' : ''}${text.slice(from, to)}${to < text.length ? '…' : ''}`;
}

export function referencedChapterCounts(scripts = []) {
  const counts = {};
  for (const script of scripts) for (const chapterId of script.sourceChapterIds || []) counts[chapterId] = (counts[chapterId] || 0) + 1;
  return counts;
}
