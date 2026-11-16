import { Button, Image, Input, ScrollView, Text, Textarea, Video, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import Taro from "@tarojs/taro";
import { bookMediaOptions, stagePrompt, readAssetImage, mediaFailureMessage } from "./bookMedia";
import type { BatchFactoryAsset, BatchFactoryCapability, BatchFactoryBook, BatchFactoryBookWorkspace } from "../../services/batchFactoryV11";
import { statusTone } from "../../services/batchFactoryModels";
import { forgetAuthenticatedMedia, loadAuthenticatedMedia } from "../../services/mediaCache";
import { bookWorkbenchStageRows } from "./bookWorkbenchState";
import AppIcon from "../../components/AppIcon";
import OperationDock from "../../components/OperationDock";
import { sessionRevision } from "../../services/sessionBoundary";
import { previewContentWindow, readContentWindow, validateContentWindow } from "./contentWindow";

export type BookWorkbenchActions = {
  runDirector: () => void;
  runProduction: () => void;
  runMerge: () => void;
  runSubmit?: () => void;
  retryProduction?: () => void;
  refreshWorkspace?: () => void;
  saveAssetPrompt?: (asset: BatchFactoryAsset, prompt: string) => Promise<void>;
  saveVideoPrompt?: (videoId: string, prompt: string, revision: number) => Promise<void>;
  saveContentWindow?: (values: { contentRangeLines: number; contentCaptureCharacters: number }) => Promise<void>;
};

export type BookWorkbenchProps = {
  workspace: BatchFactoryBookWorkspace;
  capabilities: Record<string, BatchFactoryCapability>;
  busy?: string;
  actions: BookWorkbenchActions;
};

type WorkbenchTab = "storyboard" | "assets" | "source" | "tasks" | "delivery";

function can(capabilities: Record<string, BatchFactoryCapability>, key: string) {
  return capabilities[key]?.available === true;
}

function reason(capabilities: Record<string, BatchFactoryCapability>, key: string) {
  return capabilities[key]?.reason || "服务端未返回可用原因";
}

function bookText(book: BatchFactoryBook | null, key: string) {
  const value = book && (book as Record<string, unknown>)[key];
  return String(value || "").trim();
}

function publicationProgress(book: BatchFactoryBook | null) {
  const metadata = book?.sourceMetadata;
  if (!metadata || typeof metadata !== "object" || Array.isArray(metadata)) return null;
  const progress = (metadata as Record<string, unknown>).websiteSubmitProgress;
  if (!progress || typeof progress !== "object" || Array.isArray(progress)) return null;
  const value = progress as Record<string, unknown>;
  return { status: String(value.status || "").trim().toLowerCase(), message: String(value.message || "").trim() };
}

function extensionFor(url: string, fallback: string) {
  const match = String(url || "").match(/\.([a-z0-9]{2,5})(?:\?|$)/i);
  return match?.[1] || fallback;
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return <View className="mp-batch-zone"><Text className="mp-batch-zone-title">{title}</Text>{children}</View>;
}

function Tabs({ active, onChange }: { active: WorkbenchTab; onChange: (tab: WorkbenchTab) => void }) {
  const tabs: Array<[WorkbenchTab, string]> = [["storyboard", "分镜"], ["assets", "资产"], ["source", "原文"], ["tasks", "任务"]];
  return <View className="mp-batch-workbench-tabs">{tabs.map(([key, label]) => <Button key={key} aria-pressed={active === key} className={active === key ? "mp-batch-workbench-tab mp-batch-workbench-tab-active" : "mp-batch-workbench-tab"} onClick={() => onChange(key)}>{label}</Button>)}</View>;
}

export function AssetImagePreview({ imagePath, error, loading, onRetry }: { imagePath: string; error: string; loading: boolean; onRetry: () => void }) {
  return imagePath ? <Image className="mp-batch-asset-image" src={imagePath} mode="aspectFit" onClick={() => void Taro.previewImage({ current: imagePath, urls: [imagePath] })} /> : <View className="mp-batch-asset-placeholder"><Text>{error || (loading ? "正在读取图片…" : "暂无可用图片")}</Text>{error ? <Button className="mp-secondary mp-batch-action" onClick={onRetry}>重新读取图片</Button> : null}</View>;
}

export function AssetRow({ asset, imageUrl, recordsLoading=false, onSavePrompt }: { asset: BatchFactoryAsset; imageUrl?: string; recordsLoading?:boolean; onSavePrompt?: (asset: BatchFactoryAsset, prompt: string) => Promise<void> }) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState("");
  const [media, setMedia] = useState({ path: "", error: "" });
  const [loading, setLoading] = useState(false);
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    let active = true;
    setMedia({ path: "", error: "" });
    setLoading(Boolean(imageUrl));
    void readAssetImage(imageUrl || "", loadAuthenticatedMedia).then(result => {
      if (active) { setMedia(result); setLoading(false); }
    });
    return () => { active = false; };
  }, [asset.id, imageUrl, retry]);
  const prompt = String(asset.prompt || asset.visualPrompt || asset.description || "");
  return <View className="mp-batch-asset-row">
    <AssetImagePreview imagePath={media.path} error={media.error} loading={loading||recordsLoading} onRetry={() => setRetry(value => value + 1)} />
    <View className="mp-row-copy"><Text className="mp-row-title">{asset.name || asset.id}</Text><Text className="mp-row-description">{({ character: "人物", scene: "场景", prop: "道具" } as Record<string, string>)[asset.kind || asset.type || ""] || asset.kind || asset.type || "资产"}</Text></View>
    <View className="mp-batch-asset-detail">{editing ? <><Textarea className="mp-batch-prompt-editor" value={draft} maxlength={12000} autoHeight onInput={event => setDraft(event.detail.value)} /><View className="mp-batch-prompt-actions"><Button className="mp-batch-text-button" disabled={saving} onClick={() => { setEditing(false); setSaveError(""); }}>取消</Button><Button className="mp-primary mp-batch-action mp-batch-prompt-save-button" disabled={saving || !draft.trim()} loading={saving} onClick={() => { if (!onSavePrompt) return; setSaving(true); setSaveError(""); void onSavePrompt(asset, draft.trim()).then(() => setEditing(false)).catch(cause => setSaveError(cause instanceof Error ? cause.message : "资产提示词保存失败")).finally(() => setSaving(false)); }}>保存提示词</Button></View>{saveError ? <Text className="mp-batch-zone-warning">{saveError}</Text> : null}</> : <><Text className="mp-batch-prompt">{prompt || "服务端未返回资产提示词"}</Text><View className="mp-batch-prompt-actions">{onSavePrompt ? <Button className="mp-batch-prompt-edit-button" onClick={() => { setDraft(prompt); setEditing(true); }}>编辑提示词</Button> : null}{prompt ? <Button className="mp-batch-text-button" onClick={() => void Taro.setClipboardData({ data: prompt })}>复制提示词</Button> : null}</View></>}</View>
  </View>;
}

export default function BookWorkbenchView({ workspace, capabilities, busy = "", actions }: BookWorkbenchProps) {
  const [tab, setTab] = useState<WorkbenchTab>("storyboard");
  const [videoMedia, setVideoMedia] = useState({ url: "", path: "" });
  const [mediaError, setMediaError] = useState("");
  const [selectedMedia, setSelectedMedia] = useState("");
  const [selectedMerge, setSelectedMerge] = useState("");
  const [toolsOpen, setToolsOpen] = useState(false);
  const [mediaRetry, setMediaRetry] = useState(0);
  const [confirmationError, setConfirmationError] = useState("");
  const [readingSize, setReadingSize] = useState(14);
  const [videoEditing, setVideoEditing] = useState(false);
  const [videoDraft, setVideoDraft] = useState("");
  const [videoSaving, setVideoSaving] = useState(false);
  const [videoSaveError, setVideoSaveError] = useState("");
  const [contentWindowDraft, setContentWindowDraft] = useState(() => {
    const initial = readContentWindow(workspace.book?.sourceMetadata);
    return { contentRangeLines: String(initial.contentRangeLines), contentCaptureCharacters: String(initial.contentCaptureCharacters) };
  });
  const [contentWindowSaving, setContentWindowSaving] = useState(false);
  const [contentWindowError, setContentWindowError] = useState("");
  const [videoRatio, setVideoRatio] = useState(16 / 9);
  const confirmationBusy = useRef(false);
  const current = useRef({ batchId: workspace.batchId, bookId: workspace.bookId, mounted: true });
  current.current.batchId = workspace.batchId;
  current.current.bookId = workspace.bookId;
  useEffect(() => { current.current.mounted = true; return () => { current.current.mounted = false; }; }, []);
  useEffect(() => {
    const confirmed = readContentWindow(workspace.book?.sourceMetadata);
    setContentWindowDraft({ contentRangeLines: String(confirmed.contentRangeLines), contentCaptureCharacters: String(confirmed.contentCaptureCharacters) });
    setContentWindowSaving(false);
    setContentWindowError("");
  }, [workspace.batchId, workspace.bookId, workspace.book?.revision]);
  const book = workspace.book;
  const submitProgress = publicationProgress(book);
  const mediaOptions = bookMediaOptions(workspace);
  const visibleMedia = mediaOptions.filter(item => tab === "delivery" ? item.key.startsWith("merge-") : !item.key.startsWith("merge-"));
  const activeMedia = visibleMedia.find((item) => item.key === (tab === "delivery" ? selectedMerge : selectedMedia)) || visibleMedia.find((item) => item.url) || visibleMedia[0];
  const activeTask = activeMedia?.key.startsWith("shot-") ? workspace.production?.tasks[Number(activeMedia.key.slice(5))] : undefined;
  const videoPath = videoMedia.url === activeMedia?.url ? videoMedia.path : "";

  useEffect(() => { setTab("storyboard"); setSelectedMedia(""); setSelectedMerge(""); setToolsOpen(false); setVideoEditing(false); setVideoSaveError(""); }, [workspace.bookId, workspace.batchId]);

  useEffect(() => {
    let active = true;
    setVideoMedia({ url: "", path: "" });
    setMediaError("");
    setVideoRatio(16 / 9);
    if (workspace.readState === "ready" && activeMedia?.url) {
      void loadAuthenticatedMedia(activeMedia.url, extensionFor(activeMedia.url, "mp4"))
        .then((result) => { if (active) setVideoMedia({ url: activeMedia.url, path: result.path }); })
        .catch(cause => { if (active) setMediaError(mediaFailureMessage(cause, "download")); });
    }
    return () => { active = false; };
  }, [workspace.readState, workspace.bookId, workspace.batchId, activeMedia?.url, mediaRetry]);


  const videoStatus = statusTone(activeMedia?.status);
  const mergeRow = book ? bookWorkbenchStageRows(book).find((row) => row.key === "merge") : undefined;
  const storyboardPrompt = activeMedia ? activeMedia.prompt : stagePrompt(workspace.stages);
  const activeVideoRevision = Number(activeMedia?.revision);
  const mergeJob = workspace.mergeStatus?.jobs?.find((job) => String(job.bookId) === String(workspace.bookId));
  const mergeLabel = mergeJob?.status ? batchStatusLabel(mergeJob.status) : mergeRow?.state || "状态未返回";
  const mergedPreviewReady = (workspace.mergeStatus?.jobs || []).some((job) => String(job.bookId) === String(workspace.bookId)
    && ["succeeded", "completed"].includes(String(job.status || "").toLowerCase())
    && Boolean(String(job.outputUrl || job.mediaUrl || "").trim()));
  const sourceText = workspace.sourceText || bookText(book, "sourceText") || bookText(book, "contentPreview");
  const savedContentWindow = readContentWindow(book?.sourceMetadata);
  const contentWindowValidation = validateContentWindow(contentWindowDraft);
  const contentWindowValue = contentWindowValidation.ok ? contentWindowValidation.value : savedContentWindow;
  const contentWindowPreview = previewContentWindow(sourceText, contentWindowValue.contentRangeLines);
  const contentWindowUnchanged = contentWindowValidation.ok && contentWindowValidation.value.contentRangeLines === savedContentWindow.contentRangeLines && contentWindowValidation.value.contentCaptureCharacters === savedContentWindow.contentCaptureCharacters;
  const productionAvailable = can(capabilities, "production.submit");
  const mergeAvailable = can(capabilities, "merge.run");
  const publishProvider = String((book as Record<string, unknown> | null)?.publishProvider || "121");
  const publishCapability = `publish.${publishProvider}`;
  const publishAvailable = can(capabilities, publishCapability) && Boolean(actions.runSubmit);
  const mergeReason = mergeAvailable ? "" : reason(capabilities, "merge.run");
  const submitReason = publishAvailable ? "" : reason(capabilities, publishCapability);
  const hasPlayableVideo = Boolean(videoPath);
  const changeTab = (next: WorkbenchTab) => { setTab(next); };
  const confirmAction = async (title: string, action: () => void) => {
    if (busy || confirmationBusy.current) return;
    confirmationBusy.current = true;
    setConfirmationError("");
    const session = sessionRevision(), batchId = workspace.batchId, bookId = workspace.bookId;
    try {
      const result = await Taro.showModal({ title, content: `将对《${book?.title || book?.name || "当前小说"}》整本执行。生成操作可能产生模型费用，请确认。`, confirmText: "确认执行" });
      if (result.confirm && current.current.mounted && current.current.batchId === batchId && current.current.bookId === bookId && session === sessionRevision()) action();
    } catch { setConfirmationError("无法打开确认窗口，请重试"); }
    finally { confirmationBusy.current = false; }
  };

  if (workspace.readState === "loading") return <View className="mp-batch-book-workspace-state"><Text className="mp-row-title">正在读取当前小说</Text><Text className="mp-row-description">正文、资产、提示词和生产阶段来自公网服务端。</Text></View>;
  if (workspace.readState === "error") return <View className="mp-batch-book-workspace-state mp-batch-state-error"><Text className="mp-row-title">当前小说读取失败</Text><Text className="mp-row-description">{workspace.error || "服务端没有返回当前小说"}</Text></View>;
  if (workspace.readState !== "ready" || !book) return <View className="mp-batch-book-workspace-state"><Text className="mp-row-title">当前小说不存在</Text><Text className="mp-row-description">请返回作品重新选择。</Text></View>;

  return <View className="mp-batch-book-workspace">
    <View className="mp-batch-workbench-heading"><View><Text className="mp-batch-book-title">{book.title || book.name || "未命名小说"}</Text><Text className="mp-batch-subtitle">单书工作区 · {mediaOptions.filter(item => !item.key.startsWith("merge-")).length} 个分镜</Text></View>{mergedPreviewReady ? <Button className="mp-batch-delivery-link" onClick={() => changeTab("delivery")}>预览成片</Button> : null}{actions.refreshWorkspace ? <Button className="mp-batch-icon-button" aria-label="刷新状态" disabled={busy !== ""} onClick={actions.refreshWorkspace}><AppIcon name="refresh-ccw" size={18} /></Button> : null}</View>
      {Object.entries(workspace.sectionErrors || {}).map(([key, message]) => <Text key={key} className="mp-batch-zone-warning">{({ stages: "分镜", assets: "资产", production: "生产", merge: "合成" } as Record<string, string>)[key] || "资产图片"}读取失败：{message}</Text>)}
    {tab === "storyboard" || tab === "delivery" ? <>
      {activeMedia?.url ? <View className={`mp-batch-video-preview${videoRatio < 1 ? " mp-batch-video-preview-portrait" : ""}`} style={{ aspectRatio: "16 / 9", maxHeight: "60vh", width: "100%", marginLeft: "auto", marginRight: "auto" }}>
        {hasPlayableVideo ? <Video key={videoPath} className="mp-batch-video" src={videoPath} controls showCenterPlayBtn objectFit="contain" onLoadedMetaData={event => { const { width, height } = event.detail; if (width > 0 && height > 0) setVideoRatio(width / height); }} onError={event => { setVideoMedia({ url: "", path: "" }); setMediaError(mediaFailureMessage(event.detail, "playback")); }} /> : <View className="mp-batch-video-empty"><Text>{mediaError || "正在读取视频…"}</Text>{mediaError ? <Button className="mp-secondary mp-batch-action" onClick={() => { forgetAuthenticatedMedia(activeMedia.url); setMediaRetry(value => value + 1); }}>重新读取视频</Button> : null}</View>}
      </View> : <View className="mp-batch-media-pending"><AppIcon name="clapperboard" size={28} /><View><Text className="mp-row-title">{activeMedia?.status ? batchStatusLabel(activeMedia.status) : tab === "delivery" ? "尚无合成成片" : "当前分镜暂无视频"}</Text><Text className="mp-row-description">{activeTask?.error || (activeTask as any)?.errorMessage || (tab === "delivery" ? "完成视频生成后，可在这里合成整本作品。" : "可先查看提示词，或前往任务查看制作进度。")}</Text></View></View>}
      {visibleMedia.length ? <ScrollView className="mp-batch-shot-scroll" scrollX><View className="mp-batch-shot-strip">{visibleMedia.map((item, index) => <Button key={item.key} aria-pressed={item.key === activeMedia?.key} className={`mp-batch-shot ${item.key === activeMedia?.key ? "is-selected" : ""}`} onClick={() => tab === "delivery" ? setSelectedMerge(item.key) : setSelectedMedia(item.key)}><Text className="mp-batch-shot-number">{String(index + 1).padStart(2, "0")}</Text><Text className="mp-row-title">{item.label}</Text><Text className="mp-row-description">{item.url ? "可预览" : batchStatusLabel(item.status)}</Text></Button>)}</View></ScrollView> : null}
    </> : null}
    <Tabs active={tab} onChange={changeTab} />
    {tab === "source" ? <Section title="原文"><View className="mp-batch-content-window"><Text className="mp-row-title">AI 使用内容</Text><View className="mp-batch-content-window-row"><Text className="mp-field-label">内容范围</Text><View className="mp-batch-content-window-input"><Input aria-label="内容范围" type="number" value={contentWindowDraft.contentRangeLines} onInput={event => setContentWindowDraft(current => ({ ...current, contentRangeLines: event.detail.value }))} /><Text>行</Text></View></View><View className="mp-batch-content-window-row"><View><Text className="mp-field-label">获取上限</Text><Text className="mp-row-description">仅用于下次重新获取原文</Text></View><View className="mp-batch-content-window-input"><Input aria-label="获取上限" type="number" value={contentWindowDraft.contentCaptureCharacters} onInput={event => setContentWindowDraft(current => ({ ...current, contentCaptureCharacters: event.detail.value }))} /><Text>字</Text></View></View>{!contentWindowValidation.ok ? <Text className="mp-batch-zone-warning">{contentWindowValidation.error}</Text> : null}{contentWindowError ? <Text className="mp-batch-zone-warning">{contentWindowError}</Text> : null}<Button className="mp-primary mp-batch-content-window-save" disabled={!actions.saveContentWindow || !contentWindowValidation.ok || contentWindowUnchanged || contentWindowSaving} loading={contentWindowSaving} onClick={() => { if (!contentWindowValidation.ok || !actions.saveContentWindow) return; setContentWindowSaving(true); setContentWindowError(""); void actions.saveContentWindow(contentWindowValidation.value).catch(cause => setContentWindowError(cause instanceof Error ? cause.message : "内容范围保存失败")).finally(() => setContentWindowSaving(false)); }}>保存范围</Button><Text className="mp-row-description">当前 AI 使用前 {contentWindowValue.contentRangeLines} 个非空行，共 {contentWindowPreview.selectedLines.length} / {contentWindowPreview.availableLines} 行。</Text><View className="mp-batch-content-window-preview"><Text className="mp-field-label">截取预览</Text><Text className="mp-batch-prompt" selectable>{contentWindowPreview.selectedLines.join("\n") || "原文暂无可用内容"}</Text></View></View><View className="mp-batch-reading-tools">{[14, 16, 18, 20].map(size => <Button key={size} aria-pressed={readingSize === size} className={`mp-batch-text-button${readingSize === size ? " is-selected" : ""}`} onClick={() => setReadingSize(size)}>{size}号字</Button>)}{sourceText ? <Button className="mp-batch-text-button" onClick={() => void Taro.setClipboardData({ data: sourceText })}>复制原文</Button> : null}</View><Text className="mp-batch-source-text" selectable style={{ fontSize: `${readingSize}px`, lineHeight: 1.7 }}>{sourceText || "服务端未返回原文"}</Text></Section> : null}
    {tab === "storyboard" ? <Section title={activeMedia ? `${activeMedia.label} · 视频提示词` : "视频提示词"}><View className="mp-batch-zone-line"><Text className="mp-row-description">当前分镜</Text><Text className={`mp-status-text mp-status-text-${videoStatus.tone}`}>{batchStatusLabel(activeMedia?.status)}</Text></View>{activeTask?.error || (activeTask as any)?.errorMessage ? <Text className="mp-batch-zone-warning">{activeTask?.error || (activeTask as any)?.errorMessage}</Text> : null}{videoEditing ? <><Textarea className="mp-batch-prompt-editor" value={videoDraft} maxlength={12000} autoHeight onInput={event => setVideoDraft(event.detail.value)} /><View className="mp-batch-prompt-actions"><Button className="mp-batch-text-button" disabled={videoSaving} onClick={() => { setVideoEditing(false); setVideoSaveError(""); }}>取消</Button><Button className="mp-primary mp-batch-action mp-batch-prompt-save-button" disabled={videoSaving || !videoDraft.trim()} loading={videoSaving} onClick={() => { if (!activeMedia?.videoId || !Number.isSafeInteger(activeVideoRevision) || !actions.saveVideoPrompt) return; setVideoSaving(true); setVideoSaveError(""); void actions.saveVideoPrompt(activeMedia.videoId, videoDraft.trim(), activeVideoRevision).then(() => setVideoEditing(false)).catch(cause => setVideoSaveError(cause instanceof Error ? cause.message : "视频提示词保存失败")).finally(() => setVideoSaving(false)); }}>保存提示词</Button></View>{videoSaveError ? <Text className="mp-batch-zone-warning">{videoSaveError}</Text> : null}</> : <><Text className="mp-batch-prompt">{storyboardPrompt || "当前分镜未返回视频提示词"}</Text><View className="mp-batch-prompt-actions">{actions.saveVideoPrompt && activeMedia?.videoId && Number.isSafeInteger(activeVideoRevision) ? <Button className="mp-batch-prompt-edit-button" onClick={() => { setVideoDraft(storyboardPrompt); setVideoEditing(true); }}>编辑提示词</Button> : null}{storyboardPrompt ? <Button className="mp-batch-text-button" onClick={() => void Taro.setClipboardData({ data: storyboardPrompt })}>复制提示词</Button> : null}</View></>}</Section> : null}
    {tab === "assets" ? <Section title={`资产提示词与图片 · ${workspace.assets.length}`} >{workspace.assets.length ? <View className="mp-batch-asset-grid">{workspace.assets.map((asset) => <AssetRow key={`${workspace.batchId}:${workspace.bookId}:${asset.id}`} asset={asset} onSavePrompt={actions.saveAssetPrompt} recordsLoading={workspace.assetImagesLoading} imageUrl={workspace.assetImages[asset.id]?.find(image => String(image.url || "").trim())?.url} />)}</View> : <View className="mp-batch-inline-empty"><AppIcon name="users-round" size={28} /><Text className="mp-row-description">暂无人物、场景或道具资产</Text></View>}</Section> : null}
    {tab === "tasks" ? <Section title="生产任务">{workspace.production?.tasks.length ? workspace.production.tasks.map((task, index) => <View className="mp-batch-task-row" key={task.id || task.videoId || index}><Text className="mp-row-title">{task.videoId || task.id || `任务 ${index + 1}`}</Text><Text className="mp-row-description">{batchStatusLabel(task.status)}{task.error ? ` · ${task.error}` : ""}{task.mediaUrl ? " · 已返回视频地址" : ""}</Text></View>) : <Text className="mp-row-description">服务端未返回生产任务</Text>}</Section> : null}
    {tab === "delivery" ? <Section title="合成与提交"><View className="mp-batch-zone-line"><Text className="mp-row-description">整书合成</Text><Text className="mp-row-title">{mergeLabel}</Text></View><Text className="mp-row-description">将本书已生成的视频合成为成片，再提交到目标平台。</Text>{submitProgress ? <View className="mp-batch-zone-line"><Text className="mp-row-description">视频管理系统状态</Text><Text className="mp-row-title">{submitProgress.message || (submitProgress.status === "succeeded" ? "已确认上传" : "等待后台列表回读")}</Text></View> : null}{mergeReason ? <Text className="mp-batch-zone-warning">{mergeReason}</Text> : null}{submitReason ? <Text className="mp-batch-zone-warning">{submitReason}</Text> : null}</Section> : null}
    {confirmationError ? <Text className="mp-batch-zone-warning">{confirmationError}</Text> : null}
    <OperationDock>
      {tab === "delivery" ? <><Button className="mp-batch-dock-tool" onClick={() => changeTab("storyboard")}><AppIcon name="clapperboard" size={18} /><Text>分镜</Text></Button><Button className="mp-secondary" disabled={busy !== "" || !mergeAvailable} onClick={() => void confirmAction("开始合成本书", actions.runMerge)}>开始合成</Button><Button className="mp-primary" disabled={busy !== "" || !publishAvailable || !mergeJob?.outputUrl || !["succeeded", "completed"].includes(String(mergeJob.status))} onClick={actions.runSubmit}>提交到视频管理系统</Button></> : <><Button className="mp-batch-dock-tool" aria-expanded={toolsOpen} onClick={() => setToolsOpen(true)}><AppIcon name="sliders" size={18} /><Text>制作工具</Text></Button><Button className="mp-secondary" onClick={() => changeTab("delivery")}>合成与提交</Button><Button className="mp-primary" disabled={busy !== "" || !productionAvailable} onClick={() => confirmAction("生成本书视频", actions.runProduction)}>生成本书视频</Button></>}
      {tab === "storyboard" && (!productionAvailable || !can(capabilities, "director.run")) ? <Text className="mp-batch-dock-note">{!productionAvailable ? reason(capabilities, "production.submit") : reason(capabilities, "director.run")}</Text> : null}
    </OperationDock>
    {toolsOpen ? <View className="mp-batch-config-overlay" onClick={() => setToolsOpen(false)}><View className="mp-batch-config-panel" role="dialog" aria-modal aria-label="制作工具" onClick={event => event.stopPropagation()}><View className="mp-batch-sheet-handle" /><View className="mp-batch-section-head"><Text className="mp-section-title">制作工具</Text><Button className="mp-batch-text-button" onClick={() => setToolsOpen(false)}>关闭</Button></View><Text className="mp-row-description">操作范围：《{book.title || book.name || "当前小说"}》整本</Text><Button className="mp-batch-tool-row" disabled={busy !== "" || !can(capabilities, "director.run")} onClick={() => { setToolsOpen(false); void confirmAction("生成本书分镜", actions.runDirector); }}><AppIcon name="clapperboard" size={20} /><Text>生成本书分镜</Text><AppIcon name="chevron-right" size={16} /></Button>{!can(capabilities, "director.run") ? <Text className="mp-batch-zone-warning">{reason(capabilities, "director.run")}</Text> : null}{actions.retryProduction ? <Button className="mp-batch-tool-row" disabled={busy !== "" || !productionAvailable} onClick={() => { setToolsOpen(false); void confirmAction("重试本书失败视频", actions.retryProduction!); }}><AppIcon name="refresh-ccw" size={20} /><Text>重试失败视频</Text><AppIcon name="chevron-right" size={16} /></Button> : null}<Button className="mp-batch-tool-row" onClick={() => { setToolsOpen(false); changeTab("tasks"); }}><AppIcon name="timer" size={20} /><Text>查看生产任务</Text><AppIcon name="chevron-right" size={16} /></Button></View></View> : null}
  </View>;
}

function batchStatusLabel(status: unknown) {
  const value = String(status || "").trim().toLowerCase();
  return ({ queued: "排队中", running: "生成中", succeeded: "已完成", completed: "已完成", failed: "失败", error: "异常" } as Record<string, string>)[value] || "状态未返回";
}
