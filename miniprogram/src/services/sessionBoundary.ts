let revision = 0;
const listeners = new Set<() => void>();

export function sessionRevision() { return revision; }
export function invalidateSessionResources() {
  revision += 1;
  listeners.forEach((listener) => listener());
}
export function onSessionInvalidated(listener: () => void) {
  listeners.add(listener);
  return () => { listeners.delete(listener); };
}
