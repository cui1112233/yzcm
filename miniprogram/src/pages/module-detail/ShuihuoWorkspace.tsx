import Taro, { useDidHide, useDidShow } from '@tarojs/taro';
import { Audio, Button, Image, Input, Text, Textarea, Video, View } from '@tarojs/components';
import { useEffect, useRef, useState } from 'react';
import AppIcon from '../../components/AppIcon';
import BrandHeader from '../../components/BrandHeader';
import AmbientStarfield from '../../components/AmbientStarfield';
import { apiRequest } from '../../services/api';
import { loadBatchFactoryWorks, type BatchFactoryBatch } from '../../services/batchFactoryV11';
import { loadAuthenticatedMedia } from '../../services/mediaCache';
import { createProductionClients, latestRequestGuard, type Row } from '../../services/productionClients';
import { confirmReviewedSegmentation, createSegmentationReview, sourceRevisionKey } from '../../services/segmentationReview';
import { withThemedPage } from '../../theme';
const client = createProductionClients(apiRequest);
export type ShuihuoWorkspaceProps = { projectId?: string };
function ShuihuoWorkspace({ projectId = '' }: ShuihuoWorkspaceProps) {
  const [projects, setProjects] = useState<Row[]>([]), [models, setModels] = useState<Row[]>([]), [selected, setSelected] = useState(projectId), [detail, setDetail] = useState<Row | null>(null);
  const [sourceText, setSourceText] = useState('');
  const [sourceEdit, setSourceEdit] = useState(''), [candidates, setCandidates] = useState<Row[]>([]);
  const review = useRef(createSegmentationReview());
  const selectedProject = useRef(selected);
  selectedProject.current = selected;
  const [candidateTicket, setCandidateTicket] = useState(0);
  const [segmentEdit, setSegmentEdit] = useState<Row | null>(null), [assetEdit, setAssetEdit] = useState<Row | null>(null);
  useEffect(() => { setSourceEdit(detail?.project.sourceText || ''); review.current.invalidate(); setCandidates([]); }, [detail?.project.id, detail?.project.sourceText, detail?.project.sourceRevision, detail?.project.revision]);
  const [name, setName] = useState(''), [segmentId, setSegmentId] = useState(''), [modelId, setModelId] = useState(''), [error, setError] = useState(''), [busy, setBusy] = useState(false);
  const [preview, setPreview] = useState<{ path: string; kind: string } | null>(null);
  const guard = useRef(latestRequestGuard()), listGuard = useRef(latestRequestGuard());
  const shown = useRef(false);
  const [catalogState, setCatalogState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [catalogError, setCatalogError] = useState(''), [modelsError, setModelsError] = useState('');
  const [batchWorks, setBatchWorks] = useState<BatchFactoryBatch[]>([]);
  const [batchState, setBatchState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [batchError, setBatchError] = useState('');
  useEffect(() => () => { guard.current.invalidate(); listGuard.current.invalidate(); }, []);
  useEffect(() => { if (projectId) setSelected(projectId); }, [projectId]);
  async function load() {
    const ticket = listGuard.current.begin();
    setCatalogState('loading'); setCatalogError(''); setModelsError(''); setModels([]);
    setBatchState('loading'); setBatchError(''); setBatchWorks([]);
    await Promise.all([
      loadBatchFactoryWorks({ request: apiRequest }).then(next => { if (listGuard.current.current(ticket)) { setBatchWorks(next); setBatchState('ready'); } })
        .catch(cause => { if (listGuard.current.current(ticket)) { setBatchState('error'); setBatchError(cause.message || '请重试'); } }),
      client.waterProjects().then(next => { if (listGuard.current.current(ticket)) { setProjects(next); setCatalogState('ready'); } })
        .catch(cause => { if (listGuard.current.current(ticket)) { setCatalogState('error'); setCatalogError(cause.message || '请重试'); } }),
      client.waterModels().then(next => { if (listGuard.current.current(ticket)) setModels(next); })
        .catch(cause => { if (listGuard.current.current(ticket)) setModelsError(cause.message || '请重试'); }),
    ]);
  }
  async function read() { const ticket = guard.current.begin(); setBusy(true); setError(''); try { const next = await client.waterDetail(selected); if (guard.current.current(ticket)) setDetail(next); } catch (cause: any) { if (guard.current.current(ticket)) setError(cause.message); } finally { if (guard.current.current(ticket)) setBusy(false); } }
  useDidShow(() => { void load(); if (shown.current && selected) void read(); shown.current = true; });
  useDidHide(() => { guard.current.invalidate(); listGuard.current.invalidate(); review.current.invalidate(); setCandidates([]); setBusy(false); });
  useEffect(() => { review.current.invalidate(); setDetail(null); setPreview(null); setSegmentId(''); setModelId(''); setCandidates([]); setSegmentEdit(null); setAssetEdit(null); setBusy(false); if (selected) void read(); return () => guard.current.invalidate(); }, [selected]);
  async function act(create: boolean) { const ticket = guard.current.begin(); setBusy(true); setError(''); try { if (create) { const project = await client.createWater(name, sourceText); if (guard.current.current(ticket)) { setProjects(current => [project, ...current]); setName(''); setSourceText(''); setSelected(String(project.id)); } } else { const segment = detail?.segments.find((row: Row) => String(row.id) === segmentId), model = models.find(row => String(row.id) === modelId); if (!segment?.confirmed || !model || model.enabled === false) throw new Error('请选择已确认分镜与可用模型'); await client.waterTask(selected, segmentId, modelId, model.kind); const next = await client.waterDetail(selected); if (guard.current.current(ticket)) setDetail(next); } } catch (cause: any) { if (guard.current.current(ticket)) setError(cause.message || '提交失败，结果尚未确认'); } finally { if (guard.current.current(ticket)) setBusy(false); } }
  async function mutate(operation: (ticket: number) => Promise<unknown>) {
    const ticket = guard.current.begin(); setBusy(true); setError('');
    try { await operation(ticket); if (!guard.current.current(ticket) || selectedProject.current !== selected) return; const next = await client.waterDetail(selected); if (guard.current.current(ticket)) { setDetail(next); setSegmentEdit(null); setAssetEdit(null); } }
    catch (cause: any) { if (guard.current.current(ticket)) setError(cause.message || '操作失败，保存结果尚未确认'); }
    finally { if (guard.current.current(ticket)) setBusy(false); }
  }
  async function previewSegments() {
    if (!detail || sourceEdit !== detail.project.sourceText) { setError('请先保存修改后的原文，再生成候选'); return; }
    const sourceKey = sourceRevisionKey(detail.project);
    const sourceTicket = review.current.begin(sourceKey);
    const ticket = guard.current.begin(); setBusy(true); setError(''); setCandidates([]);
    try { const next = await client.waterCandidates(selected); if (guard.current.current(ticket) && review.current.current(sourceTicket, sourceKey)) { setCandidates(next); setCandidateTicket(sourceTicket); } }
    catch (cause: any) { if (guard.current.current(ticket)) setError(cause.message || '候选分镜读取失败'); }
    finally { if (guard.current.current(ticket)) setBusy(false); }
  }
  function editSource(value: string) { review.current.invalidate(); setCandidates([]); setSourceEdit(value); }
  async function saveSource() {
    review.current.invalidate(); setCandidates([]);
    await mutate(() => client.saveWaterSource(selected, sourceEdit));
  }
  async function confirmSegments() {
    await mutate(async requestTicket => {
      if (!detail || sourceEdit !== detail.project.sourceText) throw new Error('原文已修改，请保存后重新生成候选');
      const key = sourceRevisionKey(detail.project);
      await confirmReviewedSegmentation({
        review: review.current, ticket: candidateTicket, sourceKey: key,
        read: () => client.waterDetail(selected) as Promise<{ project: Row }>,
        isCurrent: () => guard.current.current(requestTicket) && selectedProject.current === selected,
        submit: () => client.confirmWaterCandidates(selected, candidates),
        clear: () => setCandidates([]),
      });
    });
  }
  async function media(item: Row) { const ticket = guard.current.begin(); const target = item.media || item; try { const result = await loadAuthenticatedMedia(`/api/shuihuo-production/media/${encodeURIComponent(target.id)}/download`, target.kind === 'video' ? 'mp4' : target.kind === 'audio' ? 'mp3' : 'png'); if (guard.current.current(ticket)) setPreview({ path: result.path, kind: target.kind }); } catch (cause: any) { if (guard.current.current(ticket)) setError(cause.message); } }
  return <View className="mp-page mp-detail-page"><AmbientStarfield /><BrandHeader eyebrow="水货生产" title="水货项目" showBack /><View className="mp-section"><View className="mp-function-hero"><AppIcon name="clapperboard" size={23} /><Text>水货作品与分镜生产</Text></View>{error ? <Text className="mp-error">{error}</Text> : null}<View className="mp-card"><Text className="mp-row-title">水货作品库</Text>{catalogState === 'loading' ? <Text className="mp-row-description">正在读取作品库…</Text> : catalogState === 'error' ? <Text className="mp-error">作品库读取失败：{catalogError}</Text> : !projects.length ? <Text className="mp-row-description">暂无水货项目，可以新建项目或刷新。</Text> : null}{modelsError ? <Text className="mp-error">模型读取失败：{modelsError}，可刷新作品库重试。</Text> : null}{projects.map(project => <View className="mp-row mp-setting-row" key={project.id} onClick={() => setSelected(String(project.id))}><Text>{project.name || project.id}</Text><Text>{String(project.id) === selected ? '当前' : '进入'}</Text></View>)}<Input className="mp-input" value={name} placeholder="新建解说项目名称" onInput={event => setName(event.detail.value)} /><Textarea maxlength={-1} className="mp-textarea" value={sourceText} placeholder="粘贴小说原文（可选）" onInput={event => setSourceText(event.detail.value)} /><Button className="mp-secondary" disabled={busy || !name.trim()} onClick={() => act(true)}>新建项目</Button><Button className="mp-secondary" onClick={load}>刷新作品库</Button></View><View className="mp-card"><Text className="mp-row-title">批量工厂作品</Text>{batchState === 'loading' ? <Text>正在读取批量工厂作品…</Text> : batchState === 'error' ? <Text>批量工厂作品读取失败：{batchError}</Text> : !batchWorks.length ? <Text>暂无批量工厂作品</Text> : batchWorks.map(batch => <Button className="mp-secondary" key={batch.id} onClick={() => { void Taro.navigateTo({ url: `/pages/batch/index?batchId=${encodeURIComponent(batch.id)}` }); }}>{batch.title || batch.name || '未命名批量'} · {batch.bookCount ?? batch.books.length} 本小说</Button>)}<Button className="mp-secondary" onClick={load}>刷新共享作品库</Button></View>{busy ? <Text>正在读取或提交…</Text> : null}{detail ? <><View className="mp-card"><Text className="mp-row-title">原文与分镜确认</Text><Textarea maxlength={-1} className="mp-textarea" value={sourceEdit} disabled={busy} onInput={event => editSource(event.detail.value)} placeholder="项目原文" /><Button disabled={busy || !sourceEdit.trim()} onClick={saveSource}>保存原文</Button><Text className="mp-row-description">先保存原文，再生成分镜候选；确认候选会替换项目分镜，请核对后操作。</Text><Button disabled={busy || !detail.project.sourceText || sourceEdit !== detail.project.sourceText} onClick={previewSegments}>按段落生成候选</Button>{candidates.map((candidate, index) => <View key={index}><Text>候选 {index + 1}</Text><Textarea maxlength={-1} className="mp-textarea" value={candidate.text || ''} onInput={event => setCandidates(current => current.map((row, rowIndex) => rowIndex === index ? { ...row, text: event.detail.value } : row))} /><Input value={candidate.speaker || '旁白'} onInput={event => setCandidates(current => current.map((row, rowIndex) => rowIndex === index ? { ...row, speaker: event.detail.value } : row))} /></View>)}<Button disabled={busy || !candidates.length || !review.current.current(candidateTicket, sourceRevisionKey(detail.project)) || sourceEdit !== detail.project.sourceText} onClick={confirmSegments}>确认候选并保存分镜</Button></View><View className="mp-card"><Text className="mp-row-title">{detail.project.name}</Text><Text>{detail.project.sourceText || '尚未导入原文'}</Text>{detail.segments.map((segment: Row) => <View className="mp-row mp-setting-row" key={segment.id} onClick={() => { setSegmentId(String(segment.id)); setSegmentEdit({ ...segment }); }}><Text>{segment.orderIndex} · {segment.subtitleText || segment.sourceText}</Text><Text>{segmentId === String(segment.id) ? '当前' : segment.confirmed ? '已确认' : '待确认'}</Text></View>)}{segmentEdit ? <View><Text className="mp-row-title">编辑当前分镜</Text>{['subtitleText', 'speaker', 'imagePrompt', 'videoPrompt'].map((field, index) => <View key={field}><Text>{['字幕', '说话人', '图片提示词', '视频提示词'][index]}</Text><Textarea maxlength={-1} className="mp-textarea" value={segmentEdit[field] || ''} onInput={event => setSegmentEdit(current => current ? { ...current, [field]: event.detail.value } : null)} /></View>)}<Button disabled={busy} onClick={() => mutate(() => client.saveWaterSegment(segmentEdit))}>保存分镜文案与提示词</Button></View> : null}{models.filter(model => ['image', 'video', 'audio'].includes(model.kind) && model.enabled !== false).map(model => <View className="mp-row mp-setting-row" key={model.id} onClick={() => setModelId(String(model.id))}><Text>{model.name} · {model.kind}</Text><Text>{modelId === String(model.id) ? '当前' : '选择'}</Text></View>)}<Button className="mp-primary" disabled={busy || !segmentId || !modelId} onClick={() => act(false)}>按选定模型提交任务</Button></View><View className="mp-card"><Text className="mp-row-title">资产与素材</Text>{detail.assets.map((asset: Row) => <View key={asset.id}><Text>{asset.name} · {asset.category}</Text><Text>{asset.description || asset.prompt || ''}</Text><Button disabled={busy} onClick={() => setAssetEdit({ ...asset })}>编辑资产</Button></View>)}<Button disabled={busy} onClick={() => setAssetEdit({ name: '', category: 'character', description: '', prompt: '' })}>新建资产</Button>{assetEdit ? <View><Text className="mp-row-title">资产编辑</Text>{['character', 'scene', 'prop'].map(category => <Button key={category} disabled={Boolean(assetEdit.id)} onClick={() => setAssetEdit(current => current ? { ...current, category } : null)}>{category === assetEdit.category ? '✓ ' : ''}{({ character: '人物', scene: '场景', prop: '道具' })[category]}</Button>)}{['name', 'description', 'prompt'].map((field, index) => <View key={field}><Text>{['名称', '描述', '提示词'][index]}</Text><Textarea maxlength={-1} className="mp-textarea" value={assetEdit[field] || ''} onInput={event => setAssetEdit(current => current ? { ...current, [field]: event.detail.value } : null)} /></View>)}<Button disabled={busy || !assetEdit.name?.trim()} onClick={() => mutate(() => client.saveWaterAsset(selected, assetEdit))}>保存资产</Button></View> : null}{detail.media.map((item: Row, index: number) => <Button key={(item.media || item).id || index} disabled={busy} onClick={() => media(item)}>查看{(item.media || item).kind}素材</Button>)}{preview?.kind === 'video' ? <Video src={preview.path} controls /> : preview?.kind === 'image' ? <Image src={preview.path} mode="widthFix" /> : preview ? <Audio src={preview.path} controls /> : null}</View><View className="mp-card"><Text className="mp-row-title">任务与结果</Text>{detail.tasks.map((task: Row) => <View key={task.id}><Text>#{task.id} · {task.kind} · {task.status || '状态未返回'}</Text><Text>{task.errorMessage || task.providerTaskId || ''}</Text></View>)}<Button className="mp-secondary" disabled={busy} onClick={read}>刷新项目与任务</Button></View></> : !busy ? <Text>选择水货项目后进入工作台。</Text> : null}</View></View>;
}
export default withThemedPage(ShuihuoWorkspace);
