import { batchLocalRequest } from "./batchLocalAccess";
import { deriveBookStageSummary, type BatchFactoryProjectView, type BatchFactoryReadState } from "./batchFactoryModels";

// Compatibility filename; the web client and mini-program share V12 routes.
export const BATCH_FACTORY_BASE = "/api/batch-factory/v12";

export type BatchFactoryV11Api = {
  request: <T = unknown>(path: string, options?: Record<string, unknown>) => Promise<T>;
};

export type BatchFactoryCapability = {
  available?: boolean;
  reason?: string;
};

export type BatchFactoryBook = {
  id: string;
  bookId?: string;
  title?: string;
  name?: string;
  sourceText?: string;
  contentPreview?: string;
  status?: string;
  mode?: string;
  revision?: number;
  sourceMetadata?: Record<string, unknown>;
  videos?: Array<{ id?: string; status?: string; videoPrompt?: string; visualPrompt?: string; revision?: number }>;
};

export type BatchFactoryAsset = {
  id: string;
  bookId?: string;
  kind?: string;
  type?: string;
  name?: string;
  prompt?: string;
  visualPrompt?: string;
  description?: string;
  revision?: number;
};

export type BatchFactoryAssetImage = {
  id: string;
  url?: string;
  prompt?: string;
  status?: string;
};

export type BatchFactoryProductionTask = {
  id?: string;
  bookId?: string;
  videoId?: string;
  status?: string;
  mediaUrl?: string;
  error?: string;
  prompt?: string;
  revision?: number;
};

export type BatchFactoryBookProduction = {
  status?: string;
  tasks: BatchFactoryProductionTask[];
};

export type BatchFactoryBatch = {
  id: string;
  title?: string;
  name?: string;
  status?: string;
  revision?: number;
  books: BatchFactoryBook[];
  bookCount?: number;
  coverUrl?: string;
};

export type BatchFactoryConfigVersion = {
  id: string;
  name?: string;
  config?: Record<string, unknown>;
  createdAt?: string;
};

export type BatchFactoryStatus = {
  batchId?: string;
  jobs?: Array<{
    bookId?: string;
    status?: string;
    outputUrl?: string;
    mediaUrl?: string;
    tasks?: BatchFactoryProductionTask[];
  }>;
};

export type BatchFactoryBookWorkspace = {
  readState: "loading" | "ready" | "empty" | "unavailable" | "error";
  batchId: string;
  bookId: string;
  book: BatchFactoryBook | null;
  stages: unknown;
  sourceText?: string;
  assets: BatchFactoryAsset[];
  assetImages: Record<string, BatchFactoryAssetImage[]>;
  assetImagesLoading?: boolean;
  production: BatchFactoryBookProduction | null;
  mergeStatus: BatchFactoryStatus | null;
  error?: string;
  sectionErrors?: Record<string, string>;
};

export type BatchFactoryAutomationBook = {
  bookId?: string;
  status?: string;
  stage?: string;
  message?: string;
  error?: string;
  retryAt?: string;
  /** 服务端会带阶段日志；用来区分「跳过」和「失败」，类型里保留。 */
  logs?: { stage?: string; message?: string; at?: string }[];
};

export type BatchFactoryAutomationStatus = {
  batchId?: string;
  state?: string;
  runMode?: string;
  scheduledAt?: string;
  startedAt?: string;
  updatedAt?: string;
  counts?: Record<string, number>;
  books?: BatchFactoryAutomationBook[];
};

export type BatchFactoryAutomationPreset = {
  id: string;
  name?: string;
  version?: number;
  config?: Record<string, unknown>;
};

export type BatchFactoryV11State = {
  capabilities: Record<string, BatchFactoryCapability>;
  batches: BatchFactoryBatch[];
  batch: BatchFactoryBatch | null;
  productionStatus: BatchFactoryStatus | null;
  mergeStatus: BatchFactoryStatus | null;
  readState?: BatchFactoryReadState;
  readError?: string;
};

const STATUS_LABELS: Record<string, string> = {
  draft: "待开始",
  pending: "待开始",
  waiting_review: "待审核",
  review: "待审核",
  ai_processing: "AI处理中",
  processing: "AI处理中",
  queued: "排队中",
  running: "生成中",
  failed: "异常",
  error: "异常",
  waiting_merge: "待合并",
  merged: "已合并",
  waiting_upload: "待上传",
  uploaded: "已发布",
  published: "已发布",
  succeeded: "已完成",
  completed: "已完成",
};

function object(value: unknown): Record<string, any> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, any> : {};
}

function arrayFrom(value: unknown, key: string) {
  if (Array.isArray(value)) return value;
  const candidate = object(value)[key];
  return Array.isArray(candidate) ? candidate : [];
}

function statusKey(value: unknown) {
  return String(value || "").trim().toLowerCase().replace(/[\s-]+/g, "_");
}

export function batchStatusLabel(status: unknown) {
  const value = String(status || "").trim();
  if (["待开始", "待审核", "AI处理中", "待生成", "排队中", "生成中", "异常", "待合并", "已合并", "待上传", "已发布", "已完成"].includes(value)) return value;
  return STATUS_LABELS[statusKey(value)] || "状态未返回";
}

const AUTOMATION_STATUS_LABELS: Record<string, string> = {
  idle: "未启动",
  scheduled: "已定时",
  running: "生产中",
  paused: "已暂停",
  completed: "已完成",
  needs_attention: "需处理",
  failed: "执行失败",
  cancelled: "已取消",
};

export function automationStatusLabel(status: unknown) {
  const value = statusKey(status);
  return AUTOMATION_STATUS_LABELS[value] || (String(status || "").trim() || "状态未返回");
}

export function manualBookIdsFromInput(inputText = "") {
  const seen = new Set<string>();
  const ids: string[] = [];
  for (const line of String(inputText).split(/\r?\n/)) {
    for (const part of line.split(/[,，]/)) {
      const match = part.trim().match(/^(\d{6,25})(?=\s|[^\d]|$)/);
      const id = match?.[1] || "";
      if (id && !seen.has(id)) {
        seen.add(id);
        ids.push(id);
      }
    }
  }
  return ids;
}

export function bookStatusLabel(book: BatchFactoryBook, productionStatus?: BatchFactoryStatus | null, mergeStatus?: BatchFactoryStatus | null) {
  const explicit = String(book.status || "").trim();
  if (explicit) return batchStatusLabel(explicit);
  const tasks = (productionStatus?.jobs || [])
    .filter((job) => job?.bookId === book.id || (job?.tasks || []).some((task) => task?.bookId === book.id))
    .flatMap((job) => job.tasks || []);
  const statuses = new Set(tasks.map((task) => statusKey(task.status)).filter(Boolean));
  if (statuses.has("failed") || statuses.has("error")) return "异常";
  if (statuses.has("running")) return "生成中";
  if (statuses.has("queued")) return "排队中";
  if (statuses.size && [...statuses].every((status) => ["succeeded", "completed"].includes(status))) {
    const merged = (mergeStatus?.jobs || []).some((job) => job?.bookId === book.id && ["succeeded", "completed"].includes(statusKey(job.status)));
    return merged ? "已合并" : "待合并";
  }
  if (book.mode === "viral") return "待审核";
  return "待开始";
}

function normalizeBatch(value: unknown): BatchFactoryBatch {
  const source = object(value);
  const books = arrayFrom(source.books ?? source.items, "books").map((book) => {
    const item = object(book);
    return {
      ...item,
      id: String(item.id ?? item.bookId ?? item.book_id ?? ""),
      bookId: String(item.bookId ?? item.id ?? item.book_id ?? ""),
      title: item.title ?? item.name ?? item.bookName,
      books: undefined,
    } as BatchFactoryBook;
  }).filter((book) => book.id);
  return {
    ...source,
    id: String(source.id ?? source.batchId ?? ""),
    title: source.title ?? source.name,
    coverUrl: String(source.coverUrl || object(source.coverMedia).url || object(source.cover).url || "").trim(),
    books,
  };
}

function unwrapBatchPayload(value: unknown) {
  const source = object(value);
  return object(source.batch ?? object(source.data).batch ?? source.data ?? value);
}

function normalizeCapabilities(value: unknown) {
  const source = object(value);
  const capabilities = object(source.capabilities);
  return Object.keys(capabilities).length ? capabilities as Record<string, BatchFactoryCapability> : source as Record<string, BatchFactoryCapability>;
}

export async function loadBatchFactoryV11(api: BatchFactoryV11Api, batchId = ""): Promise<BatchFactoryV11State> {
  // Project navigation already has an ID; do not download every other batch.
  const listPayload = batchId ? {batches:[]} : await api.request(`${BATCH_FACTORY_BASE}/batches`);
  const batches = arrayFrom(listPayload, "batches").map(normalizeBatch).filter((batch) => batch.id);
  const selectedId = batchId || batches[0]?.id || "";
  if (!selectedId) {
    return { capabilities: {}, batches, batch: null, productionStatus: null, mergeStatus: null, readState: batches.length ? "ready" : "empty" };
  }
  const [batchPayload, productionStatus, mergeStatus] = await Promise.all([
    api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(selectedId)}`),
    api.request<BatchFactoryStatus>(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(selectedId)}/status`).catch(() => null),
    api.request<BatchFactoryStatus>(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(selectedId)}/merge-status`).catch(() => null),
  ]);
  const selected = normalizeBatch(unwrapBatchPayload(batchPayload));
  selected.books = selected.books.map((book) => ({ ...book, status: bookStatusLabel(book, productionStatus, mergeStatus) }));
  return { capabilities: {}, batches:batchId?[selected]:batches, batch: selected, productionStatus, mergeStatus };
}

export async function loadBatchFactoryProject(api: BatchFactoryV11Api, batchId = ""): Promise<BatchFactoryProjectView> {
  const loading: BatchFactoryProjectView = {
    readState: "loading", batches: [], batch: null, books: [], capabilities: {}, productionStatus: null, mergeStatus: null, automation: null,
  };
  try {
    const listPayload = await api.request(`${BATCH_FACTORY_BASE}/batches`);
    const batches = arrayFrom(listPayload, "batches").map(normalizeBatch).filter((batch) => batch.id);
    const selectedId = batchId || batches[0]?.id || "";
    if (!selectedId) return { ...loading, batches, readState: batches.length ? "ready" : "empty" };
    const [batchPayload, productionStatus, mergeStatus, capabilities, automation] = await Promise.all([
      api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(selectedId)}`),
      api.request<BatchFactoryStatus>(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(selectedId)}/status`).catch(() => null),
      api.request<BatchFactoryStatus>(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(selectedId)}/merge-status`).catch(() => null),
      loadBatchFactoryCapabilities(api).catch(() => ({})),
      loadBatchFactoryAutomation(api, selectedId).catch(() => null),
    ]);
    const batch = normalizeBatch(unwrapBatchPayload(batchPayload));
    return {
      readState: "ready", batches, batch,
      books: batch.books.map((book) => deriveBookStageSummary(book, productionStatus, mergeStatus)),
      capabilities, productionStatus, mergeStatus, automation,
    };
  } catch (cause) {
    return { ...loading, readState: "error", readError: cause instanceof Error ? cause.message : "批量工厂读取失败" };
  }
}

export async function loadBatchFactoryCapabilities(api: BatchFactoryV11Api) {
  const payload = await api.request(`${BATCH_FACTORY_BASE}/capabilities`);
  return normalizeCapabilities(payload);
}

export const batchFactoryV11Api: BatchFactoryV11Api = { request: batchLocalRequest };

export async function loadBatchFactoryWorks(api: BatchFactoryV11Api): Promise<BatchFactoryBatch[]> {
  const payload = await api.request(`${BATCH_FACTORY_BASE}/batches/summary`);
  const summaries = object(payload).batches;
  if (!Array.isArray(summaries)) throw new Error("服务端返回的作品摘要格式无效，请重新读取。");
  return summaries.map(normalizeBatch).filter((batch) => batch.id);
}

export function batchBookCount(batch: BatchFactoryBatch): number {
  if (batch.books.length) return batch.books.length;
  const count = Number(batch.bookCount);
  return Number.isSafeInteger(count) && count >= 0 ? count : 0;
}

export async function listBatchFactoryConfigVersions(api: BatchFactoryV11Api): Promise<BatchFactoryConfigVersion[]> {
  const payload = await api.request(`${BATCH_FACTORY_BASE}/config-versions`);
  const versions = object(payload).configVersions;
  if (!Array.isArray(versions) || versions.some((item) => typeof item?.id !== "string" || !item.id.trim())) {
    throw new Error("服务端返回的配置版本格式无效，请重新读取。");
  }
  return versions;
}

export async function loadBatchFactoryBookWorkspace(api: BatchFactoryV11Api, batchId: string, bookId: string, fallbackBook?: BatchFactoryBook, onReady?: (workspace:BatchFactoryBookWorkspace)=>void): Promise<BatchFactoryBookWorkspace> {
  const sectionErrors: Record<string, string> = {};
  const readSection = <T,>(key: string, promise: Promise<T>) => promise.catch((cause) => {
    sectionErrors[key] = cause instanceof Error ? cause.message : "读取失败";
    return null;
  });
  const base: Omit<BatchFactoryBookWorkspace, "readState"> = { batchId, bookId, book: null, stages: null, sourceText: "", assets: [], assetImages: {}, production: null, mergeStatus: null };
  try {
    const [batchPayloadResponse, stagesPayload, assetsPayload, productionPayload, mergePayload] = await Promise.all([
      api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}`),
      readSection("stages", api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/books/${encodeURIComponent(bookId)}/stages`)),
      readSection("assets", api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/books/${encodeURIComponent(bookId)}/assets`)),
      readSection("production", api.request<BatchFactoryStatus>(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/status`)),
      readSection("merge", api.request<BatchFactoryStatus>(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/merge-status`)),
    ]);
    const batch = normalizeBatch(unwrapBatchPayload(batchPayloadResponse));
    const normalizedBookId = String(bookId).trim();
    const book = batch.books.find((item) => String(item.id).trim() === normalizedBookId || String(item.bookId || "").trim() === normalizedBookId) || fallbackBook || null;
    if (!book) return { ...base, readState: "empty" };
    const assetsSource = object(assetsPayload);
    const assets = (Array.isArray(assetsPayload) ? assetsPayload : arrayFrom(assetsSource.data ?? assetsPayload, "assets"))
      .map((asset) => {
        const item = object(asset);
        return {
          ...item,
          id: String(item.id ?? item.assetId ?? ""),
          bookId: String(item.bookId ?? item.book_id ?? normalizedBookId),
          kind: item.kind ?? item.type,
          name: item.name ?? item.label,
          prompt: item.prompt ?? item.visualPrompt ?? item.description,
        } as BatchFactoryAsset;
      }).filter((asset) => asset.id);
    const assetImagesPending = Promise.all(assets.map(async (asset) => {
      const payload = await readSection(`images:${asset.id}`, api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/books/${encodeURIComponent(bookId)}/assets/${encodeURIComponent(asset.id)}/images`));
      const source = object(payload);
      const images = (Array.isArray(payload) ? payload : arrayFrom(source.data ?? payload, "images"))
        .map((image) => {
          const item = object(image);
          return { ...item, id: String(item.id ?? item.imageId ?? ""), url: String(item.url ?? item.imageUrl ?? "") } as BatchFactoryAssetImage;
        }).filter((image) => image.id && image.url);
      return [asset.id, images] as const;
    }));
    const productionSource = object(productionPayload);
    const productionJobs = Array.isArray(productionPayload) ? productionPayload : arrayFrom(productionSource, "jobs");
    const productionTasks = productionJobs
      .filter((job) => {
        const item = object(job);
        return String(item.bookId ?? "") === normalizedBookId || arrayFrom(item.tasks, "tasks").some((task) => String(object(task).bookId ?? "") === normalizedBookId);
      })
      .flatMap((job) => {
        const item = object(job);
        const tasks = arrayFrom(item.tasks, "tasks");
        return tasks.length ? tasks : [item];
      })
      .map((task) => object(task) as BatchFactoryProductionTask)
      .filter((task) => !task.bookId || String(task.bookId) === normalizedBookId);
    const productionStatus = productionTasks.map((task) => statusKey(task.status)).filter(Boolean).sort().pop() || undefined;
    const ready:BatchFactoryBookWorkspace = {
      ...base,
      readState: "ready",
      sectionErrors:{...sectionErrors},
      book,
      stages: stagesPayload,
      sourceText: book.sourceText || book.contentPreview || "",
      assets,
      assetImages: {},
      assetImagesLoading:assets.length>0,
      production: { status: productionStatus, tasks: productionTasks },
      mergeStatus: mergePayload,
    };
    onReady?.(ready);
    const assetImagesEntries=await assetImagesPending;
    return {...ready,assetImagesLoading:false,assetImages:Object.fromEntries(assetImagesEntries),sectionErrors:{...sectionErrors}};
  } catch (cause) {
    return { ...base, readState: "error", error: cause instanceof Error ? cause.message : "当前小说读取失败" };
  }
}

function rejectFailedReceipt(value: unknown) {
  const receipt = object(value);
  if (receipt.ok === false || receipt.success === false || receipt.error || receipt.errorMessage || ["failed", "error", "rejected", "cancelled"].includes(statusKey(receipt.status))) {
    throw new Error(String(receipt.error || receipt.errorMessage || "服务端拒绝操作"));
  }
}

export function assertBatchActionReceipt(kind: "director" | "production" | "merge", value: unknown, batchId: string, bookId: string) {
  rejectFailedReceipt(value);
  const receipt = object(value);
  let record: Record<string, any>;
  if (kind === "director") {
    if (receipt.bookId !== bookId || !Array.isArray(receipt.runs)) throw new Error("服务端未返回当前书分镜阶段回执");
    record = object(receipt.runs.filter((run: any) => run.stage === "director").sort((a: any, b: any) => String(b.updatedAt || "").localeCompare(String(a.updatedAt || "")))[0]);
  } else record = object(receipt.job);
  rejectFailedReceipt(record);
  if (typeof record.id !== "string" || !record.id.trim() || record.batchId !== batchId || record.bookId !== bookId || !["queued", "running", "succeeded", "completed"].includes(statusKey(record.status))) {
    throw new Error("服务端未返回匹配当前小说的操作回执，请读取状态确认");
  }
}

function patchContains(actual: unknown, expected: unknown): boolean {
  if (Array.isArray(expected)) return Array.isArray(actual) && actual.length === expected.length && expected.every((item, index) => patchContains(actual[index], item));
  if (expected && typeof expected === "object") return !!actual && typeof actual === "object" && !Array.isArray(actual) && Object.entries(expected).every(([key, value]) => Object.prototype.hasOwnProperty.call(actual, key) && patchContains((actual as Record<string, unknown>)[key], value));
  return actual === expected;
}

export async function saveBatchConfigVersion(api: BatchFactoryV11Api, batchId: string, version: BatchFactoryConfigVersion, revision: number) {
  if (!version.config || typeof version.config !== "object" || Array.isArray(version.config)) throw new Error("配置版本未返回可应用配置");
  const saved = object(await api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/settings`, { method: "PUT", data: { patch: version.config, expectedRevision: revision } }));
  rejectFailedReceipt(saved);
  if (saved.scope?.kind !== "batch" || saved.scope?.batchId !== batchId || !Number.isSafeInteger(saved.revision) || saved.revision <= revision || !patchContains(saved.patch, version.config)) throw new Error("配置保存回执无效或与请求不符，未确认保存");
  const payload = await api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}`);
  rejectFailedReceipt(payload);
  const batch = object(unwrapBatchPayload(payload));
  const settings = object(batch.settingsState);
  if (batch.id !== batchId || settings.revision !== saved.revision || !patchContains(settings.patch, version.config)) throw new Error("配置读回与保存回执不一致，请重新读取确认");
  return normalizeBatch(batch);
}

function expectedRevision(value: unknown, label: string) {
  const revision = Number(value);
  if (!Number.isSafeInteger(revision) || revision < 0) throw new Error(`${label}版本号无效，请重新读取后再保存`);
  return revision;
}

function contentWindowNumber(value: unknown, minimum: number, maximum: number, label: string) {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < minimum || parsed > maximum) throw new Error(`${label}无效，请重新填写`);
  return parsed;
}

function contentWindowMatches(metadata: unknown, values: { contentRangeLines: number; contentCaptureCharacters: number }) {
  const source = object(metadata);
  return Number(source.contentRangeLines) === values.contentRangeLines && Number(source.contentCaptureCharacters) === values.contentCaptureCharacters;
}

async function saveBatchFactoryBookContentWindowReceipt(api: BatchFactoryV11Api, batchId: string, book: BatchFactoryBook, values: { contentRangeLines: number; contentCaptureCharacters: number }) {
  const bookId = String(book.id || "").trim();
  const expected = expectedRevision(book.revision, "内容范围");
  if (!bookId) throw new Error("当前小说编号无效，请重新读取后再保存");
  const metadata = { ...object(book.sourceMetadata), ...values };
  const saved = object(await api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/books/${encodeURIComponent(bookId)}/metadata`, { method: "PUT", data: { metadata, expectedRevision: expected } }));
  rejectFailedReceipt(saved);
  const receipt = object(saved.book);
  if (String(receipt.id || "") !== bookId || !Number.isSafeInteger(receipt.revision) || receipt.revision <= expected || !contentWindowMatches(receipt.sourceMetadata, values)) throw new Error("内容范围保存回执无效，请重新读取确认");
  return receipt as BatchFactoryBook;
}

function confirmedContentWindowValues(values: { contentRangeLines: number; contentCaptureCharacters: number }) {
  return {
    contentRangeLines: contentWindowNumber(values.contentRangeLines, 1, 500, "内容范围"),
    contentCaptureCharacters: contentWindowNumber(values.contentCaptureCharacters, 100, 100000, "获取上限"),
  };
}

export async function saveBatchFactoryBookContentWindowDefaults(api: BatchFactoryV11Api, batch: BatchFactoryBatch, values: { contentRangeLines: number; contentCaptureCharacters: number }): Promise<BatchFactoryBatch> {
  const batchId = String(batch.id || "").trim();
  const requested = confirmedContentWindowValues(values);
  if (!batchId || !batch.books.length) throw new Error("新建批量未返回可配置小说，请重新读取确认");
  const receipts = new Map<string, BatchFactoryBook>();
  for (const book of batch.books) {
    const receipt = await saveBatchFactoryBookContentWindowReceipt(api, batchId, book, requested);
    receipts.set(String(receipt.id), receipt);
  }
  const reloaded = normalizeBatch(unwrapBatchPayload(await api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}`)));
  if (reloaded.id !== batchId) throw new Error("内容范围读回批量不一致，请重新读取确认");
  for (const [bookId, receipt] of receipts) {
    const confirmed = reloaded.books.find(item => item.id === bookId || item.bookId === bookId);
    const confirmedRevision = Number(confirmed?.revision);
    if (!confirmed || !Number.isSafeInteger(confirmedRevision) || confirmedRevision < Number(receipt.revision) || !contentWindowMatches(confirmed.sourceMetadata, requested)) throw new Error("内容范围读回与保存不一致，请重新读取确认");
  }
  return reloaded;
}

export async function saveBatchFactoryBookContentWindow(api: BatchFactoryV11Api, batchId: string, book: BatchFactoryBook, values: { contentRangeLines: number; contentCaptureCharacters: number }): Promise<BatchFactoryBook> {
  const bookId = String(book.id || "").trim();
  const requested = confirmedContentWindowValues(values);
  const receipt = await saveBatchFactoryBookContentWindowReceipt(api, batchId, book, requested);
  const payload = await api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}`);
  rejectFailedReceipt(payload);
  const reloaded = normalizeBatch(unwrapBatchPayload(payload));
  const confirmed = reloaded.books.find(item => item.id === bookId || item.bookId === bookId);
  const confirmedRevision = Number(confirmed?.revision);
  const receiptRevision = Number(receipt.revision);
  if (!confirmed || !Number.isSafeInteger(confirmedRevision) || confirmedRevision < receiptRevision || !contentWindowMatches(confirmed.sourceMetadata, requested)) throw new Error("内容范围读回与保存不一致，请重新读取确认");
  return confirmed;
}

export async function saveBatchFactoryAssetPrompt(api: BatchFactoryV11Api, batchId: string, bookId: string, asset: BatchFactoryAsset) {
  const id = String(asset.id || "").trim();
  const name = String(asset.name || "").trim();
  const prompt = String(asset.prompt || asset.visualPrompt || asset.description || "").trim();
  const revision = expectedRevision(asset.revision, "资产");
  if (!id || !name || !prompt) throw new Error("资产名称和提示词不能为空");
  const saved = object(await api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/books/${encodeURIComponent(bookId)}/assets/${encodeURIComponent(id)}`, { method: "PATCH", data: { name, prompt, expectedRevision: revision } }));
  rejectFailedReceipt(saved);
  const value = object(saved.asset);
  if (value.id !== id || value.name !== name || value.prompt !== prompt || !Number.isSafeInteger(value.revision) || value.revision <= revision) throw new Error("资产提示词保存回执无效，请重新读取确认");
  return value as BatchFactoryAsset;
}

export async function saveBatchFactoryVideoPrompt(api: BatchFactoryV11Api, batchId: string, bookId: string, videoId: string, prompt: string, revision: number) {
  const id = String(videoId || "").trim();
  const value = String(prompt || "").trim();
  const expected = expectedRevision(revision, "分镜");
  if (!id || !value) throw new Error("分镜提示词不能为空");
  const saved = object(await api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/books/${encodeURIComponent(bookId)}/videos/${encodeURIComponent(id)}/override`, { method: "PUT", data: { patch: { videoPrompt: value }, expectedRevision: expected } }));
  rejectFailedReceipt(saved);
  const scope = object(saved.scope);
  if (scope.kind !== "video" || scope.batchId !== batchId || scope.bookId !== bookId || scope.videoId !== id || !Number.isSafeInteger(saved.revision) || saved.revision <= expected || !patchContains(saved.patch, { videoPrompt: value })) throw new Error("分镜提示词保存回执无效，请重新读取确认");
  return saved;
}

export async function importBatchBooks(api: BatchFactoryV11Api, input: {
  title: string; groups: Array<{ platformId: string; platformName: string; inputText: string }>;
  execution: "none" | "immediate" | "scheduled"; presetId: string; runMode: string; concurrency: number; scheduledAt?: string;
  configVersion?: BatchFactoryConfigVersion;
  contentRangeLines?: number; contentCaptureCharacters?: number;
}) {
  if (input.execution !== "none") executionPayload(input);
  const contentWindow = confirmedContentWindowValues({ contentRangeLines: input.contentRangeLines ?? 5, contentCaptureCharacters: input.contentCaptureCharacters ?? 4000 });
  const groups = [];
  const warnings: string[] = [];
  for (const group of input.groups) {
    const ids = manualBookIdsFromInput(group.inputText);
    const sourceTextByBookId: Record<string, string> = {};
    const sourceMetadataByBookId: Record<string, unknown> = {};
    // A production guard rejection must abort before any subsequent request.
    const fetched = object(await fetchBatchFactoryOriginals(api, { platform: group.platformId, bookIds: ids, maxTxt: contentWindow.contentCaptureCharacters }));
    if (!Array.isArray(fetched.results)) throw new Error("原文读取响应格式无效，未创建批量");
    for (const item of fetched.results) {
      const id = String(item?.bookId || "");
      if (ids.includes(id) && item.status === "ok" && String(item.data || "").trim()) {
        sourceTextByBookId[id] = String(item.data);
        if (item.sourceMetadata && typeof item.sourceMetadata === "object") sourceMetadataByBookId[id] = item.sourceMetadata;
      }
    }
    const missing = ids.filter(id => !sourceTextByBookId[id]);
    if (missing.length) warnings.push(`${group.platformName}: ${missing.length} 本正文未返回，需单书重试`);
    groups.push({ ...group, inputText: ids.join("\n"), sourceTextByBookId, sourceMetadataByBookId });
  }
  const created = await createManualIntake(api, { title: input.title.trim(), groups, automationEnabled: false });
  const intakeId = String(object(created).intake?.id || object(created).intakeId || "");
  const payload = intakeId ? await createBatchFromIntake(api, intakeId, { title: input.title.trim() }) : created;
  let batch = normalizeBatch(unwrapBatchPayload(payload));
  if (!batch.id) throw new Error("服务端未返回批量编号，未启动自动化");
  try {
    const createdBatch = normalizeBatch(unwrapBatchPayload(await api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batch.id)}`)));
    batch = await saveBatchFactoryBookContentWindowDefaults(api, createdBatch, contentWindow);
  } catch (cause) {
    warnings.push(`批量 ${batch.id} 已创建，内容范围未确认，未启动自动化：${cause instanceof Error ? cause.message : "内容范围保存失败"}`);
    return { batch, warnings };
  }
  if (input.configVersion) {
    try { batch = await saveBatchConfigVersion(api, batch.id, input.configVersion, Number(object(batch).settingsState?.revision ?? 0)); }
    catch (cause) {
      warnings.push(`批量 ${batch.id} 已创建，统一配置未确认，未启动自动化：${cause instanceof Error ? cause.message : "配置保存失败"}`);
      return { batch, warnings };
    }
  }
  if (input.execution !== "none") {
    try {
      await startBatchExecution(api, batch.id, input);
    } catch (cause) { warnings.push(`批量已创建，自动化未确认：${cause instanceof Error ? cause.message : "启动失败"}`); }
  }
  return { batch, warnings };
}

export type BatchExecutionInput = { presetId: string; runMode: string; concurrency: number; scheduledAt?: string };
function executionPayload(input: BatchExecutionInput) {
  if (!input.presetId.trim()) throw new Error("请先选择服务端自动化预设");
  if (!["video_no_submit", "full_submit", "storyboard_only"].includes(input.runMode)) throw new Error("请选择有效执行范围");
  if (![1, 2, 4].includes(input.concurrency)) throw new Error("请选择有效并发数");
  if (input.scheduledAt && (!Number.isFinite(Date.parse(input.scheduledAt)) || Date.parse(input.scheduledAt) <= Date.now())) throw new Error("定时执行必须选择未来时间");
  return { presetId: input.presetId, runMode: input.runMode, autoPublish: input.runMode === "full_submit", concurrency: input.concurrency, scheduledAt: input.scheduledAt || "" };
}
export async function startBatchExecution(api: BatchFactoryV11Api, batchId: string, input: BatchExecutionInput) {
  if (!batchId.trim()) throw new Error("请先选择当前作品");
  const payload = executionPayload(input);
  const response = object(await startBatchAutomation(api, batchId, payload));
  rejectFailedReceipt(response);
  const receipt = object(response.automation || response);
  if (!receipt.state || (receipt.batchId && receipt.batchId !== batchId)) throw new Error("启动回执无效，请刷新作品确认，勿重复启动");
  const status = await loadBatchFactoryAutomation(api, batchId);
  if (!status?.state || !["scheduled", "running", "paused", "needs_attention", "completed"].includes(status.state) || (status.batchId && status.batchId !== batchId)) throw new Error(`启动后的状态未确认（${status?.state || "无状态"}），请刷新作品，勿重复启动`);
  if (payload.scheduledAt && Date.parse(status.scheduledAt || "") !== Date.parse(payload.scheduledAt)) throw new Error("定时时间读回不一致，请刷新作品确认，勿重复启动");
  return status;
}

export function createBatchFactoryBatch(payload: Record<string, unknown> = {}) {
  return batchFactoryV11Api.request(`${BATCH_FACTORY_BASE}/batches`, { method: "POST", data: payload });
}

function resolveApi(value: unknown): BatchFactoryV11Api {
  return value && typeof value === "object" && typeof (value as BatchFactoryV11Api).request === "function"
    ? value as BatchFactoryV11Api
    : batchFactoryV11Api;
}

export function fetchBatchFactoryOriginals(payload: Record<string, unknown>): Promise<unknown>;
export function fetchBatchFactoryOriginals(api: BatchFactoryV11Api, payload: Record<string, unknown>): Promise<unknown>;
export function fetchBatchFactoryOriginals(apiOrPayload: BatchFactoryV11Api | Record<string, unknown>, maybePayload?: Record<string, unknown>) {
  const api = resolveApi(maybePayload === undefined ? null : apiOrPayload);
  const payload = maybePayload === undefined ? apiOrPayload as Record<string, unknown> : maybePayload;
  return api.request(`${BATCH_FACTORY_BASE}/fetch-originals`, { method: "POST", data: payload });
}

export function createManualIntake(payload: Record<string, unknown>): Promise<unknown>;
export function createManualIntake(api: BatchFactoryV11Api, payload: Record<string, unknown>): Promise<unknown>;
export function createManualIntake(apiOrPayload: BatchFactoryV11Api | Record<string, unknown>, maybePayload?: Record<string, unknown>) {
  const api = resolveApi(maybePayload === undefined ? null : apiOrPayload);
  const payload = maybePayload === undefined ? apiOrPayload as Record<string, unknown> : maybePayload;
  return api.request(`${BATCH_FACTORY_BASE}/intakes/manual`, { method: "POST", data: payload });
}

export function createBatchFromIntake(intakeId: string, payload?: Record<string, unknown>): Promise<unknown>;
export function createBatchFromIntake(api: BatchFactoryV11Api, intakeId: string, payload?: Record<string, unknown>): Promise<unknown>;
export function createBatchFromIntake(apiOrIntakeId: BatchFactoryV11Api | string, intakeIdOrPayload?: string | Record<string, unknown>, maybePayload: Record<string, unknown> = {}) {
  const hasApi = typeof apiOrIntakeId === "object";
  const api = resolveApi(hasApi ? apiOrIntakeId : null);
  const intakeId = String(hasApi ? intakeIdOrPayload : apiOrIntakeId);
  const payload = (hasApi ? maybePayload : (intakeIdOrPayload || {})) as Record<string, unknown>;
  return api.request(`${BATCH_FACTORY_BASE}/intakes/${encodeURIComponent(intakeId)}/batches`, { method: "POST", data: payload });
}

export function listBatchFactoryAutomationPresets(api?: BatchFactoryV11Api) {
  return (api || batchFactoryV11Api).request<{ presets?: BatchFactoryAutomationPreset[] }>(`${BATCH_FACTORY_BASE}/automation-presets`)
    .then(payload => Array.isArray(payload?.presets) ? payload.presets : []);
}

export function loadBatchFactoryAutomation(
  api: BatchFactoryV11Api,
  batchId: string,
): Promise<BatchFactoryAutomationStatus | null> {
  return api.request<{ automation?: BatchFactoryAutomationStatus }>(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/automation`)
    .then(payload => payload?.automation || null);
}

export function startBatchAutomation(batchId: string, payload: Record<string, unknown>): Promise<unknown>;
export function startBatchAutomation(api: BatchFactoryV11Api, batchId: string, payload: Record<string, unknown>): Promise<unknown>;
export function startBatchAutomation(apiOrBatchId: BatchFactoryV11Api | string, batchIdOrPayload: string | Record<string, unknown>, maybePayload?: Record<string, unknown>) {
  const hasApi = typeof apiOrBatchId === "object";
  const api = resolveApi(hasApi ? apiOrBatchId : null);
  const batchId = String(hasApi ? batchIdOrPayload : apiOrBatchId);
  const payload = (hasApi ? maybePayload : batchIdOrPayload) as Record<string, unknown>;
  return api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/automation/start`, { method: "POST", data: payload });
}

function automationAction(api: BatchFactoryV11Api, batchId: string, action: "pause" | "resume" | "retry" | "cancel", payload: Record<string, unknown> = {}) {
  return api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/automation/${action}`, { method: "POST", data: payload });
}

export function pauseBatchAutomation(batchId: string, api: BatchFactoryV11Api = batchFactoryV11Api) { return automationAction(api, batchId, "pause"); }
export function resumeBatchAutomation(batchId: string, api: BatchFactoryV11Api = batchFactoryV11Api) { return automationAction(api, batchId, "resume"); }
export function retryBatchAutomation(batchId: string, bookIds: string[] = [], api: BatchFactoryV11Api = batchFactoryV11Api) { return automationAction(api, batchId, "retry", { bookIds }); }
export function cancelBatchAutomation(batchId: string, api: BatchFactoryV11Api = batchFactoryV11Api) { return automationAction(api, batchId, "cancel"); }

export function runBatchDirector(batchId: string) {
  return batchFactoryV11Api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/director`, { method: "POST", data: {} });
}

export function runBookDirector(batchId: string, bookId: string) {
  return batchFactoryV11Api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/books/${encodeURIComponent(bookId)}/stages/director`, { method: "POST", data: {} });
}

export function runProduction(batchId: string, bookId?: string, payload?: Record<string, unknown>): Promise<unknown>;
export function runProduction(api: BatchFactoryV11Api, batchId: string, bookId?: string, payload?: Record<string, unknown>): Promise<unknown>;
export function runProduction(apiOrBatchId: BatchFactoryV11Api | string, batchIdOrBookId = "", bookIdOrPayload: string | Record<string, unknown> = "", maybePayload: Record<string, unknown> = {}) {
  const hasApi = typeof apiOrBatchId === "object";
  const api = resolveApi(hasApi ? apiOrBatchId : null);
  const batchId = String(hasApi ? batchIdOrBookId : apiOrBatchId);
  const bookId = String(hasApi ? (typeof bookIdOrPayload === "string" ? bookIdOrPayload : "") : batchIdOrBookId);
  const payload = (hasApi ? (typeof bookIdOrPayload === "object" ? bookIdOrPayload : maybePayload) : (bookIdOrPayload as Record<string, unknown>)) || {};
  const suffix = bookId ? `books/${encodeURIComponent(bookId)}/production` : "production";
  return api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/${suffix}`, { method: "POST", data: payload });
}

export function runMerge(batchId: string, bookId: string, payload: Record<string, unknown> = {}) {
  return batchFactoryV11Api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/books/${encodeURIComponent(bookId)}/merge`, { method: "POST", data: payload });
}

// The V12 server owns the session, upload, receipt, and background readback.
export function submitBatchFactoryBookTo121(batchId: string, bookId: string, payload?: Record<string, unknown>): Promise<unknown>;
export function submitBatchFactoryBookTo121(api: BatchFactoryV11Api, batchId: string, bookId: string, payload?: Record<string, unknown>): Promise<unknown>;
export function submitBatchFactoryBookTo121(apiOrBatchId: BatchFactoryV11Api | string, batchIdOrBookId: string, bookIdOrPayload: string | Record<string, unknown> = "", maybePayload: Record<string, unknown> = {}) {
  const hasApi = typeof apiOrBatchId === "object";
  const api = resolveApi(hasApi ? apiOrBatchId : null);
  const batchId = String(hasApi ? batchIdOrBookId : apiOrBatchId).trim();
  const bookId = String(hasApi ? (typeof bookIdOrPayload === "string" ? bookIdOrPayload : "") : batchIdOrBookId).trim();
  const payload = (hasApi ? (typeof bookIdOrPayload === "object" ? bookIdOrPayload : maybePayload) : (bookIdOrPayload as Record<string, unknown>)) || {};
  return api.request(`${BATCH_FACTORY_BASE}/batches/${encodeURIComponent(batchId)}/books/${encodeURIComponent(bookId)}/publish-121`, { method: "POST", data: payload });
}
