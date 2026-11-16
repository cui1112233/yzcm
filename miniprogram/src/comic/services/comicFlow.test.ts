import { beforeEach, describe, expect, test, vi } from "vitest";

const deps = vi.hoisted(() => ({
  request: vi.fn(),
  readFile: vi.fn(),
  works: vi.fn(),
  presets: vi.fn(),
  loadV11: vi.fn(),
  start: vi.fn(),
  automation: vi.fn(),
  create: vi.fn(),
}));
const apiPath = vi.hoisted(() => ({ batchFactoryV11Api: { request: deps.request } }));
const media = vi.hoisted(() => ({ load: vi.fn() }));

vi.mock("../../services/api", () => ({ apiRequest: deps.request }));
vi.mock("../../services/mediaCache", () => ({ loadAuthenticatedMedia: media.load }));
vi.mock("@tarojs/taro", () => ({
  default: {
    env: { USER_DATA_PATH: "/tmp" },
    getFileSystemManager: () => ({ readFile: deps.readFile }),
  },
}));
vi.mock("../../services/batchFactoryV11", () => ({
  BATCH_FACTORY_BASE: "/api/batch-factory/v12",
  batchFactoryV11Api: apiPath.batchFactoryV11Api,
  listBatchFactoryAutomationPresets: deps.presets,
  loadBatchFactoryAutomation: deps.automation,
  loadBatchFactoryV11: deps.loadV11,
  loadBatchFactoryWorks: deps.works,
  startBatchAutomation: deps.start,
  createBatchFactoryBatch: deps.create,
}));

import {
  buildComicSettingsPatch,
  buildComicStartPayload,
  comicBooksWithoutSource,
  comicFailureText,
  comicNewErrorText,
  comicPollDelay,
  comicPresetAvailability,
  comicPresetBlockReason,
  comicPresetComicConfig,
  comicPresetComicReason,
  comicPresetOptions,
  comicProgress,
  comicSourceLength,
  createComicBatch,
  buildComicInitialBatchSettings,
  defaultComicBatchTitle,
  fillComicBookSource,
  loadComicBooks,
  loadComicPrivateSetup,
  loadComicSetup,
  MIN_COMIC_SOURCE_CHARS,
  normalizeStylePresets,
  resolveComicStyleThumb,
  suggestComicTitle,
  submitComicRun,
  uploadComicReference,
  validateComicSource,
  validateComicSubmission,
  startComicPolling,
} from "./comicFlow";

const baseSubmission = {
  batchId: "batch-1",
  bookIds: ["book-1"],
  presetId: "preset-1",
  runMode: "video_no_submit" as const,
  aspectRatio: "9:16",
  stylePresetId: "style-1",
};

beforeEach(() => { vi.clearAllMocks(); });

describe("漫画提交链路", () => {
  test("只保留 batch.comic-style 的已发布画风预设", () => {
    expect(normalizeStylePresets({
      catalog: [
        { id: "a", name: "日漫", slot: "batch.comic-style" },
        { id: "b", name: "视频", slot: "batch.video-meta" },
        { id: "c", name: "无槽位", slot: null },
      ],
    })).toEqual([{ id: "a", name: "日漫", previewUrl: "" }]);
  });

  test("settingsPatch 只发送契约确认过的字段", () => {
    expect(buildComicSettingsPatch({ aspectRatio: "3:4", stylePresetId: "style-1" })).toEqual({
      productionMode: "comic",
      imageAspectRatio: "3:4",
      aiPromptConfig: { comic: { enabled: true, style: { presetId: "style-1" } } },
    });
    const withReference = buildComicSettingsPatch({
      aspectRatio: "9:16",
      stylePresetId: "style-1",
      reference: { assetId: "asset-1", url: "https://cdn/x.png", previewUrl: "https://cdn/p.png" },
    });
    expect((withReference.aiPromptConfig.comic as any).globalReferenceImage).toEqual({
      assetId: "asset-1", url: "https://cdn/x.png", previewUrl: "https://cdn/p.png",
      enabled: true, applyToAssets: true, applyToPages: true, source: "manual-upload",
    });
  });

  test("带完整预设时整块发送 comic：只改画风，资产与分页提示词不被抹掉", () => {
    const preset = {
      id: "p2",
      name: "漫画预设",
      config: {
        textModelId: "t1",
        imageModelId: "i1",
        // automation-presets 的实际字段路径：config.aiPromptConfig.comic
        aiPromptConfig: {
          comic: {
            enabled: true,
            body: "分页提示词正文",
            assets: { presetId: "assets-p1", presetName: "资产预设", body: "资产提取正文", version: 3 },
            style: { presetId: "old-style", presetName: "旧画风", body: "旧画风正文" },
            constraints: [{ presetId: "c1", body: "约束一" }],
            imageDerivation: { presetId: "d1", body: "派生正文" },
            imagePromptPrefix: "前缀",
            imagePromptSuffix: "后缀",
          },
        },
      },
    };
    expect(comicPresetComicConfig(preset as any)).toMatchObject({ body: "分页提示词正文", imagePromptPrefix: "前缀" });
    expect(comicPresetComicReason(preset as any)).toBe("");

    const patch = buildComicSettingsPatch({ aspectRatio: "9:16", stylePresetId: "style-1" }, preset as any);
    const comic = patch.aiPromptConfig.comic as any;
    expect(comic.assets).toEqual({ presetId: "assets-p1", presetName: "资产预设", body: "资产提取正文", version: 3 });
    expect(comic.body).toBe("分页提示词正文");
    expect(comic.constraints).toEqual([{ presetId: "c1", body: "约束一" }]);
    expect(comic.imageDerivation).toEqual({ presetId: "d1", body: "派生正文" });
    expect(comic.imagePromptPrefix).toBe("前缀");
    expect(comic.imagePromptSuffix).toBe("后缀");
    // 画风只替换 presetId，预设 style 的其余字段保留；enabled 强制为 true。
    expect(comic.style).toEqual({ presetId: "style-1", presetName: "旧画风", body: "旧画风正文" });
    expect(comic.enabled).toBe(true);
    // 画幅仍是顶层设置项。
    expect(patch.imageAspectRatio).toBe("9:16");
  });

  test("带参考图时保留预设其它字段，只覆盖 globalReferenceImage 的用户改动", () => {
    const preset = {
      id: "p2",
      config: {
        aiPromptConfig: {
          comic: {
            enabled: true,
            body: "分页提示词正文",
            assets: { body: "资产提取正文" },
            globalReferenceImage: { assetId: "old", url: "https://cdn/old.png", enabled: false, applyToAssets: false },
          },
        },
      },
    };
    const patch = buildComicSettingsPatch({
      aspectRatio: "9:16", stylePresetId: "style-1",
      reference: { assetId: "asset-1", url: "https://cdn/x.png" },
    }, preset as any);
    const comic = patch.aiPromptConfig.comic as any;
    expect(comic.assets.body).toBe("资产提取正文");
    expect(comic.body).toBe("分页提示词正文");
    expect(comic.globalReferenceImage).toMatchObject({
      assetId: "asset-1", url: "https://cdn/x.png",
      enabled: true, applyToAssets: true, applyToPages: true, source: "manual-upload",
    });
  });

  test("预设缺 comic.assets.body 或 comic.body 时提交前拦下并给具体原因", async () => {
    const noAssets = { id: "p1", config: { textModelId: "t1", imageModelId: "i1", aiPromptConfig: { comic: { enabled: true, body: "分页" } } } };
    expect(comicPresetComicReason(noAssets as any)).toContain("资产提示词");
    await expect(submitComicRun(baseSubmission, noAssets as any)).rejects.toThrow("资产提示词");
    expect(deps.start).not.toHaveBeenCalled();

    const noBody = { id: "p2", config: { textModelId: "t1", imageModelId: "i1", aiPromptConfig: { comic: { enabled: true, assets: { body: "资产" } } } } };
    expect(comicPresetComicReason(noBody as any)).toContain("分页提示词");
    await expect(submitComicRun(baseSubmission, noBody as any)).rejects.toThrow("分页提示词");
    expect(deps.start).not.toHaveBeenCalled();
  });

  test("只有 presetId 没有 body 的预设能通过预检，载荷保留引用交给后端 hydrate", async () => {
    // 线上 P#1「漫画」的真实结构：只有引用，没有 assets.body / comic.body。
    const preset = {
      id: "p1",
      name: "漫画",
      config: {
        textModelId: "t1", imageModelId: "i1",
        aiPromptConfig: {
          comic: {
            enabled: true,
            presetId: "batch-comic-page",
            assets: { presetId: "batch-comic-assets", presetName: "Q版漫画资产", presetSlot: "batch.comic-assets" },
          },
        },
      },
    };
    expect(comicPresetComicReason(preset as any)).toBe("");
    expect(comicPresetOptions([preset as any])[0]).toMatchObject({ usable: true, reason: "" });

    deps.start.mockResolvedValue({});
    await submitComicRun(baseSubmission, preset as any);
    const comic = (deps.start.mock.calls[0][2] as any).settingsPatch.aiPromptConfig.comic;
    expect(comic.assets.presetId).toBe("batch-comic-assets");
    expect(comic.presetId).toBe("batch-comic-page");
    expect(comic.body).toBeUndefined();
  });

  test("comic 缺失或未启用时给出「没有配置漫画」", () => {
    expect(comicPresetComicReason({ id: "p0", config: { textModelId: "t1", imageModelId: "i1" } } as any)).toBe("这个预设没有配置漫画");
    expect(comicPresetComicReason({ id: "p0", config: { aiPromptConfig: { comic: { enabled: false, presetId: "x", assets: { presetId: "y" } } } } } as any)).toBe("这个预设没有配置漫画");
  });

  test("默认选中第一条漫画可用的预设，而不是列表第一条", () => {
    const options = comicPresetOptions([
      { id: "p0", name: "普通", config: { textModelId: "t1", imageModelId: "i1" } },
      { id: "p1", name: "漫画", config: { textModelId: "t1", imageModelId: "i1", aiPromptConfig: { comic: { enabled: true, presetId: "page", assets: { presetId: "assets" } } } } },
    ]);
    expect(options[0]).toMatchObject({ usable: false, reason: "这个预设没有配置漫画" });
    expect(options.find((option) => option.usable)?.preset.id).toBe("p1");
  });

  test("提交时把预设 comic 整块放进启动载荷", async () => {
    const preset = {
      id: "p2",
      config: {
        textModelId: "t1", imageModelId: "i1",
        aiPromptConfig: { comic: { enabled: true, body: "分页提示词正文", assets: { body: "资产提取正文" } } },
      },
    };
    deps.start.mockResolvedValue({});
    await submitComicRun(baseSubmission, preset as any);
    const payload = deps.start.mock.calls[0][2] as any;
    expect(payload.settingsPatch.aiPromptConfig.comic.assets.body).toBe("资产提取正文");
    expect(payload.settingsPatch.aiPromptConfig.comic.body).toBe("分页提示词正文");
    expect(payload.settingsPatch.aiPromptConfig.comic.style.presetId).toBe("style-1");
  });

  test("提交前校验必填项", () => {
    expect(validateComicSubmission(baseSubmission)).toBe("");
    expect(validateComicSubmission({ ...baseSubmission, bookIds: [] })).toBe("请至少选择一本书");
    expect(validateComicSubmission({ ...baseSubmission, presetId: "" })).toBe("请选择自动化预设");
    expect(validateComicSubmission({ ...baseSubmission, stylePresetId: "" })).toBe("请选择漫画画风");
    expect(validateComicSubmission({ ...baseSubmission, aspectRatio: "5:4" })).toBe("画幅比例无效");
  });

  test("启动载荷包含 productionMode 与设置补丁", () => {
    expect(buildComicStartPayload(baseSubmission)).toEqual({
      presetId: "preset-1",
      productionMode: "comic",
      runMode: "video_no_submit",
      bookIds: ["book-1"],
      autoPublish: false,
      settingsPatch: buildComicSettingsPatch(baseSubmission),
    });
  });

  test("提交补丁带上预设里的文本/图片模型 id", () => {
    const preset = {
      id: "auto-preset-1",
      config: {
        textModelId: "custom-a-gemini-3-7-flash",
        imageModelId: "custom-image-1",
        aiPromptConfig: { comic: { enabled: true, presetId: "p-comic", assets: { presetId: "p-assets" } } },
      },
    };
    const payload = buildComicStartPayload(baseSubmission, preset);
    expect(payload.settingsPatch).toMatchObject({
      productionMode: "comic",
      textModelId: "custom-a-gemini-3-7-flash",
      imageModelId: "custom-image-1",
      aiPromptConfig: {
        comic: {
          enabled: true,
          style: expect.objectContaining({ presetId: "style-1" }),
          assets: expect.objectContaining({ presetId: "p-assets" }),
        },
      },
    });
  });

  test("轮询间隔从 3 秒指数退避到 10 秒封顶", () => {
    expect([0, 1, 2, 3, 4, 8].map(comicPollDelay)).toEqual([3000, 6000, 10000, 10000, 10000, 10000]);
  });

  test("进度只使用服务端 counts，结束状态才停止轮询", () => {
    expect(comicProgress({ state: "running", counts: { total: 8, running: 2, queued: 1, succeeded: 4, failed: 1 } })).toMatchObject({
      state: "running", total: 8, running: 3, succeeded: 4, failed: 1, finished: false,
    });
    expect(comicProgress({ state: "completed", counts: {} }).finished).toBe(true);
    expect(comicProgress(null)).toMatchObject({ state: "idle", total: 0, finished: false });
  });

  test("读取批次书籍只映射 id、标题、是否有正文与修订号", async () => {
    deps.loadV11.mockResolvedValue({ batch: { id: "batch-1", books: [{ id: "book-1", title: "第一本", sourceText: "正文", revision: 2 }, { id: "book-2", name: "第二本" }] } });
    await expect(loadComicBooks("batch-1")).resolves.toEqual([
      { id: "book-1", title: "第一本", hasSource: true, revision: 2 },
      { id: "book-2", title: "第二本", hasSource: false, revision: 0 },
    ]);
    expect(deps.loadV11).toHaveBeenCalledWith(apiPath.batchFactoryV11Api, "batch-1");
  });

  test("读取配置项时并行拉取作品、自动化预设与画风目录", async () => {
    deps.works.mockResolvedValue([{ id: "batch-1", title: "批次一", books: [{ id: "b" }], bookCount: 1 }]);
    deps.presets.mockResolvedValue([{ id: "preset-1", name: "默认" }]);
    deps.request.mockResolvedValue({ catalog: [{ id: "style-1", name: "国漫", slot: "batch.comic-style" }] });
    await expect(loadComicSetup()).resolves.toEqual({
      batches: [{ id: "batch-1", title: "批次一", bookCount: 1 }],
      presets: [{ id: "preset-1", name: "默认" }],
      styles: [{ id: "style-1", name: "国漫", previewUrl: "" }],
    });
  });

  test("画风缩略图带鉴权读取，图片走 request、失败时静默退回占位", async () => {
    media.load.mockResolvedValueOnce({ path: "/tmp/style.jpg", fromCache: false });
    await expect(resolveComicStyleThumb("/api/presets/comic-style/style-1/preview")).resolves.toBe("/tmp/style.jpg");
    expect(media.load).toHaveBeenCalledWith("/api/presets/comic-style/style-1/preview", "jpg");
    media.load.mockRejectedValueOnce(new Error("500"));
    await expect(resolveComicStyleThumb("/api/presets/comic-style/style-2/preview")).resolves.toBe("");
    await expect(resolveComicStyleThumb("")).resolves.toBe("");
  });

  test("只有文本模型的预设标记为不可用，模型齐且漫画已选的预设才保留", () => {
    const textOnly = comicPresetOptions([{ id: "p1", name: "纯文本", config: { textModelId: "t1" } }])[0];
    expect(textOnly.usable).toBe(false);
    expect(textOnly.reason).toContain("图片模型");
    expect(comicPresetBlockReason({ id: "p1", config: { textModelId: "t1" } })).toContain("图片模型");

    const dual = comicPresetOptions([{
      id: "p2", name: "双模型",
      config: { textModelId: "t1", imageModelId: "i1", aiPromptConfig: { comic: { enabled: true, presetId: "page", assets: { presetId: "assets" } } } },
    }])[0];
    expect(dual).toMatchObject({ usable: true, reason: "", name: "双模型" });
    expect(comicPresetBlockReason({ id: "p2", config: { textModelId: "t1", imageModelId: "i1" } })).toBe("");
  });

  test("一个可用预设都没有时给出配置图片模型的提示，并带上第一条的具体原因", () => {
    const allBlocked = comicPresetOptions([{ id: "p1", config: { textModelId: "t1" } }]);
    expect(comicPresetAvailability(allBlocked)).toBe("当前没有可用于漫画的预设，请先在执行器里配置图片模型：这个预设没有配置图片模型");
    const usable = comicPresetOptions([{
      id: "p2", config: { textModelId: "t1", imageModelId: "i1", aiPromptConfig: { comic: { enabled: true, presetId: "page", assets: { presetId: "assets" } } } },
    }]);
    expect(comicPresetAvailability(usable)).toBe("");
  });

  test("私人定制只拉预设与画风，不读作品列表", async () => {
    deps.presets.mockResolvedValue([{
      id: "p2", name: "双模型",
      config: { textModelId: "t1", imageModelId: "i1", aiPromptConfig: { comic: { enabled: true, presetId: "page", assets: { presetId: "assets" } } } },
    }]);
    deps.request.mockResolvedValue({ catalog: [{ id: "style-1", name: "国漫", slot: "batch.comic-style" }] });
    await expect(loadComicPrivateSetup()).resolves.toEqual({
      presets: [{
        preset: { id: "p2", name: "双模型", config: { textModelId: "t1", imageModelId: "i1", aiPromptConfig: { comic: { enabled: true, presetId: "page", assets: { presetId: "assets" } } } } },
        name: "双模型", usable: true, reason: "",
      }],
      styles: [{ id: "style-1", name: "国漫", previewUrl: "" }],
    });
    expect(deps.works).not.toHaveBeenCalled();
  });

  test("参考图按 base64 上传并校验客户端 3 MB 上限", async () => {
    deps.readFile.mockImplementation(({ success, fail, filePath }: any) => {
      if (filePath === "/tmp/big.png") return success({ data: "A".repeat(4 * 1024 * 1024 + 8) });
      if (filePath === "/bad.png") return fail(new Error("read fail"));
      return success({ data: "QUJD" });
    });
    deps.request.mockResolvedValue({ reference: { assetId: "asset-1", url: "https://cdn/x.png", previewUrl: "https://cdn/p.png" } });
    await expect(uploadComicReference("batch-1", "/tmp/x.jpg")).resolves.toEqual({ assetId: "asset-1", url: "https://cdn/x.png", previewUrl: "https://cdn/p.png" });
    expect(deps.request).toHaveBeenCalledWith(
      "/api/batch-factory/v12/batches/batch-1/comic-reference/upload",
      expect.objectContaining({ method: "POST", data: { dataUrl: "data:image/jpeg;base64,QUJD" } }),
    );
    await expect(uploadComicReference("batch-1", "/tmp/big.png")).rejects.toThrow("参考图超过 3 MB");
    await expect(uploadComicReference("batch-1", "/bad.png")).rejects.toThrow("read fail");
  });

  test("服务端未返回参考图资产地址时报错", async () => {
    deps.readFile.mockImplementation(({ success }: any) => success({ data: "QUJD" }));
    deps.request.mockResolvedValue({ reference: {} });
    await expect(uploadComicReference("batch-1", "/tmp/x.jpg")).rejects.toThrow("未返回资产地址");
  });

  test("提交调用自动化启动接口（不额外造请求层）", async () => {
    deps.start.mockResolvedValue({});
    await expect(submitComicRun(baseSubmission)).resolves.toEqual({ batchId: "batch-1", bookIds: ["book-1"] });
    expect(deps.start).toHaveBeenCalledWith(apiPath.batchFactoryV11Api, "batch-1", buildComicStartPayload(baseSubmission));
  });

  test("轮询立即读取、按 3s/6s/10s 调度且可停止", async () => {
    vi.useFakeTimers();
    const read = vi.fn().mockResolvedValue("a");
    const publish = vi.fn();
    const stop = startComicPolling(read, publish, vi.fn(), { timeoutMs: 60000, now: () => 0 });
    await vi.advanceTimersByTimeAsync(0);
    expect(read).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(3000);
    expect(read).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(6000);
    expect(read).toHaveBeenCalledTimes(3);
    stop();
    await vi.advanceTimersByTimeAsync(30000);
    expect(read).toHaveBeenCalledTimes(3);
    vi.useRealTimers();
  });

  test("超过页面级等待时间后停止并提示", async () => {
    vi.useFakeTimers();
    let clock = 0;
    const read = vi.fn().mockResolvedValue(null);
    const fail = vi.fn();
    startComicPolling(read, vi.fn(), fail, { timeoutMs: 50, now: () => clock });
    await vi.advanceTimersByTimeAsync(0);
    expect(read).toHaveBeenCalledTimes(1);
    clock = 100;
    await vi.advanceTimersByTimeAsync(3000);
    expect(fail).toHaveBeenCalledWith(expect.objectContaining({ message: "已超过等待时间，自动刷新已停止，请稍后重新进入" }));
    expect(read).toHaveBeenCalledTimes(2);
    vi.useRealTimers();
  });
});

describe("从零开始做漫画", () => {
  test("正文为空或过短时在本地拦住，不发请求", () => {
    expect(validateComicSource("")).toBe("先把小说正文粘贴进来，再点下一步");
    expect(validateComicSource(" \n\t ")).toBe("先把小说正文粘贴进来，再点下一步");
    expect(validateComicSource("字".repeat(MIN_COMIC_SOURCE_CHARS - 1))).toBe(`正文只有 ${MIN_COMIC_SOURCE_CHARS - 1} 字，至少 ${MIN_COMIC_SOURCE_CHARS} 字才能做成漫画`);
    expect(validateComicSource("字".repeat(MIN_COMIC_SOURCE_CHARS))).toBe("");
    expect(comicSourceLength("第一 行\n第二行")).toBe(6);
  });

  test("作品名留空时取正文首句，最多 20 字", () => {
    expect(suggestComicTitle("第一章 雨夜。他推门进来。")).toBe("第一章 雨夜");
    expect(suggestComicTitle("\n\n  第一行没有句号\n第二行")).toBe("第一行没有句号");
    expect(suggestComicTitle("字".repeat(30))).toBe(`${"字".repeat(20)}…`);
    expect(suggestComicTitle("   ")).toBe("");
  });

  test("批次名留空时先用第一本书名，再退回日期名", () => {
    expect(defaultComicBatchTitle("第一本书")).toBe("第一本书");
    expect(defaultComicBatchTitle("", new Date(2026, 10, 4))).toBe("11月4日的批次");
    expect(defaultComicBatchTitle("   ", new Date(2026, 10, 4))).toBe("11月4日的批次");
  });

  test("一步建批：只打 private-comic/tasks，体里只有 title 与 sourceText", async () => {
    deps.request.mockReset();
    deps.request.mockResolvedValueOnce({
      batch: { id: "batch-new", title: "第一章 雨夜" },
      book: { id: "book-new", title: "第一章 雨夜", bookId: "private-comic-1" },
    });
    const sourceText = `第一章 雨夜。${"字".repeat(110)}`;
    await expect(createComicBatch({ batchTitle: "", bookTitle: "", sourceText })).resolves.toEqual({
      batchId: "batch-new",
      batchTitle: "第一章 雨夜",
      bookId: "book-new",
      bookTitle: "第一章 雨夜",
    });
    expect(deps.request).toHaveBeenCalledTimes(1);
    expect(deps.request).toHaveBeenCalledWith("/api/batch-factory/v12/private-comic/tasks", expect.objectContaining({
      method: "POST",
      data: { title: "第一章 雨夜", sourceText },
    }));
    // 多传字段只会增加失败面：请求体必须严格只有这两个键。
    expect(Object.keys(deps.request.mock.calls[0][1].data).sort()).toEqual(["sourceText", "title"]);
  });

  test("批次名与书名同时填写时按填写值创建，并兼容顶层 batchId / bookId", async () => {
    deps.request.mockReset();
    deps.request.mockResolvedValueOnce({ batchId: "batch-new", bookId: "book-new" });
    await expect(createComicBatch({ batchTitle: "雨夜合集", bookTitle: "雨夜", sourceText: "字".repeat(120) })).resolves.toEqual({
      batchId: "batch-new",
      batchTitle: "雨夜合集",
      bookId: "book-new",
      bookTitle: "雨夜",
    });
    expect(deps.request).toHaveBeenCalledWith("/api/batch-factory/v12/private-comic/tasks", expect.objectContaining({
      data: { title: "雨夜合集", sourceText: "字".repeat(120) },
    }));
  });

  test("建批带上网页版同款的 initialBatchSettings", async () => {
    deps.request.mockReset();
    deps.request.mockResolvedValueOnce({ batch: { id: "batch-new" }, book: { id: "book-new" } });
    const initialBatchSettings = buildComicInitialBatchSettings(
      { aspectRatio: "9:16", stylePresetId: "style-1", reference: null },
      {
        id: "auto-preset-1",
        config: {
          textModelId: "custom-a-gemini-3-7-flash",
          imageModelId: "custom-image-1",
          imageProvider: "doubao",
          aiPromptConfig: { comic: { style: { presetId: "p-style" }, assets: { presetId: "p-assets" } } },
        },
      },
    );
    await createComicBatch({ batchTitle: "甲", bookTitle: "甲", sourceText: "字".repeat(120), initialBatchSettings });
    expect(deps.request).toHaveBeenCalledWith("/api/batch-factory/v12/private-comic/tasks", expect.objectContaining({
      data: expect.objectContaining({
        initialBatchSettings: expect.objectContaining({
          productionMode: "comic",
          textModelId: "custom-a-gemini-3-7-flash",
          imageModelId: "custom-image-1",
          imageProvider: "doubao",
          aiPromptConfig: {
            comic: expect.objectContaining({
              enabled: true,
              scope: "private_customization",
              style: expect.objectContaining({ presetId: "style-1" }),
              assets: expect.objectContaining({ presetId: "p-assets" }),
            }),
          },
        }),
      }),
    }));
  });

  test("正文过短时建批次会直接抛错，不调用接口", async () => {
    deps.request.mockReset();
    await expect(createComicBatch({ batchTitle: "短篇", bookTitle: "短篇", sourceText: "太短了" })).rejects.toThrow("至少");
    expect(deps.request).not.toHaveBeenCalled();
  });

  test("建批成功后的 bookId 取 book.id，而不是 private-comic 业务书号", async () => {
    deps.request.mockReset();
    deps.request.mockResolvedValueOnce({
      batch: { id: "batch-9" },
      book: { id: "book-9", bookId: "private-comic-1720000000000000000", title: "甲" },
    });
    const created = await createComicBatch({ batchTitle: "甲", bookTitle: "甲", sourceText: "字".repeat(120) });
    expect(created.bookId).toBe("book-9");

    deps.start.mockResolvedValue({});
    await submitComicRun({ ...baseSubmission, batchId: created.batchId, bookIds: [created.bookId] }, null);
    expect(deps.start).toHaveBeenCalledWith(apiPath.batchFactoryV11Api, "batch-9", expect.objectContaining({ bookIds: ["book-9"] }));
  });

  test("建批 401：给中文登录失效提示，不漏 unauthorized 原文", async () => {
    deps.request.mockReset();
    deps.request.mockRejectedValueOnce(Object.assign(new Error("unauthorized"), { statusCode: 401 }));
    const error = await createComicBatch({ batchTitle: "甲", bookTitle: "甲", sourceText: "字".repeat(120) })
      .then(() => null, (cause: Error) => cause);
    expect(error?.message).toBe("登录已失效，请重新登录");
    expect(error?.message).not.toMatch(/unauthorized|401/i);
  });

  test("建批 500：中文兜底且不暴露状态码", async () => {
    deps.request.mockReset();
    deps.request.mockRejectedValueOnce(Object.assign(new Error("请求失败：500 internal error"), { statusCode: 500 }));
    const error = await createComicBatch({ batchTitle: "甲", bookTitle: "甲", sourceText: "字".repeat(120) })
      .then(() => null, (cause: Error) => cause);
    expect(error?.message).toBe("服务端暂时忙不过来，稍后再试一次");
    expect(error?.message).not.toMatch(/500|internal error/i);
    expect(deps.request).toHaveBeenCalledTimes(1);
  });

  test("服务端没返回批次时报错", async () => {
    deps.request.mockReset();
    deps.request.mockResolvedValueOnce({ batch: {} });
    await expect(createComicBatch({ batchTitle: "甲", bookTitle: "甲", sourceText: "字".repeat(120) })).rejects.toThrow("服务端没有返回这个批次");
  });

  test("给已有书补正文走 PUT source，并带修订号做并发保护", async () => {
    deps.request.mockResolvedValue({});
    await fillComicBookSource({ batchId: "batch-1", bookId: "book-1", sourceText: "字".repeat(120), revision: 3 });
    expect(deps.request).toHaveBeenCalledWith(
      "/api/batch-factory/v12/batches/batch-1/books/book-1/source",
      expect.objectContaining({ method: "PUT", data: { sourceText: "字".repeat(120), replaceSource: true, expectedRevision: 3 } }),
    );
  });

  test("只挑出已选但没有正文的书", () => {
    const books = [
      { id: "book-1", title: "甲", hasSource: false, revision: 1 },
      { id: "book-2", title: "乙", hasSource: true, revision: 1 },
    ];
    expect(comicBooksWithoutSource(books, ["book-1", "book-2"]).map((book) => book.title)).toEqual(["甲"]);
    expect(comicBooksWithoutSource(books, ["book-2"])).toEqual([]);
  });

  test("失败文案按状态码给可操作提示", () => {
    expect(comicNewErrorText({ statusCode: 401, message: "unauthorized" })).toBe("登录已失效，请重新登录");
    expect(comicNewErrorText({ statusCode: 503 })).toBe("服务端暂时忙不过来，稍后再试一次");
    expect(comicNewErrorText({ message: "request:fail timeout" })).toBe("网络不太好，稍后再试");
    expect(comicNewErrorText({ statusCode: 400, message: "标题不能为空" })).toBe("标题不能为空");
    expect(comicNewErrorText(null)).toBe("没能保存，请稍后再试一次");
    expect(comicNewErrorText({ statusCode: 400, message: "appCode: E_PRESET" })).toBe("这段正文服务端没有接受，检查一下再试");
  });

  test("加载失败原因不把后端原文漏到界面", () => {
    expect(comicFailureText({ statusCode: 401, message: "unauthorized" })).toBe("登录已失效，请重新登录");
    expect(comicFailureText(new Error("unauthorized"))).toBe("登录已失效，请重新登录");
    expect(comicFailureText({ message: "request:fail timeout" })).toBe("网络不太好，稍后再试");
    expect(comicFailureText({ message: "Not Found" })).toBe("没能加载内容，稍后再试");
    expect(comicFailureText(null)).toBe("没能加载内容，稍后再试");
    ["unauthorized", "401", "appCode"].forEach((raw) => {
      expect(comicFailureText({ message: raw })).not.toContain(raw);
      expect(comicFailureText({ statusCode: 401, message: raw })).not.toContain(raw);
    });
  });

  test("响应体只在漫画链路补齐：有中文原因才附服务端原文，且剪短", () => {
    const detail = comicFailureText({
      statusCode: 400,
      message: "预设缺少漫画提示词（HTTP 400）",
      body: { error: "comic.assets.body missing" },
    });
    expect(detail).toContain("预设缺少漫画提示词（HTTP 400）");
    expect(detail).toContain("服务端原文");
    expect(detail).toContain("comic.assets.body missing");

    // 技术报错（无中文可读原因）不补原文，避免把英文/JSON 露给用户
    expect(comicFailureText({ statusCode: 400, message: "appCode: E_PRESET", body: { error: "boom" } }))
      .toBe("没能加载内容，稍后再试");
    // 登录失效优先，不附原文
    expect(comicFailureText({ statusCode: 401, message: "未登录", body: { error: "boom" } })).toBe("登录已失效，请重新登录");
  });
});

describe("自动化状态容错（实测：服务重启会给出 stopped）", () => {
  test("null 视为未启动，不抛错", () => {
    const progress = comicProgress(null);
    expect(progress.state).toBe("idle");
    expect(progress.finished).toBe(false);
    expect(progress.interrupted).toBe(false);
    expect(progress.books).toEqual([]);
  });

  test("stopped 算结束并标记中断，提示保留服务端原文", () => {
    const progress = comicProgress({ state: "stopped", counts: {}, books: [{ message: "服务重启，自动化已中断，请重新立即执行。" }] });
    expect(progress.finished).toBe(true);
    expect(progress.interrupted).toBe(true);
    expect(progress.notice).toContain("服务重启");
  });

  test("needs_attention 也算结束", () => {
    expect(comicProgress({ state: "needs_attention" }).finished).toBe(true);
  });

  test("空对象不会当成已结束", () => {
    const progress = comicProgress({});
    expect(progress.state).toBe("idle");
    expect(progress.finished).toBe(false);
  });
});
