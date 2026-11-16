import type { BatchFactoryAutomationStatus } from "../../services/batchFactoryV11";

/**
 * 服务端自动化 stage → 界面四步条（只读状态机）。
 *
 * id 与标签全部取自网页端权威清单，不靠前缀猜：
 * - frontend/src/user/pages/shuihuo/automationStages.json（stages[] 的 id / label / terminal）
 * - frontend/src/user/pages/shuihuo/BatchFactoryNovelList.jsx 的 automationStageLabel
 *
 * 漫画模式在 automationStages.json 里的四步是：
 *   assets（漫画资产已就绪）→ director（漫画分页与提示词已就绪）→ video（漫画图片生成）→ complete（漫画合集）
 * 这里只做「已经走完几步」的推导，不新增任何服务端没有的阶段。
 */
export const RUN_STAGE_LABELS: Record<string, string> = {
  // 视频链路（漫画不会出现，保留原文枚举，避免未知值被前缀猜测）
  source: "原文已就绪",
  assets: "漫画资产已就绪",
  director: "文本提示词已就绪",
  video: "视频生成",
  upload: "上传网络",
  complete: "完成",
  preflight: "预检",
  fetch_original: "获取原文",
  storyboards: "分镜生成",
  openings: "片头生成",
  audio: "音频生成",
  merge: "视频合成",
  submit: "定时提交",
  // 运行时漫画阶段
  comic_assets: "漫画资产",
  comic_asset_images: "漫画资产出图",
  comic_bootstrap: "漫画分段与提示词",
  comic_prompt_recovery: "补齐漫画提示词",
  comic_resume: "恢复漫画生产",
  comic_images: "漫画图片",
  comic_collection: "漫画合集整理",
  comic_publish: "发布漫画合集",
  // 收尾阶段
  audio_planning: "配音与逐行时长",
  waiting_submit: "等待发布",
  upload_confirm: "已上传待确认",
  published: "发布完成",
  completed: "已完成",
  // 实测补充：漫画跑完时 stage=ready_for_upload（不在 automationStages.json 清单内），
  // 此时 automation.state 已经是 completed，四步全部完成。
  ready_for_upload: "漫画合集已完成，等待上传",
};

/** 四步条的四个节点，顺序即 comic 模式的 assets → director → video → complete。 */
export const COMIC_RUN_STEPS = ["漫画资产", "分页与提示词", "漫画出图", "漫画合集"] as const;

/** stage → 该 stage 正在进行的步骤下标；4 表示四步都已完成。 */
const STAGE_STEP: Record<string, number> = {
  queued: 0,
  preflight: 0,
  assets: 0,
  comic_assets: 0,
  comic_asset_images: 0,
  comic_bootstrap: 1,
  comic_prompt_recovery: 1,
  comic_resume: 1,
  director: 1,
  video: 2,
  comic_images: 2,
  comic_collection: 3,
  comic_publish: 3,
  upload_confirm: 3,
  waiting_submit: 3,
  published: 4,
  complete: 4,
  completed: 4,
  ready_for_upload: 4,
};

/** 未在清单里的 stage：返回 -1，界面原样显示 stage，不推测到哪一步。 */
export function runStageStep(stage: string) {
  const key = String(stage || "").trim().toLowerCase();
  const step = STAGE_STEP[key];
  return typeof step === "number" ? step : -1;
}

export function runStageLabel(stage: string) {
  const raw = String(stage || "").trim();
  if (!raw) return "";
  return RUN_STAGE_LABELS[raw.toLowerCase()] || raw;
}

export type ComicRunStageView = {
  state: string;
  /** 服务端原始 stage，未观测值直接给界面显示。 */
  stage: string;
  label: string;
  /** -1 表示 stage 不在清单里，四步条保持不动。 */
  step: number;
  terminal: boolean;
  /** 可操作提示（服务重启中断 / 失败原因），服务端没给就是空串。 */
  notice: string;
};

const TERMINAL_STATES = new Set(["completed", "failed", "cancelled", "canceled", "partial", "stopped", "needs_attention"]);

/**
 * 只读视图：状态全来自 GET /automation，不持有本地阶段机。
 * bookId 命中不了就退回第一本书，避免空数组把整块状态清掉。
 */
export function runStageView(status: BatchFactoryAutomationStatus | null, bookId = ""): ComicRunStageView {
  const state = String(status?.state || "").trim().toLowerCase();
  const books = Array.isArray(status?.books) ? status.books : [];
  const book = (bookId ? books.find((item) => String(item?.bookId || "") === String(bookId)) : undefined) || books[0] || {};
  const stage = String(book.stage || "").trim();
  const key = state;
  const terminal = TERMINAL_STATES.has(key);
  const notice = terminal ? String(book.message || "").trim() || String(book.error || "").trim() : "";
  return {
    state,
    stage,
    // 没有 stage 时退回 automation.state，两者都没有才留空。
    label: runStageLabel(stage) || runStageLabel(key),
    step: runStageStep(stage),
    terminal,
    notice,
  };
}

/**
 * 提交闸门：automation.state → 客户端该怎么处理。
 * - active：后端 activeAutomationJob 只把 running/scheduled/paused/needs_attention 算活动，
 *   此时再提交一定拿 409，客户端不提交，直接提示「已有任务在跑」；
 * - finished：终态再提交会新建 run（go-auto-<纳秒>）并重新消耗出图额度，必须二次确认；
 * - idle：没有历史任务，或状态读不到，按原流程提交。
 */
const ACTIVE_RUN_STATES = new Set(["running", "scheduled", "paused", "needs_attention"]);
const FINISHED_RUN_STATES = new Set(["completed", "failed", "cancelled", "canceled", "partial", "stopped"]);

export type ComicRunSubmitGate = "active" | "finished" | "idle";

export function comicRunSubmitGate(state: string): ComicRunSubmitGate {
  const key = String(state || "").trim().toLowerCase();
  if (ACTIVE_RUN_STATES.has(key)) return "active";
  if (FINISHED_RUN_STATES.has(key)) return "finished";
  return "idle";
}

const RUN_STATE_LABELS: Record<string, string> = {
  running: "生成中",
  scheduled: "已排队",
  paused: "已暂停",
  needs_attention: "需人工处理",
  completed: "已完成",
  failed: "已失败",
  cancelled: "已取消",
  canceled: "已取消",
  partial: "部分完成",
  stopped: "已中断",
};

export function runStateLabel(state: string) {
  const raw = String(state || "").trim();
  if (!raw) return "";
  return RUN_STATE_LABELS[raw.toLowerCase()] || raw;
}

/**
 * 出图开关关闭时服务端在 comic_asset_images 阶段直接跳过资产图（实测日志：
 * 「因资产图片开关关闭，已跳过 2 个漫画资产参考图生成」）。这是跳过，不是失败，
 * 界面不能因此显示成错误或空白。
 */
export function assetImagesSkipped(status: BatchFactoryAutomationStatus | null) {
  const books = Array.isArray(status?.books) ? status.books : [];
  return books.some((book) => (book.logs || []).some((log) => String(log?.message || "").includes("资产图片开关关闭")));
}

/**
 * 页数与时长预估（界面已展示，必须始终按「估算」措辞，不能写成承诺）：
 * 后端按句切页、单页下限 45 字，实测 181 字出 2 页，约 90~100 字/页。
 * 出图实测单张 4.5~5 分钟，服务端并发 4 路，所以按「每 4 页一轮」折算墙钟时间。
 * 注意 limits.maxPages=35 是上限，不是预计页数。
 */
export const COMIC_CHARS_PER_PAGE_LOW = 90;
export const COMIC_CHARS_PER_PAGE_HIGH = 100;
export const COMIC_MAX_PAGES = 35;
export const COMIC_IMAGE_CONCURRENCY = 4;
export const COMIC_PAGE_MINUTES_LOW = 4.5;
export const COMIC_PAGE_MINUTES_HIGH = 5;
/** 固定前置耗时：资产提取 ~65s + 分页 ~15s + 收尾，统一按 1.5 分钟计。 */
export const COMIC_PREPARE_MINUTES = 1.5;

export function estimateComicPages(charCount: number) {
  const chars = Math.max(0, Math.floor(charCount));
  if (!chars) return 0;
  const pages = Math.ceil(chars / COMIC_CHARS_PER_PAGE_HIGH);
  return Math.min(COMIC_MAX_PAGES, Math.max(1, pages));
}

/** 出图按并发 4 路折算轮次，再固定加 1.5 分钟前置耗时，覆盖资产提取与分页。 */
export function estimateComicRunMinutes(pages: number) {
  const safe = Math.max(0, Math.min(COMIC_MAX_PAGES, Math.floor(pages)));
  if (!safe) return { low: 0, high: 0, waves: 0 };
  const waves = Math.ceil(safe / COMIC_IMAGE_CONCURRENCY);
  return {
    low: waves * COMIC_PAGE_MINUTES_LOW + COMIC_PREPARE_MINUTES,
    high: waves * COMIC_PAGE_MINUTES_HIGH + COMIC_PREPARE_MINUTES,
    waves,
  };
}

/**
 * 提交区展示文案：给出覆盖全程的真实区间（含 1.5 分钟前置耗时），
 * 页数少时也不向上圆成一个大数字，措辞固定为「预计 / 约 / 估算」。
 * 返回空串表示正文还没到可提交长度，界面不展示估算。
 */
export function estimateComicRunText(charCount: number) {
  const pages = estimateComicPages(charCount);
  if (!pages) return "";
  const { low, high } = estimateComicRunMinutes(pages);
  return `预计 ${pages} 页 · 约 ${low === high ? low : `${low}~${high}`} 分钟（估算）`;
}
