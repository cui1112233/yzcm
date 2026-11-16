// Coalesce bridge-bound gesture updates, keeping the final input for release.
export function latestFrame<T>(update: (value: T) => void, interval = 34) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  let pending: { value: T } | undefined;
  const flush = () => {
    clearTimeout(timer); timer = undefined;
    const next = pending; pending = undefined;
    if (next) update(next.value);
  };
  return {
    push(value: T) {
      pending = { value };
      if (timer === undefined) timer = setTimeout(flush, interval);
    },
    flush,
    cancel() { clearTimeout(timer); timer = undefined; pending = undefined; },
  };
}
