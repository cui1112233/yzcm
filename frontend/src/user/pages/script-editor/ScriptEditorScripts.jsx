import { Button, Checkbox, Empty, Input, InputNumber, message, Modal, Popconfirm, Select, Tag } from 'antd';
import { Copy, Plus, Sparkles, Star, Trash2 } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { copyScriptEditorScript, createScriptEditorGenerationJob, createScriptEditorReviewJob, createScriptEditorScript, deleteScriptEditorScript, preferScriptEditorScript, updateScriptEditorScript } from '../../../shared/api/scriptEditor.js';
import { listAvailableModels, modelSelectOptions } from '../../../shared/api/modelCatalog.js';
import ScriptEditorJobs from './ScriptEditorJobs.jsx';
import { scriptEditorJobTitle } from './scriptEditorJobs.js';

const requestKey = () => globalThis.crypto?.randomUUID?.() || `${Date.now()}-${Math.random()}`;

export default function ScriptEditorScripts({ work, onReload }) {
  const scripts = work.scripts || [];
  const [activeId, setActiveId] = useState(scripts[0]?.id || '');
  const [draft, setDraft] = useState(null);
  const [status, setStatus] = useState('');
  const [createOpen, setCreateOpen] = useState(false);
  const [episode, setEpisode] = useState(1);
  const [jobOpen, setJobOpen] = useState(false);
  const [jobKind, setJobKind] = useState('script-generation');
  const [models, setModels] = useState([]);
  const [modelId, setModelId] = useState('');
  const [chapterIds, setChapterIds] = useState([]);
  const [extra, setExtra] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [jobsRefresh, setJobsRefresh] = useState(0);
  const active = useMemo(() => scripts.find(row => row.id === activeId) || scripts[0], [scripts, activeId]);

  useEffect(() => { setDraft(active ? { ...active } : null); setStatus(''); }, [active?.id, active?.revision]);
  useEffect(() => {
    if (!jobOpen) return;
    listAvailableModels('text').then(rows => { setModels(rows); if (!modelId) setModelId(rows[0]?.id || ''); }).catch(error => message.error(error.message));
    if (!chapterIds.length) setChapterIds((work.chapters || []).map(row => row.id));
  }, [jobOpen]);
  useEffect(() => {
    if (!draft || !active || (draft.title === active.title && draft.content === active.content)) return;
    setStatus('正在保存…');
    const timer = setTimeout(async () => {
      try {
        await updateScriptEditorScript(work.id, active.id, { title: draft.title, content: draft.content, expectedRevision: active.revision });
        setStatus('已保存'); await onReload();
      } catch (error) { setStatus(error.status === 409 ? '保存冲突：本地文字仍保留，请刷新后处理' : '保存失败'); }
    }, 800);
    return () => clearTimeout(timer);
  }, [draft?.title, draft?.content]);

  function openJob(kind) { if (kind !== 'script-generation' && !active) return message.warning('请先选择一个剧本版本'); setJobKind(kind); setJobOpen(true); }
  async function add() { const result = await createScriptEditorScript(work.id, { episode, title: `第${episode}集`, content: '', generationSource: 'manual' }); setCreateOpen(false); setActiveId(result.script.id); await onReload(); }
  async function copy() { const result = await copyScriptEditorScript(work.id, active.id); setActiveId(result.script.id); await onReload(); }
  async function prefer() { try { await preferScriptEditorScript(work.id, active.id, active.revision); await onReload(); } catch (error) { message.error(error.message); } }
  async function remove() { await deleteScriptEditorScript(work.id, active.id, active.revision); setActiveId(''); await onReload(); }
  async function submitJob() {
    if (!modelId) return message.warning('请选择文字模型');
    if (jobKind === 'script-generation' && !chapterIds.length) return message.warning('请选择原文章节');
    setSubmitting(true);
    try {
      if (jobKind === 'script-generation') await createScriptEditorGenerationJob(work.id, { requestKey: requestKey(), episodes: [episode], chapterIds, modelId, extra });
      else await createScriptEditorReviewJob(work.id, { requestKey: requestKey(), scriptId: active.id, kind: jobKind, modelId, extra });
      setJobOpen(false); setJobsRefresh(value => value + 1); message.success('任务已提交，可以离开页面，完成后结果会保留');
    } catch (error) { message.error(error.message || '任务提交失败'); }
    finally { setSubmitting(false); }
  }

  return <div className="script-editor-edit-layout">
    <aside className="script-editor-outline">
      <div className="script-editor-pane-title"><strong>剧本版本</strong><span><Button type="text" icon={<Sparkles size={15} />} onClick={() => openJob('script-generation')} /><Button type="text" icon={<Plus size={15} />} onClick={() => setCreateOpen(true)} /></span></div>
      {scripts.length ? scripts.map(script => <button className={script.id === active?.id ? 'is-active' : ''} key={script.id} onClick={() => setActiveId(script.id)}><span>第{script.episode}集 · V{script.version} {script.preferred && <Star size={12} fill="currentColor" />}</span><small>{script.content?.replace(/\s/g, '').length || 0} 字</small></button>) : <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无剧本" />}
    </aside>
    <div className="script-editor-main-column">
      <section className="script-editor-document">
        {draft ? <><div className="script-editor-document-actions"><Tag color="purple">第{draft.episode}集 V{draft.version}</Tag><Button icon={<Sparkles size={15} />} onClick={() => openJob('script-generation')}>AI 出剧本</Button><Button onClick={() => openJob('review')}>AI 审稿</Button><Button onClick={() => openJob('polish')}>AI 优化</Button><Button onClick={() => openJob('viral-opening')}>爆款开头</Button><Button icon={<Copy size={15} />} onClick={copy}>复制为新版本</Button><Button type={draft.preferred ? 'primary' : 'default'} icon={<Star size={15} />} onClick={prefer}>{draft.preferred ? '正式版' : '设为正式版'}</Button><Popconfirm title="删除这个版本？" onConfirm={remove}><Button danger icon={<Trash2 size={15} />} /></Popconfirm><span>{status}</span></div><Input value={draft.title} onChange={event => setDraft({ ...draft, title: event.target.value })} className="script-editor-document-title" /><Input.TextArea value={draft.content} onChange={event => setDraft({ ...draft, content: event.target.value })} autoSize={{ minRows: 22 }} placeholder="在这里编写分集剧本。停止输入后自动保存。" /><footer>{draft.content?.replace(/\s/g, '').length || 0} 字</footer></> : <Empty description="新建或 AI 生成一个剧本版本后开始编辑"><Button type="primary" icon={<Sparkles size={15} />} onClick={() => openJob('script-generation')}>AI 出剧本</Button></Empty>}
      </section>
      <ScriptEditorJobs workId={work.id} refreshToken={jobsRefresh} onReload={onReload} />
    </div>
    <Modal title={scriptEditorJobTitle(jobKind)} width={620} open={jobOpen} onCancel={() => setJobOpen(false)} onOk={submitJob} okText="提交任务（会调用模型）" confirmLoading={submitting}>
      <div className="script-editor-generate-form">
        {jobKind === 'script-generation' && <><label>目标集数<InputNumber min={1} value={episode} onChange={value => setEpisode(value || 1)} /></label><label>采用原文章节<Checkbox.Group value={chapterIds} onChange={setChapterIds} options={(work.chapters || []).map(row => ({ value: row.id, label: row.title }))} /></label></>}
        <label>文字模型<Select value={modelId || undefined} placeholder="请选择已配置的文字模型" options={modelSelectOptions(models, 'text')} onChange={setModelId} /></label>
        <label>额外要求<Input.TextArea value={extra} onChange={event => setExtra(event.target.value)} rows={3} placeholder={jobKind === 'review' ? '例如：重点检查人物动机和结尾悬念' : '例如：前三秒出现冲突，结尾留强悬念'} /></label>
        <small>提交后会使用所选模型，可能产生模型费用。结果会先保存；AI 优化和爆款开头只有点“采用为新版本”后才写入，不会覆盖原稿。</small>
      </div>
    </Modal>
    <Modal title="新建剧本版本" open={createOpen} onCancel={() => setCreateOpen(false)} onOk={add} okText="创建"><label>集数 <InputNumber min={1} value={episode} onChange={value => setEpisode(value || 1)} /></label></Modal>
  </div>;
}
