export function resolveCreatorLevel(center: any) {
  const backend = [center?.level, center?.member?.level, center?.member?.creatorLevel].find(value => value && Number.isFinite(Number(value.level)) && Number(value.level) > 0);
  if (backend) {
    const current = Number(backend.current ?? backend.growth ?? 0), next = Number(backend.next ?? backend.nextThreshold ?? current);
    return { level: Number(backend.level), name: String(backend.name || backend.title || "创作成长"), current: Number.isFinite(current) ? current : 0, percent: Number.isFinite(current) && next > 0 ? Math.max(0, Math.min(100, Math.round(current / next * 100))) : 0, source: "backend" };
  }
  const tokens = Number(center?.usage?.month?.totalTokens);
  if (!Number.isFinite(tokens) || tokens < 0) return null;
  const thresholds = [0, 50000, 200000, 500000, 1000000, 3000000];
  const names = ["灵感新芽", "创作学徒", "灵感导演", "故事导演", "AI 制片人", "创作大师"];
  const index = thresholds.reduce((found, threshold, i) => tokens >= threshold ? i : found, 0);
  return { level: index + 1, name: names[index], current: tokens - thresholds[index], percent: index === 5 ? 100 : Math.round((tokens - thresholds[index]) / (thresholds[index + 1] - thresholds[index]) * 100), source: "usage" };
}
