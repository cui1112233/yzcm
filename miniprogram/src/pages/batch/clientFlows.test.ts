import { expect, test, vi } from "vitest";
vi.mock("../../services/batchLocalAccess", () => ({ batchLocalRequest: vi.fn() }));
import { loadBatchFactoryBookWorkspace, importBatchBooks, saveBatchConfigVersion, assertBatchActionReceipt, startBatchExecution } from "../../services/batchFactoryV11";

test("new batch applies the selected global template and reads it back before starting automation", async () => {
  const calls: string[] = [];
  let batchReads = 0;
  const api = { request: async (path: string, options?: any): Promise<any> => {
    calls.push(path);
    if (path.endsWith("fetch-originals")) return { results: [{ bookId: "123456", status: "ok", data: "正文" }] };
    if (path.endsWith("manual")) return { batch: { id: "created", settingsState: { revision: 0 }, books: [] } };
    if (path.endsWith("/books/book-1/metadata")) return { book: { id: "book-1", revision: 3, sourceMetadata: { contentRangeLines: 5, contentCaptureCharacters: 4000 } } };
    if (path.endsWith("/settings")) return { scope: { kind: "batch", batchId: "created" }, revision: 1, patch: { model: "chosen" } };
    if (path.endsWith("/batches/created")) {
      batchReads += 1;
      return { batch: { id: "created", settingsState: batchReads >= 3 ? { revision: 1, patch: { model: "chosen" } } : { revision: 0 }, books: [{ id: "book-1", revision: batchReads === 1 ? 2 : 3, sourceMetadata: batchReads === 1 ? {} : { contentRangeLines: 5, contentCaptureCharacters: 4000 } }] } };
    }
    return { automation: { batchId: "created", state: "running" } };
  } };
  const result = await importBatchBooks(api, { title: "作品", groups: [{ platformId: "p", platformName: "平台", inputText: "123456" }], configVersion: { id: "v", config: { model: "chosen" } }, execution: "immediate", presetId: "preset", runMode: "video_no_submit", concurrency: 2 });
  expect(result.warnings).toEqual([]);
  expect(calls.map(path => path.split("/").pop())).toEqual(["fetch-originals", "manual", "created", "metadata", "created", "settings", "created", "start", "automation"]);
});

test("new batch saves the selected capture and AI line defaults for every created book before automation", async () => {
  const calls: Array<{ path: string; data?: unknown }> = [];
  let batchReads = 0;
  const api = { request: async (path: string, options?: { data?: unknown }): Promise<any> => {
    calls.push({ path, data: options?.data });
    if (path.endsWith("/fetch-originals")) return { results: [{ bookId: "123456", status: "ok", data: "正文" }] };
    if (path.endsWith("/intakes/manual")) return { intakeId: "intake-1" };
    if (path.endsWith("/intakes/intake-1/batches")) return { batch: { id: "created", books: [] } };
    if (path.endsWith("/batches/created")) {
      batchReads += 1;
      return { batch: { id: "created", books: [{ id: "book-1", revision: batchReads === 1 ? 2 : 3, sourceMetadata: batchReads === 1 ? {} : { contentRangeLines: 8, contentCaptureCharacters: 8000 } }] } };
    }
    if (path.endsWith("/books/book-1/metadata")) return { book: { id: "book-1", revision: 3, sourceMetadata: { contentRangeLines: 8, contentCaptureCharacters: 8000 } } };
    if (path.endsWith("/automation/start")) return { automation: { batchId: "created", state: "running" } };
    return { automation: { batchId: "created", state: "running" } };
  } };

  await importBatchBooks(api, {
    title: "内容窗口测试", groups: [{ platformId: "p", platformName: "平台", inputText: "123456" }],
    contentRangeLines: 8, contentCaptureCharacters: 8000,
    execution: "immediate", presetId: "preset", runMode: "video_no_submit", concurrency: 2,
  });

  expect(calls[0]).toEqual({ path: "/api/batch-factory/v12/fetch-originals", data: { platform: "p", bookIds: ["123456"], maxTxt: 8000 } });
  expect(calls.find(call => call.path.endsWith("/books/book-1/metadata"))?.data).toEqual({ metadata: { contentRangeLines: 8, contentCaptureCharacters: 8000 }, expectedRevision: 2 });
  expect(calls.filter(call => call.path.endsWith("/batches/created"))).toHaveLength(2);
});

test("new batch does not start automation when the imported book defaults cannot be confirmed", async () => {
  const paths: string[] = [];
  const api = { request: async (path: string): Promise<any> => {
    paths.push(path);
    if (path.endsWith("/fetch-originals")) return { results: [{ bookId: "123456", status: "ok", data: "正文" }] };
    if (path.endsWith("/intakes/manual")) return { intakeId: "intake-1" };
    if (path.endsWith("/intakes/intake-1/batches")) return { batch: { id: "created", books: [] } };
    if (path.endsWith("/batches/created")) return { batch: { id: "created", books: [{ id: "book-1", revision: 2, sourceMetadata: {} }] } };
    throw new Error("内容范围保存被拒绝");
  } };

  const result = await importBatchBooks(api, {
    title: "内容窗口失败", groups: [{ platformId: "p", platformName: "平台", inputText: "123456" }],
    contentRangeLines: 8, contentCaptureCharacters: 8000,
    execution: "immediate", presetId: "preset", runMode: "video_no_submit", concurrency: 2,
  });

  expect(result.warnings.join(" ")).toContain("内容范围保存被拒绝");
  expect(paths.some(path => path.endsWith("/automation/start"))).toBe(false);
});

test("template failure keeps the newly created batch reachable and prevents production startup", async () => {
  const paths: string[] = [];
  const api = { request: async (path: string): Promise<any> => { paths.push(path); if (path.endsWith("fetch-originals")) return { results: [] }; if (path.endsWith("manual")) return { batch: { id: "created", books: [] } }; throw new Error("配置写入失败"); } };
  const result = await importBatchBooks(api, { title: "作品", groups: [{ platformId: "p", platformName: "平台", inputText: "123456" }], configVersion: { id: "v", config: { model: "chosen" } }, execution: "immediate", presetId: "preset", runMode: "video_no_submit", concurrency: 2 });
  expect(result.batch.id).toBe("created"); expect(result.warnings.join(" ")).toContain("配置写入失败");
  expect(paths.some(path => path.endsWith("/start"))).toBe(false);
});

test("an existing batch starts through its own automation endpoint and verifies persisted schedule", async () => {
  const request = vi.fn().mockResolvedValueOnce({ automation: { batchId: "existing", state: "scheduled" } }).mockResolvedValueOnce({ automation: { batchId: "existing", state: "scheduled", scheduledAt: "2099-01-01T00:00:00.000Z" } });
  await expect(startBatchExecution({ request }, "existing", { presetId: "p", runMode: "video_no_submit", concurrency: 2, scheduledAt: "2099-01-01T00:00:00.000Z" })).resolves.toMatchObject({ state: "scheduled" });
  expect(request.mock.calls).toEqual([["/api/batch-factory/v12/batches/existing/automation/start", { method: "POST", data: { presetId: "p", runMode: "video_no_submit", autoPublish: false, concurrency: 2, scheduledAt: "2099-01-01T00:00:00.000Z" } }], ["/api/batch-factory/v12/batches/existing/automation"]]);
});

test("invalid schedules and preset selections do not send a production request", async () => {
  const api = { request: vi.fn() };
  await expect(startBatchExecution(api, "b", { presetId: "p", runMode: "video_no_submit", concurrency: 2, scheduledAt: "2020-01-01T00:00:00Z" })).rejects.toThrow(/未来/);
  await expect(startBatchExecution(api, "b", { presetId: "", runMode: "video_no_submit", concurrency: 2 })).rejects.toThrow(/预设/);
  expect(api.request).not.toHaveBeenCalled();
});

test.each(["idle", "failed", "cancelled", "unknown"])("startup does not report success when readback is %s", async (state) => {
  const request = vi.fn().mockResolvedValueOnce({ automation: { batchId: "b", state: "running" } }).mockResolvedValueOnce({ automation: { batchId: "b", state } });
  await expect(startBatchExecution({ request }, "b", { presetId: "p", runMode: "video_no_submit", concurrency: 2 })).rejects.toThrow(/状态未确认/);
  expect(request).toHaveBeenCalledTimes(2);
});

test("startup rejects another batch's receipt before reading further", async () => {
  const request = vi.fn().mockResolvedValueOnce({ automation: { batchId: "other", state: "running" } });
  await expect(startBatchExecution({ request }, "b", { presetId: "p", runMode: "video_no_submit", concurrency: 2 })).rejects.toThrow(/回执无效/);
  expect(request).toHaveBeenCalledTimes(1);
});

test("scheduled startup rejects a different persisted time", async () => {
  const request = vi.fn().mockResolvedValueOnce({ automation: { state: "scheduled" } }).mockResolvedValueOnce({ automation: { state: "scheduled", scheduledAt: "2099-01-02T00:00:00Z" } });
  await expect(startBatchExecution({ request }, "b", { presetId: "p", runMode: "video_no_submit", concurrency: 2, scheduledAt: "2099-01-01T00:00:00Z" })).rejects.toThrow(/时间读回不一致/);
});

test("partial book failures remain visible instead of becoming empty results", async () => {
  const workspace = await loadBatchFactoryBookWorkspace({ request: async (path: string) => {
    if (path.endsWith("/batches/b")) return { batch: { id: "b", books: [{ id: "book" }] } } as any;
    throw new Error("read unavailable");
  } }, "b", "book");
  expect(workspace.sectionErrors).toMatchObject({ stages: "read unavailable", assets: "read unavailable", production: "read unavailable", merge: "read unavailable" });
});

test("manual import fetches source before intake and starts only after batch receipt", async () => {
  const calls: Array<{path: string; data: any}> = [];
  let batchReads = 0;
  const api = { request: async (path: string, options?: any) => {
    calls.push({path, data: options?.data});
    if (path.endsWith("fetch-originals")) return { results: [{ bookId: "123456", status: "ok", data: "正文" }] } as any;
    if (path.endsWith("manual")) return { batch: { id: "created", books: [] } } as any;
    if (path.endsWith("/batches/created")) {
      batchReads += 1;
      return { batch: { id: "created", books: [{ id: "book-1", revision: batchReads === 1 ? 2 : 3, sourceMetadata: batchReads === 1 ? {} : { contentRangeLines: 5, contentCaptureCharacters: 4000 } }] } } as any;
    }
    if (path.endsWith("/books/book-1/metadata")) return { book: { id: "book-1", revision: 3, sourceMetadata: { contentRangeLines: 5, contentCaptureCharacters: 4000 } } } as any;
    return { automation: { state: "running" } } as any;
  } };
  const result = await importBatchBooks(api, { title: "测试", groups: [{ platformId: "p", platformName: "平台", inputText: "123456" }], execution: "immediate", presetId: "preset", runMode: "video_no_submit", concurrency: 2 });
  expect(result.batch.id).toBe("created");
  expect(calls.map(call => call.path.split("/").pop())).toEqual(["fetch-originals", "manual", "created", "metadata", "created", "start", "automation"]);
  expect(calls[1].data.groups[0].sourceTextByBookId).toEqual({ "123456": "正文" });
});

test("saving configuration uses revision and returns fresh server readback", async () => {
  const calls: any[] = [];
  const api = { request: vi.fn(async (path: string, options?: any) => {
    calls.push({ path, options });
    return options ? { scope: { kind: "batch", batchId: "b" }, patch: { model: "chosen" }, revision: 4 } : { batch: { id: "b", settingsState: { revision: 4, patch: { model: "chosen" } }, books: [] } } as any;
  }) };
  const result = await saveBatchConfigVersion(api, "b", { id: "v", config: { model: "chosen" } }, 3);
  expect(calls[0].options.data).toEqual({ patch: { model: "chosen" }, expectedRevision: 3 });
  expect(calls[1].options).toBeUndefined();
  expect((result as any).settingsState.revision).toBe(4);
});

test.each([{ ok: false }, { error: "rejected" }, { metadata: "unknown" }, { job: { id: "j", batchId: "b", bookId: "book", status: "failed" } }])("rejects unsuccessful or unknown action response %j", receipt => {
  expect(() => assertBatchActionReceipt("production", receipt, "b", "book")).toThrow();
});

test("only matching persisted jobs and director runs qualify as action receipts", () => {
  expect(() => assertBatchActionReceipt("production", { job: { id: "j", batchId: "b", bookId: "book", status: "queued" } }, "b", "book")).not.toThrow();
  expect(() => assertBatchActionReceipt("merge", { job: { id: "j", batchId: "other", bookId: "book", status: "queued" } }, "b", "book")).toThrow();
  expect(() => assertBatchActionReceipt("director", { bookId: "book", runs: [{ id: "r", batchId: "b", bookId: "book", stage: "director", status: "succeeded" }] }, "b", "book")).not.toThrow();
});

test.each([
  {},
  { batch: { id: "other", settingsState: { revision: 4, patch: { model: "chosen" } } } },
  { batch: { id: "b", settingsState: { revision: 3, patch: { model: "chosen" } } } },
  { batch: { id: "b", settingsState: { revision: 4, patch: { model: "old" } } } },
])("never confirms malformed, wrong entity, stale or mismatched configuration readback %j", async readback => {
  const api = { request: vi.fn().mockResolvedValueOnce({ scope: { kind: "batch", batchId: "b" }, patch: { model: "chosen" }, revision: 4 }).mockResolvedValueOnce(readback) };
  await expect(saveBatchConfigVersion(api, "b", { id: "v", config: { model: "chosen" } }, 3)).rejects.toThrow();
});

test.each([{ ok: false }, { error: "rejected" }, { ok: true }, { scope: { kind: "batch", batchId: "b" }, revision: 3, patch: { model: "chosen" } }])("rejects unconfirmed settings save without readback %j", async receipt => {
  const api = { request: vi.fn().mockResolvedValue(receipt) };
  await expect(saveBatchConfigVersion(api, "b", { id: "v", config: { model: "chosen" } }, 3)).rejects.toThrow();
  expect(api.request).toHaveBeenCalledTimes(1);
});

test("write guard rejection stops import before intake or automation requests", async () => {
  const paths: string[] = [];
  const api = { request: async (path: string): Promise<any> => { paths.push(path); throw new Error("未发送操作请求"); } };
  await expect(importBatchBooks(api, { title: "", groups: [{ platformId: "p", platformName: "P", inputText: "123456" }], execution: "immediate", presetId: "preset", runMode: "full_submit", concurrency: 2 })).rejects.toThrow("未发送操作请求");
  expect(paths).toEqual(["/api/batch-factory/v12/fetch-originals"]);
});
