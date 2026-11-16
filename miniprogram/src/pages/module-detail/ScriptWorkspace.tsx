import Taro, { useDidShow } from "@tarojs/taro";
import { Button, Input, Picker, ScrollView, Text, Textarea, View } from "@tarojs/components";
import { useEffect, useRef, useState } from "react";
import BrandHeader from "../../components/BrandHeader";
import AppIcon from "../../components/AppIcon";
import ScriptVideoProduction from "./ScriptVideoProduction";
import ScriptPersonalPrompts from "./ScriptPersonalPrompts";
import ScriptEntityMedia from "./ScriptEntityMedia";
import { apiRequest } from "../../services/api";
import { readTtsOptions, synthesizeSpeech } from "../../services/tts";
import { buildExtractRequest, buildGenerateRequest, buildHistoryEntry, normalizeScriptEntities, parseScriptOutput, restoreScriptHistory, scriptConstraintChoices, updateScriptConstraint, scriptEntityKey, ScriptEntity, ScriptWorkspaceState, ScriptConstraints } from "../../services/scriptWorkspace";
import { withThemedPage } from "../../theme";
import './script-studio.scss';

const OPENINGS = ["连续开头", "爆款开头", "分段开头"];
const FORMATS = ["画布", "剧本", "剧情", "分镜", "Q版"];
const DURATIONS = ["10 秒", "15 秒"];
const LAYERS = [["baseSetup", "基本设定"], ["prefix", "视觉前缀"], ["quality", "质量要求"], ["restriction", "限制规则"], ["negative", "负面约束"]] as const;
type Option = { id: string; name: string; constraintCategory?: string; kind?: string; extractionPreset?: boolean };
const emptyState: ScriptWorkspaceState = { novelText: "", characters: [], scenes: [], opening: "分段开头", format: "分镜", duration: "15 秒", matchAudio: false, constraints: {}, protagonists: [] };

function entityName(entity: any, fallback: string) {
  const data = entity?.data || entity;
  return String(data?.name || data?.角色名称 || data?.场景名称 || data?.名称 || entity?.name || fallback);
}
function options(payload: any): Option[] {
  const source = Array.isArray(payload) ? payload : payload?.models || payload?.entries || payload?.catalog;
  if (!Array.isArray(source)) throw new Error("服务端未返回有效选项目录");
  return source.filter((item) => item?.id || item?.key).map((item) => ({ ...item, id: String(item.id || item.key), name: String(item.name || item.label || item.id || item.key) }));
}

function ScriptWorkspace({ historyId = "" }: { historyId?: string }) {
  const [state, setState] = useState<ScriptWorkspaceState>(emptyState);
  const stateRef = useRef(state); stateRef.current = state;
  const [tab, setTab] = useState("编辑");
  const [models, setModels] = useState<Option[]>([]);
  const [imageModels, setImageModels] = useState<Option[]>([]);
  const [savedHistoryId, setSavedHistoryId] = useState("");
  const [personalCategory, setPersonalCategory] = useState("");
  const [catalog, setCatalog] = useState<Option[]>([]);
  const [catalogError, setCatalogError] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState("");
  const [result, setResult] = useState("");
  const [showConstraints, setShowConstraints] = useState(false);
  const [entityDraft, setEntityDraft] = useState<{ characters: ScriptEntity[]; scenes: ScriptEntity[] }>({ characters: [], scenes: [] });
  const [editingEntities, setEditingEntities] = useState(false);
  const [sourceExpanded, setSourceExpanded] = useState(true);
  const [showAudio, setShowAudio] = useState(false);
  const [audioPlaying, setAudioPlaying] = useState(false);
  const audio = useRef<Taro.InnerAudioContext | null>(null);
  const audioFile = useRef("");
  const version = useRef(0);
  const catalogVersion = useRef(0);
  const restored = useRef("");
  const patch = (next: Partial<ScriptWorkspaceState>) => setState((current) => ({ ...current, ...next }));
  const clearAudio = () => {
    audio.current?.destroy(); audio.current = null;
    if (audioFile.current) Taro.getFileSystemManager().unlink({ filePath: audioFile.current, fail: () => undefined });
    audioFile.current = ""; setAudioPlaying(false);
  };
  useEffect(() => () => { version.current += 1; catalogVersion.current += 1; clearAudio(); }, []);
  const loadCatalog = async () => {
    const current = ++catalogVersion.current;
    setCatalogError("");
    try {
      const [modelPayload, presetPayload] = await Promise.all([apiRequest("/api/models?kind=text"), apiRequest("/api/presets?module=script")]);
      if (current !== catalogVersion.current) return;
      const nextModels = options(modelPayload); const nextCatalog = options(presetPayload);
      setModels(nextModels); setCatalog(nextCatalog);
      void apiRequest("/api/models?kind=image").then(payload => { if (current === catalogVersion.current) setImageModels(options(payload).filter((item: any) => item.enabled !== false)); }).catch(cause => { if (current === catalogVersion.current) setCatalogError(`图片模型读取失败：${cause.message}`); });
      setState((value) => ({ ...value, textModelId: value.textModelId || nextModels[0]?.id || "", extractionPreset: value.extractionPreset || nextCatalog.find((item) => item.kind === "base" && item.extractionPreset)?.id || "" }));
    } catch (cause: any) { if (current === catalogVersion.current) setCatalogError(cause.message || "生成选项读取失败"); }
  };
  useDidShow(() => { void loadCatalog(); });
  useEffect(() => {
    if (!historyId || restored.current === historyId) return;
    const current = ++version.current;
    setBusy("恢复历史"); setError(""); setState(emptyState); setResult("");
    void apiRequest(`/api/history/${encodeURIComponent(historyId)}`).then((payload) => {
      if (current !== version.current) return;
      const next = restoreScriptHistory(payload); setState(next); setResult(next.output); setSavedHistoryId(historyId); setTab(next.output ? "结果" : "编辑"); restored.current = historyId;
    }).catch((cause) => { if (current === version.current) setError(cause.message || "历史恢复失败"); })
      .finally(() => { if (current === version.current) setBusy(""); });
    return () => { version.current += 1; };
  }, [historyId]);

  const extract = async () => {
    if (busy) return;
    if (!state.novelText.trim() || !state.extractionPreset) { setError("请填写原文并选择服务端提取模板"); return; }
    const current = ++version.current; setBusy("提取人物场景"); setError("");
    try {
      const payload = await apiRequest("/api/chat", { method: "POST", timeout: 180000, data: buildExtractRequest(state) });
      if (current !== version.current) return;
      const entities = normalizeScriptEntities(payload);
      if (!entities.characters.length && !entities.scenes.length) throw new Error("模型未返回人物或场景");
      patch({ ...entities, protagonists: [], videoTasks: {} }); setResult(""); setSavedHistoryId(""); setSourceExpanded(false);
    } catch (cause: any) { if (current === version.current) setError(cause.message || "提取失败"); }
    finally { if (current === version.current) setBusy(""); }
  };
  const generate = async () => {
    if (busy) return;
    if (!state.novelText.trim() || (!state.characters.length && !state.scenes.length) || !state.textModelId) { setError("请先填写原文、提取人物场景并选择模型"); return; }
    if (state.matchAudio && !state.audioTotalSeconds) { setError("请先为当前原文生成配音"); return; }
    const current = ++version.current; setBusy("生成剧本"); setError("");
    try {
      let input = { ...state, constraints: JSON.parse(JSON.stringify(state.constraints || {})) as ScriptConstraints };
      if (input.constraints.prefix?.enabled && input.constraints.prefix.source === "system" && input.constraints.prefix.presetId === "script-constraint-prefix-smart-unified") {
        const style = await apiRequest<any>("/api/script/smart-unified-style", { method: "POST", timeout: 180000, data: { novelText: input.novelText, characters: input.characters, scenes: input.scenes } });
        if (current !== version.current) return;
        const prompt = style?.smartUnifiedStyle?.prompt;
        if (typeof prompt !== "string" || !prompt.trim()) throw new Error("智能统一视觉未返回有效风格");
        input = { ...input, visualStyle: prompt, constraints: { ...input.constraints, prefix: { ...input.constraints.prefix, smartUnifiedStyle: prompt } } };
      }
      const payload = await apiRequest("/api/chat", { method: "POST", timeout: 180000, data: buildGenerateRequest(input) });
      if (current !== version.current) return;
      const output = parseScriptOutput(payload); patch({ videoTasks: {} }); setResult(output); setSavedHistoryId(""); setTab("结果");
      try {
        const entry = buildHistoryEntry(input, output);
        await apiRequest("/api/history", { method: "POST", data: entry });
        const readback = await apiRequest<any>(`/api/history/${encodeURIComponent(entry.id)}`);
        if ((readback?.entry || readback)?.id !== entry.id) throw new Error("历史保存后的编号读回不一致");
        if (current === version.current) { setSavedHistoryId(entry.id); Taro.showToast({ title: "剧本已生成并保存", icon: "success" }); }
      } catch (cause: any) { if (current === version.current) setError(`剧本已生成，历史保存失败：${cause.message}`); }
    } catch (cause: any) { if (current === version.current) setError(cause.message || "生成失败"); }
    finally { if (current === version.current) setBusy(""); }
  };
  const generateVoice = async () => {
    if (busy || !state.novelText.trim()) return;
    const current = ++version.current; setBusy("生成原文配音"); setError(""); clearAudio(); patch({ audioTotalSeconds: null, matchAudio: false });
    try {
      const options = await readTtsOptions();
      if (current !== version.current) return;
      const filePath = await synthesizeSpeech(state.novelText.trim(), options);
      if (current !== version.current) return;
      audioFile.current = filePath;
      if (current !== version.current) { clearAudio(); return; }
      const context = Taro.createInnerAudioContext(); audio.current = context;
      const seconds = await new Promise<number>((resolve, reject) => {
        let interval: ReturnType<typeof setInterval> | undefined;
        let settled = false;
        const finish = (failure?: Error, duration?: number) => { if (settled) return; settled = true; clearTimeout(deadline); clearInterval(interval); failure ? reject(failure) : resolve(duration!); };
        const deadline = setTimeout(() => finish(new Error("配音时长读取超时，请重试")), 12000);
        context.onCanplay(() => { if (settled || interval) return; interval = setInterval(() => { if (context.duration > 0) finish(undefined, Math.ceil(context.duration)); }, 100); });
        context.onError(() => finish(new Error("配音文件无法播放"))); context.src = filePath;
      });
      if (current !== version.current) return;
      context.onEnded(() => setAudioPlaying(false)); context.onPause(() => setAudioPlaying(false));
      patch({ audioTotalSeconds: seconds });
    } catch (cause: any) { if (current === version.current) { clearAudio(); setError(cause.message || "原文配音失败"); } }
    finally { if (current === version.current) setBusy(""); }
  };

  const pick = (label: string, list: string[], value: string, select: (value: string) => void) => <Picker disabled={Boolean(busy) || !list.length} range={list} value={Math.max(0, list.indexOf(value))} onChange={(event) => { const selected = list[Number(event.detail.value)]; if (!busy && selected !== undefined) select(selected); }}><View className="studio-setting"><Text className="studio-setting-label">{label}</Text><View className="studio-setting-value"><Text>{value || (list.length ? "请选择" : "暂无可用选项")}</Text><AppIcon name="chevron-right" size={12} /></View></View></Picker>;
  const presets = catalog.filter((item) => item.kind === "base" && item.extractionPreset);
  const constraintCount = LAYERS.filter(([key]) => state.constraints?.[key]?.enabled).length;
  const hasEntities = Boolean(state.characters.length || state.scenes.length);
  const closePanel = () => { setEditingEntities(false); setShowConstraints(false); setShowAudio(false); setPersonalCategory(""); };
  const openEntities = () => { setEntityDraft(JSON.parse(JSON.stringify({ characters: state.characters, scenes: state.scenes }))); setEditingEntities(true); };
  const readiness = !state.novelText.trim() ? "先放入你的小说原文" : !state.textModelId ? "请选择可用文本模型" : !hasEntities && !state.extractionPreset ? "请选择人物场景提取模板" : !hasEntities ? "先提取人物与场景，再开始创作" : state.matchAudio && !state.audioTotalSeconds ? "请先生成当前原文的配音" : "人物场景已就绪，可以开始创作";
  const actionDisabled = Boolean(busy) || !state.novelText.trim() || !state.textModelId || (!hasEntities && !state.extractionPreset) || (hasEntities && state.matchAudio && !state.audioTotalSeconds);
  return <View className="mp-page mp-script-page script-studio"><BrandHeader eyebrow="剧本生成" title="剧本生成" showBack />
    <View className="mp-script-console">
      <View className="studio-titlebar"><View><Text className="studio-eyebrow">YOUR NEXT STORY</Text><View className="studio-title"><AppIcon name="file-pen-line" size={20} /><Text>故事，从这里开始</Text></View></View><Button className="studio-history" aria-label="历史记录" onClick={() => Taro.switchTab({ url: "/pages/tasks/index" })}><AppIcon name="folder-clock" size={19} /></Button></View>
      <View className="studio-tabs">{["编辑", "结果"].map((item) => <Button key={item} className={tab === item ? "studio-tab is-active" : "studio-tab"} disabled={Boolean(busy)} onClick={() => setTab(item)}>{item === "编辑" ? "创作" : "结果"}{item === "结果" && result ? <Text className="studio-result-dot" /> : null}</Button>)}</View>
      {error ? <Text className="mp-error">{error}</Text> : null}{busy ? <Text className="mp-row-description">正在{busy}…</Text> : null}
      {tab === "编辑" ? <View className="mp-script-editor">
        <View className="studio-paper"><View className="studio-paper-head"><Text>小说原文</Text><Text className="studio-paper-tag">{state.novelText.trim() ? "当前创作" : "等待灵感"}</Text></View>
          {sourceExpanded ? <Textarea maxlength={-1} className="studio-source-input" disabled={Boolean(busy)} value={state.novelText} onInput={(event) => { if (busy) return; version.current += 1; clearAudio(); patch({ novelText: event.detail.value, characters: [], scenes: [], protagonists: [], audioTotalSeconds: null, matchAudio: false, videoTasks: {} }); setResult(""); setSavedHistoryId(""); }} placeholder="粘贴小说原文" /> : <Text className="studio-source-excerpt">{state.novelText.trim() || "还没有原文，展开后开始输入。"}</Text>}
          <View className="studio-paper-foot"><Text>{state.novelText.length.toLocaleString()} 字</Text><Button className="studio-link" disabled={Boolean(busy)} onClick={() => setSourceExpanded(!sourceExpanded)}>{sourceExpanded ? "收起原文" : "展开编辑"}</Button></View></View>
        {catalogError ? <View className="mp-card"><Text className="mp-error">{catalogError}</Text><Button onClick={loadCatalog}>重试选项</Button></View> : null}
        <View className="studio-section-head"><Text>创作设定</Text><Text className="studio-caption">按你的方式讲故事</Text></View>
        <View className="studio-settings">{pick("文本模型", models.map((item) => item.name), models.find((item) => item.id === state.textModelId)?.name || "", (name) => patch({ textModelId: models.find((item) => item.name === name)?.id }))}{pick("提取模板", presets.map((item) => item.name), presets.find((item) => item.id === state.extractionPreset)?.name || "", (name) => patch({ extractionPreset: presets.find((item) => item.name === name)?.id }))}{pick("图片模型", imageModels.map(item => item.name), imageModels.find(item => item.id === state.imageModelId)?.name || "", name => patch({ imageModelId: imageModels.find(item => item.name === name)?.id }))}{pick("开头模式", OPENINGS, state.opening, (opening) => patch({ opening }))}{pick("输出模式", FORMATS, state.format, (format) => patch({ format }))}{pick("时长上限", DURATIONS, state.duration, (duration) => patch({ duration }))}</View>
        <View className="studio-section-head"><Text>人物与场景</Text>{hasEntities ? <Button className="studio-link" disabled={Boolean(busy)} onClick={openEntities}>查看与编辑</Button> : <Text className="studio-caption">提取后在这里查看</Text>}</View>
        {hasEntities ? <><ScrollView scrollX className="studio-entities"><View className="studio-entity-track">{[...state.characters.map(entity => ({ entity, type: "character" })), ...state.scenes.map(entity => ({ entity, type: "scene" }))].map(({ entity, type }, index) => <Button key={`${type}-${index}`} className="studio-entity" disabled={Boolean(busy)} onClick={openEntities}><View className={`studio-entity-icon ${type}`}><AppIcon name={type === "character" ? "user-round" : "book-open"} size={25} /></View><Text>{entityName(entity, `${type === "character" ? "人物" : "场景"} ${index + 1}`)}</Text></Button>)}</View></ScrollView><View className="studio-focus-list">{state.characters.map((entity, index) => { const id = scriptEntityKey(entity, index); const focused = state.protagonists?.includes(id); return <Button className={focused ? "studio-focus is-focused" : "studio-focus"} disabled={Boolean(busy)} key={id} onClick={() => patch({ protagonists: focused ? state.protagonists?.filter((item) => item !== id) : [...(state.protagonists || []), id] })}>{focused ? "已聚焦 · " : "聚焦 · "}{entityName(entity, `人物 ${index + 1}`)}</Button>; })}</View><Button className="studio-link studio-reextract" disabled={Boolean(busy)} onClick={extract}>重新提取</Button></> : <View className="studio-entity-empty"><AppIcon name="users-round" size={22} /><View><Text>故事里的世界，还未展开</Text><Text className="studio-caption">填写原文后，点击下方提取人物场景。</Text></View></View>}
        <View className="studio-entry-list"><Button className="studio-entry" disabled={Boolean(busy)} onClick={() => setShowConstraints(true)}><View><AppIcon name="sliders" size={17} /><Text>约束与提示词</Text></View><View><Text className="studio-caption">已开启 {constraintCount} 项</Text><AppIcon name="chevron-right" size={14} /></View></Button><Button className="studio-entry" disabled={Boolean(busy)} onClick={() => setShowAudio(true)}><View><AppIcon name="volume" size={17} /><Text>原文配音</Text></View><View><Text className="studio-caption">{state.audioTotalSeconds ? `${state.audioTotalSeconds} 秒` : "未生成"} · 匹配{state.matchAudio ? "开" : "关"}</Text><AppIcon name="chevron-right" size={14} /></View></Button></View>
      </View> : <View className="mp-script-results"><View className="studio-result-header"><Text className="studio-eyebrow">SCREENPLAY</Text><Text className="studio-result-title">{result ? "故事，有了下一幕" : "等待你的第一幕"}</Text><Text className="studio-caption">{result ? savedHistoryId ? "已保存到历史记录" : "历史尚未保存，请留意上方提示" : "完成原文与人物场景后，即可开始生成。"}</Text></View><Text selectable className="studio-result-text">{result || "还没有生成结果"}</Text><Button className="studio-copy mp-secondary" disabled={!result} onClick={() => Taro.setClipboardData({ data: result })}>复制完整剧本</Button>{result ? <ScriptVideoProduction key={`${savedHistoryId}:${result}`} output={result} historyId={savedHistoryId} tasks={state.videoTasks || {}} onTasksChange={tasks => patch({ videoTasks: tasks })} duration={state.duration} entities={[...state.characters, ...state.scenes]} /> : null}</View>}
    </View>
    <View className="studio-dock"><View className="studio-dock-copy"><Text>{busy ? `正在${busy}…` : tab === "结果" ? "继续打磨你的故事" : hasEntities ? "让文字，长出画面" : "从故事里，发现人物"}</Text><Text className="studio-caption">{tab === "结果" ? "返回创作可调整设定" : readiness}</Text></View><Button className="studio-primary" loading={Boolean(busy)} disabled={tab === "结果" ? Boolean(busy) : actionDisabled} onClick={() => { if (tab === "结果") setTab("编辑"); else if (hasEntities) return generate(); else return extract(); }}>{tab === "结果" ? "返回编辑" : hasEntities ? "生成剧本" : "提取人物场景"}</Button></View>
    {editingEntities || showConstraints || showAudio ? <View className="studio-overlay"><View className="studio-backdrop" onClick={() => { if (!busy) closePanel(); }} onTouchMove={event => event.stopPropagation()} /><View className="studio-sheet"><View className="studio-sheet-handle" /><View className="studio-sheet-header"><Text>{editingEntities ? "人物与场景" : showConstraints ? "约束与提示词" : "原文配音"}</Text><Button className="studio-link" disabled={Boolean(busy)} onClick={closePanel}>{editingEntities ? "取消修改" : "完成"}</Button></View><ScrollView scrollY className="studio-sheet-scroll"><View className="studio-sheet-body">
      {editingEntities ? <><Text className="studio-caption">修改仅在应用后生效；参考图片会在生成剧本时随历史保存。</Text><ScriptEntityEditor value={entityDraft} onChange={setEntityDraft} model={state.imageModelId || ""} novel={state.novelText} preset={state.extractionPreset || ""} /><Button className="studio-primary studio-apply" onClick={() => { patch({ ...entityDraft, protagonists: (state.protagonists || []).filter(id => entityDraft.characters.some((entity, index) => scriptEntityKey(entity, index) === id)), videoTasks: {} }); setResult(""); setSavedHistoryId(""); setEditingEntities(false); }}>应用修改</Button></> : null}
      {showConstraints ? <><Text className="mp-field-label">统一视觉风格</Text><Input className="mp-input" value={state.visualStyle || ""} onInput={(event) => patch({ visualStyle: event.detail.value })} />{LAYERS.map(([key, label]) => { const layer = state.constraints?.[key] || {}; const update = (next: object) => setState((current) => ({ ...current, constraints: updateScriptConstraint(current.constraints || {}, key, next) })); const choices = scriptConstraintChoices(catalog, key); return <View className="studio-constraint-layer" key={key}><Button className="studio-layer-toggle" onClick={() => update({ enabled: !layer.enabled })}><Text>{label}</Text><Text className={layer.enabled ? "studio-toggle is-on" : "studio-toggle"}>{layer.enabled ? "已开启" : "已关闭"}</Text></Button>{pick("系统模板", choices.map((item) => item.name), choices.find((item) => item.id === layer.presetId)?.name || "", (name) => update({ source: "system", presetId: choices.find((item) => item.name === name)?.id, enabled: true }))}<Textarea maxlength={-1} className="mp-textarea" value={layer.body || ""} placeholder="填写本次约束；填写后以自定义内容生成" onInput={(event) => update({ body: event.detail.value, source: "draft", presetId: "", enabled: Boolean(event.detail.value.trim()) })} />{key !== "baseSetup" ? <><Button className="studio-link" onClick={() => setPersonalCategory(personalCategory === key ? "" : key)}>我的{label}提示词</Button>{personalCategory === key ? <ScriptPersonalPrompts key={key} category={key} onSelect={prompt => { update({ source: "personal", personalPromptId: prompt.id, presetId: "", body: prompt.body, enabled: true }); setPersonalCategory(""); }} /> : null}</> : null}</View>; })}</> : null}
      {showAudio ? <View className="studio-audio"><AppIcon name="volume" size={32} /><Text className="studio-result-title">让故事拥有声音</Text><Text className="studio-caption">{state.audioTotalSeconds ? `${state.audioTotalSeconds} 秒${audio.current ? "" : " · 历史时长，需重新生成才能播放"}` : "生成后可播放原文配音，并用真实时长匹配分镜。"}</Text><Button className="studio-primary studio-apply" disabled={Boolean(busy) || !state.novelText.trim()} loading={busy === "生成原文配音"} onClick={generateVoice}>生成原文配音</Button>{audio.current ? <Button className="mp-secondary" onClick={() => { if (audioPlaying) audio.current?.pause(); else audio.current?.play(); setAudioPlaying(!audioPlaying); }}>{audioPlaying ? "暂停" : "播放配音"}</Button> : null}<Button className="studio-layer-toggle" disabled={Boolean(busy) || !state.audioTotalSeconds} onClick={() => state.audioTotalSeconds && patch({ matchAudio: !state.matchAudio })}><Text>匹配音频</Text><Text className={state.matchAudio ? "studio-toggle is-on" : "studio-toggle"}>{state.matchAudio ? "已开启" : "已关闭"}</Text></Button></View> : null}
      {error ? <Text className="mp-error">{error}</Text> : null}
      </View></ScrollView></View></View> : null}
  </View>;
}
export default withThemedPage(ScriptWorkspace);

function ScriptEntityEditor({ value, onChange, model, novel, preset }: { value: { characters: ScriptEntity[]; scenes: ScriptEntity[] }; onChange: (value: { characters: ScriptEntity[]; scenes: ScriptEntity[] }) => void; model: string; novel: string; preset: string }) {
  const currentValue = useRef(value); currentValue.current = value;
  const labels: Record<string, string> = { name: "名称", description: "描述", appearance: "外形", voice: "声音", scene: "场景", style: "风格" };
  return <View>{(["characters", "scenes"] as const).map((type) => value[type].map((entity, index) => {
    const nested = entity.data && typeof entity.data === "object" && !Array.isArray(entity.data);
    const fields = (nested ? entity.data : entity) as Record<string, any>;
    return <View key={`${type}-${index}`}><Text className="mp-row-title">{type === "characters" ? "人物" : "场景"} · {entityName(entity, String(index + 1))}</Text>{Object.entries(fields).filter(([key, item]) => key !== "id" && typeof item === "string" && !/url|token|key|secret/i.test(key)).map(([key, text]) => <View key={key}><Text className="mp-field-label">{labels[key] || key}</Text><Textarea maxlength={-1} className="mp-textarea" value={text} onInput={(event) => onChange({ ...value, [type]: value[type].map((item, position) => position !== index ? item : nested ? { ...item, data: { ...fields, [key]: event.detail.value } } : { ...item, [key]: event.detail.value }) })} /></View>)}<ScriptEntityMedia type={type} entity={entity} model={model} novel={novel} preset={preset} onChange={next => { const source = currentValue.current; onChange({ ...source, [type]: source[type].map((item, position) => position === index ? { ...item, id: next.id || item.id, imageUrls: next.imageUrls, mainImageUrl: next.mainImageUrl } : item) }); }} /></View>;
  }))}</View>;
}
