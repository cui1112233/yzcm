import Taro from "@tarojs/taro";
import { apiRequest } from "../../services/api";
import { loadAuthenticatedMedia } from "../../services/mediaCache";
import {
  BATCH_FACTORY_BASE,
  batchFactoryV11Api,
  listBatchFactoryAutomationPresets,
  loadBatchFactoryAutomation,
  loadBatchFactoryV11,
  loadBatchFactoryWorks,
  startBatchAutomation,
  type BatchFactoryAutomationPreset,
  type BatchFactoryAutomationStatus,
} from "../../services/batchFactoryV11";

export const COMIC_ASPECT_RATIOS = ["9:16", "16:9", "3:4", "4:3", "1:1", "2:3", "3:2"] as const;
export type ComicRunMode = "storyboard_only" | "full_submit" | "video_no_submit";
/**
 * 漫画固定「只出图、不发布」，所以用 video_no_submit：
 * - full_submit 在漫画模式下会被后端强制 autoPublish=true 并走 comic_publish 发布到 N8，新手用不到；
 * - storyboard_only 只做资产 + 分页 + 提示词就停，不出图，用户看不到首图；
 * - video_no_submit 出图并建合集后停在 ready_for_upload。三个取值对漫画都有效，它不代表「只做视频」。
 */
export const DEFAULT_COMIC_RUN_MODE: ComicRunMode = "video_no_submit";
/** 漫画默认竖版。 */
export const DEFAULT_COMIC_ASPECT_RATIO = "9:16";

/** 服务端未在参考图上传接口内显式限制大小，客户端按 3 MB 原图上限约束。 */
export const CLIENT_REFERENCE_LIMIT_BYTES = 3 * 1024 * 1024;
/**
 * 正文最少 100 字：后端分页单页下限 45 字（comicPageTextMin=45），低于 45 连一页都切不出来；
 * 取 100 留出句子边界余量，保证至少能出 1~2 页有内容的画面。
 */
export const MIN_COMIC_SOURCE_CHARS = 100;
/** 后端漫画最多 35 页（ComicMaxPageCount），超过约 3000 字只会用到前面的部分。 */
export const COMIC_SOURCE_TRUNCATE_HINT_CHARS = 3000;
export const COMIC_POLL_BASE_MS = 3000;
export const COMIC_POLL_MAX_MS = 10000;
export const COMIC_POLL_TIMEOUT_MS = 20 * 60 * 1000;

export type ComicBatchOption = { id: string; title: string; bookCount: number };
export type ComicBookOption = { id: string; title: string; hasSource: boolean; revision: number };
export type ComicStylePreset = { id: string; name: string; previewUrl: string };
export type ComicReferenceInfo = { assetId: string; url: string; previewUrl?: string };

export type ComicSetup = {
  batches: ComicBatchOption[];
  presets: BatchFactoryAutomationPreset[];
  styles: ComicStylePreset[];
};

/** 预设能不能跑漫画：后端逐书预检要求文本模型必填，出图还要求图片模型。 */
export type ComicPresetOption = {
  preset: BatchFactoryAutomationPreset;
  name: string;
  usable: boolean;
  reason: string;
};

/** 一个可用预设都没有时的统一文案，避免用户点下去干等。 */
export const COMIC_PRESET_EMPTY_HINT = "当前没有可用于漫画的预设，请先在执行器里配置图片模型";

/**
 * 后端 start 的全局预检只查执行器，真正的拦截在逐书预检：
 * textModelId 必填、comic 且非 storyboard 时 imageModelId 必填，缺一项整本书就是 blocked。
 * 预设只带文本模型时接口仍 200，但零产出，所以提交前必须先把这类预设筛掉。
 */
export function comicPresetBlockReason(preset: BatchFactoryAutomationPreset) {
  const config = object(preset.config);
  if (!text(config.textModelId)) return "这个预设没有配置文本模型";
  if (!text(config.imageModelId)) return "这个预设没有配置图片模型";
  return "";
}

export function comicPresetOptions(presets: BatchFactoryAutomationPreset[]): ComicPresetOption[] {
  return presets.map((preset) => {
    // 模型缺一项整本书就是 blocked；漫画提示词只校验「有没有引用」，不要求 body。
    const reason = comicPresetBlockReason(preset) || comicPresetComicReason(preset);
    return {
      preset,
      name: text(preset.name) || text(preset.id) || "未命名预设",
      usable: !reason,
      reason,
    };
  });
}

/** 没有任何可用预设时返回提示文案（含第一条的具体原因），有则返回空串，页面据此禁用主按钮。 */
export function comicPresetAvailability(presets: ComicPresetOption[]) {
  if (presets.some((option) => option.usable)) return "";
  const firstReason = presets.find((option) => option.reason)?.reason;
  return firstReason ? `${COMIC_PRESET_EMPTY_HINT}：${firstReason}` : COMIC_PRESET_EMPTY_HINT;
}

/**
 * 自动化预设的真实返回结构（对照 GET /api/batch-factory/v12/automation-presets）：
 *   { presets: [{ id, name, version, config }] }
 * config 是整套批量设置补丁，漫画配置在 **config.aiPromptConfig.comic**：
 *   { enabled, body(分页提示词), assets: { presetId, presetName, body(资产提取正文) },
 *     style, constraints, imageDerivation, imagePromptPrefix, imagePromptSuffix, globalReferenceImage }
 * 后端 automation/start 先套用预设，再把 settingsPatch 当 overlay 保存；而
 * ResolveSettings 只对 aiPromptConfig 做**顶层 key 浅合并**（settings.go#L53-L67），
 * 所以 settingsPatch 里只写 comic 的几项会把整个 comic 块替换掉，
 * comic.assets.body / comic.body / constraints 一并丢失，资产阶段随即报
 * 「请先在资产设置中选择漫画资产提示词」（comic_assets_service.go#L35-L38）。
 * 提交前必须把预设的 comic 整块取出来合并。
 */
export function comicPresetComicConfig(preset?: BatchFactoryAutomationPreset | null): Record<string, unknown> {
  const config = object(preset?.config);
  return object(object(config.aiPromptConfig).comic);
}

/**
 * 预设里有没有选漫画提示词。
 * automation-presets 返回的是**引用**（presetId/presetName/presetSlot/presetVersion），
 * 正文由后端在保存设置 / 启动自动化时通过 hydrateBatchFactoryPromptConfig →
 * freezePromptSelection 调 catalog.PublishedBody 冻结进 comic.body / comic.assets.body
 * （batch_factory_prompt_config.go#L32-L122），浏览器侧没有任何接口能取到 body。
 * 所以这里只判断「引用或正文任一存在」，不能再要求 body 非空。
 */
export function comicPresetComicReason(preset?: BatchFactoryAutomationPreset | null) {
  const config = object(preset?.config);
  const comic = object(object(config.aiPromptConfig).comic);
  if (!Object.keys(comic).length || comic.enabled !== true) return "这个预设没有配置漫画";
  const assets = object(comic.assets);
  if (!text(assets.body) && !text(assets.presetId)) return "这个预设没有选择漫画资产提示词";
  if (!text(comic.body) && !text(comic.presetId)) return "这个预设没有选择漫画分页提示词";
  return "";
}

export type ComicSubmission = {
  batchId: string;
  bookIds: string[];
  presetId: string;
  runMode: ComicRunMode;
  aspectRatio: string;
  stylePresetId: string;
  reference?: ComicReferenceInfo | null;
};

export type ComicProgress = {
  state: string;
  total: number;
  running: number;
  succeeded: number;
  failed: number;
  books: BatchFactoryAutomationStatus["books"];
  finished: boolean;
  /** 服务重启导致的中断：状态是 stopped，不是正常失败，要提示「重新执行」。 */
  interrupted: boolean;
  stage: string;
  notice: string;
};

/**
 * 终态集合。实测补充：服务端重启后 automation.state 会变成 stopped
 * （本书 message =「服务重启，自动化已中断，请重新立即执行。」），
 * 不在集合里页面就会一直转圈，所以 stopped / needs_attention 必须算结束。
 */
const FINISHED_STATES = new Set(["completed", "failed", "cancelled", "canceled", "partial", "stopped", "needs_attention"]);

function text(value: unknown) {
  return typeof value === "string" && value.trim() ? value.trim() : "";
}

function object(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

export function comicPollDelay(attempt: number) {
  const step = Math.max(0, Math.floor(attempt));
  return Math.min(COMIC_POLL_MAX_MS, COMIC_POLL_BASE_MS * (2 ** Math.min(step, 4)));
}

export function comicProgress(status: BatchFactoryAutomationStatus | null): ComicProgress {
  const counts = object(status?.counts);
  const pick = (key: string) => {
    const value = counts[key];
    return typeof value === "number" && Number.isFinite(value) ? value : 0;
  };
  // 实测出现过 GET /automation 返回空对象的一帧：这里一律按「未启动」兜底，不抛错。
  const state = text(status?.state) || "idle";
  const key = state.toLowerCase();
  const books = Array.isArray(status?.books) ? status.books : [];
  const book = object(books[0]);
  const finished = FINISHED_STATES.has(key);
  const interrupted = key === "stopped";
  return {
    state,
    total: pick("total"),
    running: pick("running") + pick("queued"),
    succeeded: pick("succeeded") + pick("completed"),
    failed: pick("failed"),
    books,
    finished,
    interrupted,
    stage: text(book.stage),
    // 服务端下发的 message 本身就是可操作文案（服务重启中断 / 失败原因），原样透出。
    notice: interrupted || finished ? text(book.message) || text(book.error) : "",
  };
}

export function normalizeStylePresets(payload: unknown): ComicStylePreset[] {
  const catalog = Array.isArray(object(payload).catalog) ? object(payload).catalog as unknown[] : [];
  return catalog
    .map((item) => object(item))
    .filter((item) => text(item.id) && text(item.slot) === "batch.comic-style")
    .map((item) => ({
      id: text(item.id),
      name: text(item.name) || text(item.id),
      previewUrl: text(item.previewUrl) || text(item.referenceUrl),
    }));
}

/**
 * 画风缩略图：previewUrl 是后端下发的相对地址，且需要登录鉴权，直接塞给 <Image> 会
 * 走渲染层网络请求失败（开发者工具会刷红色渲染层错误）。改为带鉴权下载到本地再显示；
 * 图片已在 mediaCache 里改成走 request，不再先试探 downloadFile。
 * 任一步失败都静默返回空字符串，页面继续用色块 + 首字占位。
 */
export async function resolveComicStyleThumb(previewUrl: string) {
  const url = text(previewUrl);
  if (!url) return "";
  try {
    const cached = await loadAuthenticatedMedia(url, "jpg");
    return cached.path || "";
  } catch {
    return "";
  }
}

/**
 * 以预设的 comic 块为底，只覆盖用户真正改动的那几项，整块发出去。
 * 这样 assets.body、assets 的模型配置、body(分页提示词)、imageDerivation、
 * constraints、imagePromptPrefix/Suffix 都不会被后端的一层合并抹掉。
 * 未传预设时保留旧的最小结构（其它调用方/测试不依赖预设）。
 */
export function buildComicSettingsPatch(
  input: Pick<ComicSubmission, "aspectRatio" | "stylePresetId" | "reference">,
  preset?: BatchFactoryAutomationPreset | null,
) {
  const presetConfig = object(preset?.config);
  const textModelId = text(presetConfig.textModelId);
  const imageModelId = text(presetConfig.imageModelId);
  return {
    productionMode: "comic",
    // 画幅是顶层设置项（AIReasoning config 里没有 comic.imageAspectRatio 字段）。
    imageAspectRatio: input.aspectRatio,
    // 模型 id 取自预设 config（automation-presets 的实测字段路径），与网页版 settingsPatch 对齐；
    // 预设没给就不写空串，交给服务端已保存的值。
    ...(textModelId ? { textModelId } : {}),
    ...(imageModelId ? { imageModelId } : {}),
    aiPromptConfig: { comic: buildComicBlock(input, preset) },
  };
}

/**
 * 建批时随请求带过去的初始设置，形状照抄网页版
 * frontend/src/user/pages/privateCustomizationComic.js 的 buildPrivateComicSettings：
 * { productionMode, textModelId, imageModelId, imageProvider, aiPromptConfig:{ comic:{...comic, enabled, scope} } }。
 * 服务端在建批事务里据此 hydrate style.body，消掉「建完批再出图 409」的窗口。
 */
export function buildComicInitialBatchSettings(
  input: Pick<ComicSubmission, "aspectRatio" | "stylePresetId" | "reference">,
  preset?: BatchFactoryAutomationPreset | null,
) {
  const presetConfig = object(preset?.config);
  return {
    productionMode: "comic",
    textModelId: text(presetConfig.textModelId),
    imageModelId: text(presetConfig.imageModelId),
    imageProvider: text(presetConfig.imageProvider),
    aiPromptConfig: {
      comic: {
        ...buildComicBlock(input, preset),
        enabled: true,
        scope: "private_customization",
      },
    },
  };
}

/** 预设 comic 块 + 用户改动的画风/参考图；两处 payload 共用，避免形状分叉。 */
function buildComicBlock(
  input: Pick<ComicSubmission, "aspectRatio" | "stylePresetId" | "reference">,
  preset?: BatchFactoryAutomationPreset | null,
) {
  const base = comicPresetComicConfig(preset);
  const baseStyle = object(base.style);
  const baseReference = object(base.globalReferenceImage);
  const comic: Record<string, unknown> = {
    ...base,
    enabled: true,
    // 契约确认字段：服务端按 presetId 冻结发布预设并按 batch.comic-style 槽位校验；
    // 保留预设 style 的其余字段（presetName/body 等），只替换用户选中的 presetId。
    style: { ...baseStyle, presetId: input.stylePresetId },
  };
  if (input.reference?.assetId && input.reference.url) {
    comic.globalReferenceImage = {
      ...baseReference,
      assetId: input.reference.assetId,
      url: input.reference.url,
      ...(input.reference.previewUrl ? { previewUrl: input.reference.previewUrl } : {}),
      enabled: true,
      applyToAssets: true,
      applyToPages: true,
      source: "manual-upload",
    };
  }
  return comic;
}

export function validateComicSubmission(input: ComicSubmission) {
  if (!input.batchId) return "请选择作品批次";
  if (!input.bookIds.length) return "请至少选择一本书";
  if (!input.presetId) return "请选择自动化预设";
  if (!input.stylePresetId) return "请选择漫画画风";
  if (!COMIC_ASPECT_RATIOS.includes(input.aspectRatio as typeof COMIC_ASPECT_RATIOS[number])) return "画幅比例无效";
  if (!["storyboard_only", "full_submit", "video_no_submit"].includes(input.runMode)) return "生成模式无效";
  return "";
}

/** 去掉空白后的字数，和后端 comicSourceCharacterCount 的口径一致。 */
export function comicSourceLength(source: string) {
  return Array.from(String(source || "").replace(/\s+/g, "")).length;
}

/** 作品名留空时，取正文首个非空行里的第一句话，最多 20 字。 */
export function suggestComicTitle(source: string) {
  const line = String(source || "").split(/\r?\n/).map((item) => item.trim()).find(Boolean) || "";
  const end = line.search(/[。！？!?]/);
  const head = (end > 0 ? line.slice(0, end) : line).trim();
  const characters = Array.from(head || line);
  if (!characters.length) return "";
  return characters.length > 20 ? `${characters.slice(0, 20).join("")}…` : characters.join("");
}

/** 提交前拦住「没有正文 / 正文过短」，避免进入异步阶段后被静默跳过。 */
export function validateComicSource(source: string) {
  const length = comicSourceLength(source);
  if (!length) return "先把小说正文粘贴进来，再点下一步";
  if (length < MIN_COMIC_SOURCE_CHARS) return `正文只有 ${length} 字，至少 ${MIN_COMIC_SOURCE_CHARS} 字才能做成漫画`;
  return "";
}

/** 已选书目里没有正文的那些书。 */
export function comicBooksWithoutSource(books: ComicBookOption[], bookIds: string[]) {
  return books.filter((book) => bookIds.includes(book.id) && !book.hasSource);
}

export type ComicNewBatchResult = { batchId: string; batchTitle: string; bookId: string; bookTitle: string };

/** 批次名留空时：先用第一本书名，没有就用「11月4日的批次」这种日期名。 */
export function defaultComicBatchTitle(bookTitle: string, now: Date = new Date()) {
  const book = text(bookTitle);
  if (book) return book;
  return `${now.getMonth() + 1}月${now.getDate()}日的批次`;
}

/** 粘贴正文直接建一个批次（批次里先放第一本书），成功后可以继续加书或直接去出漫画。 */
export async function createComicBatch(input: {
  batchTitle: string;
  bookTitle: string;
  sourceText: string;
  /** 网页版同款：建批时就带完整漫画设置，服务端在事务里 hydrate。 */
  initialBatchSettings?: Record<string, unknown>;
}): Promise<ComicNewBatchResult> {
  const reason = validateComicSource(input.sourceText);
  if (reason) throw new Error(reason);
  const sourceText = input.sourceText.trim();
  const bookTitle = text(input.bookTitle) || suggestComicTitle(sourceText) || "第一本书";
  const batchTitle = text(input.batchTitle) || defaultComicBatchTitle(bookTitle);
  // 与网页版同源的一步建批：服务端一次性建好 batch + book，并自己补齐
  // bookId / platform / sourceMetadata。不要再走 intakes/novel-fetch 两步：
  // 第二步在线上稳定 500，正是因为它没有给书生成 bookId 与 sourceMetadata。
  let payload: Record<string, unknown>;
  try {
    payload = object(await apiRequest(`${BATCH_FACTORY_BASE}/private-comic/tasks`, {
      method: "POST",
      data: {
        title: batchTitle,
        sourceText,
        ...(input.initialBatchSettings ? { initialBatchSettings: input.initialBatchSettings } : {}),
      },
    }));
  } catch (cause) {
    throw new Error(comicNewErrorText(cause));
  }
  const batch = object(payload.batch);
  const book = object(payload.book);
  const batchId = text(batch.id) || text(payload.batchId);
  if (!batchId) throw new Error("服务端没有返回这个批次，请稍后再试一次");
  return {
    batchId,
    batchTitle: text(batch.title) || batchTitle,
    // book.id 才是内部分页 / 自动化要用的 id；book.bookId 是 private-comic-* 业务书号。
    bookId: text(book.id) || text(payload.bookId),
    bookTitle: text(book.title) || bookTitle,
  };
}

/** 给已有的书补正文：只用于没有正文的书，需带当前修订号做乐观并发。 */
export async function fillComicBookSource(input: { batchId: string; bookId: string; sourceText: string; revision: number }) {
  const reason = validateComicSource(input.sourceText);
  if (reason) throw new Error(reason);
  await apiRequest(
    `${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(input.batchId)}/books/${encodeURIComponent(input.bookId)}/source`,
    { method: "PUT", data: { sourceText: input.sourceText.trim(), replaceSource: true, expectedRevision: input.revision } },
  );
}

/**
 * 是否把后端 4xx/5xx 原文透出到界面。发布前改成 false：
 * false 时退回「中文兜底文案」，用户不会再看到英文报错与状态码。
 */
export const SURFACE_BACKEND_ERRORS = true;

/** 后端原文只要带这些技术词，就不能直接展示给用户。 */
const COMIC_TECHNICAL_MESSAGE = /unauthorized|forbidden|401|403|appcode|app_code|statuscode|exception|not found|failed/i;

/** 后端消息只有「是中文、且不含技术词」时才允许原样展示。 */
function comicUserFacingMessage(message: string) {
  const value = message.trim();
  if (!value) return "";
  // 带 HTTP 状态码的失败原文是排障依据，截断后透出，不再被技术词规则吞掉。
  if (SURFACE_BACKEND_ERRORS && /（HTTP \d{3}）/.test(value)) {
    return value.length > 160 ? `${value.slice(0, 160)}…` : value;
  }
  if (COMIC_TECHNICAL_MESSAGE.test(value) || !/[\u4e00-\u9fa5]/.test(value)) return "";
  return value;
}

/**
 * 后端响应体只在排障开关打开时展示，并剪到 160 字：
 * api.ts 的 message 里不再拼 body，是否透出由漫画链路在这里决定。
 */
function comicErrorDetail(cause: unknown) {
  if (!SURFACE_BACKEND_ERRORS) return "";
  const raw = (cause as { body?: unknown } | null)?.body;
  if (raw == null) return "";
  let value = "";
  try {
    value = typeof raw === "string" ? raw : JSON.stringify(raw);
  } catch {
    return "";
  }
  const trimmed = String(value || "").trim();
  if (!trimmed) return "";
  return trimmed.length > 160 ? `${trimmed.slice(0, 160)}…` : trimmed;
}

/**
 * 接口失败原因 → 用户可读的中文兜底。
 * 登录失效 / 网络失败 / 其他，三条路径统一，绝不让 unauthorized、401、appCode 这类原文漏到界面上。
 */
export function comicFailureText(cause: unknown, fallback = "没能加载内容，稍后再试") {
  const status = Number((cause as { statusCode?: number } | null)?.statusCode || 0);
  const message = text((cause as { message?: string } | null)?.message);
  if (status === 401 || /unauthorized|401/i.test(message) || message.includes("登录")) return "登录已失效，请重新登录";
  if (message.includes("request:fail") || message.includes("网络") || message.includes("超时") || /timeout|network/i.test(message)) return "网络不太好，稍后再试";
  const readable = comicUserFacingMessage(message);
  const base = readable || fallback;
  // 只有已经给出中文可读原因时才补服务端原文；纯技术报错仍然只显示兜底文案。
  const detail = readable ? comicErrorDetail(cause) : "";
  return detail ? `${base}（服务端原文：${detail}）` : base;
}

/** 新建 / 补正文失败时给用户看的可操作文案，不暴露状态码之外的技术细节。 */
export function comicNewErrorText(cause: unknown) {
  const status = Number((cause as { statusCode?: number } | null)?.statusCode || 0);
  if (status >= 500) return "服务端暂时忙不过来，稍后再试一次";
  if (status >= 400) return comicFailureText(cause, "这段正文服务端没有接受，检查一下再试");
  return comicFailureText(cause, "没能保存，请稍后再试一次");
}

export function buildComicStartPayload(
  input: ComicSubmission,
  preset?: BatchFactoryAutomationPreset | null,
) {
  return {
    presetId: input.presetId,
    productionMode: "comic",
    runMode: input.runMode,
    bookIds: input.bookIds,
    // 漫画只出图不发布，显式关掉，避免被后端按 runMode 反推成 autoPublish=true。
    autoPublish: false,
    settingsPatch: buildComicSettingsPatch(input, preset),
  };
}

export async function loadComicSetup(): Promise<ComicSetup> {
  const [works, presets, catalog] = await Promise.all([
    loadBatchFactoryWorks(batchFactoryV11Api),
    listBatchFactoryAutomationPresets(batchFactoryV11Api),
    apiRequest<unknown>("/api/presets?module=batch-factory", { timeout: 20000 }),
  ]);
  return {
    batches: works.map((batch) => ({
      id: batch.id,
      title: text(batch.title) || text(batch.name) || batch.id,
      bookCount: batch.books.length || Number(batch.bookCount) || 0,
    })),
    presets,
    styles: normalizeStylePresets(catalog),
  };
}

/** 私人定制漫画只需要画风和可用预设，不读批次、不读书目。 */
export async function loadComicPrivateSetup(): Promise<{ presets: ComicPresetOption[]; styles: ComicStylePreset[] }> {
  const [presets, catalog] = await Promise.all([
    listBatchFactoryAutomationPresets(batchFactoryV11Api),
    apiRequest<unknown>("/api/presets?module=batch-factory", { timeout: 20000 }),
  ]);
  return { presets: comicPresetOptions(presets), styles: normalizeStylePresets(catalog) };
}

export async function loadComicBooks(batchId: string): Promise<ComicBookOption[]> {
  const state = await loadBatchFactoryV11(batchFactoryV11Api, batchId);
  const batch = state.batch;
  if (!batch) throw new Error("未找到该作品批次");
  return batch.books.map((book) => ({
    id: book.id,
    title: text(book.title) || text(book.name) || book.id,
    // 后端分页只读 sourceText，缺正文的书会在异步阶段被跳过，这里提前暴露给页面拦截。
    hasSource: text(book.sourceText) !== "",
    revision: typeof book.revision === "number" ? book.revision : 0,
  }));
}

function base64Bytes(value: string) {
  const padding = value.endsWith("==") ? 2 : value.endsWith("=") ? 1 : 0;
  return Math.max(0, Math.floor((value.length - padding) * 3 / 4));
}

function mimeFromPath(filePath: string) {
  const extension = filePath.split(".").pop()?.toLowerCase() || "";
  if (extension === "png") return "image/png";
  if (extension === "webp") return "image/webp";
  if (extension === "gif") return "image/gif";
  return "image/jpeg";
}

export async function uploadComicReference(batchId: string, filePath: string): Promise<ComicReferenceInfo> {
  const base64 = await new Promise<string>((resolve, reject) => {
    Taro.getFileSystemManager().readFile({
      filePath,
      encoding: "base64",
      success: (result) => resolve(String(result.data || "")),
      fail: reject,
    });
  });
  if (!base64) throw new Error("参考图读取失败");
  if (base64Bytes(base64) > CLIENT_REFERENCE_LIMIT_BYTES) throw new Error("参考图超过 3 MB，请压缩后再上传");
  const payload = await apiRequest<unknown>(
    `${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/comic-reference/upload`,
    { method: "POST", timeout: 60000, data: { dataUrl: `data:${mimeFromPath(filePath)};base64,${base64}` } },
  );
  const reference = object(object(payload).reference);
  const assetId = text(reference.assetId) || text(reference.assetID);
  const url = text(reference.url);
  if (!assetId || !url) throw new Error("参考图上传成功但服务端未返回资产地址");
  return { assetId, url, previewUrl: text(reference.previewUrl) || text(reference.previewURL) || undefined };
}

export async function submitComicRun(
  input: ComicSubmission,
  preset?: BatchFactoryAutomationPreset | null,
) {
  const error = validateComicSubmission(input);
  if (error) throw new Error(error);
  // 预设缺 comic.assets.body / comic.body 时提前拦下，返回具体中文原因，
  // 不让请求落到后端再报「请先在资产设置中选择漫画资产提示词」。
  if (preset) {
    const presetError = comicPresetComicReason(preset);
    if (presetError) throw new Error(presetError);
  }
  await startBatchAutomation(batchFactoryV11Api, input.batchId, buildComicStartPayload(input, preset));
  return { batchId: input.batchId, bookIds: input.bookIds };
}

/**
 * 读一次自动化状态。服务端偶发返回空对象（实测有一帧没有 state），
 * 直接返回 null 让页面跳过这一帧，绝不当成「已结束」清掉进度。
 */
export async function readComicAutomation(batchId: string) {
  const status = await loadBatchFactoryAutomation(batchFactoryV11Api, batchId);
  if (!status || !text(status.state)) return null;
  return status;
}

/**
 * 立即读一次，随后按 3s→6s→10s 退避；页面侧超时后停止并回报。
 * 返回停止函数，供 onHide/onUnload 调用。
 */
export function startComicPolling<T>(
  read: () => Promise<T>,
  publish: (value: T) => void,
  fail: (cause: unknown) => void,
  options: { timeoutMs?: number; now?: () => number } = {},
) {
  let active = true;
  let attempt = 0;
  let timer: ReturnType<typeof setTimeout>;
  const timeoutMs = options.timeoutMs ?? COMIC_POLL_TIMEOUT_MS;
  const now = options.now ?? Date.now;
  const startedAt = now();
  const schedule = () => { timer = setTimeout(poll, comicPollDelay(attempt)); attempt += 1; };
  const poll = async () => {
    if (!active) return;
    try {
      const value = await read();
      if (active) publish(value);
    } catch (cause) {
      if (active) fail(cause);
    }
    if (!active) return;
    if (now() - startedAt >= timeoutMs) {
      active = false;
      fail(new Error("已超过等待时间，自动刷新已停止，请稍后重新进入"));
      return;
    }
    schedule();
  };
  void poll();
  return () => { active = false; clearTimeout(timer); };
}

