import Taro, { useDidShow } from "@tarojs/taro";
import { Button, Image, Input, Picker, Text, Textarea, Video, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import AmbientStarfield from "../../components/AmbientStarfield";
import BrandHeader from "../../components/BrandHeader";
import AppIcon from "../../components/AppIcon";
import { apiRequest } from "../../services/api";
import { PanelProject, panelEntities, panelImageUrl, panelOutlineRequest, readPanelProject, savePanelProject, updatePanelEntity, rememberPanelProject, selectedPanelProject } from "../../services/novelPanel";
import { loadAuthenticatedMedia } from "../../services/mediaCache";
import { withThemedPage } from "../../theme";

type SelectedRow = { key: string; index: number };
function PanelMedia({ url, video = false }: { url: string; video?: boolean }) {
  const [path, setPath] = useState(""); const [error, setError] = useState(""); const [retry, setRetry] = useState(0);
  useEffect(() => { let active = true; setPath(""); setError(""); if (url) void loadAuthenticatedMedia(url, video ? "mp4" : "png").then((result) => { if (active) setPath(result.path); }).catch((cause) => { if (active) setError(cause.message || "媒体读取失败"); }); return () => { active = false; }; }, [url, video, retry]);
  if (!url) return null;
  if (error) return <View><Text className="mp-error">{error}</Text><Button onClick={() => setRetry((value) => value + 1)}>重新读取媒体</Button></View>;
  return path ? video ? <Video src={path} controls onError={() => setError("视频解码或播放失败")} /> : <Image src={path} mode="widthFix" onClick={() => Taro.previewImage({ urls: [path] })} onError={() => setError("图片显示失败")} /> : <Text>正在读取媒体…</Text>;
}

function PanelWorkspace() {
  const [projects, setProjects] = useState<Array<{ id: string; name: string }>>([]);
  const [project, setProject] = useState<PanelProject | null>(null);
  const [tab, setTab] = useState("原文"); const [busy, setBusy] = useState(""); const [error, setError] = useState("");
  const [dirty, setDirty] = useState(false); const [selected, setSelected] = useState<SelectedRow | null>(null); const [draft, setDraft] = useState<Record<string, string>>({});
  const [assetType, setAssetType] = useState("characters"); const [assetName, setAssetName] = useState(""); const [assetPrompt, setAssetPrompt] = useState("");
  const version = useRef(0); const listVersion = useRef(0); const projectRef = useRef(project); projectRef.current = project;
  useEffect(() => () => { version.current += 1; listVersion.current += 1; }, []);
  const loadProjects = async () => {
    const current = ++listVersion.current; setError("");
    try { const payload = await apiRequest<any>("/api/novel-panel/projects"); if (current !== listVersion.current) return; if (!Array.isArray(payload?.projects)) throw new Error("小说面板项目列表格式无效"); setProjects(payload.projects); const previous = selectedPanelProject(); if (!projectRef.current && previous && payload.projects.some((item: any) => item.id === previous)) await open(previous); }
    catch (cause: any) { if (current === listVersion.current) setError(cause.message || "项目读取失败"); }
  };
  useDidShow(() => { void loadProjects(); });
  const confirmDiscard = async () => !dirty || (await Taro.showModal({ title: "尚未保存", content: "放弃当前修改并切换项目？" })).confirm;
  const open = async (id: string) => {
    if (busy || !(await confirmDiscard())) return;
    const current = ++version.current; setBusy("读取项目"); setError(""); setSelected(null);
    try { const next = await readPanelProject(id); if (current === version.current) { rememberPanelProject(next.id); setProject(next); setDirty(false); setTab("原文"); } }
    catch (cause: any) { if (current === version.current) { setProject(null); setError(cause.message || "项目详情读取失败"); } }
    finally { if (current === version.current) setBusy(""); }
  };
  const patchData = (patch: Record<string, any>) => { setProject((value) => value && ({ ...value, data: { ...value.data, ...patch } })); setDirty(true); };
  const save = async () => {
    if (!project || busy) return;
    const current = ++version.current; setBusy("保存并读回项目"); setError("");
    try { const next = await savePanelProject(project); if (current === version.current) { rememberPanelProject(next.id); setProject(next); setDirty(false); await loadProjects(); Taro.showToast({ title: "项目已保存并核对", icon: "success" }); } }
    catch (cause: any) { if (current === version.current) setError(cause.message || "项目保存失败"); }
    finally { if (current === version.current) setBusy(""); }
  };
  const create = async () => {
    if (busy || !(await confirmDiscard())) return;
    version.current += 1; setProject({ id: "", name: "未命名项目", data: { novel_text: "", characters: [], scenes: [], outline_shots: [], final_segments: [] } }); setDirty(true); setSelected(null); setTab("原文");
  };
  const analyze = async (outline: boolean) => {
    if (!project || busy) return;
    if (!String(project.data.novel_text || "").trim()) { setError("请先填写小说原文"); return; }
    if (outline && !project.data.characters?.length) { setError("请先分析人物与场景"); return; }
    const current = ++version.current; setBusy(outline ? "生成分镜" : "分析原文"); setError("");
    try {
      const payload = await apiRequest<any>(outline ? "/api/novel-panel/outline-scenes" : "/api/novel-panel/analyze", { method: "POST", timeout: 180000, data: outline ? panelOutlineRequest(project.data) : { novel_text: project.data.novel_text } });
      if (current !== version.current) return;
      if (outline) {
        if (!Array.isArray(payload?.outline_shots) || !payload.outline_shots.length) throw new Error("分镜接口未返回有效分镜，保留现有内容");
        patchData({ outline_shots: payload.outline_shots, outputs: payload.outline_shots, ...(Array.isArray(payload.scenes) ? { scenes: payload.scenes } : {}) }); setTab("分镜");
      } else {
        if (!Array.isArray(payload?.characters) || !payload.characters.length) throw new Error("分析接口未返回人物，保留现有内容");
        patchData({ characters: payload.characters, scene_options: payload.scene_options || [], analysis_source_text: project.data.novel_text, analysis_complete: true }); setTab("人物");
      }
    } catch (cause: any) { if (current === version.current) setError(cause.message || "生成失败"); }
    finally { if (current === version.current) setBusy(""); }
  };
  const editableLabels: Record<string, string> = { name: "名称", scene_name: "场景名称", title: "标题", appearance: "人物外形", visual_description: "视觉描述", description: "描述", prompt: "提示词", source_text: "原文", text: "内容", current_text: "最终提示词", voice: "声音" };
  const edit = (key: string, index: number, raw: any) => { setSelected({ key, index }); setDraft(Object.fromEntries(Object.keys(editableLabels).filter((field) => typeof raw[field] === "string").map((field) => [field, raw[field]]))); };
  const applyEdit = () => {
    if (!project || !selected) return;
    try { setProject({ ...project, data: updatePanelEntity(project.data, selected.key, selected.index, draft) }); setDirty(true); setSelected(null); }
    catch (cause: any) { setError(cause.message || "条目格式不正确"); }
  };
  const assetRows = project ? ["characters", "scenes", "props"].flatMap((type) => (project.data.reference_assets?.[type] || []).map((asset: any, index: number) => ({ type, index, asset }))) : [];
  const updateAsset = (type: string, index: number, patch: Record<string, any>) => {
    const refs = projectRef.current?.data.reference_assets || {};
    patchData({ reference_assets: { ...refs, [type]: (refs[type] || []).map((item: any, position: number) => position === index ? { ...item, ...patch } : item) } });
  };
  const assetAction = async (type: string, index: number, asset: any, generate: boolean) => {
    if (!project || busy) return;
    const current = ++version.current; setBusy(generate ? "生成参考图" : "设为主参考图"); setError("");
    try {
      const response = await apiRequest<any>(generate ? "/api/novel-panel/reference-assets/generate" : "/api/novel-panel/reference-assets/use-source-as-main", {
        method: "POST", timeout: 180000, data: {
          asset_type: asset.type || (type === "characters" ? "character" : type === "scenes" ? "scene" : "prop"), asset_id: asset.asset_id,
          ...(generate ? { description: asset.prompt || asset.description || asset.name, context: project.data.novel_text || "", generation_guidance: asset.generation_guidance || "", ...(type === "characters" ? { character: { name: asset.name } } : {}) } : {}),
        },
      });
      if (current !== version.current) return;
      if (response?.ok !== true || response.asset_id !== asset.asset_id || !response.url) throw new Error("参考图操作未返回有效资产回执，请刷新核对");
      updateAsset(type, index, { ...response, main_image_url: response.url, has_main_image: true });
      Taro.showToast({ title: "图片已更新，请保存项目", icon: "none" });
    } catch (cause: any) { if (current === version.current) setError(cause.message || "图片操作失败"); }
    finally { if (current === version.current) setBusy(""); }
  };
  const uploadAsset = async (type: string, index: number, asset: any) => {
    if (!project || busy) return;
    const current = ++version.current; setBusy("上传参考图"); setError("");
    try {
      const selectedImage = await Taro.chooseImage({ count: 1, sizeType: ["compressed"] });
      const file = selectedImage.tempFiles[0]; if (!file || file.size > 10 * 1024 * 1024) throw new Error("请选择 10MB 以内的图片");
      if (!asset.asset_id) throw new Error("资产缺少服务端 ID，无法上传");
      const info = await Taro.getImageInfo({ src: selectedImage.tempFilePaths[0] });
      const mime = info.type === "png" ? "image/png" : info.type === "webp" ? "image/webp" : ["jpg", "jpeg"].includes(info.type) ? "image/jpeg" : "";
      if (!mime) throw new Error("仅支持 JPG、PNG、WebP 图片");
      const base64 = await new Promise<string>((resolve, reject) => Taro.getFileSystemManager().readFile({ filePath: selectedImage.tempFilePaths[0], encoding: "base64", success: (result) => resolve(String(result.data)), fail: reject }));
      if (current !== version.current) return;
      const response = await apiRequest<any>("/api/novel-panel/reference-assets/upload", { method: "POST", data: { asset_type: type === "characters" ? "character" : type === "scenes" ? "scene" : "prop", asset_id: asset.asset_id, variant: "source", data_url: `data:${mime};base64,${base64}` } });
      if (current !== version.current) return;
      const url = response?.url || response?.asset?.source_url || response?.data?.url;
      if (response?.ok !== true || response.asset_id !== asset.asset_id || !url) throw new Error("上传回执缺少匹配的资产和图片地址，请刷新核对");
      updateAsset(type, index, { ...response, source_url: url, has_source_image: true });
      Taro.showToast({ title: "已上传，请保存项目", icon: "none" });
    } catch (cause: any) { if (current === version.current) setError(cause.message || cause.errMsg || "上传未完成"); }
    finally { if (current === version.current) setBusy(""); }
  };

  const rowKeys = tab === "人物" ? ["characters", "scenes"] : tab === "分镜" ? ["outline_shots"] : ["final_segments"];
  const rows = project ? rowKeys.flatMap((key) => panelEntities(project.data, key).map((row, position) => ({ ...row, bucket: key, position }))) : [];
  return <View className="mp-page mp-detail-page"><AmbientStarfield /><BrandHeader eyebrow="小说面板" title="小说面板" showBack />
    <View className="mp-function-hero"><AppIcon name="notebook-tabs" size={23} /><View><Text className="mp-row-title">小说项目工作台</Text><Text className="mp-row-description">选择公网同一项目，查看原文、人物、分镜和参考资产。</Text></View></View>
    <View className="mp-card"><Picker range={projects.map((item) => item.name)} onChange={(event) => void open(projects[Number(event.detail.value)].id)}><Text className="mp-row-title">{project?.name || "选择小说项目"}</Text></Picker><Button className="mp-secondary" disabled={Boolean(busy)} onClick={create}>新建项目</Button><Button className="mp-secondary" disabled={Boolean(busy)} onClick={loadProjects}>刷新项目列表</Button>{project?.id ? <Button disabled={Boolean(busy)} onClick={() => void open(project.id)}>重新读取当前项目</Button> : null}{!projects.length ? <Text className="mp-row-description">当前未读取到项目；可刷新或新建。</Text> : null}</View>
    {error ? <Text className="mp-error">{error}</Text> : null}{busy ? <Text>正在{busy}…</Text> : null}
    {project ? <><View className="mp-tabs">{["原文", "人物", "分镜", "结果", "资产"].map((item) => <Text className={tab === item ? "mp-tab mp-tab-active" : "mp-tab"} key={item} onClick={() => { setTab(item); setSelected(null); }}>{item}</Text>)}</View>
      {tab === "原文" ? <View className="mp-card"><Text className="mp-field-label">项目名称</Text><Input disabled={Boolean(busy)} className="mp-input" value={project.name} onInput={(event) => { setProject({ ...project, name: event.detail.value }); setDirty(true); }} /><Text className="mp-field-label">小说原文</Text><Textarea maxlength={-1} disabled={Boolean(busy)} className="mp-result" value={project.data.novel_text || ""} onInput={(event) => patchData({ novel_text: event.detail.value, source_dirty: true })} /><Button disabled={Boolean(busy)} className="mp-primary" onClick={() => analyze(false)}>分析人物与场景</Button><Button disabled={Boolean(busy)} className="mp-secondary" onClick={() => analyze(true)}>生成分镜</Button><Text className="mp-field-label">风格与制作要求</Text>{["genre", "trailer_style", "camera"].map((key) => <Input key={key} disabled={Boolean(busy)} className="mp-input" value={project.data.style?.[key] || ""} placeholder={{ genre: "题材", trailer_style: "视觉风格", camera: "镜头要求" }[key]} onInput={(event) => patchData({ style: { ...project.data.style, [key]: event.detail.value } })} />)}</View> : null}
      {["人物", "分镜", "结果"].includes(tab) ? <View className="mp-section">{rows.map((row) => <View className="mp-card" key={`${row.bucket}-${row.id}`}><Text className="mp-row-title">{row.bucket === "scenes" ? "场景 · " : ""}{row.name}</Text><Text className="mp-row-description">{row.description || "服务端未返回文字内容"}</Text><PanelMedia url={String(row.raw.video_url || row.raw.videoUrl || "")} video /><Button disabled={Boolean(busy)} className="mp-secondary" onClick={() => edit(row.bucket, row.position, row.raw)}>查看 / 编辑完整内容</Button></View>)}{!rows.length ? <View className="mp-card"><Text>当前项目暂无{tab}数据。</Text></View> : null}</View> : null}
      {tab === "资产" ? <View className="mp-section"><View className="mp-card"><Text className="mp-row-title">添加参考资产</Text><Picker range={["人物", "场景", "道具"]} onChange={(event) => setAssetType(["characters", "scenes", "props"][Number(event.detail.value)])}><Text>{({ characters: "人物", scenes: "场景", props: "道具" })[assetType]}</Text></Picker><Input disabled={Boolean(busy)} className="mp-input" value={assetName} placeholder="资产名称" onInput={(event) => setAssetName(event.detail.value)} /><Textarea maxlength={-1} disabled={Boolean(busy)} className="mp-textarea" value={assetPrompt} placeholder="资产图片提示词" onInput={(event) => setAssetPrompt(event.detail.value)} /><Button disabled={Boolean(busy) || !assetName.trim()} onClick={() => { const refs = project.data.reference_assets || {}; patchData({ reference_assets: { ...refs, [assetType]: [...(refs[assetType] || []), { asset_id: `mp-${Date.now().toString(36)}`, type: assetType === "characters" ? "character" : assetType === "scenes" ? "scene" : "prop", name: assetName.trim(), description: assetPrompt, prompt: assetPrompt }] } }); setAssetName(""); setAssetPrompt(""); }}>添加资产</Button></View>{assetRows.map(({ type, index, asset }) => <View className="mp-card" key={asset.asset_id || `${type}-${index}`}><Text className="mp-row-title">{asset.name || asset.asset_id}</Text><Textarea maxlength={-1} disabled={Boolean(busy)} className="mp-textarea" value={asset.prompt || asset.description || ""} placeholder="资产提示词" onInput={(event) => updateAsset(type, index, { prompt: event.detail.value, description: event.detail.value })} /><PanelMedia url={panelImageUrl(asset)} /><Button disabled={Boolean(busy)} onClick={() => uploadAsset(type, index, asset)}>上传参考图片</Button><Button disabled={Boolean(busy)} onClick={() => assetAction(type, index, asset, true)}>AI 生成参考图</Button>{asset.has_source_image ? <Button disabled={Boolean(busy)} onClick={() => assetAction(type, index, asset, false)}>将上传图片设为主图</Button> : null}</View>)}{!assetRows.length ? <Text className="mp-row-description">当前项目尚未保存参考资产。</Text> : null}</View> : null}
      {selected ? <View className="mp-card"><Text className="mp-row-title">编辑内容</Text>{Object.entries(draft).map(([key, value]) => <View key={key}><Text className="mp-field-label">{editableLabels[key]}</Text><Textarea maxlength={-1} disabled={Boolean(busy)} className="mp-result" value={value} onInput={(event) => setDraft((current) => ({ ...current, [key]: event.detail.value }))} /></View>)}{!Object.keys(draft).length ? <Text className="mp-row-description">这条记录暂无可编辑的文本字段。</Text> : null}<Button disabled={Boolean(busy) || !Object.keys(draft).length} onClick={applyEdit}>应用修改</Button><Button onClick={() => setSelected(null)}>取消</Button></View> : null}
      <Button disabled={Boolean(busy)} className="mp-primary" onClick={save}>{dirty ? "保存当前修改" : "保存并核对项目"}</Button>
    </> : null}
  </View>;
}
export default withThemedPage(PanelWorkspace);
