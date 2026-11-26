export function validateDelivery(scripts) {
  const episodes = [...new Set((scripts || []).map(row => row.episode))].sort((a, b) => a - b);
  const missing = episodes.filter(episode => !(scripts || []).some(row => row.episode === episode && row.preferred));
  return { ok: episodes.length > 0 && missing.length === 0, missing };
}
