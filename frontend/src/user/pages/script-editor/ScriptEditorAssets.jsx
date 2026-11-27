import { Button, Empty, Input, Select, Tag, message } from 'antd';
import { Boxes, Link2 } from 'lucide-react';
import { useEffect, useState } from 'react';
import { createScriptEditorAssetLink, listScriptEditorAssets } from '../../../shared/api/scriptEditor.js';

const categories = [{ value: 'character', label: '人物' }, { value: 'scene', label: '场景' }, { value: 'prop', label: '道具' }, { value: 'director', label: '导演台' }];

export default function ScriptEditorAssets({ work }) {
  const [rows, setRows] = useState([]), [assetId, setAssetId] = useState(''), [category, setCategory] = useState('character'), [busy, setBusy] = useState(false);
  async function load() { try { const result = await listScriptEditorAssets(work.id); setRows(result.assets || []); } catch (error) { message.error(error.message || '资产读取失败'); } }
  useEffect(() => { load(); }, [work.id]);
  async function link() { if (!assetId.trim()) return; setBusy(true); try { await createScriptEditorAssetLink(work.id, { assetId: assetId.trim(), category, targetId: work.id }); setAssetId(''); await load(); message.success('资产已绑定到当前作品'); } catch (error) { message.error(error.message || '绑定失败'); } finally { setBusy(false); } }
  return <section className="script-editor-assets"><header className="script-editor-panel-heading"><div><span className="script-editor-kicker">ASSET WORKBENCH</span><h2><Boxes size={20} />资产工作台</h2><p>复用现有资产 ID，按人物、场景、道具和导演台归到当前作品。</p></div></header><div className="script-editor-asset-linker"><Input value={assetId} onChange={event => setAssetId(event.target.value)} placeholder="粘贴现有资产 ID" /><Select value={category} onChange={setCategory} options={categories} /><Button type="primary" icon={<Link2 size={15} />} loading={busy} onClick={link}>绑定资产</Button></div>{rows.length ? <div className="script-editor-asset-grid">{rows.map(row => <article key={row.id} className="script-editor-card"><strong>{row.assetId}</strong><Tag>{categories.find(item => item.value === row.targetType)?.label || row.targetType}</Tag><small>同步状态：{row.syncState}</small></article>)}</div> : <Empty description="当前作品还没有绑定资产" />}</section>;
}
