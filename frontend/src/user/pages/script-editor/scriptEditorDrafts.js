export function createAutosaveController({ save, delay = 800, onChange = () => {} }) {
  let draft = null, timer = null, pending = null, status = 'idle', error = null;
  const publish = () => onChange({ draft, status, error });
  async function perform() {
    if (!draft || pending) return pending;
    const snapshot = { ...draft };
    status = 'saving'; error = null; publish();
    pending = Promise.resolve(save(snapshot)).then(saved => {
      if (draft?.content === snapshot.content) draft = saved || snapshot;
      status = 'saved'; publish(); return saved;
    }).catch(reason => {
      error = reason; status = reason?.status === 409 ? 'conflict' : 'error'; publish(); return null;
    }).finally(() => { pending = null; });
    return pending;
  }
  return {
    edit(next) { draft = { ...(draft || {}), ...next }; status = 'dirty'; error = null; clearTimeout(timer); timer = setTimeout(perform, Math.max(0, delay)); publish(); },
    async flush() { clearTimeout(timer); timer = null; if (pending) await pending; if (status === 'dirty') await perform(); return draft; },
    reset(next) { clearTimeout(timer); timer = null; pending = null; draft = next ? { ...next } : null; status = 'idle'; error = null; publish(); },
    state() { return { draft, status, error }; },
    dispose() { clearTimeout(timer); timer = null; }
  };
}
