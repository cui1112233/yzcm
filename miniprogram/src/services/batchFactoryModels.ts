import type {
  BatchFactoryBatch,
  BatchFactoryBook,
  BatchFactoryCapability,
  BatchFactoryStatus,
} from "./batchFactoryV11";

export type BatchFactoryStageKey = "source" | "assets" | "storyboard" | "video" | "merge" | "publish";

export type BatchFactoryStageState = {
  status: string;
  label: string;
  tone: "muted" | "info" | "attention" | "success" | "danger" | "unknown";
  reason?: string;
  taskId?: string;
};

export type BatchFactoryBookView = BatchFactoryBook & {
  coverUrl?: string;
  bookId: string;
  sourceBookId: string;
  platformId?: string;
  platformName?: string;
  gender?: string;
  style?: string;
  source?: string;
  stages: Record<BatchFactoryStageKey, BatchFactoryStageState>;
};

export type BatchFactoryReadState = "loading" | "ready" | "empty" | "unavailable" | "error";

export type BatchFactoryProjectView = {
  readState: BatchFactoryReadState;
  batches: BatchFactoryBatch[];
  batch: BatchFactoryBatch | null;
  books: BatchFactoryBookView[];
  capabilities: Record<string, BatchFactoryCapability>;
  productionStatus: BatchFactoryStatus | null;
  mergeStatus: BatchFactoryStatus | null;
  automation: unknown;
  readError?: string;
};

function record(value: unknown): Record<string, any> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, any> : {};
}
function normalizedStatus(value: unknown) {
  return String(value || "").trim().toLowerCase().replace(/[\s-]+/g, "_");
}

export function statusTone(status: unknown): Pick<BatchFactoryStageState, "tone" | "label"> {
  const raw = String(status || "").trim();
  const key = normalizedStatus(raw);
  const label = ({ succeeded: "已完成", completed: "已完成", merged: "已合并", published: "已发布", uploaded: "已上传", failed: "失败", error: "异常", cancelled: "已取消", running: "生成中", processing: "处理中", ai_processing: "AI处理中", queued: "排队中", pending: "待处理", waiting: "等待中", waiting_merge: "待合并", waiting_upload: "待上传" } as Record<string, string>)[key] || raw;
  if (["succeeded", "completed", "merged", "published", "uploaded"].includes(key) || ["已完成", "已合并", "已发布"].includes(raw)) return { tone: "success", label };
  if (["failed", "error", "cancelled"].includes(key) || ["异常", "失败", "已取消"].includes(raw)) return { tone: "danger", label };
  if (["running", "processing", "ai_processing"].includes(key) || ["生成中", "AI处理中"].includes(raw)) return { tone: "info", label };
  if (["queued", "pending", "waiting", "waiting_merge", "waiting_upload"].includes(key) || ["排队中", "待开始", "待合并", "待上传"].includes(raw)) return { tone: "attention", label };
  if (raw) return { tone: "unknown", label: raw };
  return { tone: "unknown", label: "状态未返回" };
}

function stage(value: unknown, fallback = "") : BatchFactoryStageState {
  const item = record(value);
  const status = String(item.status ?? (typeof value === "string" ? value : fallback)).trim();
  const tone = statusTone(status);
  const reason = String(item.reason ?? item.error ?? item.message ?? "").trim();
  const taskId = String(item.taskId ?? item.providerTaskId ?? item.id ?? "").trim();
  return { status, label: tone.label, tone: tone.tone, ...(reason ? { reason } : {}), ...(taskId ? { taskId } : {}) };
}

export function normalizeBatchFactoryBook(value: unknown): BatchFactoryBookView {
  const item = record(value);
  const metadata = record(item.sourceMetadata ?? item.metadata);
  const production = record(item.production ?? item.video ?? item.productionJob);
  const merge = record(item.merge ?? item.mergeJob);
  const publish = record(item.publish ?? item.publishStatus);
  const stages = {
    source: stage(item.source ?? item.sourceText ? { status: "ready" } : "", ""),
    assets: stage(item.assets ?? item.assetSummary, ""),
    storyboard: stage(item.storyboard ?? item.director ?? item.directorRevision, ""),
    video: stage(production, ""),
    merge: stage(merge, ""),
    publish: stage(publish, ""),
  } as Record<BatchFactoryStageKey, BatchFactoryStageState>;
  return {
    ...item,
    id: String(item.id ?? item.bookId ?? ""),
    // View navigation and task ownership use the factory record ID.
    // The upstream bookId is the source bookstore ID, not a route key.
    bookId: String(item.id ?? item.bookId ?? ""),
    sourceBookId: String(item.bookId ?? ""),
    title: item.title ?? item.name ?? item.bookName,
    coverUrl: String(item.coverUrl ?? item.cover?.url ?? item.media?.coverUrl ?? "").trim() || undefined,
    platformId: String(item.platformId ?? item.platform ?? metadata.platformId ?? "").trim() || undefined,
    platformName: String(item.platformName ?? metadata.platformName ?? metadata.platformLabel ?? "").trim() || undefined,
    gender: String(item.gender ?? metadata.gender ?? "").trim() || undefined,
    style: String(item.style ?? metadata.style ?? "").trim() || undefined,
    source: String(item.source ?? metadata.source ?? "").trim() || undefined,
    stages,
  };
}

export function deriveBookStageSummary(
  book: BatchFactoryBook,
  productionStatus?: BatchFactoryStatus | null,
  mergeStatus?: BatchFactoryStatus | null,
): BatchFactoryBookView {
  const view = normalizeBatchFactoryBook(book);
  const productionJob = (productionStatus?.jobs || []).find((job) => job.bookId === book.id || (job.tasks || []).some((task) => task.bookId === book.id));
  const productionTask = productionJob?.tasks?.find((task) => task.bookId === book.id || !task.bookId && productionJob.bookId === book.id);
  const mergeJob = (mergeStatus?.jobs || []).find((job) => job.bookId === book.id);
  if (!view.stages.video.status && productionTask?.status) view.stages.video = stage({ ...productionTask, id: productionJob?.bookId });
  if (!view.stages.merge.status && mergeJob?.status) view.stages.merge = stage(mergeJob);
  if (!view.stages.source.status && book.status) view.stages.source = stage(book.status);
  return view;
}
