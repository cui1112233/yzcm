export function groupScriptsByEpisode(scripts = []) {
  const groups = new Map();
  for (const script of scripts) {
    if (!groups.has(script.episode)) groups.set(script.episode, []);
    groups.get(script.episode).push(script);
  }
  return [...groups.entries()].sort(([a], [b]) => a - b).map(([episode, rows]) => ({ episode, scripts: rows.sort((a, b) => a.version - b.version) }));
}

export function diffScriptLines(left = '', right = '') {
  const a = left.split('\n'), b = right.split('\n');
  const dp = Array.from({ length:a.length + 1 }, () => Array(b.length + 1).fill(0));
  for (let i = a.length - 1; i >= 0; i -= 1) for (let j = b.length - 1; j >= 0; j -= 1) dp[i][j] = a[i] === b[j] ? dp[i + 1][j + 1] + 1 : Math.max(dp[i + 1][j], dp[i][j + 1]);
  const out = []; let i = 0, j = 0;
  while (i < a.length || j < b.length) {
    if (i < a.length && j < b.length && a[i] === b[j]) { out.push({ type:'equal', text:a[i] }); i += 1; j += 1; }
    else if (i < a.length && (j >= b.length || dp[i + 1][j] >= dp[i][j + 1])) { out.push({ type:'delete', text:a[i] }); i += 1; }
    else { out.push({ type:'insert', text:b[j] }); j += 1; }
  }
  return out;
}
