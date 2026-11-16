import { describe, expect, test } from "vitest";
import { createFetchRequestGuard } from "./fetchWorkspaceRequests";

function deferred() { let resolve!: () => void; const promise = new Promise<void>((done) => { resolve = done; }); return { promise, resolve }; }

describe("fetch workspace request ordering", () => {
  test("a deferred refresh cannot overwrite edited configuration or a newer task read", async () => {
    const guard = createFetchRequestGuard();
    const request = deferred();
    const snapshot = guard.beginRefresh();
    let config = "initial", tasks = "initial", schedules = "initial";
    const refresh = request.promise.then(() => {
      if (guard.current("config", snapshot.config)) config = "old config";
      if (guard.current("tasks", snapshot.tasks)) tasks = "old tasks";
      if (guard.current("schedules", snapshot.schedules)) schedules = "old schedules";
    });
    guard.next("config"); config = "user edit";
    guard.next("tasks"); tasks = "new tasks";
    guard.next("schedules"); schedules = "new schedules";
    request.resolve(); await refresh;
    expect({ config, tasks, schedules }).toEqual({ config: "user edit", tasks: "new tasks", schedules: "new schedules" });
  });

  test("model writes stay locked through deferred server readback", async () => {
    const guard = createFetchRequestGuard();
    const write = deferred(), read = deferred();
    const calls: string[] = [];
    async function save(id: string) {
      if (!guard.beginModelSave()) return;
      try { calls.push(id); await write.promise; await read.promise; } finally { guard.endModelSave(); }
    }
    const first = save("A"); await save("B");
    write.resolve(); await Promise.resolve(); await save("B");
    expect(calls).toEqual(["A"]);
    read.resolve(); await first; await save("B");
    expect(calls).toEqual(["A", "B"]);
  });

  test("closing detail invalidates a deferred response", async () => {
    const guard = createFetchRequestGuard(), request = deferred();
    const version = guard.next("detail"); let detail: string | null = "loading";
    const pending = request.promise.then(() => { if (guard.current("detail", version)) detail = "reopened"; });
    guard.next("detail"); detail = null; request.resolve(); await pending;
    expect(detail).toBeNull();
  });
});
