import Taro, { useDidShow } from "@tarojs/taro";
import { Button, Image, Input, Picker, Text, Textarea, Video, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import AppIcon from "../../components/AppIcon";
import BrandHeader from "../../components/BrandHeader";
import AmbientStarfield from "../../components/AmbientStarfield";
import { apiRequest } from "../../services/api";
import { buildAgentPlanRequest, buildStudioRun, createStudioClient, pendingStudioRun, settleStudioRuns, type StudioProject, type StudioRecord } from "../../services/agentStudio";
import { loadAuthenticatedMedia } from "../../services/mediaCache";
import { onSessionInvalidated, sessionRevision } from "../../services/sessionBoundary";
import { withThemedPage } from "../../theme";

const uid = (prefix: string) => `${prefix}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
const emptyCatalog = { models: [] as StudioRecord[], assets: [] as StudioRecord[], runs: [] as StudioRecord[], brandKits: [] as StudioRecord[], skills: [] as StudioRecord[], enabled: false, errors: [] as string[] };
export type AgentWorkspaceProps = { taskId?: string; projectId?: string };
function AgentWorkspace({ taskId = "", projectId = "" }: AgentWorkspaceProps) {
  const renderSession = sessionRevision();
  const request = <T,>(path: string, options?: any): Promise<T> => { if (renderSession !== sessionRevision()) return Promise.reject(new Error("账号已切换，已取消旧页面操作")); return apiRequest<T>(path, options); };
  const client = createStudioClient(request);
  const [projects, setProjects] = useState<StudioRecord[]>([]), [project, setProject] = useState<StudioProject | null>(null), [catalog, setCatalog] = useState(emptyCatalog);
  const [name, setName] = useState(""), [prompt, setPrompt] = useState(""), [error, setError] = useState(""), [notice, setNotice] = useState("");
  const [plan, setPlan] = useState<StudioRecord | null>(null), [busy, setBusy] = useState(false), [dirty, setDirty] = useState(false);
  const [textModel, setTextModel] = useState(""), [imageModel, setImageModel] = useState(""), [videoModel, setVideoModel] = useState("");
  const [selectedNode, setSelectedNode] = useState(""), [assetId, setAssetId] = useState(""), [rawDocument, setRawDocument] = useState(""), [showDocument, setShowDocument] = useState(false);
  const [media, setMedia] = useState<{ kind: string; path: string } | null>(null);
  const [library, setLibrary] = useState<{ kind: "brand-kits" | "skills"; id?: string; name: string; revision?: number; document: string } | null>(null);
  const epoch = useRef(0), listEpoch = useRef(0), lock = useRef(false), interaction = useRef(false), unknown = useRef<{ key: string; kind: string } | null>(null);
  const renderEpoch = epoch.current;
  type Ticket = { session: number; epoch: number };
  const valid = (ticket: Ticket) => ticket.session === sessionRevision() && ticket.epoch === epoch.current;
  async function confirmThen(title: string, content: string, operation: (ticket: Ticket) => Promise<void>) {
    if (lock.current || interaction.current) return;
    const ticket = { session: renderSession, epoch: epoch.current };
    if (!valid(ticket)) return;
    interaction.current = true; setBusy(true);
    try { const answer = await Taro.showModal({ title, content }); if (answer.confirm && valid(ticket)) await operation(ticket); }
    catch (e: any) { if (valid(ticket)) setError(e.message || "操作取消或失败"); }
    finally { interaction.current = false; if (ticket.session === sessionRevision()) setBusy(false); }
  }
  useEffect(() => {
    const off = onSessionInvalidated(() => { epoch.current++; listEpoch.current++; setProjects([]); setProject(null); setCatalog(emptyCatalog); setMedia(null); setPlan(null); setPrompt(""); setLibrary(null); unknown.current = null; setError("登录已变化，请重新读取 Studio"); });
    return () => { epoch.current++; listEpoch.current++; off(); };
  }, []);
  const acceptProject = (p: StudioProject) => { setProject(p); setRawDocument(JSON.stringify(p.document, null, 2)); setDirty(false); };
  const edit = (document: StudioRecord) => { if (!project || lock.current) return; epoch.current++; setProject({ ...project, document }); setRawDocument(JSON.stringify(document, null, 2)); setDirty(true); };
  async function readProject(id: string) {
    if (lock.current) return;
    if (dirty && !(await Taro.showModal({ title: "重新读取项目", content: "放弃当前未保存修改并读取服务端项目？" })).confirm) return;
    const version = ++epoch.current; setError(""); setPlan(null); setMedia(null); setBusy(true);
    try { const next = await client.project(id); if (version === epoch.current) { acceptProject(next); setSelectedNode(""); if (unknown.current?.kind === "project" && unknown.current.key === id) unknown.current = null; } }
    catch (e: any) { if (version === epoch.current) { setProject(null); setError(e.message || "项目读取失败"); } }
    finally { if (version === epoch.current) setBusy(false); }
  }
  async function load() {
    const version = ++listEpoch.current; setError("");
    try {
      const next = await client.projects(); if (version !== listEpoch.current) return; setProjects(next);
      const data = await client.catalog(); if (version !== listEpoch.current) return; setCatalog(data); settleStudioRuns(data.runs);
      if (!textModel) setTextModel(String(data.models.find(m => m.kind === "text" && m.enabled !== false)?.id || ""));
      if (!imageModel) setImageModel(String(data.models.find(m => m.kind === "image" && m.enabled !== false)?.id || ""));
      if (!videoModel) setVideoModel(String(data.models.find(m => m.kind === "video" && m.enabled !== false)?.id || ""));
      if (unknown.current?.kind === "run" && data.runs.some(r => r.request?.request_key === unknown.current?.key)) { unknown.current = null; setNotice("已核对到上次运行，请查看其状态"); }
      if (projectId && !project && next.some(p => p.id === projectId)) await readProject(projectId);
    } catch (e: any) { if (version === listEpoch.current) { setProjects([]); setProject(null); setMedia(null); setCatalog(emptyCatalog); setError(e.message || "Studio 不可用"); } }
  }
  useDidShow(() => { void load(); });
  async function write(kind: string, operation: () => Promise<void>, key = uid(kind), ticket: Ticket = { session: renderSession, epoch: renderEpoch }) {
    if (lock.current) return;
    if (!valid(ticket)) { setError("账号或目标项目已变化，已取消旧操作"); return; }
    if (unknown.current) { setError("上次操作结果未知，请刷新核对对应项目或运行记录，禁止重复提交"); return; }
    lock.current = true; setBusy(true); setError(""); setNotice(""); const version = ++epoch.current;
    try { await operation(); }
    catch (e: any) { if (version !== epoch.current) return; if (!e.unsubmitted && (!e.statusCode || e.statusCode >= 500 || String(e.message).includes("结果未知"))) unknown.current = { kind, key }; setError(`${e.message || "操作失败"}${unknown.current ? "；结果未知，请核对服务端记录，禁止重复提交" : ""}`); }
    finally { lock.current = false; if (version === epoch.current) setBusy(false); }
  }
  const nodes: StudioRecord[] = Array.isArray(project?.document.nodes) ? project!.document.nodes : [];
  const node = nodes.find(n => n.id === selectedNode);
  const changeNode = (changes: StudioRecord) => edit({ ...project!.document, nodes: nodes.map(n => n.id === selectedNode ? { ...n, ...changes } : n) });
  const modelPicker = (kind: string, value: string, update: (id: string) => void) => {
    const models = catalog.models.filter(m => m.kind === kind && m.enabled !== false);
    return <View key={kind}><Text className="mp-field-label">{({ text: "文本模型", image: "图片模型", video: "视频模型" } as any)[kind]}</Text><Picker disabled={busy} range={models.map(m => m.name)} value={Math.max(0, models.findIndex(m => String(m.id) === value))} onChange={e => update(String(models[Number(e.detail.value)]?.id || ""))}><View className="mp-input">{models.find(m => String(m.id) === value)?.name || "暂无可用模型"}</View></Picker></View>;
  };
  async function generate(mode: "image" | "image_edit" | "video" | "image_to_video") {
    if (!project || lock.current) return;
    if (pendingStudioRun()) { setError(`上次生成结果未知：${pendingStudioRun()}。请刷新核对运行回执，禁止重复提交`); return; }
    if (!catalog.enabled) { setError("Studio 当前未开放生成能力"); return; }
    if (mode === "image_edit" && !catalog.models.find(m => String(m.id) === imageModel)?.capabilities?.edit) { setError("所选模型没有声明 edit 能力"); return; }
    await confirmThen("确认媒体生成", "将调用真实模型，可能产生费用。确认按当前要求提交一次？", async ticket => {
    const key = `agent-${project.id}-${uid("run")}`;
    let payload: StudioRecord;
    try { payload = buildStudioRun({ mode, instruction: prompt, imageModelId: imageModel, videoModelId: videoModel, assetId: mode === "image" || mode === "image_to_video" ? "" : assetId, requestKey: key, confirmed: true }); } catch (e: any) { setError(e.message); return; }
    await write("run", async () => {
      const run = await client.submitRun(payload); setCatalog(c => ({ ...c, runs: [run, ...c.runs.filter(r => r.id !== run.id)] }));
      const document = { ...project.document, events: [...(project.document.events || []), { id: uid("event"), type: "generation_submitted", runId: run.id, requestKey: key, createdAt: new Date().toISOString() }] };
      acceptProject(await client.saveProject({ ...project, document }));
      setNotice(`运行已提交：${run.id}；状态 ${run.status || "待读取"}，不代表生成完成。`);
    }, key, ticket);
    });
  }
  async function previewAsset(asset: StudioRecord) {
    const version = ++epoch.current; setError("");
    try { const loaded = await loadAuthenticatedMedia(`/api/studio/assets/${encodeURIComponent(asset.id)}/content`, asset.kind === "video" ? "mp4" : "png"); if (version === epoch.current) setMedia({ kind: asset.kind, path: loaded.path }); } catch (e: any) { if (version === epoch.current) setError(e.message || "素材读取失败"); }
  }
  function addAsset(asset: StudioRecord) {
    if (!project) return; const id = uid("version");
    edit({ ...project.document, nodes: [...nodes, { id: uid("node"), type: asset.kind === "video" ? "video" : "image", title: "服务端素材", x: 180, y: 160, w: 300, h: 320, z: nodes.length + 1, assetId: asset.id, currentVersionId: id, versions: [{ id, assetId: asset.id, src: "", label: "素材库", createdAt: new Date().toISOString() }] }] });
  }
  async function openLibrary(kind: "brand-kits" | "skills", id: string) {
    const version = lock.current ? epoch.current : ++epoch.current;
    try { const r = await request<StudioRecord>(`/api/studio/agent/${kind}/${encodeURIComponent(id)}`); if (version === epoch.current) setLibrary({ kind, id: r.id, name: r.name, revision: r.revision, document: JSON.stringify(r.document || {}, null, 2) }); } catch (e: any) { if (version === epoch.current) setError(e.message); if (lock.current) throw e; }
  }
  return <View className="mp-page mp-detail-page"><AmbientStarfield /><BrandHeader eyebrow="Agent" title="Agent Studio" showBack /><View className="mp-section">
    <View className="mp-function-hero"><AppIcon name="bot" size={23} /><Text>Studio 项目与创作画布</Text></View>{error ? <Text className="mp-error">{error}</Text> : null}{notice ? <Text className="mp-row-description">{notice}</Text> : null}
    <View className="mp-card"><Text className="mp-row-title">服务端项目</Text><Text className="mp-row-description">需要合法 Cookie。未保存编辑仅在当前页面保留。</Text>{projects.map(p => <Button disabled={busy} key={p.id} onClick={() => readProject(p.id)}>打开{p.name || p.id}</Button>)}<Input maxlength={120} disabled={busy} className="mp-input" value={name} onInput={e => setName(e.detail.value)} placeholder="新项目名称" /><Button disabled={busy || !name.trim()} onClick={() => { const id = uid("project"); return write("project", async () => { acceptProject(await client.createProject(name, id)); setName(""); setProjects(await client.projects()); }, id); }}>新建项目</Button><Button disabled={busy} onClick={load}>刷新 Studio</Button></View>
    {catalog.errors.length ? <Text className="mp-error">{catalog.errors.join("；")}</Text> : null}
    {project ? <>
      <View className="mp-card"><Text className="mp-row-title">{project.name} · 版本 {project.revision}{dirty ? " · 未保存" : ""}</Text><Input maxlength={120} disabled={busy} className="mp-input" value={project.name} onInput={e => { epoch.current++; setProject({ ...project, name: e.detail.value }); setDirty(true); }} /><Button disabled={busy} onClick={() => write("project", async () => { acceptProject(await client.saveProject(project)); setNotice("项目已保存并读回"); }, project.id)}>保存项目并读回</Button><Button disabled={busy} onClick={() => readProject(project.id)}>重新读取当前项目</Button><Button disabled={busy} onClick={async () => { await confirmThen("删除项目", "确认删除服务端项目？", ticket => write("project", async () => { setProjects(await client.removeProject(project.id)); setProject(null); }, project.id, ticket)); }}>删除项目</Button></View>
      <View className="mp-card"><Text className="mp-row-title">画布节点</Text>{nodes.map(n => <Button key={n.id} disabled={busy} onClick={() => setSelectedNode(n.id)}>{n.title || n.type} {selectedNode === n.id ? "（当前）" : ""}</Button>)}<Button disabled={busy} onClick={() => edit({ ...project.document, nodes: [...nodes, { id: uid("node"), type: "text", title: "文本", text: "", x: 180, y: 160, w: 320, h: 160, z: nodes.length + 1, visible: true, locked: false, versions: [] }] })}>添加文本节点</Button>{node ? <><Input maxlength={256} disabled={busy} value={node.title || ""} onInput={e => changeNode({ title: e.detail.value })} /><Textarea maxlength={-1} disabled={busy} className="mp-textarea" value={node.text || node.content || ""} onInput={e => changeNode({ text: e.detail.value })} /><Button disabled={busy} onClick={() => changeNode({ visible: node.visible === false })}>{node.visible === false ? "显示节点" : "隐藏节点"}</Button><Button disabled={busy} onClick={() => changeNode({ locked: !node.locked })}>{node.locked ? "解锁节点" : "锁定节点"}</Button><Button disabled={busy} onClick={() => edit({ ...project.document, nodes: nodes.filter(n => n.id !== selectedNode) })}>移除当前节点</Button></> : null}<Button disabled={busy} onClick={() => setShowDocument(!showDocument)}>完整项目文档编辑</Button>{showDocument ? <><Textarea maxlength={-1} disabled={busy} className="mp-textarea" value={rawDocument} onInput={e => setRawDocument(e.detail.value)} /><Button disabled={busy} onClick={() => { try { const d = JSON.parse(rawDocument); if (!d || Array.isArray(d) || !Array.isArray(d.nodes)) throw new Error("文档须为包含 nodes 数组的对象"); edit(d); } catch (e: any) { setError(e.message); } }}>应用文档草稿</Button></> : null}</View>
      <View className="mp-card"><Text className="mp-row-title">创作规划 / 媒体生成</Text>{modelPicker("text", textModel, setTextModel)}{modelPicker("image", imageModel, setImageModel)}{modelPicker("video", videoModel, setVideoModel)}<Textarea maxlength={-1} disabled={busy} className="mp-textarea" value={prompt} onInput={e => setPrompt(e.detail.value)} placeholder="输入创作要求" /><Button disabled={busy || !prompt.trim()} onClick={() => write("plan", async () => { const selected = node ? [{ id: node.id, type: node.type, title: node.title || node.type, asset_id: node.assetId || "" }] : []; setPlan(await client.plan({ ...buildAgentPlanRequest(textModel, prompt, project.name), selected })); })}>生成规划（不执行媒体）</Button>{plan ? <><Text>{plan.reply || "服务端未返回规划回复"}</Text><Textarea maxlength={-1} className="mp-result" disabled value={JSON.stringify(plan.actions || [], null, 2)} /></> : null}<Text className="mp-row-description">规划操作需审阅，媒体单独确认。</Text><Text>首帧 / 编辑素材：{assetId || "未选择"}</Text><Button disabled={busy || !catalog.enabled} onClick={() => generate("image")}>确认生成图片</Button><Button disabled={busy || !catalog.enabled || !assetId} onClick={() => generate("image_edit")}>确认编辑图片</Button><Button disabled={busy || !catalog.enabled || !assetId} onClick={() => generate("video")}>确认图生视频</Button><Button disabled={busy || !catalog.enabled} onClick={() => generate("image_to_video")}>确认先图后视频</Button></View>
      <View className="mp-card"><Text className="mp-row-title">项目历史对话</Text>{(project.document.chats || []).map((c: StudioRecord) => <View key={c.id}><Text>{c.title}</Text>{(c.messages || []).map((m: StudioRecord, i: number) => <Text key={m.id || i} className="mp-row-description">{m.role}：{String(m.content || "")}</Text>)}</View>)}</View>
    </> : null}
    <View className="mp-card"><Text className="mp-row-title">素材库</Text>{catalog.assets.map(a => <View key={a.id}><Text>{a.kind} · {a.id} · {a.bytes ?? "?"} 字节</Text><Button disabled={busy} onClick={() => previewAsset(a)}>查看素材</Button>{project ? <Button disabled={busy} onClick={() => addAsset(a)}>加入当前画布</Button> : null}{a.kind === "image" ? <Button disabled={busy} onClick={() => setAssetId(a.id)}>用作首帧 / 编辑输入</Button> : null}</View>)}{media ? media.kind === "video" ? <Video src={media.path} controls onError={() => setError("视频播放失败")} /> : <Image src={media.path} mode="widthFix" onError={() => setError("图片展示失败")} /> : null}<Button disabled={busy} onClick={async () => { await confirmThen("上传素材", "选择图片上传至当前账号 Studio 素材库？", async ticket => { try { const picked = await Taro.chooseImage({ count: 1 }); if (!valid(ticket)) return; const data = await new Promise<ArrayBuffer>((resolve, reject) => Taro.getFileSystemManager().readFile({ filePath: picked.tempFilePaths[0], success: r => typeof r.data === "string" ? reject(new Error("图片须为二进制内容")) : resolve(r.data), fail: reject })); if (!valid(ticket)) return; await write("asset", async () => { await request("/api/studio/assets", { method: "POST", data, header: { "content-type": "application/octet-stream" } }); await load(); }, uid("asset"), ticket); } catch (e: any) { if (valid(ticket)) setError(e.message || "选择取消或读取失败"); } }); }}>上传图片到素材库</Button></View>
    <View className="mp-card"><Text className="mp-row-title">账号全部 Studio 运行</Text>{taskId ? <Text>从任务页打开：{taskId}</Text> : null}{catalog.runs.map(r => <View key={r.id}><Text>{r.id}{r.id === taskId ? "（当前查看）" : ""} · {r.status || "未知状态"}</Text><Text className="mp-row-description">{r.request?.instruction || ""}{r.error_code ? ` · ${r.error_code}` : ""}</Text>{(r.assets || []).map((a: StudioRecord) => <Button disabled={busy} key={a.id} onClick={() => previewAsset(a)}>查看运行素材 {a.id}</Button>)}</View>)}<Text className="mp-row-description">运行回执不代表完成，素材读取与播放分别核对。</Text></View>
    <View className="mp-card"><Text className="mp-row-title">Brand Kit / Skills</Text>{([['brand-kits', catalog.brandKits], ['skills', catalog.skills]] as const).map(([kind, values]) => <View key={kind}><Text>{kind}</Text>{values.map(v => <Button disabled={busy} key={v.id} onClick={() => openLibrary(kind, v.id)}>{v.name || v.id}</Button>)}<Button disabled={busy} onClick={() => setLibrary({ kind, name: "", document: "{}" })}>新建 {kind}</Button></View>)}{library ? <><Input maxlength={120} disabled={busy} value={library.name} onInput={e => setLibrary({ ...library, name: e.detail.value })} placeholder="名称" /><Textarea maxlength={-1} disabled={busy} className="mp-textarea" value={library.document} onInput={e => setLibrary({ ...library, document: e.detail.value })} /><Button disabled={busy || !library.name.trim()} onClick={async () => { let document: StudioRecord; try { document = JSON.parse(library.document); if (!document || typeof document !== "object" || Array.isArray(document)) throw new Error("文档必须为对象"); } catch (e: any) { setError(e.message); return; } await write("library", async () => { const r = await request<StudioRecord>(`/api/studio/agent/${library.kind}${library.id ? `/${encodeURIComponent(library.id)}` : ""}`, { method: library.id ? "PUT" : "POST", data: { ...(library.id ? { id: library.id, revision: library.revision } : {}), name: library.name, document } }); await openLibrary(library.kind, r.id); await load(); }); }}>保存资料并读回</Button>{project && library.id ? <Button disabled={busy} onClick={() => edit({ ...project.document, [library.kind === "skills" ? "skillId" : "brandKitId"]: library.id })}>绑定当前项目（请保存）</Button> : null}</> : null}</View>
  </View></View>;
}
export default withThemedPage(AgentWorkspace);
