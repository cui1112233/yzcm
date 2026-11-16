type ReadKind = "config" | "tasks" | "schedules" | "detail";

// Keep independent read generations: a newer task read must not cancel config
// loading, while local configuration edits must invalidate older config reads.
export function createFetchRequestGuard() {
  const versions = { config: { current: 0 }, tasks: { current: 0 }, schedules: { current: 0 }, detail: { current: 0 } };
  let modelSaving = false;
  const next = (kind: ReadKind) => ++versions[kind].current;
  return {
    versions,
    next,
    current: (kind: ReadKind, value: number) => versions[kind].current === value,
    beginRefresh: () => ({ config: next("config"), tasks: next("tasks"), schedules: next("schedules") }),
    beginModelSave: () => { if (modelSaving) return false; modelSaving = true; return true; },
    endModelSave: () => { modelSaving = false; },
  };
}
