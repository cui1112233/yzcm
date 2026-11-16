import { describe, expect, it, vi } from "vitest";

vi.mock("./api", () => ({ apiRequest: vi.fn() }));

import {
  batchStatusLabel,
  bookStatusLabel,
  createBatchFromIntake,
  createManualIntake,
  submitBatchFactoryBookTo121,
  fetchBatchFactoryOriginals,
  listBatchFactoryAutomationPresets,
  loadBatchFactoryV11,
  loadBatchFactoryProject,
  loadBatchFactoryBookWorkspace,
  loadBatchFactoryWorks,
  listBatchFactoryConfigVersions,
  batchBookCount,
  runProduction,
  saveBatchFactoryAssetPrompt,
  saveBatchFactoryBookContentWindow,
  saveBatchFactoryVideoPrompt,
  manualBookIdsFromInput,
  startBatchAutomation,
  type BatchFactoryV11Api,
} from "./batchFactoryV11";
import { normalizeBatchFactoryBook } from "./batchFactoryModels";

it("saves an asset prompt with its revision and verifies the returned asset", async () => {
  const api = { request: vi.fn().mockResolvedValue({ asset: { id: "asset?1", name: "林晚", prompt: "新版资产提示词", revision: 3 } }) };
  await expect(saveBatchFactoryAssetPrompt(api, "batch 1", "book/1", { id: "asset?1", name: "林晚", prompt: "新版资产提示词", revision: 2 })).resolves.toMatchObject({ prompt: "新版资产提示词", revision: 3 });
  expect(api.request).toHaveBeenCalledWith("/api/batch-factory/v12/batches/batch%201/books/book%2F1/assets/asset%3F1", { method: "PATCH", data: { name: "林晚", prompt: "新版资产提示词", expectedRevision: 2 } });
});

it("saves a video prompt only through the selected video override", async () => {
  const api = { request: vi.fn().mockResolvedValue({ scope: { kind: "video", batchId: "batch 1", bookId: "book/1", videoId: "video?1" }, revision: 4, patch: { videoPrompt: "新版镜头提示词" } }) };
  await expect(saveBatchFactoryVideoPrompt(api, "batch 1", "book/1", "video?1", "新版镜头提示词", 3)).resolves.toMatchObject({ revision: 4 });
  expect(api.request).toHaveBeenCalledWith("/api/batch-factory/v12/batches/batch%201/books/book%2F1/videos/video%3F1/override", { method: "PUT", data: { patch: { videoPrompt: "新版镜头提示词" }, expectedRevision: 3 } });
});

it("saves a book content window and verifies the persisted metadata by readback", async () => {
  const metadata = { keep: "yes", contentRangeLines: 8, contentCaptureCharacters: 8000 };
  const api = { request: vi.fn()
    .mockResolvedValueOnce({ book: { id: "book/1", revision: 8, sourceMetadata: metadata } })
    .mockResolvedValueOnce({ batch: { id: "batch 1", books: [{ id: "book/1", revision: 8, sourceMetadata: metadata }] } }) };
  await expect(saveBatchFactoryBookContentWindow(api, "batch 1", { id: "book/1", revision: 7, sourceMetadata: { keep: "yes" } }, { contentRangeLines: 8, contentCaptureCharacters: 8000 })).resolves.toMatchObject({ id: "book/1", revision: 8, sourceMetadata: metadata });
  expect(api.request).toHaveBeenNthCalledWith(1, "/api/batch-factory/v12/batches/batch%201/books/book%2F1/metadata", { method: "PUT", data: { metadata, expectedRevision: 7 } });
  expect(api.request).toHaveBeenNthCalledWith(2, "/api/batch-factory/v12/batches/batch%201");
});

it("rejects a content-window receipt or readback that does not confirm the requested values", async () => {
  const book = { id: "book-1", revision: 7, sourceMetadata: { keep: "yes" } };
  const values = { contentRangeLines: 8, contentCaptureCharacters: 8000 };
  await expect(saveBatchFactoryBookContentWindow({ request: vi.fn().mockResolvedValue({ book: { id: "book-1", revision: 7, sourceMetadata: values } }) }, "batch-1", book, values)).rejects.toThrow("内容范围保存回执无效");
  const api = { request: vi.fn()
    .mockResolvedValueOnce({ book: { id: "book-1", revision: 8, sourceMetadata: values } })
    .mockResolvedValueOnce({ batch: { id: "batch-1", books: [{ id: "book-1", revision: 8, sourceMetadata: { contentRangeLines: 5, contentCaptureCharacters: 4000 } }] } }) };
  await expect(saveBatchFactoryBookContentWindow(api, "batch-1", book, values)).rejects.toThrow("内容范围读回与保存不一致");
});

it('opens a known batch without waiting for the unselected full library', async () => {
  const request = vi.fn(async (path: string) => {
    if(path.endsWith('/batches')) return new Promise<any>(()=>{});
    return path.endsWith('/chosen') ? {batch:{id:'chosen',books:[]}} : {jobs:[]};
  });
  const result = await Promise.race([loadBatchFactoryV11({request} as BatchFactoryV11Api,'chosen'),new Promise<null>(resolve=>setTimeout(()=>resolve(null),30))]);
  expect(result?.batch?.id).toBe('chosen');
  expect(request.mock.calls.map(([path])=>path)).not.toContain('/api/batch-factory/v12/batches');
});

it('shows book content before slow asset image records and still completes those records', async () => {
  let complete!: (value: unknown)=>void;
  const images=new Promise(resolve=>{complete=resolve;});
  const request=vi.fn(async(path:string)=>path.endsWith('/images')?images:path.endsWith('/assets')?{assets:[{id:'hero'}]}:path.endsWith('/batches/b')?{batch:{id:'b',books:[{id:'book',sourceText:'正文'}]}}:{jobs:[]});
  const ready=vi.fn();
  const loading=loadBatchFactoryBookWorkspace({request} as BatchFactoryV11Api,'b','book',undefined,ready);
  await vi.waitFor(()=>expect(ready).toHaveBeenCalled());
  expect(ready.mock.calls[0][0]).toMatchObject({readState:'ready',sourceText:'正文',assetImagesLoading:true});
  complete({images:[{id:'img',url:'/image.png'}]});
  expect((await loading).assetImages.hero[0].id).toBe('img');
});

it("opens a source book by its factory record ID and reads its assets and completed videos", async () => {
  const rawBook = { id: "factory-book-1", bookId: "10151", title: "言笑晏晏", sourceText: "真实原文" };
  const view = normalizeBatchFactoryBook(rawBook);
  const api: BatchFactoryV11Api = { request: async (path: string): Promise<any> => {
    if (path.endsWith("/batches/batch-1")) return { batch: { id: "batch-1", books: [rawBook] } };
    if (path.endsWith("/books/factory-book-1/stages")) return { bookId: "factory-book-1", runs: [] };
    if (path.endsWith("/books/factory-book-1/assets")) return { assets: [{ id: "asset-1", name: "女主", prompt: "人物提示词" }] };
    if (path.endsWith("/assets/asset-1/images")) return { images: [{ id: "image-1", url: "/image.png" }] };
    if (path.endsWith("/status")) return { jobs: [{ bookId: "factory-book-1", tasks: [{ videoId: "video-1", status: "succeeded", mediaUrl: "/real.mp4" }] }] };
    if (path.endsWith("/merge-status")) return { jobs: [{ bookId: "factory-book-1", status: "succeeded", outputUrl: "/final.mp4" }] };
    throw new Error("not found");
  } };
  const workspace = await loadBatchFactoryBookWorkspace(api, "batch-1", view.bookId);
  expect(workspace.sectionErrors).toEqual({});
  expect(view.sourceBookId).toBe("10151");
  expect(workspace.assets[0]?.prompt).toBe("人物提示词");
  expect(workspace.assetImages["asset-1"]?.[0]?.url).toBe("/image.png");
  expect(workspace.production?.tasks[0]?.mediaUrl).toBe("/real.mp4");
  expect(workspace.mergeStatus?.jobs?.[0]?.bookId).toBe(workspace.bookId);
});

describe("batch factory v11 service", () => {
  it("keeps live summary coverMedia for authenticated rendering", async () => {
    const url = "/api/batch-factory/v11/batches/b/merge-cover/m";
    const works = await loadBatchFactoryWorks({ request: vi.fn().mockResolvedValue({ batches: [{ id: "b", coverMedia: { kind: "image", url } }] }) });
    expect(works[0].coverUrl).toBe(url);
  });
  it("reads V12 work summaries without downloading book details", async () => {
    const api = { request: vi.fn().mockResolvedValue({ batches: [{ id: "batch-1", bookCount: 7, title: "作品" }] }) };
    const works = await loadBatchFactoryWorks(api);
    expect(api.request.mock.calls).toEqual([["/api/batch-factory/v12/batches/summary"]]);
    expect(works[0].books).toEqual([]);
    expect(batchBookCount(works[0])).toBe(7);
    expect(batchBookCount({ id: "detail", books: [{ id: "book" }], bookCount: 9 })).toBe(1);
  });

  it("does not turn invalid summaries or failed reads into an empty account", async () => {
    await expect(loadBatchFactoryWorks({ request: vi.fn().mockResolvedValue({ error: "unexpected" }) })).rejects.toThrow("作品摘要");
    await expect(loadBatchFactoryWorks({ request: vi.fn().mockRejectedValue(new Error("offline")) })).rejects.toThrow("offline");
  });

  it("reads configuration versions separately from automation presets", async () => {
    const version = { id: "system-default-v1", name: "System Default", config: { source: "system" } };
    const api = { request: vi.fn().mockResolvedValue({ configVersions: [version] }) };
    expect(await listBatchFactoryConfigVersions(api)).toEqual([version]);
    expect(api.request).toHaveBeenCalledWith("/api/batch-factory/v12/config-versions");
    await expect(listBatchFactoryConfigVersions({ request: vi.fn().mockResolvedValue({ presets: [] }) })).rejects.toThrow("配置版本");
    await expect(listBatchFactoryConfigVersions({ request: vi.fn().mockRejectedValue(new Error("forbidden")) })).rejects.toThrow("forbidden");
  });
  it("loads a complete project through confirmed V12 routes", async () => {
    const api: BatchFactoryV11Api = {
      request: vi.fn()
        .mockResolvedValueOnce({ batches: [{ id: "batch-1", title: "作品" }] })
        .mockResolvedValueOnce({ batch: { id: "batch-1", books: [{ id: "book-1" }] } })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] })
        .mockResolvedValueOnce({ capabilities: { "production.submit": { available: false, reason: "provider offline" } } })
        .mockResolvedValueOnce({ automation: null }),
    };
    const result = await loadBatchFactoryProject(api, "batch-1");
    expect(result.readState).toBe("ready");
    expect(result.books[0].bookId).toBe("book-1");
    expect(result.capabilities["production.submit"]?.available).toBe(false);
    expect(vi.mocked(api.request).mock.calls.map(([path]) => path)).toEqual([
      "/api/batch-factory/v12/batches",
      "/api/batch-factory/v12/batches/batch-1",
      "/api/batch-factory/v12/batches/batch-1/status",
      "/api/batch-factory/v12/batches/batch-1/merge-status",
      "/api/batch-factory/v12/capabilities",
      "/api/batch-factory/v12/batches/batch-1/automation",
    ]);
  });

  it("loads a book workspace and submits production through the V12 book route", async () => {
    const api: BatchFactoryV11Api = {
      request: vi.fn()
        .mockResolvedValueOnce({ batch: { id: "batch-1", books: [{ id: "book-2", title: "第二本" }] } })
        .mockResolvedValueOnce({ stages: [{ stage: "director", status: "succeeded" }] })
        .mockResolvedValueOnce({ assets: [{ id: "asset-1", type: "character" }] })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] })
        .mockResolvedValueOnce({ images: [] }),
    };
    const result = await loadBatchFactoryBookWorkspace(api, "batch-1", "book-2");
    expect(result.readState).toBe("ready");
    expect(result.book?.title).toBe("第二本");
    expect(result.assets).toHaveLength(1);
    await runProduction(api as any, "batch-1", "book-2", { requestId: "req-1" });
    expect(api.request).toHaveBeenLastCalledWith("/api/batch-factory/v12/batches/batch-1/books/book-2/production", { method: "POST", data: { requestId: "req-1" } });
  });

  it("keeps the selected book usable when detail response is wrapped or omits the book list", async () => {
    const api: BatchFactoryV11Api = {
      request: vi.fn()
        .mockResolvedValueOnce({ data: { batch: { id: "batch-1" } } })
        .mockResolvedValueOnce({ data: { stages: [] } })
        .mockResolvedValueOnce({ data: { assets: [] } })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] }),
    };
    const result = await loadBatchFactoryBookWorkspace(api, "batch-1", "book-2", { id: "book-2", title: "第二本" });
    expect(result.readState).toBe("ready");
    expect(result.book?.id).toBe("book-2");
  });

  it("loads source, asset images, and only the selected book production media", async () => {
    const api: BatchFactoryV11Api = {
      request: vi.fn()
        .mockResolvedValueOnce({ data: { batch: { id: "batch-1", books: [{ id: "book-1", title: "第一本", sourceText: "其他正文" }, { id: "book-2", title: "第二本", sourceText: "第二本原文" }] } } })
        .mockResolvedValueOnce({ data: { stages: [{ stage: "director", status: "succeeded", prompt: "分镜提示词" }] } })
        .mockResolvedValueOnce({ data: { assets: [{ id: "asset-1", kind: "character", name: "林晟", prompt: "资产提示词" }] } })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [{ bookId: "book-1", tasks: [{ bookId: "book-1", videoId: "video-other", status: "succeeded", mediaUrl: "/other.mp4" }] }, { bookId: "book-2", tasks: [{ bookId: "book-2", videoId: "video-2", status: "succeeded", mediaUrl: "/video-2.mp4" }] }] })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] })
        .mockResolvedValueOnce({ data: { images: [{ id: "image-1", url: "/image-1.png", prompt: "资产图片提示词" }] } }),
    };

    const result = await loadBatchFactoryBookWorkspace(api, "batch-1", "book-2");

    expect(result.sourceText).toBe("第二本原文");
    expect(result.assets).toEqual([expect.objectContaining({ id: "asset-1", prompt: "资产提示词" })]);
    expect(result.assetImages).toEqual({ "asset-1": [expect.objectContaining({ id: "image-1", url: "/image-1.png" })] });
    expect(result.production?.tasks).toEqual([expect.objectContaining({ bookId: "book-2", videoId: "video-2", mediaUrl: "/video-2.mp4" })]);
    expect(result.production?.tasks).not.toEqual(expect.arrayContaining([expect.objectContaining({ videoId: "video-other" })]));
    expect(vi.mocked(api.request).mock.calls.map(([path]) => path)).toEqual([
      "/api/batch-factory/v12/batches/batch-1",
      "/api/batch-factory/v12/batches/batch-1/books/book-2/stages",
      "/api/batch-factory/v12/batches/batch-1/books/book-2/assets",
      "/api/batch-factory/v12/batches/batch-1/status",
      "/api/batch-factory/v12/batches/batch-1/merge-status",
      "/api/batch-factory/v12/batches/batch-1/books/book-2/assets/asset-1/images",
    ]);
  });
  it("loads batches and the selected batch with real status endpoints", async () => {
    const api: BatchFactoryV11Api = {
      request: vi.fn()
        .mockResolvedValueOnce({ batches: [{ id: "batch-1", title: "第一批" }] })
        .mockResolvedValueOnce({ batch: { id: "batch-1", books: [{ id: "book-1", title: "原著" }] } })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [{ status: "running", tasks: [{ bookId: "book-1", status: "running" }] }] })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] }),
    };

    const result = await loadBatchFactoryV11(api);
    expect(result.batches).toHaveLength(1);
    expect(result.batch?.books[0].status).toBe("生成中");
    expect(api.request).toHaveBeenNthCalledWith(1, "/api/batch-factory/v12/batches");
    expect(api.request).toHaveBeenNthCalledWith(3, "/api/batch-factory/v12/batches/batch-1/status");
  });

  it("keeps books when the selected batch is wrapped by the service response", async () => {
    const api: BatchFactoryV11Api = {
      request: vi.fn()
        .mockResolvedValueOnce({ batches: [{ id: "batch-1", title: "第一批" }] })
        .mockResolvedValueOnce({ data: { batch: { id: "batch-1", books: [{ bookId: "book-1", title: "原著" }] } } })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] }),
    };

    const result = await loadBatchFactoryV11(api);

    expect(result.batch?.books).toEqual([expect.objectContaining({ id: "book-1", bookId: "book-1", title: "原著" })]);
  });

  it("uses explicit server status before deriving a production status", async () => {
    const api: BatchFactoryV11Api = {
      request: vi.fn()
        .mockResolvedValueOnce({ batches: [{ id: "batch-1", title: "第一批" }] })
        .mockResolvedValueOnce({ batch: { id: "batch-1", books: [{ id: "book-1", title: "原著", status: "待审核" }] } })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [{ status: "succeeded", tasks: [{ bookId: "book-1", status: "succeeded" }] }] })
        .mockResolvedValueOnce({ batchId: "batch-1", jobs: [] }),
    };

    const result = await loadBatchFactoryV11(api);
    expect(result.batch?.books[0].status).toBe("待审核");
  });

  it("keeps unknown and empty states explicit", () => {
    expect(batchStatusLabel("queued")).toBe("排队中");
    expect(batchStatusLabel("new-status")).toBe("状态未返回");
    expect(bookStatusLabel({ id: "book-1" })).toBe("待开始");
  });

  it("parses and de-duplicates book IDs from line or comma input", () => {
    expect(manualBookIdsFromInput("2084012035524801698\n2084012035524801698 书名\n567168, 705142")).toEqual([
      "2084012035524801698",
      "567168",
      "705142",
    ]);
  });

  it("uses the real intake and automation contracts", async () => {
    const api: BatchFactoryV11Api = { request: vi.fn()
      .mockResolvedValueOnce({ results: [{ bookId: "567168", status: "ok", data: "正文" }] })
      .mockResolvedValueOnce({ intake: { id: "intake-1" } })
      .mockResolvedValueOnce({ batch: { id: "batch-1" } })
      .mockResolvedValueOnce({ presets: [{ id: "preset-1", name: "默认预设" }] })
      .mockResolvedValueOnce({ automation: { state: "running" } }) };

    await fetchBatchFactoryOriginals(api, { platform: "3", bookIds: ["567168"], maxTxt: 4000 });
    await createManualIntake(api, { title: "测试批次", platformId: "3", platformName: "七猫付费", parseMode: "smart", columnPresetId: "full_metadata", inputText: "567168", sourceTextByBookId: { "567168": "正文" } });
    await createBatchFromIntake(api, "intake-1", { title: "测试批次" });
    await listBatchFactoryAutomationPresets(api);
    await startBatchAutomation(api, "batch-1", { presetId: "preset-1", runMode: "video_no_submit", concurrency: 2, scheduledAt: "" });

    expect(api.request).toHaveBeenNthCalledWith(1, "/api/batch-factory/v12/fetch-originals", { method: "POST", data: { platform: "3", bookIds: ["567168"], maxTxt: 4000 } });
    expect(api.request).toHaveBeenNthCalledWith(2, "/api/batch-factory/v12/intakes/manual", { method: "POST", data: expect.objectContaining({ inputText: "567168", sourceTextByBookId: { "567168": "正文" } }) });
    expect(api.request).toHaveBeenNthCalledWith(3, "/api/batch-factory/v12/intakes/intake-1/batches", { method: "POST", data: { title: "测试批次" } });
    expect(api.request).toHaveBeenNthCalledWith(4, "/api/batch-factory/v12/automation-presets");
    expect(api.request).toHaveBeenNthCalledWith(5, "/api/batch-factory/v12/batches/batch-1/automation/start", { method: "POST", data: { presetId: "preset-1", runMode: "video_no_submit", concurrency: 2, scheduledAt: "" } });
  });

  it("submits one book through the deployed V12 publish-121 route", async () => {
    const api: BatchFactoryV11Api = { request: vi.fn().mockResolvedValue({
      status: "accepted_pending",
      receipt: { verified: false, remote_record: { found: false } },
    }) };

    await submitBatchFactoryBookTo121(api, "batch-1", "book-2");

    expect(api.request).toHaveBeenCalledWith(
      "/api/batch-factory/v12/batches/batch-1/books/book-2/publish-121",
      { method: "POST", data: {} },
    );
  });
});
