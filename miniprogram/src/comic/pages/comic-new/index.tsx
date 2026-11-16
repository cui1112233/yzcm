import Taro, { useDidHide, useDidShow } from "@tarojs/taro";
import { Button, Image, ScrollView, Text, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import BrandHeader from "../../../components/BrandHeader";
import AppIcon from "../../../components/AppIcon";
import ComicStickySpacer from "../../components/ComicStickySpacer";
import ComicSettingsStage from "./ComicSettingsStage";
import ComicAssetsStage from "./ComicAssetsStage";
import ComicConfigConfirmStage from "./ComicConfigConfirmStage";
import ComicStageHeader from "./ComicStageHeader";
import type { ComicStage } from "./stageState";
import { canEnterPrivateComicStage } from "./privateComicParity";
import { ThemedPage } from "../../../theme";
import type { BatchFactoryAutomationStatus } from "../../../services/batchFactoryV11";
import {
  COMIC_PRESET_EMPTY_HINT, DEFAULT_COMIC_ASPECT_RATIO,
  DEFAULT_COMIC_RUN_MODE, MIN_COMIC_SOURCE_CHARS, buildComicInitialBatchSettings,
  comicFailureText, comicNewErrorText, comicPresetAvailability,
  comicPresetComicReason, comicProgress, comicSourceLength, createComicBatch, loadComicPrivateSetup,
  readComicAutomation, resolveComicStyleThumb, startComicPolling, submitComicRun, uploadComicReference, type ComicReferenceInfo,
  validateComicSource, type ComicPresetOption, type ComicStylePreset,
} from "../../services/comicFlow";
import {
  extractComicAssets,
  generateComicAssetImages, loadComicAssetImagePath, loadComicAssetImages, loadComicAssets,
  readComicAssetImageTask, retryComicBookStage,
  type ComicAssetImageItem, type ComicAssetItem,
} from "../../services/comicAssets";
import {
  buildPreviewRequest, createComicCollection, downloadComicCollectionZip, loadComicImagePaths,
  loadComicPages, saveComicImageToAlbum, type ComicPageItem,
} from "../../services/comicResult";
import {
  COMIC_RUN_STEPS, assetImagesSkipped, comicRunSubmitGate, runStageView, runStateLabel,
} from "../../services/runStage";
import {
  clearComicActiveRun, readComicActiveRun, saveComicActiveRun, shouldForgetComicRun,
} from "../../services/comicRecovery";
import "../../styles/comic.scss";

/**
 * 私人定制漫画：对标剧本生成的单页工作区。
 * 粘贴正文 → 选画风 / 画幅 / 参考图 → 吸底「开始生成」→ 同页「结果」tab 里看进度和页图。
 * 界面上不出现批次、不出现选书；批次由 createComicBatch 在后台建一个单书批次。
 */

type ComicProjectSnapshot = Readonly<{
  sourceLength: number;
  styleId: string;
  aspectRatio: string;
  reference: Readonly<ComicReferenceInfo> | null;
}>;

type ComicAssetScope = Readonly<{ batchId: string; bookId: string }>;

type PrivateComicState = {
  stage: ComicStage;
  tab: "create" | "result";
  source: string;
  styleId: string;
  aspectRatio: string;
  referencePath: string;
  projectSnapshot: ComicProjectSnapshot | null;
  styles: ComicStylePreset[];
  thumbs: Record<string, string>;
  /** 缩略图加载失败单独记一位：界面要从骨架切到「重试」，不能停在永远空着。 */
  thumbFailed: Record<string, boolean>;
  presets: ComicPresetOption[];
  setupLoading: boolean;
  setupError: string;
  submitting: boolean;
  runSubmitted: boolean;
  error: string;
  batchId: string;
  bookId: string;
  status: BatchFactoryAutomationStatus | null;
  progressError: string;
  pages: ComicPageItem[];
  paths: Record<string, string>;
  pageIndex: number;
  resultLoading: boolean;
  resultError: string;
  busy: string;
  referenceOpen: boolean;
  assets: ComicAssetItem[];
  assetImages: Record<string, ComicAssetImageItem[]>;
  assetThumbs: Record<string, string>;
  assetsLoading: boolean;
  assetError: string;
  assetBusy: "" | "extract" | "images" | "retry";
  assetBusyAt: number;
  pollToken: number;
  /** 本页是从本地指针恢复出来的：界面要标出来，别假装是实时进度。 */
  restored: boolean;
};

const INITIAL_STATE: PrivateComicState = {
  stage: "source",
  tab: "create",
  source: "",
  styleId: "",
  aspectRatio: DEFAULT_COMIC_ASPECT_RATIO,
  referencePath: "",
  projectSnapshot: null,
  styles: [],
  thumbs: {},
  thumbFailed: {},
  presets: [],
  setupLoading: true,
  setupError: "",
  submitting: false,
  runSubmitted: false,
  error: "",
  batchId: "",
  bookId: "",
  status: null,
  progressError: "",
  pages: [],
  paths: {},
  pageIndex: 0,
  resultLoading: false,
  resultError: "",
  busy: "",
  referenceOpen: false,
  assets: [],
  assetImages: {},
  assetThumbs: {},
  assetsLoading: false,
  assetError: "",
  assetBusy: "",
  assetBusyAt: 0,
  pollToken: 0,
  restored: false,
};

export default function ComicPrivatePage() {
  const [state, setState] = useState<PrivateComicState>(INITIAL_STATE);
  const stopPolling = useRef<(() => void) | undefined>(undefined);
  const resultLoaded = useRef(false);
  const imageVersion = useRef(0);
  const assetsLoadedFor = useRef("");
  const assetsInFlight = useRef("");
  const runSubmitting = useRef(false);
  const pageActive = useRef(true);
  const assetScope = useRef<ComicAssetScope | null>(null);
  const submittedScope = useRef<ComicAssetScope | null>(null);
  // 资产阶段自己的 5s 轮询，退后台时和进度轮询一起停表。
  const assetPollStop = useRef<(() => void) | undefined>(undefined);
  // onShow / onHide 是页面级回调，拿不到最新闭包；用 ref 读当前 state。
  const latest = useRef(state);
  latest.current = state;

  const patch = (next: Partial<PrivateComicState>) => setState((prev) => ({ ...prev, ...next }));

  /**
   * 本地活动指针：只记服务端已经存在的批次与提交状态，用于退出重进 / 被回收后恢复。
   * 进行中的提取、补齐、重试没有可持久化句柄，恢复后只能按服务端结果手动重试。
   */
  const rememberActiveRun = (batchId: string, bookId: string, submitted: boolean, project: ComicProjectSnapshot | null) => {
    if (!batchId || !bookId) return;
    const current = latest.current;
    const snapshot = project || current.projectSnapshot;
    const existing = readComicActiveRun();
    const sameRun = existing?.batchId === batchId && existing?.bookId === bookId;
    saveComicActiveRun({
      batchId,
      bookId,
      source: sameRun && existing ? existing.source : current.source,
      sourceLength: snapshot?.sourceLength ?? comicSourceLength(current.source),
      styleId: snapshot?.styleId || current.styleId,
      aspectRatio: snapshot?.aspectRatio || current.aspectRatio,
      reference: snapshot?.reference
        ? {
          assetId: snapshot.reference.assetId,
          url: snapshot.reference.url,
          ...(snapshot.reference.previewUrl ? { previewUrl: snapshot.reference.previewUrl } : {}),
        }
        : null,
      submitted,
      startedAt: sameRun && existing ? existing.startedAt : Date.now(),
    });
  };

  /**
   * 资产只读加载：列表 + 每个资产的首张定妆图（优先 isPrimary）。
   * 任何一张图失败都静默落空，界面上显示「未生成」，不弹错误层。
   */
  const loadAssets = async (batchId: string, bookId: string, force = false, scope = assetScope.current) => {
    if (!scope || scope !== assetScope.current || scope.batchId !== batchId || scope.bookId !== bookId) return;
    const key = `${batchId}:${bookId}`;
    if (!force && assetsLoadedFor.current === key) return;
    // 并发窗口：切页 effect 与轮询回调可能同时触发，同一批次在途时直接复用，不发第二次。
    if (assetsInFlight.current === key) return;
    assetsInFlight.current = key;
    assetsLoadedFor.current = key;
    patch({ assetsLoading: true, assetError: "" });
    try {
      const assets = await loadComicAssets(batchId, bookId);
      if (scope !== assetScope.current) return;
      const entries = await Promise.all(assets.map(async (asset) => {
        let images: ComicAssetImageItem[] = [];
        try {
          images = await loadComicAssetImages(batchId, bookId, asset.id);
        } catch {
          images = [];
        }
        const primary = images.find((image) => image.isPrimary) || images[0];
        // 单张媒体失败只影响这一项，不能阻断其余资产列表。
        let path = "";
        try { path = primary ? await loadComicAssetImagePath(primary) : ""; } catch { /* 显示本项占位。 */ }
        return [asset.id, { images, path }] as const;
      }));
      if (scope !== assetScope.current) return;
      patch({
        assets,
        assetsLoading: false,
        assetImages: Object.fromEntries(entries.map(([id, value]) => [id, value.images])),
        assetThumbs: Object.fromEntries(entries.filter(([, value]) => value.path).map(([id, value]) => [id, value.path])),
      });
    } catch (cause) {
      if (scope !== assetScope.current) return;
      assetsLoadedFor.current = "";
      patch({ assetsLoading: false, assetError: comicFailureText(cause, "资产暂时读不到，请刷新重试") });
    } finally {
      if (assetsInFlight.current === key) assetsInFlight.current = "";
    }
  };

  const loadPageImages = async (page: ComicPageItem, version = imageVersion.current) => {
    const loaded = await loadComicImagePaths(page);
    if (version !== imageVersion.current) return;
    setState((prev) => ({ ...prev, paths: { ...prev.paths, ...loaded } }));
  };

  const loadPages = async (batchId: string, bookId: string) => {
    if (resultLoaded.current) return;
    resultLoaded.current = true;
    const version = imageVersion.current;
    patch({ resultLoading: true, resultError: "" });
    try {
      const next = await loadComicPages(batchId, bookId);
      if (version !== imageVersion.current) return;
      const hasImageReceipt = next.some((page) => page.images.length > 0 && Boolean(page.primaryImageId));
      patch({ pages: next, resultLoading: false, pageIndex: 0, paths: {}, ...(hasImageReceipt ? { stage: "result" as const } : {}) });
      if (next[0]) await loadPageImages(next[0], version);
    } catch (cause: any) {
      // 终态不再轮询；页图未落库时允许用户只重读结果。
      if (version !== imageVersion.current) return;
      resultLoaded.current = false;
      patch({ resultLoading: false, resultError: cause?.message || "暂时打不开这些图，请稍后再试" });
    }
  };

  /** 画风缩略图：成功进 thumbs，失败单独记一位，界面从骨架切到「重试」。 */
  const loadStyleThumb = async (style: ComicStylePreset) => {
    if (!style.previewUrl) return;
    const path = await resolveComicStyleThumb(style.previewUrl);
    if (!pageActive.current) return;
    setState((prev) => path
      ? { ...prev, thumbs: { ...prev.thumbs, [style.id]: path } }
      : { ...prev, thumbFailed: { ...prev.thumbFailed, [style.id]: true } });
  };

  /** 失败态的重试入口：失败不会写缓存，直接重跑即可重新请求。 */
  const retryStyleThumb = (styleId: string) => {
    const style = state.styles.find((item) => item.id === styleId);
    if (!style?.previewUrl) return;
    setState((prev) => ({ ...prev, thumbFailed: { ...prev.thumbFailed, [styleId]: false } }));
    void loadStyleThumb(style);
  };

  useEffect(() => {
    let active = true;
    void loadComicPrivateSetup().then(({ styles, presets }) => {
      if (!active) return;
      patch({ styles, presets, styleId: state.styleId || styles[0]?.id || "", setupLoading: false, setupError: "" });
      styles.forEach((style) => { void loadStyleThumb(style); });
    }).catch((cause: any) => {
      if (active) patch({ setupLoading: false, setupError: comicFailureText(cause) });
    });
    return () => { active = false; };
  }, []);

  // Image requests can outlive the component or a completed run.
  useEffect(() => () => { pageActive.current = false; imageVersion.current += 1; assetScope.current = null; }, []);

  useEffect(() => {
    if (state.tab !== "result" || !state.batchId || !state.bookId || !state.runSubmitted || state.stage !== "render" || comicProgress(state.status).finished) return;
    const batchId = state.batchId;
    const bookId = state.bookId;
    let active = true;
    let stop: (() => void) | undefined;
    const cancel = () => { active = false; stop?.(); };
    stop = startComicPolling(
      () => readComicAutomation(batchId),
      (value) => {
        // 服务端偶发返回空对象：跳过这一帧，保留上一次进度，绝不当成已结束。
        if (!active || !value) return;
        patch({ status: value, progressError: "", restored: false });
        if (!comicProgress(value).finished) return;
        cancel();
        void loadPages(batchId, bookId);
        void loadAssets(batchId, bookId, true);
      },
      (cause) => {
        if (!active) return;
        cancel();
        // 401 / 404 说明本地指针指向的批次已经不可用，必须清掉，避免每次进来都撞同一堵墙。
        if (shouldForgetComicRun(cause)) clearComicActiveRun();
        patch({ progressError: cause instanceof Error ? cause.message : "暂时读不到进度，请重新进入分页出图阶段刷新" });
      },
    );
    // The callback can finish synchronously (for example cached status).
    if (!active) stop();
    stopPolling.current = cancel;
    void loadAssets(batchId, bookId);
    return () => { cancel(); stopPolling.current = undefined; };
  }, [state.tab, state.stage, state.batchId, state.bookId, state.runSubmitted, state.pollToken, comicProgress(state.status).finished]);

  useEffect(() => {
    if (!state.batchId || !state.bookId || (state.stage !== "assets" && state.stage !== "storyboard")) return;
    void loadAssets(state.batchId, state.bookId);
  }, [state.batchId, state.bookId, state.stage]);

  // 资产阶段只读服务端：资产由服务端在 comic_assets 阶段提取，
  // 这里只在阶段变化时重读列表，不再由客户端触发 stages/assets。
  useEffect(() => {
    if (state.stage !== "assets" || !state.batchId || !state.bookId) return;
    if (state.assets.length || state.assetError) return;
    const batchId = state.batchId;
    const bookId = state.bookId;
    const scope = { batchId, bookId };
    let active = true;
    let lastStage = "";
    const stop = startComicPolling(
      () => readComicAutomation(batchId),
      (value) => {
        if (!active || !value) return;
        patch({ status: value });
        const stage = String(value.books?.[0]?.stage || "");
        if (!stage || stage === lastStage) return;
        lastStage = stage;
        void loadAssets(batchId, bookId, true, scope);
      },
      (cause) => {
        // 资产阶段读不到进度不打扰，保留「刷新资产」入口；但 401/404 要清掉本地指针。
        if (shouldForgetComicRun(cause)) clearComicActiveRun();
      },
    );
    assetPollStop.current = stop;
    return () => { active = false; stop(); assetPollStop.current = undefined; };
  }, [state.stage, state.batchId, state.bookId, state.assets.length, state.assetError, state.pollToken]);

  /**
   * 回前台校准：
   * - 有批次：让轮询立即重启（pollToken +1），由服务端状态覆盖本地；
   * - 没批次：读 24h 内的本地活动指针，恢复批次和提交状态。
   * 只恢复「批次 + 提交状态」，退出瞬间正在跑的动作没有句柄，恢复后按服务端结果手动重试。
   */
  useDidShow(() => {
    const current = latest.current;
    if (current.batchId && current.bookId) {
      if (current.runSubmitted) setState((prev) => ({ ...prev, pollToken: prev.pollToken + 1 }));
      else {
        // 资产阶段：重启被 onHide 停掉的 5s 轮询，并强制重读一次服务端资产。
        setState((prev) => ({ ...prev, pollToken: prev.pollToken + 1 }));
        void loadAssets(current.batchId, current.bookId, true);
      }
      return;
    }
    const run = readComicActiveRun();
    if (!run) return;
    const scope = { batchId: run.batchId, bookId: run.bookId };
    assetScope.current = scope;
    assetsLoadedFor.current = "";
    assetsInFlight.current = "";
    resultLoaded.current = false;
    imageVersion.current += 1;
    patch({
      stage: run.submitted ? "render" : "assets",
      tab: "result",
      batchId: run.batchId,
      bookId: run.bookId,
      source: run.source,
      styleId: run.styleId || current.styleId,
      aspectRatio: run.aspectRatio || DEFAULT_COMIC_ASPECT_RATIO,
      projectSnapshot: Object.freeze({
        sourceLength: run.sourceLength || comicSourceLength(run.source),
        styleId: run.styleId || current.styleId,
        aspectRatio: run.aspectRatio || DEFAULT_COMIC_ASPECT_RATIO,
        reference: run.reference ? Object.freeze({ ...run.reference }) : null,
      }),
      runSubmitted: run.submitted,
      status: null,
      pages: [], paths: {}, pageIndex: 0,
      progressError: "", resultError: "", error: "",
      assets: [], assetImages: {}, assetThumbs: {}, assetsLoading: !run.submitted, assetError: "",
      assetBusy: "", assetBusyAt: 0,
      restored: true,
    });
  });

  // 退到后台两路轮询都停表：不空转；本地指针与服务端任务都保留，回前台再校准。
  useDidHide(() => {
    stopPolling.current?.();
    assetPollStop.current?.();
  });

  const progress = comicProgress(state.status);
  const finished = Boolean(state.status) && progress.finished;
  // 提交后只读服务端状态：automation.state 与本书 stage 都是后端字段，这里不推测阶段。
  const serverBook = state.status?.books?.find((book) => String(book.bookId || "") === String(state.bookId || ""));
  // 四步条只读：步骤全部由服务端 stage 映射，未在清单里的 stage 原样显示。
  const runView = runStageView(state.status, state.bookId);
  const running = state.runSubmitted && !finished;
  const length = comicSourceLength(state.source);
  const remaining = Math.max(0, MIN_COMIC_SOURCE_CHARS - length);
  const presetHint = comicPresetAvailability(state.presets);
  const usablePreset = state.presets.find((option) => option.usable);
  // 默认用第一条「模型齐 + 漫画提示词已选」的预设；引用交给后端 hydrate，不在前端兜底。
  const presetConfigHint = usablePreset ? comicPresetComicReason(usablePreset.preset) : "";
  const blockReason = state.submitting ? "正在提交…"
    : running ? "上一次还在生成，先去「结果」看进度"
      : state.setupLoading ? "正在读取画风和预设…"
        : state.setupError ? state.setupError
          : !state.styles.length ? "还没有可用画风，稍后再试"
            : presetHint || presetConfigHint
              || (!length ? "先粘贴小说正文"
                : remaining > 0 ? `还需 ${remaining} 字`
                  : "");
  const canSubmit = !blockReason;

  const chooseReference = async () => {
    try {
      const result = await Taro.chooseMedia({ count: 1, mediaType: ["image"], sourceType: ["album", "camera"] });
      const file = result.tempFiles?.[0] as { tempFilePath?: string; path?: string } | undefined;
      const path = String(file?.tempFilePath || file?.path || "");
      if (path) patch({ referencePath: path });
    } catch { /* 取消选择不需要提示。 */ }
  };

  const prepareProject = async () => {
    if (state.submitting) return;
    try {
      // 预检也放在 try 内：任何一步抛错都必须落到下面的 catch，不能静默吞掉。
      if (!canSubmit) { patch({ error: blockReason || validateComicSource(state.source) }); return; }
      const usable = state.presets.find((option) => option.usable);
      if (!usable) { patch({ error: COMIC_PRESET_EMPTY_HINT }); return; }
      const presetReason = comicPresetComicReason(usable.preset);
      if (presetReason) { patch({ error: presetReason }); return; }
      // 原生遮罩先于 React 渲染出现，保证点击瞬间就有可见反馈。
      Taro.showLoading({ title: "正在创建项目…", mask: true });
      patch({ submitting: true, error: "", assetsLoading: false, assetBusy: "" });
      const created = await createComicBatch({
        batchTitle: "",
        bookTitle: "",
        sourceText: state.source,
        // 网页版同款：建批就把画面配置带过去，服务端事务内 hydrate style.body。
        // 参考图此时还没上传，跟网页版一样留到 automation/start 的 settingsPatch 里补。
        initialBatchSettings: buildComicInitialBatchSettings(
          { aspectRatio: state.aspectRatio, stylePresetId: state.styleId, reference: null },
          usable.preset,
        ),
      });
      const reference = state.referencePath ? await uploadComicReference(created.batchId, state.referencePath) : null;
      // 当前项目只使用创建时的设置；返回设置修改的草稿属于下一次创作。
      const projectSnapshot: ComicProjectSnapshot = Object.freeze({
        sourceLength: comicSourceLength(state.source),
        styleId: state.styleId,
        aspectRatio: state.aspectRatio,
        reference: reference ? Object.freeze({ ...reference }) : null,
      });
      // 把完整预设配置一起带上：settingsPatch 里的 comic 必须整块，否则后端一层合并会把
      // comic.assets.body / comic.body / constraints 抹掉，资产阶段直接失败。
      resultLoaded.current = false;
      imageVersion.current += 1;
      const scope = { batchId: created.batchId, bookId: created.bookId };
      assetScope.current = scope;
      // 不再手工调 stages/assets：建批时已经带上 initialBatchSettings，服务端在 comic_assets 阶段自己提取。
      assetsLoadedFor.current = "";
      patch({
        submitting: false, batchId: created.batchId, bookId: created.bookId, stage: "assets", tab: "result",
        projectSnapshot, runSubmitted: false,
        status: null, pages: [], paths: {}, pageIndex: 0, progressError: "", resultError: "",
        assets: [], assetImages: {}, assetThumbs: {}, assetsLoading: true, assetError: "",
        assetBusy: "", assetBusyAt: 0,
      });
      Taro.hideLoading();
      // 建完批立刻落本地指针：退出或被回收后能回到这个批次，不必重新建批。
      rememberActiveRun(created.batchId, created.bookId, false, projectSnapshot);
      // 先读一次服务端已落库的资产；服务端还在提取时这里就是空列表，由资产阶段的轮询补齐。
      await loadAssets(created.batchId, created.bookId, true, scope);
    } catch (cause) {
      Taro.hideLoading();
      const message = comicNewErrorText(cause);
      patch({ submitting: false, error: message });
      // 页内错误行可能被滚出屏幕/被吸底条遮住，失败必须再给一次 toast，保证可见。
      void Taro.showToast({ title: message, icon: "none", duration: 2600 });
    }
  };

  const confirmConfigAndStart = async () => {
    const scope = assetScope.current;
    if (!scope || scope.batchId !== state.batchId || scope.bookId !== state.bookId || runSubmitting.current) return;
    // The ref also guards callbacks retained from the pre-submit render.
    if (state.runSubmitted || submittedScope.current === scope) {
      setState((prev) => ({ ...prev, stage: prev.pages.length ? "result" : "render", tab: "result" }));
      return;
    }
    const project = state.projectSnapshot;
    // 资产不再作为提交门禁：服务端在跑提取，客户端等到齐再放行只会让人干等。
    if (runSubmitting.current || state.stage !== "storyboard" || !project || !state.batchId || !state.bookId || state.submitting) return;
    const usable = state.presets.find((option) => option.usable);
    if (!usable) { patch({ error: COMIC_PRESET_EMPTY_HINT }); return; }
    const presetReason = comicPresetComicReason(usable.preset);
    if (presetReason) { patch({ error: presetReason }); return; }
    runSubmitting.current = true;
    try {
      // 提交前只读一次 automation：活动任务直接拦下（否则只会拿 409），
      // 终态必须二次确认（会新建 run 并重新消耗出图额度）。读失败按「无法判断」走原流程。
      let automationState = "";
      let stage = "";
      try {
        const value = await readComicAutomation(state.batchId);
        automationState = String(value?.state || "");
        stage = String(value?.books?.[0]?.stage || "");
      } catch { automationState = ""; stage = ""; }
      const gate = comicRunSubmitGate(automationState);
      const stateLabel = runStateLabel(automationState);
      if (gate === "active") {
        await Taro.showModal({
          title: "已有任务在跑",
          content: `服务端当前状态是「${stateLabel}」，现在提交会被直接拒绝。请等它结束，或在进度页暂停、停止后再提交。`,
          showCancel: false,
          confirmText: "知道了",
        });
        patch({ submitting: false, error: `已有任务在跑（${stateLabel}），等它结束再提交` });
        return;
      }
      if (gate === "finished") {
        const result = await Taro.showModal({
          title: "重新生成会再扣额度",
          content: `当前任务${stateLabel}。继续会新建一个任务，从头重新生成全部漫画页，出图额度会重新扣一次。`,
          confirmText: "重新生成",
          cancelText: "先不提交",
        });
        if (!result?.confirm) {
          patch({ submitting: false, error: `当前任务${stateLabel}，已取消重新生成` });
          return;
        }
      } else if (!automationState && (stage === "comic_assets" || stage === "comic_asset_images")) {
        // 读不到 state 但 stage 显示还在提取资产：保留「只提示不拦」的兜底。
        const result = await Taro.showModal({
          title: "服务端还在提取资产",
          content: "建议等资产提取完成再提交，避免生成中途冲突。你也可以现在继续提交。",
          confirmText: "继续提交",
          cancelText: "再等等",
        });
        if (!result?.confirm) {
          patch({ submitting: false, error: "服务端还在提取资产，建议等它完成再提交" });
          return;
        }
      }
      patch({ submitting: true, error: "" });
      await submitComicRun({
        batchId: state.batchId,
        bookIds: [state.bookId],
        presetId: usable.preset.id,
        runMode: DEFAULT_COMIC_RUN_MODE,
        aspectRatio: project.aspectRatio,
        stylePresetId: project.styleId,
        reference: project.reference,
      }, usable.preset);
      submittedScope.current = scope;
      resultLoaded.current = false;
      imageVersion.current += 1;
      // 提交成功才把指针标成 submitted：恢复时直接回到进度页并由服务端校准。
      rememberActiveRun(state.batchId, state.bookId, true, project);
      patch({ submitting: false, runSubmitted: true, stage: "render", tab: "result", status: null, pages: [], paths: {}, pageIndex: 0, progressError: "", resultError: "" });
    } catch (cause) {
      const message = comicNewErrorText(cause);
      patch({ submitting: false, error: message });
      void Taro.showToast({ title: message, icon: "none", duration: 2600 });
    } finally {
      runSubmitting.current = false;
    }
  };

  const startAnother = () => {
    stopPolling.current?.();
    clearComicActiveRun();
    assetScope.current = null;
    submittedScope.current = null;
    assetsLoadedFor.current = "";
    resultLoaded.current = false;
    imageVersion.current += 1;
    patch({
      stage: "source", tab: "create", batchId: "", bookId: "", status: null, pages: [], paths: {}, pageIndex: 0,
      referencePath: "", projectSnapshot: null, runSubmitted: false, error: "", progressError: "", resultError: "",
      assets: [], assetImages: {}, assetThumbs: {}, assetsLoading: false, assetBusy: "", assetError: "",
    });
  };

  const selectPage = (index: number) => {
    const page = state.pages[index];
    if (!page) return;
    patch({ pageIndex: index });
    void loadPageImages(page);
  };

  const current = state.pages[state.pageIndex];
  const primaryImage = current?.images.find((image) => image.id === current.primaryImageId);
  const stagePath = primaryImage ? state.paths[primaryImage.id] : "";
  const downloadable = state.pages.filter((page) => page.primaryImageId).length;
  const failedBook = state.status?.books?.find((book) => String(book.status || "").toLowerCase() === "failed");

  // ---- 资产只读展示（结果 tab 顶部）----
  const presetConfig = (usablePreset?.preset.config || {}) as Record<string, unknown>;
  const presetTextModelId = typeof presetConfig.textModelId === "string" ? presetConfig.textModelId : "";
  const presetImageModelId = typeof presetConfig.imageModelId === "string" ? presetConfig.imageModelId : "";
  const reloadAssets = async (scope = assetScope.current) => {
    if (!scope || scope !== assetScope.current) return;
    assetsLoadedFor.current = "";
    await loadAssets(scope.batchId, scope.bookId, true, scope);
  };

  /** 重新提取资产：整本强制重提，文案口径与网页版「重新提取资产」一致。 */
  const runExtractAssets = async () => {
    const scope = assetScope.current;
    if (!scope || scope.batchId !== state.batchId || scope.bookId !== state.bookId) return;
    if (!state.batchId || !state.bookId || state.assetBusy) return;
    patch({ assetBusy: "extract", assetBusyAt: Date.now(), assetError: "" });
    try {
      await extractComicAssets({
        batchId: state.batchId, bookId: state.bookId, mode: "force",
        textModelId: presetTextModelId || undefined,
      });
      await reloadAssets(scope);
    } catch (cause) {
      if (scope !== assetScope.current) return;
      patch({ assetError: comicFailureText(cause, "资产提取失败，稍后再试") });
    } finally {
      if (scope === assetScope.current) patch({ assetBusy: "" });
    }
  };

  /** 补齐资产图：只挑还没有图的资产，异步任务复用 3s→10s 的退避轮询。 */
  const runMissingAssetImages = async () => {
    const scope = assetScope.current;
    if (!scope || scope.batchId !== state.batchId || scope.bookId !== state.bookId) return;
    const project = state.projectSnapshot;
    const ids = state.assets.filter((asset) => !(state.assetImages[asset.id] || []).length).map((asset) => asset.id);
    if (!project || !ids.length || !state.batchId || state.assetBusy) return;
    patch({ assetBusy: "images", assetBusyAt: Date.now(), assetError: "" });
    let pendingTask = false;
    try {
      const result = await generateComicAssetImages({
        batchId: state.batchId, bookId: state.bookId, assetIds: ids,
        modelId: presetImageModelId || undefined, imageAspectRatio: project.aspectRatio,
      });
      if (scope !== assetScope.current) return;
      if (!result.images.length && result.taskId) {
        pendingTask = true;
        let stopImages: (() => void) | undefined;
        stopImages = startComicPolling(
          () => readComicAssetImageTask(state.batchId, state.bookId, result.taskId),
          (value) => {
            if (scope !== assetScope.current) { stopImages?.(); return; }
            const status = String((value as { status?: unknown } | null)?.status || "");
            if (!status || status === "processing" || status === "pending") return;
            stopImages?.();
            pendingTask = false;
            patch({ assetBusy: "" });
            if (status === "failed") {
              const message = String((value as { error?: unknown } | null)?.error || "");
              patch({ assetError: message || "资产图生成失败，可再补齐一次" });
            }
            void reloadAssets(scope);
          },
          (cause) => { if (scope === assetScope.current) patch({ assetError: comicFailureText(cause, "资产图生成失败，可再补齐一次") }); },
        );
      }
      await reloadAssets(scope);
    } catch (cause) {
      if (scope !== assetScope.current) return;
      patch({ assetError: comicFailureText(cause, "资产图生成失败，可再补齐一次") });
    } finally {
      if (!pendingTask && scope === assetScope.current) patch({ assetBusy: "" });
    }
  };

  /** 重试这一步：网页版「重试漫画资产」用的就是 stages/retry（只重跑最后失败阶段）。 */
  const runRetryStage = async () => {
    const scope = assetScope.current;
    if (!scope || scope.batchId !== state.batchId || scope.bookId !== state.bookId) return;
    if (!state.batchId || !state.bookId || state.assetBusy) return;
    patch({ assetBusy: "retry", assetBusyAt: Date.now(), assetError: "", pollToken: state.pollToken + 1, status: null });
    try {
      await retryComicBookStage({
        batchId: state.batchId, bookId: state.bookId, textModelId: presetTextModelId || undefined,
      });
      await reloadAssets(scope);
    } catch (cause) {
      if (scope !== assetScope.current) return;
      patch({ assetError: comicFailureText(cause, "重试失败，稍后再试") });
    } finally {
      if (scope === assetScope.current) patch({ assetBusy: "" });
    }
  };

  const preview = async (imageId: string) => {
    if (!current) return;
    const version = imageVersion.current;
    patch({ resultError: "" });
    try {
      const loaded = { ...state.paths, ...(await loadComicImagePaths(current)) };
      if (version !== imageVersion.current) return;
      setState((prev) => ({ ...prev, paths: { ...prev.paths, ...loaded } }));
      const ids = current.images.map((image) => image.id).filter((id) => loaded[id]);
      if (!ids.length) throw new Error("这张图还没准备好，稍等一下再点");
      const request = buildPreviewRequest(ids.map((id) => loaded[id]), Math.max(0, ids.indexOf(imageId)));
      await Taro.previewImage({ urls: request.urls, current: request.current });
    } catch (cause: any) {
      if (version !== imageVersion.current) return;
      patch({ resultError: cause?.message || "暂时打不开大图，请稍后再试" });
    }
  };

  const savePrimary = async () => {
    if (!current) return;
    const image = current.images.find((item) => item.id === current.primaryImageId);
    if (!image) { patch({ resultError: "这一页还没有可以保存的图" }); return; }
    patch({ busy: "save", resultError: "" });
    try {
      const loaded = state.paths[image.id] ? state.paths : await loadComicImagePaths(current);
      const filePath = loaded[image.id];
      if (!filePath) throw new Error("这张图还没准备好，稍等一下再试");
      await saveComicImageToAlbum(filePath);
      await Taro.showToast({ title: "已保存到相册", icon: "success" });
    } catch (cause: any) {
      const message = String(cause?.message || "保存失败");
      if (/权限/.test(message)) {
        const choice = await Taro.showModal({ title: "需要相册权限", content: message, confirmText: "去设置", cancelText: "取消" });
        if (choice.confirm) void Taro.openSetting({});
      } else {
        patch({ resultError: message });
      }
    } finally {
      patch({ busy: "" });
    }
  };

  const downloadZip = async () => {
    const videoIds = state.pages.filter((page) => page.primaryImageId).map((page) => page.videoId);
    if (!videoIds.length) { patch({ resultError: "还没有可以打包的图，等生成完成再来" }); return; }
    patch({ busy: "zip", resultError: "" });
    try {
      const collection = await createComicCollection(state.batchId, state.bookId, videoIds);
      const file = await downloadComicCollectionZip(state.batchId, state.bookId, collection.id);
      const choice = await Taro.showModal({
        title: "已打包下载",
        content: `共 ${collection.items.length} 页，文件名 comic-collection.zip`,
        confirmText: "知道了",
        cancelText: "复制链接",
      });
      if (choice.cancel) await Taro.setClipboardData({ data: file.url });
    } catch (cause: any) {
      patch({ resultError: cause?.message || "打包下载失败，请稍后再试" });
    } finally {
      patch({ busy: "" });
    }
  };

  const canSelectStage = (stage: ComicStage) => {
    const mapped = stage === "source" ? "settings" : stage === "storyboard" ? "config" : stage;
    // 资产是否齐、是否正在提取都不再拦导航：只挡提交中的重复点击。
    return !runSubmitting.current && !state.submitting && canEnterPrivateComicStage(mapped, {
      sourceLength: state.projectSnapshot?.sourceLength || length,
      minSourceLength: MIN_COMIC_SOURCE_CHARS,
      styleId: state.projectSnapshot?.styleId || state.styleId,
      batchId: state.bookId ? state.batchId : "",
      assetsCount: state.assets.length, pagesCount: state.pages.filter((page) => page.images.length > 0 && Boolean(page.primaryImageId)).length, submitted: state.runSubmitted,
    });
  };
  const selectStage = (stage: ComicStage) => {
    if (!canSelectStage(stage)) return;
    patch({ stage, tab: stage === "source" ? "create" : "result" });
  };

  const stageTitle = state.stage === "source" ? "粘贴正文，做一本漫画"
    : state.stage === "assets" ? "先定下角色与场景"
      : state.stage === "storyboard" ? "生成配置确认"
        : state.stage === "render" ? (finished ? "本次生成已结束" : "正在生成你的漫画")
          : "你的漫画完成了";
  const stageSubtitle = state.stage === "source" ? "先建立故事和人物的视觉基底，再进入分镜与出图。"
    : state.stage === "assets" ? "资产会自动提取并展示，需要时再补齐定妆图；也可以直接继续。"
      : state.stage === "storyboard" ? "核对正文、画风、画幅与资产，确认后开始自动分页出图。"
        : state.stage === "render" ? "可以退出这一页；回来后会从服务端恢复真实进度。"
          : "逐页查看、换候选图、保存单页或整本下载。";

  return (
    <ThemedPage className="mp-page comic-page comic-new">
      <BrandHeader eyebrow="漫画" title="私人定制" showBack />

      <ComicStageHeader stage={state.stage} title={stageTitle} subtitle={stageSubtitle} hasResult={state.pages.length > 0} canSelectStage={canSelectStage} onSelectStage={selectStage} />
      <Text className="comic-stage-legacy-title">{state.stage === "source" ? "粘贴正文，做一本漫画" : ""}</Text>

      <View className="comic-tabs">
        <View className={`comic-tab ${state.tab === "create" ? "is-active" : ""}`} onClick={() => { if (!runSubmitting.current) patch({ tab: "create", stage: "source" }); }}>
          <Text>创作</Text>
        </View>
        <View className={`comic-tab ${state.tab === "result" ? "is-active" : ""}`} onClick={() => { if (!runSubmitting.current) patch({ tab: "result", stage: state.pages.length ? "result" : state.stage === "source" && state.assets.length ? "assets" : state.stage }); }}>
          <Text>结果</Text>
          {state.pages.length ? <Text className="comic-result-dot" /> : null}
        </View>
      </View>

      {state.tab === "result" && state.batchId ? <View className="comic-stage-context">
        <View className="comic-stage-context-copy">
          <Text className="comic-stage-context-kicker">当前阶段</Text>
          <Text className="comic-stage-context-title">{stageTitle}</Text>
          <Text className="comic-stage-context-detail">{state.stage === "assets"
            ? "资产会自动出现在这里，需要时再补齐定妆图；也可以直接继续。"
            : state.stage === "storyboard"
              ? "核对生成配置后再提交，当前不支持逐页分镜编辑。"
              : state.stage === "render"
                ? "生成会在后台继续；失败时只重试具体阶段。"
                : "当前页面可以换候选图、保存本页或整本下载。"}</Text>
        </View>
        <Text className="comic-stage-context-action" onClick={() => {
          if (runSubmitting.current) return;
          if (state.stage === "render") patch({ stage: "assets" });
          else if (state.stage === "assets") patch({ stage: "storyboard" });
          else if (state.stage === "storyboard") patch({ stage: "assets" });
          else patch({ stage: "source", tab: "create" });
        }}>
          {state.stage === "render" ? "看资产" : state.stage === "assets" ? "确认配置" : state.stage === "storyboard" ? "看资产" : "回到创作"}
        </Text>
      </View> : null}

      {state.tab === "create" ? <ComicSettingsStage
        source={state.source} styleId={state.styleId} aspectRatio={state.aspectRatio}
        referencePath={state.referencePath} styles={state.styles} thumbs={state.thumbs}
        thumbFailed={state.thumbFailed} onRetryThumb={retryStyleThumb}
        setupLoading={state.setupLoading} error={state.error}
        onSourceChange={(source) => patch({ source })}
        onStyleChange={(styleId) => patch({ styleId })}
        onAspectChange={(aspectRatio) => patch({ aspectRatio })}
        onOpenReference={() => patch({ referenceOpen: true })}
        onContinue={() => { void prepareProject(); }}
        continueDisabled={!canSubmit} continueLoading={state.submitting}
        continueHint={blockReason}
      /> : null}

      {state.tab === "result" ? <>
        {state.stage === "assets" && state.batchId ? <ComicAssetsStage
          assets={state.assets} assetImages={state.assetImages} assetThumbs={state.assetThumbs}
          loading={state.assetsLoading} busy={state.assetBusy} busyAt={state.assetBusyAt} error={state.assetError}
          serverExtracting={!state.assets.length && runView.stage === "comic_assets"}
          imagesSkipped={assetImagesSkipped(state.status)}
          onExtract={() => { void runExtractAssets(); }}
          onGenerateMissingImages={() => { void runMissingAssetImages(); }}
          onRetry={() => { void runRetryStage(); }} onRefresh={() => { void reloadAssets(); }}
          onContinue={() => patch({ stage: "storyboard" })}
          onBack={() => patch({ stage: "source", tab: "create" })}
        /> : null}

        {state.stage === "storyboard" && state.batchId && state.projectSnapshot ? <ComicConfigConfirmStage
          sourceLength={state.projectSnapshot.sourceLength} styleName={state.styles.find((style) => style.id === state.projectSnapshot?.styleId)?.name || ""}
          aspectRatio={state.projectSnapshot.aspectRatio} referenceReady={Boolean(state.projectSnapshot.reference)} assets={state.assets}
          submitting={state.submitting} submitted={state.runSubmitted} error={state.error}
          onBack={() => { if (!runSubmitting.current) patch({ stage: "assets" }); }}
          onConfirm={() => { void confirmConfigAndStart(); }}
        /> : null}

        {state.stage === "render" || state.stage === "result" ? (state.batchId ? <>
          <View className="mp-card">
            <Text className="mp-row-title">{finished
              ? (progress.succeeded ? "漫画做好了" : progress.interrupted ? "生成被中断了" : "这次没有出图")
              : "正在生成漫画"}</Text>
            {/* 四步条只读：节点状态全部由服务端 stage 推导，服务端没见过的 stage 不点亮任何一步。 */}
            {state.runSubmitted ? <View className="comic-stage-rail">
              {COMIC_RUN_STEPS.map((label, index) => {
                const done = runView.step === 4 || runView.step > index;
                const active = runView.step === index;
                return <View key={label} className={`comic-stage-node ${active ? "is-active" : ""} ${done ? "is-done" : ""}`}>
                  <Text className="comic-stage-node-dot">{done ? "✓" : index + 1}</Text>
                  <Text className="comic-stage-node-label">{label}</Text>
                </View>;
              })}
            </View> : null}
            <Text className="mp-row-description">{finished
              ? (progress.succeeded
                ? "可以一页页看，也可以打包下载。"
                : progress.interrupted ? "服务重启打断了这次生成，重新执行一次就能继续。" : "这一本没有生成成功，返回资产或配置阶段检查后再试。")
              : runView.stage || runView.state
                ? `当前步骤：${runView.step >= 0 ? `第 ${Math.min(runView.step + 1, 4)}/4 步 · ` : ""}${runView.label}${runView.stage && runView.stage !== runView.label ? `（${runView.stage}）` : ""}`
                : "正式任务已经提交，进度会从服务端恢复。"}</Text>
            {state.restored ? <Text className="comic-step-hint">已恢复上次进度，正在向服务端校准…</Text> : null}
            {!finished ? <Text className="comic-hint">退出后进度会从服务端恢复；退出瞬间正在进行的提取、补齐或重试没有可续接的句柄，失败后需手动重试。</Text> : null}
            {runView.notice ? <Text className="mp-row-description">{runView.notice}</Text> : null}
            {progress.failed ? <Text className="mp-error">{failedBook?.error || "这一本没有生成成功"}。可回到资产或配置阶段处理。</Text> : null}
            {state.progressError ? <Text className="mp-error">{state.progressError}</Text> : null}
          </View>

          {state.resultLoading ? <View className="mp-card"><Text className="mp-row-title">正在打开页图…</Text></View> : null}

          {finished && !state.pages.length && !state.resultLoading ? <View className="mp-card">
            <Text className="mp-row-description">页图暂时没有读到，可以重新读取结果。</Text>
            {state.resultError ? <Text className="mp-error">{state.resultError}</Text> : null}
            <Button className="mp-secondary" onClick={() => { resultLoaded.current = false; void loadPages(state.batchId, state.bookId); }}>重新读取页图</Button>
          </View> : null}

          {state.pages.length ? <>
            {current ? <>
              <View className="comic-stage">
                {stagePath
                  ? <Image src={stagePath} lazyLoad mode="aspectFill" className="comic-stage-image" onClick={() => { if (primaryImage) void preview(primaryImage.id); }} />
                  : <View className="comic-stage-empty" onClick={() => { if (current.images[0]) void preview(current.images[0].id); }}>
                    <Text>这一页还在生成，稍后回来看看</Text>
                  </View>}
                {primaryImage ? <Text className="comic-badge">当前采用</Text> : null}
              </View>

              {current.images.length > 1 ? <ScrollView scrollX className="comic-cand-row">
                {current.images.map((image) => (
                  <View key={image.id} className={`comic-cand ${image.isPrimary ? "is-primary" : ""}`} onClick={() => { void preview(image.id); }}>
                    {state.paths[image.id] ? <Image src={state.paths[image.id]} lazyLoad mode="aspectFill" className="comic-cand-image" /> : <View className="comic-cand-image" />}
                    <Text className="comic-cand-label">{image.isPrimary ? "采用" : "候选"}</Text>
                  </View>
                ))}
              </ScrollView> : null}

              <ScrollView scrollX className="comic-strip">
                {state.pages.map((page, index) => (
                  <View key={page.pageId || page.videoId || String(index)} className={`comic-tile ${index === state.pageIndex ? "is-active" : ""} ${page.images.length ? "" : "is-empty"}`} onClick={() => selectPage(index)}>
                    <Text>{page.ordinal}</Text>
                  </View>
                ))}
              </ScrollView>

              {state.resultError ? <Text className="mp-error">{state.resultError}</Text> : null}

              <View className="mp-card">
                <Text className="mp-row-title">保存和下载</Text>
                <Text className="mp-row-description">保存这张图，或把整本打包成一个压缩包。</Text>
                <Button className="mp-secondary" loading={state.busy === "save"} disabled={state.busy === "save" || !current.primaryImageId} onClick={() => { void savePrimary(); }}>保存这张图到相册</Button>
                <Button className="mp-secondary" loading={state.busy === "zip"} disabled={state.busy === "zip" || !downloadable} onClick={() => { void downloadZip(); }}>打包下载全部（{downloadable} 页）</Button>
              </View>
            </> : null}
          </> : null}
        </> : <View className="mp-card">
          <Text className="mp-row-title">还没有开始</Text>
          <Text className="mp-row-description">回到「创作」粘贴正文、选好画风，先创建项目并提取资产。</Text>
          <Button className="mp-secondary" onClick={() => patch({ tab: "create" })}>去创作</Button>
        </View>) : null}
      </> : null}

      {state.referenceOpen ? <>
        <View className="comic-sheet-mask" onClick={() => patch({ referenceOpen: false })} />
        <View className="comic-sheet">
          <View className="comic-sheet-handle" />
          <View className="comic-sheet-header">
            <Text>参考图</Text>
            <Text className="comic-sheet-close" onClick={() => patch({ referenceOpen: false })}>完成</Text>
          </View>
          <View className="comic-sheet-body">
            <Text className="comic-hint">选一张和想要画风接近的图，出图会照着它的调子走。</Text>
            {state.referencePath ? <Image src={state.referencePath} mode="aspectFill" className="comic-reference" onClick={() => { void chooseReference(); }} /> : null}
            <View className="comic-sheet-actions">
              <Button className="mp-secondary" onClick={() => { void chooseReference(); }}>{state.referencePath ? "换一张" : "选一张图"}</Button>
              {state.referencePath ? <Button className="mp-secondary" onClick={() => patch({ referencePath: "" })}>去掉</Button> : null}
            </View>
          </View>
        </View>
      </> : null}

      <ComicStickySpacer />
      {state.tab === "result" && state.stage !== "assets" && state.stage !== "storyboard" ? <View className="comic-dock">
        <View className="comic-dock-copy">
          <Text className="comic-dock-line">{finished ? "想再画一本，就回创作。" : running ? "正在生成，可以退出这一页。" : "还没有开始。"}</Text>
          <Text className="comic-dock-caption">{finished ? "结果已保存，可以打包下载" : "生成会在后台继续，回来再看进度"}</Text>
        </View>
        <Button className="mp-primary" disabled={running}
          onClick={() => { if (finished) startAnother(); else patch({ tab: "create" }); }}>
          {running ? "生成中，请稍候" : finished ? "再做一本" : "去创作"}
        </Button>
      </View> : null}
    </ThemedPage>
  );
}
