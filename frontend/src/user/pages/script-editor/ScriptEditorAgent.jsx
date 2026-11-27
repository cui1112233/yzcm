import { Alert, Button, Checkbox, Input, Segmented, Select, Space, Tag, message } from 'antd';
import { Bot, ExternalLink, FileCheck2, ShieldCheck } from 'lucide-react';
import { useMemo, useState } from 'react';
import { applyScriptEditorAgentDraft, buildScriptEditorAgentContext } from '../../../shared/api/scriptEditor.js';

const modes = [
  { label: '只读', value: 'read_only' },
  { label: '先问我', value: 'ask' },
  { label: '确认后写入', value: 'confirm' },
  { label: '直接写入', value: 'direct' },
];

function newDraftId() {
  return globalThis.crypto?.randomUUID?.() || `draft-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

export default function ScriptEditorAgent({ work, onReload }) {
  const [mode, setMode] = useState('confirm');
  const [chapterIds, setChapterIds] = useState([]);
  const [scriptId, setScriptId] = useState('');
  const [draft, setDraft] = useState('');
  const [contextValue, setContextValue] = useState(null);
  const [busy, setBusy] = useState(false);
  const script = useMemo(() => work.scripts?.find(row => row.id === scriptId), [scriptId, work.scripts]);
  const references = useMemo(() => [...chapterIds.map(id => ({ type: 'chapter', id })), ...(scriptId ? [{ type: 'script', id: scriptId }] : [])], [chapterIds, scriptId]);

  async function prepareContext() {
    setBusy(true);
    try {
      const result = await buildScriptEditorAgentContext(work.id, references);
      setContextValue(result.context);
      message.success('作品上下文已锁定，Agent 只能引用这些内容');
    } catch (error) { message.error(error.message || '上下文准备失败'); }
    finally { setBusy(false); }
  }

  async function applyDraft() {
    if (!script || !draft.trim()) return message.warning('请选择目标剧本并填写 Agent 草稿');
    if (!['confirm', 'direct'].includes(mode)) return message.info('当前模式不会写入作品，请切换到确认后写入或直接写入');
    setBusy(true);
    try {
      const result = await applyScriptEditorAgentDraft(work.id, newDraftId(), { scriptId: script.id, title: `${script.title} Agent 稿`, content: draft, mode, expectedRevision: script.revision });
      await onReload();
      setDraft('');
      message.success(`已生成第 ${result.script.version} 版，并记录审计 ${result.auditId}`);
    } catch (error) { message.error(error.status === 409 ? '原剧本已经变化，请刷新后再采用' : error.message || '采用草稿失败'); }
    finally { setBusy(false); }
  }

  return <section className="script-editor-agent">
    <header className="script-editor-panel-heading"><div><span className="script-editor-kicker">WORK AGENT</span><h2><Bot size={20} />作品 Agent</h2><p>把作品内的章节和剧本作为稳定引用交给现有 Agent；采用结果时只新增版本，不覆盖原稿。</p></div><Button icon={<ExternalLink size={15} />} onClick={() => window.open(`/agent/canvas?workId=${encodeURIComponent(work.id)}&returnTo=${encodeURIComponent(`/script-editor?workId=${work.id}&tab=agent`)}`, '_blank')}>打开完整 Agent</Button></header>
    <div className="script-editor-agent-grid">
      <div className="script-editor-card"><h3>1. 选择作品上下文</h3><Checkbox.Group options={(work.chapters || []).map(row => ({ label: row.title, value: row.id }))} value={chapterIds} onChange={setChapterIds} /><Select allowClear placeholder="选择目标剧本版本" value={scriptId || undefined} onChange={value => setScriptId(value || '')} options={(work.scripts || []).map(row => ({ value: row.id, label: `第${row.episode}集 · V${row.version} · ${row.title}` }))} /><Button loading={busy} onClick={prepareContext}>锁定上下文</Button>{contextValue && <Space wrap><Tag color="blue">{contextValue.chapterIds.length} 章</Tag><Tag color="purple">{contextValue.scriptIds.length} 个剧本</Tag><Tag icon={<ShieldCheck size={12} />} color="green">已做归属校验</Tag></Space>}</div>
      <div className="script-editor-card"><h3>2. 采用 Agent 草稿</h3><Segmented block options={modes} value={mode} onChange={setMode} /><Alert type="info" showIcon message={mode === 'read_only' ? '只读：不会写入作品' : mode === 'ask' ? '先问我：只保留建议，不写入作品' : mode === 'confirm' ? '确认后写入：采用时生成新版本并留审计' : '直接写入：仍然只新增版本并留审计'} /><Input.TextArea rows={12} value={draft} onChange={event => setDraft(event.target.value)} placeholder="把现有 Agent 返回的剧本草稿放在这里。采用后会生成新的 AI 版本。" /><Button type="primary" icon={<FileCheck2 size={15} />} loading={busy} disabled={!['confirm', 'direct'].includes(mode)} onClick={applyDraft}>采用为新版本</Button></div>
    </div>
  </section>;
}
