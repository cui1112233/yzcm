import { Button, Input, Picker, Text, Textarea, View } from "@tarojs/components";
import Taro, { useDidHide, useDidShow, useRouter } from "@tarojs/taro";
import { useEffect, useRef, useState } from "react";
import BrandHeader from "../../components/BrandHeader";
import AppIcon from "../../components/AppIcon";
import OperationDock from "../../components/OperationDock";
import { ThemedPage } from "../../theme";
import {
  batchFactoryV11Api,
  importBatchBooks,
  saveBatchConfigVersion,
  assertBatchActionReceipt,
  automationStatusLabel,
  cancelBatchAutomation,
  listBatchFactoryAutomationPresets,
  listBatchFactoryConfigVersions,
  loadBatchFactoryAutomation,
  loadBatchFactoryBookWorkspace,
  loadBatchFactoryWorks,
  loadBatchFactoryCapabilities,
  loadBatchFactoryV11,
  manualBookIdsFromInput,
  pauseBatchAutomation,
  resumeBatchAutomation,
  retryBatchAutomation,
  runBookDirector,
  runMerge,
  runProduction,
  saveBatchFactoryAssetPrompt,
  saveBatchFactoryBookContentWindow,
  saveBatchFactoryVideoPrompt,
  startBatchExecution,
  submitBatchFactoryBookTo121,
  type BatchFactoryAutomationPreset,
  type BatchFactoryConfigVersion,
  type BatchFactoryAutomationStatus,
  type BatchFactoryV11State,
  type BatchFactoryBookWorkspace,
} from "../../services/batchFactoryV11";
import { deriveBookStageSummary, type BatchFactoryBookView } from "../../services/batchFactoryModels";
import BatchProjectView from "./BatchProjectView";
import PersonalWorksView from "./PersonalWorksView";
import BookWorkbenchView from "./BookWorkbenchView";
import BatchExecutionFields, { type BatchExecutionFieldsValue } from "./BatchExecutionFields";
import { backToProject, backToWorks, initialBatchFactoryPageState, openBook, openCreate, openProject, type BatchFactoryPageState } from "./batchFactoryPageState";
import { useAutomationPolling } from "./useAutomationPolling";
import { batchLocalRequest as apiRequest } from "../../services/batchLocalAccess";
import "./mobile.scss";
import { fetchWorkspaceState, type FetchPlatform } from "../../services/fetchWorkspace";
import { validateContentWindow } from "./contentWindow";

function scheduledAtFromSelection(date: string, time: string) {
  if (!date || !time) return "";
  const value = new Date(`${date}T${time}:00+08:00`);
  return Number.isFinite(value.getTime()) ? value.toISOString() : "";
}

export default function BatchPage() {
  const routeBatchId = String(useRouter().params.batchId || "").trim();
  const [state, setState] = useState<BatchFactoryV11State>({ batches: [], batch: null, capabilities: {}, productionStatus: null, mergeStatus: null });
  const [selectedBatchId, setSelectedBatchId] = useState(routeBatchId);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [error, setError] = useState("");
  const [automation, setAutomation] = useState<BatchFactoryAutomationStatus | null>(null);
  const [automationError, setAutomationError] = useState("");
  const [automationLoading, setAutomationLoading] = useState(false);
  const [presets, setPresets] = useState<BatchFactoryAutomationPreset[]>([]);
  const [importOpen, setImportOpen] = useState(false);
  const [configOpen, setConfigOpen] = useState(false);
  const [productionOpen, setProductionOpen] = useState(false);
  const [importExecution, setImportExecution] = useState<"none" | "immediate" | "scheduled">("immediate");
  const [configVersions, setConfigVersions] = useState<BatchFactoryConfigVersion[]>([]);
  const [configVersionId, setConfigVersionId] = useState("");
  const [bookRefresh, setBookRefresh] = useState(0);
  const [configLoading, setConfigLoading] = useState(false);
  const [configError, setConfigError] = useState("");
  const [configRetry, setConfigRetry] = useState(0);
  const [importTitle, setImportTitle] = useState("");
  const [importPlatformId, setImportPlatformId] = useState("");
  const [importPlatformName, setImportPlatformName] = useState("");
  const [importPlatforms, setImportPlatforms] = useState<FetchPlatform[]>([]);
  const [importPlatformLoading, setImportPlatformLoading] = useState(false);
  const [catalogRetry, setCatalogRetry] = useState(0);
  const [importIds, setImportIds] = useState("");
  const [contentRangeLines, setContentRangeLines] = useState("5");
  const [contentCaptureCharacters, setContentCaptureCharacters] = useState("4000");
  const [extraSources, setExtraSources] = useState<Array<{ key: number; platformId: string; ids: string }>>([]);
  const [sourceSequence, setSourceSequence] = useState(0);
  const [presetId, setPresetId] = useState("");
  const [runMode, setRunMode] = useState("full_submit");
  const [concurrency, setConcurrency] = useState(2);
  const [scheduleDate, setScheduleDate] = useState("");
  const [scheduleTime, setScheduleTime] = useState("");
  const [pageState, setPageState] = useState<BatchFactoryPageState>(() => routeBatchId ? openProject(initialBatchFactoryPageState, routeBatchId) : initialBatchFactoryPageState);
  const [bookWorkspace, setBookWorkspace] = useState<BatchFactoryBookWorkspace>({ readState: "empty", batchId: "", bookId: "", book: null, stages: null, sourceText: "", assets: [], assetImages: {}, production: null, mergeStatus: null });
  const readVersion = useRef(0);
  const publishTarget = useRef("");
  publishTarget.current = `${pageState.mode}:${pageState.selectedBatchId}:${pageState.selectedBookId}`;
  useDidHide(() => { readVersion.current += 1; publishTarget.current = ""; });
  useEffect(() => () => { readVersion.current += 1; publishTarget.current = ""; }, []);
  const operationBusy = useRef(false);
  const beginOperation = () => { if (operationBusy.current) return false; operationBusy.current = true; return true; };
  const executionValue = { presetId, runMode, concurrency, scheduleDate, scheduleTime };
  const importContentWindow = validateContentWindow({ contentRangeLines, contentCaptureCharacters });
  const updateExecution = (patch: Partial<BatchExecutionFieldsValue>) => {
    if (patch.presetId !== undefined) setPresetId(patch.presetId);
    if (patch.runMode !== undefined) setRunMode(patch.runMode);
    if (patch.concurrency !== undefined) setConcurrency(patch.concurrency);
    if (patch.scheduleDate !== undefined) setScheduleDate(patch.scheduleDate);
    if (patch.scheduleTime !== undefined) setScheduleTime(patch.scheduleTime);
  };

  useEffect(() => {
    if (pageState.mode !== "project" || !productionOpen) return;
    Taro.nextTick(() => { void Taro.pageScrollTo({ selector: "#batch-production", duration: 240 }).catch(() => {}); });
  }, [productionOpen, pageState.mode]);

  useEffect(() => {
    if (!configOpen && !importOpen) return;
    let active = true;
    setConfigVersions([]);
    setConfigLoading(true);
    setConfigError("");
    void listBatchFactoryConfigVersions(batchFactoryV11Api).then((versions) => {
      if (active) setConfigVersions(versions);
    }).catch((cause) => {
      if (active) setConfigError(cause instanceof Error ? cause.message : "配置版本读取失败");
    }).finally(() => { if (active) setConfigLoading(false); });
    return () => { active = false; };
  }, [configOpen, importOpen, configRetry, selectedBatchId]);

  const load = async (batchId = selectedBatchId) => {
    const version = ++readVersion.current;
    setLoading(true);
    setError("");
    setAutomationError("");
    setAutomation(null);
    setAutomationLoading(true);
    setPresets([]);
    setState((current) => ({ ...current, batch: null, capabilities:{} }));
    // Configuration metadata must not hold the work list behind a slow request.
    void loadBatchFactoryCapabilities(batchFactoryV11Api).catch(()=>({})).then(capabilities=>{
      if(version===readVersion.current)setState(current=>({...current,capabilities}));
    });
    void listBatchFactoryAutomationPresets(batchFactoryV11Api).catch(()=>[]).then(next=>{
      if(version===readVersion.current)setPresets(next);
    });
    try {
      const next = await loadBatchFactoryV11(batchFactoryV11Api, batchId);
      if (version !== readVersion.current) return;
      setState(current=>({ ...next, capabilities:current.capabilities }));
      setLoading(false);
      const nextBatchId = next.batch?.id || next.batches[0]?.id || "";
      setSelectedBatchId(nextBatchId);
      const nextAutomation = nextBatchId ? await loadBatchFactoryAutomation(batchFactoryV11Api, nextBatchId).catch((cause) => {
        if (version === readVersion.current) setAutomationError(cause instanceof Error ? cause.message : "自动化状态读取失败");
        return null;
      }) : null;
      if (version === readVersion.current) setAutomation(nextAutomation);
    } catch (cause) {
      if (version === readVersion.current) setError(cause instanceof Error ? cause.message : "批量工厂数据读取失败");
    } finally {
      if (version === readVersion.current) {setLoading(false);setAutomationLoading(false);}
    }
  };

  const loadWorks = async () => {
    const version = ++readVersion.current;
    setLoading(true);
    setError("");
    try {
      const batches = await loadBatchFactoryWorks(batchFactoryV11Api);
      if (version !== readVersion.current) return;
      setState((current) => ({ ...current, batches, batch: null, productionStatus: null, mergeStatus: null, readState: batches.length ? "ready" : "empty" }));
    } catch (cause) {
      if (version !== readVersion.current) return;
      setError(cause instanceof Error ? cause.message : "个人作品读取失败");
      setState((current) => ({ ...current, batches: [], batch: null, readState: "error", readError: cause instanceof Error ? cause.message : "个人作品读取失败" }));
    } finally {
      if (version === readVersion.current) setLoading(false);
    }
  };

  useDidShow(() => {
    if (pageState.mode === "works") void loadWorks();
    else if (pageState.mode !== "create") void load(selectedBatchId);
  });

  useAutomationPolling(pageState.mode !== "works" && pageState.mode !== "create" && ["scheduled", "running", "paused"].includes(automation?.state || ""), selectedBatchId,
    () => loadBatchFactoryAutomation(batchFactoryV11Api, selectedBatchId), setAutomation,
    cause => setAutomationError(cause instanceof Error ? cause.message : "自动化状态读取失败"));

  useEffect(() => {
    if (!importOpen) return undefined;
    let active = true;
    setImportPlatformLoading(true);
    setError("");
    setImportPlatforms([]);
    void listBatchFactoryAutomationPresets(batchFactoryV11Api).then((next) => { if (active) setPresets(next); }).catch((cause) => {
      if (active) setError(cause instanceof Error ? cause.message : "全局模板读取失败");
    });
    apiRequest<unknown>("/api/batch-rewrite/config").then((payload) => {
      if (!active) return;
      const platforms = fetchWorkspaceState(payload, []).platforms;
      setImportPlatforms(platforms);
      const selectedPlatform = platforms.find((platform) => platform.id === importPlatformId) || platforms[0];
      if (selectedPlatform) {
        setImportPlatformId(selectedPlatform.id);
        setImportPlatformName(selectedPlatform.name);
      }
      if (!platforms.length) setError("公网没有返回可用书城目录，暂时不能导入");
    }).catch((cause) => {
      if (active) setError(cause instanceof Error ? cause.message : "书城目录读取失败");
    }).finally(() => { if (active) setImportPlatformLoading(false); });
    return () => { active = false; };
  }, [importOpen, catalogRetry]);

  useEffect(() => {
    if (pageState.mode !== "book" || !pageState.selectedBatchId || !pageState.selectedBookId) return undefined;
    let active = true;
    setBookWorkspace({ readState: "loading", batchId: pageState.selectedBatchId, bookId: pageState.selectedBookId, book: null, stages: null, sourceText: "", assets: [], assetImages: {}, production: null, mergeStatus: null });
    const fallbackBook = state.batch?.id === pageState.selectedBatchId ? state.batch.books.find((book) => String(book.id) === String(pageState.selectedBookId)) : undefined;
    loadBatchFactoryBookWorkspace(batchFactoryV11Api, pageState.selectedBatchId, pageState.selectedBookId, fallbackBook, next=>{if(active)setBookWorkspace(next);}).then((next) => { if (active) setBookWorkspace(next); });
    return () => { active = false; };
  }, [pageState.mode, pageState.selectedBatchId, pageState.selectedBookId, state.batch?.id, bookRefresh]);

  const runAction = async (key: string, action: () => Promise<unknown>, successMessage: string) => {
    if (!beginOperation()) return;
    setBusy(key);
    setError("");
    try {
      const receipt = await action();
      if (key !== "save-config") assertBatchActionReceipt(key === "book-director" ? "director" : key === "book-production" ? "production" : "merge", receipt, pageState.selectedBatchId, pageState.selectedBookId);
      await load(selectedBatchId);
      setBookRefresh(value => value + 1);
      void Taro.showToast({ title: successMessage, icon: "success" });
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "服务端操作失败");
    } finally {
      operationBusy.current = false;
      setBusy("");
    }
  };

  const savePrompt = async (key: string, action: () => Promise<unknown>, successMessage: string) => {
    if (!beginOperation()) throw new Error("当前有操作进行中，请稍后再试");
    setBusy(key);
    setError("");
    try {
      await action();
      setBookRefresh(value => value + 1);
      await Taro.showToast({ title: successMessage, icon: "success" });
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : "提示词保存失败";
      setError(message);
      throw new Error(message);
    } finally {
      operationBusy.current = false;
      setBusy("");
    }
  };

  const submitBook = async () => {
    if (operationBusy.current) return;
    const bookId = pageState.selectedBookId;
    const capability = state.capabilities["publish.121"];
    const mergeJob = bookWorkspace.mergeStatus?.jobs?.find((job) => job.bookId === bookId && ["succeeded", "completed"].includes(String(job.status || "").toLowerCase()) && String(job.outputUrl || "").trim());
    if (!bookId || capability?.available !== true) {
      setError(capability?.reason || "当前账号没有可用的视频管理系统发布能力");
      return;
    }
    if (!mergeJob?.outputUrl) {
      setError("请先完成当前小说合成，并等待服务端返回成片回执");
      return;
    }
    if (!beginOperation()) return;
    setBusy("book-submit");
    setError("");
    const target = publishTarget.current;
    try {
      const approved = await Taro.showModal({ title: "确认提交到视频管理系统", content: "将提交当前小说已合成的成片。服务端会核对上传回执并同步后台列表状态。" });
      if (!approved.confirm) return;
      if (target !== publishTarget.current) throw new Error("当前作品已切换，已停止提交，请重新核对");
      const submitted = await submitBatchFactoryBookTo121(pageState.selectedBatchId, bookId);
      if (target !== publishTarget.current) throw new Error("当前作品已切换，已停止读取提交结果，请重新核对");
      const receipt = (submitted as any)?.receipt;
      const status = String((submitted as any)?.status || "").trim().toLowerCase();
      if (!receipt || typeof receipt !== "object") throw new Error("服务端未返回结构化上传回执，请刷新状态确认，勿重复提交");
      await load(selectedBatchId);
      setBookRefresh(value => value + 1);
      if (status === "confirmed") {
        void Taro.showToast({ title: "视频管理系统已确认上传", icon: "success" });
      } else if (status === "accepted_pending") {
        void Taro.showToast({ title: "已接收，等待后台列表回读", icon: "none" });
      } else {
        throw new Error("服务端未返回可确认的发布状态，请刷新状态确认，勿重复提交");
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "外部提交失败");
    } finally {
      operationBusy.current = false;
      setBusy("");
    }
  };

  const importBooks = async (execution: "none" | "immediate" | "scheduled") => {
    if (operationBusy.current) return;
    const ids = manualBookIdsFromInput(importIds);
    if (!ids.length) {
      setError("请输入有效小说 ID（每行一个，也支持逗号分隔）");
      return;
    }
    if (!importPlatforms.length) {
      setError("公网书城目录尚未读取成功，不能手动填写平台");
      return;
    }
    if (!importPlatformId.trim()) {
      setError("请从公网书城目录选择书城");
      return;
    }
    if (extraSources.some((source) => !source.platformId || !manualBookIdsFromInput(source.ids).length)) {
      setError("请为每个附加书城选择平台并填写小说 ID，或移除空书城");
      return;
    }
    if (execution !== "none" && !presetId) {
      setError("服务端还没有选择自动化预设，请先在网页端创建预设并刷新");
      return;
    }
    const scheduledAt = execution === "scheduled" ? scheduledAtFromSelection(scheduleDate, scheduleTime) : "";
    if (execution === "scheduled" && (!scheduledAt || new Date(scheduledAt).getTime() <= Date.now())) {
      setError("请选择未来的北京时间执行时间");
      return;
    }
    if (configVersionId && !configVersions.some(version => version.id === configVersionId)) { setError("所选全局配置已不可用，请重新读取模板"); return; }
    if (!importContentWindow.ok) {
      setError(importContentWindow.error);
      return;
    }
    if (!beginOperation()) return;
    setBusy("import");
    setError("");
    try {
      const result = await importBatchBooks(batchFactoryV11Api, {
        title: importTitle,
        groups: [{ platformId: importPlatformId, platformName: importPlatformName, inputText: importIds }, ...extraSources.map(source => ({ platformId: source.platformId, platformName: importPlatforms.find(platform => platform.id === source.platformId)?.name || source.platformId, inputText: source.ids }))],
        execution, presetId, runMode, concurrency, scheduledAt,
        configVersion: configVersions.find(version => version.id === configVersionId),
        ...importContentWindow.value,
      });
      setPageState(openProject(pageState, result.batch.id));
      setImportOpen(false);
      await load(result.batch.id);
      if (result.warnings.length) setError(result.warnings.join("；"));
    } catch (cause) { setError(cause instanceof Error ? cause.message : "导入失败"); }
    finally { operationBusy.current = false; setBusy(""); }
  };

  const startExisting = async (scheduled: boolean) => {
    const batchId = state.batch?.id;
    if (!batchId || operationBusy.current) return;
    const scheduledAt = scheduled ? scheduledAtFromSelection(scheduleDate, scheduleTime) : "";
    if (scheduled && !scheduledAt) { setError("请先选择定时日期和时间"); return; }
    if (!beginOperation()) return;
    setBusy("automation-start"); setError("");
    try {
      const status = await startBatchExecution(batchFactoryV11Api, batchId, { presetId, runMode, concurrency, scheduledAt });
      setAutomation(status);
      await load(batchId);
    } catch (cause) { setError(`${cause instanceof Error ? cause.message : "启动失败"}；请刷新当前作品核对状态，不要重复新建批量。`); }
    finally { operationBusy.current = false; setBusy(""); }
  };

  const automationAction = async (action: "pause" | "resume" | "retry" | "cancel") => {
    if (!batch?.id || !beginOperation()) return;
    setBusy(`automation-${action}`);
    setError("");
    try {
      if (action === "pause") await pauseBatchAutomation(batch.id);
      if (action === "resume") await resumeBatchAutomation(batch.id);
      if (action === "retry") await retryBatchAutomation(batch.id);
      if (action === "cancel") await cancelBatchAutomation(batch.id);
      setAutomation(await loadBatchFactoryAutomation(batchFactoryV11Api, batch.id).catch(() => null));
      await load(batch.id);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : "自动化操作失败");
    } finally {
      operationBusy.current = false;
      setBusy("");
    }
  };

  const batch = state.batch;
  const selectedTitle = batch?.title || batch?.name || batch?.id || "批量工厂";

  const currentBooks: BatchFactoryBookView[] = batch
    ? batch.books.map((book) => deriveBookStageSummary(book, state.productionStatus, state.mergeStatus))
    : [];
  const selectedPlatformIndex = Math.max(0, importPlatforms.findIndex((platform) => platform.id === importPlatformId));

  const pageBack = () => {
    setError("");
    if (configOpen) { setConfigOpen(false); return; }
    if (pageState.mode === "book") {
      setPageState(backToProject(pageState));
      return;
    }
    if (pageState.mode === "project" || pageState.mode === "create") {
      setImportOpen(false);
      setPageState(backToWorks(pageState));
      void loadWorks();
      return;
    }
    void Taro.navigateBack().catch(() => Taro.switchTab({ url: "/pages/functions/index" }));
  };

  const visibleWorkspace: BatchFactoryBookWorkspace = bookWorkspace.bookId === pageState.selectedBookId && bookWorkspace.batchId === pageState.selectedBatchId
    ? bookWorkspace : { ...bookWorkspace, readState: "loading", book: null, production: null, assets: [], assetImages: {} };

  return (
    <ThemedPage variant="studio" className={`mp-page mp-detail-page mp-batch-page${pageState.mode === "book" || pageState.mode === "create" ? " mp-page-with-dock" : ""}`}>
      <BrandHeader eyebrow="批量生产" title={pageState.mode === "works" ? "批量工厂" : pageState.mode === "create" ? "新建批量" : pageState.mode === "book" ? "单书工作区" : "小说列表"} showBack onBack={pageBack} />
      {pageState.mode === "works" ? <PersonalWorksView
        readState={loading ? "loading" : (error ? "error" : (state.batches.length ? "ready" : "empty"))}
        batches={state.batches}
        error={error}
        onCreate={() => { setError(""); setConfigVersionId(""); setContentRangeLines("5"); setContentCaptureCharacters("4000"); setImportExecution("immediate"); setRunMode("full_submit"); setPageState(openCreate(pageState)); setImportOpen(true); }}
        onOpen={(batchId) => { setPageState(openProject(pageState, batchId)); void load(batchId); }}
        onRetry={() => void loadWorks()}
      /> : null}
      {pageState.mode === "project" && batch ? <BatchProjectView
        batch={batch}
        books={currentBooks}
        selectedBookId={pageState.selectedBookId}
        onBack={pageBack}
        onSelectBook={(bookId) => setPageState(openBook(pageState, bookId))}
        onRefresh={() => void load(batch.id)}
        onConfigure={() => setConfigOpen(true)}
        onProduction={() => setProductionOpen(value => !value)}
      /> : null}
      {pageState.mode === "project" && configOpen ? <View className="mp-batch-config-overlay" onClick={() => setConfigOpen(false)}><View className="mp-batch-config-panel" onClick={event => event.stopPropagation()}>
        <View className="mp-batch-section-head"><Text className="mp-section-title">统一配置</Text><Button className="mp-secondary mp-batch-action" onClick={() => setConfigOpen(false)}>收起</Button></View>
        <Text className="mp-row-description">配置版本应用到本作品；自动化预设在新建批量中单独选择。保存后重新读取服务端配置。</Text>
        <Picker mode="selector" range={configVersions.map(version => version.name || version.id)} value={Math.max(0, configVersions.findIndex(version => version.id === configVersionId))} onChange={event => setConfigVersionId(configVersions[Number(event.detail.value)]?.id || "")}><View className="mp-picker-field"><Text>{configVersions.find(version => version.id === configVersionId)?.name || "选择配置版本"}</Text></View></Picker>
        <Button className="mp-primary" disabled={busy !== "" || !configVersionId || !batch} onClick={() => {
          const version = configVersions.find(item => item.id === configVersionId);
          if (!batch || !version) return;
          void runAction("save-config", () => saveBatchConfigVersion(batchFactoryV11Api, batch.id, version, Number((batch as any).settingsState?.revision || 0)), "配置已保存并读回");
        }}>保存并读取配置</Button>
        {configLoading ? <Text className="mp-row-description">正在读取全局配置版本…</Text> : null}
        {configError ? <View><Text className="mp-batch-warning">{configError}</Text><Button className="mp-secondary" onClick={() => setConfigRetry((value) => value + 1)}>重新读取配置</Button></View> : null}
        {!configLoading && !configError ? configVersions.length ? configVersions.map((version) => <View key={version.id} className="mp-batch-template"><Text className="mp-row-title">{version.name || version.id}</Text><Text className="mp-row-description">版本：{version.id}{version.createdAt ? ` · ${version.createdAt}` : ""}</Text></View>) : <Text className="mp-row-description">服务端暂无可用全局配置版本。</Text> : null}
      </View></View> : null}
      {pageState.mode === "book" && batch ? <View className="mp-batch-book-mode">
        <View className="mp-batch-book-navigation"><Button className="mp-batch-text-button" onClick={pageBack}>‹ 小说列表</Button><Picker mode="selector" range={currentBooks.map(book => book.title || book.sourceBookId || book.bookId)} value={Math.max(0, currentBooks.findIndex(book => book.bookId === pageState.selectedBookId))} onChange={event => { const book = currentBooks[Number(event.detail.value)]; if (book) setPageState(openBook(pageState, book.bookId)); }}><View className="mp-batch-switch-book"><AppIcon name="book-open" size={17} /><Text>切换小说⌄</Text></View></Picker></View>
        <View className="mp-card mp-batch-book">
          <BookWorkbenchView key={`${pageState.selectedBatchId}:${pageState.selectedBookId}`} workspace={visibleWorkspace} capabilities={state.capabilities} busy={busy} actions={{
            refreshWorkspace: () => setBookRefresh(value => value + 1),
            runDirector: () => void runAction("book-director", () => runBookDirector(batch.id, pageState.selectedBookId), "当前书分镜已提交"),
            runProduction: () => void runAction("book-production", () => runProduction(batch.id, pageState.selectedBookId, { requestId: `miniprogram-${Date.now()}` }), "当前书视频已提交"),
            runMerge: () => void runAction("book-merge", () => runMerge(batch.id, pageState.selectedBookId, { requestId: `miniprogram-${Date.now()}` }), "当前书合成已提交"),
            runSubmit: () => void submitBook(),
            saveAssetPrompt: async (asset, prompt) => savePrompt("book-asset-prompt", () => saveBatchFactoryAssetPrompt(batchFactoryV11Api, batch.id, pageState.selectedBookId, { ...asset, prompt }), "资产提示词已保存"),
            saveVideoPrompt: async (videoId, prompt, revision) => savePrompt("book-video-prompt", () => saveBatchFactoryVideoPrompt(batchFactoryV11Api, batch.id, pageState.selectedBookId, videoId, prompt, revision), "视频提示词已保存"),
            saveContentWindow: async (values) => {
              if (!visibleWorkspace.book) throw new Error("当前小说未读取完成，请稍后重试");
              return savePrompt("book-content-window", () => saveBatchFactoryBookContentWindow(batchFactoryV11Api, batch.id, visibleWorkspace.book!, values), "内容范围已由服务端保存");
            },
          }} />
        </View>
      </View> : null}
      <>

      {pageState.mode === "create" && importOpen ? <View className="mp-batch-create-page">
        <View className="mp-batch-modal mp-batch-import-modal" onClick={(event) => event.stopPropagation()}>
          <View className="mp-batch-create-label"><AppIcon name="factory" size={22} /><Text className="mp-batch-modal-title">从书城开始创作</Text></View>
          <Text className="mp-row-description">添加小说，选好配置，再决定何时制作。</Text>
          <View className="mp-batch-form-step"><Text>01</Text><Text>导入小说</Text></View>
          <Input className="mp-input mp-batch-modal-input" value={importTitle} placeholder="批次名称（可选）" maxlength={80} onInput={(event) => setImportTitle(event.detail.value)} />
          <View className="mp-batch-source-group mp-batch-source-primary">
          <Picker mode="selector" range={importPlatforms.map((platform) => platform.name)} value={selectedPlatformIndex} onChange={(event) => { const platform = importPlatforms[Number(event.detail.value)]; if (platform) { setImportPlatformId(platform.id); setImportPlatformName(platform.name); } }}>
            <View className="mp-picker-field mp-batch-platform-picker"><Text className="mp-field-label">公网书城</Text><Text className="mp-picker-value">{importPlatformLoading ? "正在读取…" : importPlatformName || "请选择公网书城"}</Text></View>
          </Picker>
          {importPlatformLoading ? <Text className="mp-batch-warning">正在读取公网书城目录…</Text> : null}
          {!importPlatformLoading ? <Button className="mp-batch-text-button" onClick={() => setCatalogRetry((value) => value + 1)}>重新读取书城与模板</Button> : null}
          {!importPlatformLoading && importPlatforms.length ? <Text className="mp-row-description">已读取 {importPlatforms.length} 个可用书城，当前选择由公网目录提供。</Text> : null}
          <Text className="mp-field-label">小说 ID · 已识别 {manualBookIdsFromInput(importIds).length} 本</Text>
          <Textarea className="mp-input mp-batch-id-input" value={importIds} placeholder="每行一个小说 ID，也支持逗号分隔" maxlength={5000} autoHeight onInput={(event) => setImportIds(event.detail.value)} />
          </View>
          {extraSources.map((source) => <View className="mp-batch-source-group" key={source.key}>
            <View className="mp-batch-section-head"><Text className="mp-field-label">附加书城 · {manualBookIdsFromInput(source.ids).length} 本</Text><Button className="mp-secondary mp-batch-action" onClick={() => setExtraSources((items) => items.filter((item) => item.key !== source.key))}>移除</Button></View>
            <Picker mode="selector" range={importPlatforms.map((platform) => platform.name)} value={Math.max(0, importPlatforms.findIndex((platform) => platform.id === source.platformId))} onChange={(event) => setExtraSources((items) => items.map((item) => item.key === source.key ? { ...item, platformId: importPlatforms[Number(event.detail.value)]?.id || "" } : item))}><View className="mp-picker-field"><Text className="mp-picker-value">{importPlatforms.find((platform) => platform.id === source.platformId)?.name || "请选择书城"}</Text></View></Picker>
            <Textarea className="mp-input mp-batch-id-input" value={source.ids} placeholder="这个书城的小说 ID，每行一个" maxlength={5000} autoHeight onInput={(event) => setExtraSources((items) => items.map((item) => item.key === source.key ? { ...item, ids: event.detail.value } : item))} />
          </View>)}
          <Button className="mp-secondary mp-batch-add-source" disabled={!importPlatforms.length} onClick={() => { setExtraSources((items) => [...items, { key: sourceSequence, platformId: "", ids: "" }]); setSourceSequence((value) => value + 1); }}>＋ 添加书城</Button>
          <View className="mp-batch-form-step"><Text>02</Text><Text>统一配置</Text></View>
          <View className="mp-batch-import-window">
            <View className="mp-batch-import-window-row"><View><Text className="mp-field-label">原文获取上限</Text><Text className="mp-row-description">本次每本抓取的正文上限</Text></View><View className="mp-batch-import-window-input"><Input aria-label="原文获取上限" type="number" value={contentCaptureCharacters} onInput={event => setContentCaptureCharacters(event.detail.value)} /><Text>字</Text></View></View>
            <View className="mp-batch-import-window-row"><View><Text className="mp-field-label">AI 内容范围</Text><Text className="mp-row-description">导入后每本默认使用前 N 个非空行</Text></View><View className="mp-batch-import-window-input"><Input aria-label="AI 内容范围" type="number" value={contentRangeLines} onInput={event => setContentRangeLines(event.detail.value)} /><Text>行</Text></View></View>
            {!importContentWindow.ok ? <Text className="mp-batch-warning">{importContentWindow.error}</Text> : null}
          </View>
          <Picker disabled={busy !== "" || configLoading} range={["沿用服务端默认配置", ...configVersions.map(version => version.name || version.id)]} value={configVersionId ? configVersions.findIndex(version => version.id === configVersionId) + 1 : 0} onChange={event => setConfigVersionId(configVersions[Number(event.detail.value) - 1]?.id || "")}>
            <View className="mp-picker-field"><Text className="mp-field-label">全局配置模板</Text><Text className="mp-picker-value">{configLoading ? "正在读取模板…" : configVersions.find(version => version.id === configVersionId)?.name || "沿用服务端默认配置"}</Text></View>
          </Picker>
          {configError ? <View><Text className="mp-batch-warning">{configError}</Text><Button className="mp-secondary" onClick={() => setConfigRetry(value => value + 1)}>重新读取全局配置</Button></View> : null}
          <View className="mp-batch-form-step"><Text>03</Text><Text>启动方式</Text></View>
          <View className="mp-batch-execution-options">{([["none", "仅导入", "保存后再制作"], ["immediate", "立即执行", "导入后开始制作"], ["scheduled", "开始定时", "在指定时间开始"]] as const).map(([value, label, description]) => <Button key={value} className={`mp-batch-execution-option${importExecution === value ? " is-selected" : ""}`} disabled={busy !== ""} onClick={() => setImportExecution(value)}><Text className="mp-row-title">{label}</Text><Text className="mp-row-description">{description}</Text></Button>)}</View>
          <Text className="mp-row-description mp-batch-execution-hint">{importExecution === "none" ? "先保存到个人作品，之后随时开始制作。" : importExecution === "immediate" ? "导入后按下方预设与执行范围开始制作。" : "在指定的北京时间，按下方预设与执行范围启动。"}</Text>
          {importExecution !== "none" ? <BatchExecutionFields value={executionValue} presets={presets} disabled={busy !== ""} showSchedule={importExecution === "scheduled"} onChange={updateExecution} /> : null}
          {importExecution !== "none" && !presets.length ? <Text className="mp-batch-warning">尚未读取到自动化预设，请重新读取，或先仅导入。</Text> : null}
          <View className="mp-batch-create-summary"><Text className="mp-row-title">本次导入</Text><Text className="mp-row-description">{extraSources.length + 1} 组书城 · {manualBookIdsFromInput(importIds).length + extraSources.reduce((sum, source) => sum + manualBookIdsFromInput(source.ids).length, 0)} 个小说 ID</Text><Text className="mp-row-description">{importContentWindow.ok ? `每本原文最多 ${importContentWindow.value.contentCaptureCharacters} 字，AI 默认使用前 ${importContentWindow.value.contentRangeLines} 个非空行` : "内容范围填写无效"}</Text><Text className="mp-row-description">{configVersions.find(version => version.id === configVersionId)?.name || "服务端默认配置"} · {importExecution === "none" ? "仅导入，不启动制作" : importExecution === "immediate" ? "导入后立即执行" : `${scheduleDate || "待选日期"} ${scheduleTime || "待选时间"}（北京时间）`}</Text></View>
          <OperationDock>
            <Button className="mp-secondary" disabled={busy !== ""} onClick={() => { setImportOpen(false); setPageState(backToWorks(pageState)); }}>取消</Button>
            <Button className="mp-primary" loading={busy === "import"} disabled={busy !== "" || !importContentWindow.ok || configLoading || (importExecution !== "none" && !presetId) || !importPlatforms.length || importPlatformLoading} onClick={() => void importBooks(importExecution)}>{importExecution === "none" ? "确认导入" : importExecution === "immediate" ? "导入并立即执行" : "导入并开始定时"}</Button>
          </OperationDock>
        </View>
      </View> : null}

      {error && pageState.mode !== "works" ? <View className="mp-batch-error"><Text className="mp-error">{error}</Text>{pageState.mode === "project" ? <Button className="mp-secondary mp-batch-action" onClick={() => void load(pageState.selectedBatchId)}>重新读取</Button> : null}</View> : null}
      {pageState.mode === "project" && loading ? <View className="mp-card mp-batch-state"><Text className="mp-row-title">正在读取作品</Text></View> : null}

      {batch && pageState.mode === "project" && (productionOpen || automation && !["idle", "completed", "cancelled"].includes(automation.state || "")) ? <View className="mp-batch-section" id="batch-production">
        <View className="mp-batch-section-head"><View><Text className="mp-section-title">自动生产</Text><Text className="mp-row-description">{batch.books.length} 本小说 · 当前批次 {batch.id}</Text></View></View>
        {automationError ? <View><Text className="mp-batch-warning">{automationError}；未确认现有任务状态，请先重新读取。</Text><Button className="mp-secondary" disabled={busy !== ""} onClick={() => void load(batch.id)}>重新读取任务状态</Button></View> : null}
        {!loading && !automationLoading && !automationError && (!automation || ["idle", "completed", "cancelled"].includes(automation.state || "")) ? <View className="mp-card mp-batch-automation">
          <Text className="mp-row-title">启动当前作品</Text>
          <Text className="mp-row-description">使用本作品已保存的统一配置，无需重新导入。</Text>
          <BatchExecutionFields value={executionValue} presets={presets} disabled={busy !== ""} onChange={updateExecution} />
          {!presets.length ? <View><Text className="mp-batch-warning">没有读取到自动化预设，请重新读取作品，或先在网页保存预设。</Text><Button className="mp-secondary" onClick={() => void load(batch.id)}>重新读取</Button></View> : null}
          <View className="mp-batch-book-actions">
            <Button className="mp-primary" disabled={busy !== "" || !presetId} loading={busy === "automation-start"} onClick={() => void startExisting(false)}>立即执行</Button>
            <Button className="mp-secondary" disabled={busy !== "" || !presetId} onClick={() => void startExisting(true)}>开始定时</Button>
          </View>
        </View> : null}
        {automation ? <View className="mp-card mp-batch-automation">
          <View className="mp-batch-automation-head"><View><Text className="mp-row-title">自动化生产</Text><Text className="mp-row-description">{automationStatusLabel(automation.state)}{automation.scheduledAt ? ` · ${new Date(automation.scheduledAt).toLocaleString()}` : ""}</Text></View><Text className="mp-status-pill">{automationStatusLabel(automation.state)}</Text></View>
          <View className="mp-batch-counts"><Text>完成 {automation.counts?.completed ?? automation.counts?.ready ?? 0}</Text><Text>处理中 {automation.counts?.running ?? 0}</Text><Text>异常 {automation.counts?.failed ?? automation.counts?.blocked ?? 0}</Text></View>
          <View className="mp-batch-book-actions">
            {automation.state === "running" ? <Button className="mp-secondary mp-batch-action" disabled={busy !== ""} onClick={() => void automationAction("pause")}>暂停</Button> : null}
            {automation.state === "paused" ? <Button className="mp-secondary mp-batch-action" disabled={busy !== ""} onClick={() => void automationAction("resume")}>继续</Button> : null}
            {automation.state === "needs_attention" || automation.state === "failed" ? <Button className="mp-secondary mp-batch-action" disabled={busy !== ""} onClick={() => void automationAction("retry")}>重试失败项</Button> : null}
            {automation.state === "scheduled" || automation.state === "running" || automation.state === "paused" ? <Button className="mp-secondary mp-batch-action" disabled={busy !== ""} onClick={() => void automationAction("cancel")}>取消任务</Button> : null}
          </View>
          {(automation.books || []).map((item) => <View className="mp-batch-result-row" key={item.bookId || item.stage}>
            <Text className="mp-row-description">{batch.books.find((book) => book.id === item.bookId)?.title || item.bookId || "当前小说"}</Text>
            <Text className="mp-row-description">{item.stage || item.status || "等待"}{item.message ? ` · ${item.message}` : item.error ? ` · ${item.error}` : ""}</Text>
          </View>)}
        </View> : null}
      </View> : null}
      </>
    </ThemedPage>
  );
}
