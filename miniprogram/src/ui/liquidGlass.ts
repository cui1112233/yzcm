import type { GlassRect } from '../pages/functions/glassOptics';
export const GLASS_PREFERENCE = 'yzcm.mobile.glass.reduceTransparency';
export const GLASS_CHANGE = 'yzcm:glass-preference';
export const GLASS_SCROLL = 'yzcm:glass-scroll';
export function liquidEnabled(className: string) {
  const classes = className.split(/\s+/);
  return classes.includes('mp-primary-page') || classes.includes('mp-profile-page') || classes.includes('mp-profile-detail-page');
}
export function liquidPageKey(className: string) {
  const classes = new Set(className.split(/\s+/));
  if (classes.has('mp-workspace-home')) return 'workspace';
  if (classes.has('mp-tasks-page')) return 'tasks';
  if (classes.has('mp-functions-page')) return 'functions';
  if (classes.has('mp-profile-detail-page')) return 'profile-detail';
  if (classes.has('mp-profile-page')) return 'profile';
  return 'public';
}
export const glassSelectors = [
  '.tools-entry', '.tools-glass-feature', '.mp-workspace-action', '.mp-workspace-current', '.mp-workspace-local',
  '.mp-card', '.mp-workspace-empty', '.mp-profile-hero', '.mp-usage-card', '.mp-profile-section',
  '.mp-task-card-new', '.studio-setting', '.studio-source', '.studio-results', '.mp-tabs',
];
export function visibleGlassRects(rects: GlassRect[], windowHeight: number) {
  const keys = new Set<string>();
  return rects.filter(r => {
    if (!r || r.width < 70 || r.height < 42 || r.height > windowHeight || r.top < 0 || r.top + r.height > windowHeight || r.width * r.height > 180000) return false;
    const key = [r.left, r.top, r.width, r.height].map(Math.round).join(':');
    if (keys.has(key)) return false; keys.add(key); return true;
  }).slice(0, 8);
}
export function glassVersion() {
  let value = 0;
  return { current: () => value, invalidate: () => ++value, valid: (candidate: number) => candidate === value };
}
