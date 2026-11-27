import { Alert, Button, Card, Empty, Space, Statistic, Tag, message } from 'antd';
import { ExternalLink, Film, RefreshCw } from 'lucide-react';
import { useEffect, useState } from 'react';
import { getScriptEditorProduction } from '../../../shared/api/scriptEditor.js';
import { PRODUCTION_STAGES, productionStageCounts } from './productionModel.js';

export default function ScriptEditorProduction({ work }) {
  const [status,setStatus] = useState({jobs:[]}), [busy,setBusy] = useState(false);
  async function load(){setBusy(true);try{const result=await getScriptEditorProduction(work.id);setStatus(result.production);}catch(error){message.error(error.message||'生产状态读取失败');}finally{setBusy(false)}}
  useEffect(()=>{load()},[work.id]); const counts=productionStageCounts(status.jobs);
  return <section className="script-editor-production"><header className="script-editor-panel-heading"><div><span className="script-editor-kicker">PRODUCTION</span><h2><Film size={20}/>生产工作台</h2><p>这里汇总真实任务；制作仍进入现有漫剧创作和 AI-CUT，不重复造引擎。</p></div><Button icon={<RefreshCw size={15}/>} loading={busy} onClick={load}>刷新状态</Button></header>{status.resultPendingConfirmation>0&&<Alert type="warning" showIcon message={`${status.resultPendingConfirmation} 个任务结果未知`} description="可能已经发送到服务商，请先确认结果，不要直接重试以免再次扣费。"/>}<div className="script-editor-production-stats"><Statistic title="运行中" value={status.running||0}/><Statistic title="已完成" value={status.succeeded||0}/><Statistic title="失败" value={status.failed||0}/><Statistic title="待确认" value={status.resultPendingConfirmation||0}/></div><div className="script-editor-production-grid">{PRODUCTION_STAGES.map(stage=><Card key={stage.key} title={stage.label} extra={<Tag>{counts[stage.key]||0}</Tag>}><p>{stage.key==='image'||stage.key==='video'||stage.key==='voice'?'进入漫剧创作继续处理':'进入 AI-CUT 继续处理'}</p><Button icon={<ExternalLink size={14}/>} onClick={()=>window.open(`${stage.href}?sourceWorkId=${encodeURIComponent(work.id)}&stage=${stage.key}`,'_blank')}>打开正式工作台</Button></Card>)}</div>{status.jobs?.length?<div className="script-editor-production-jobs">{status.jobs.map((row,index)=><Tag key={`${row.jobId}-${index}`} color={row.status==='failed'?'red':row.status==='may_have_sent'?'orange':'blue'}>{row.stage} · {row.status}</Tag>)}</div>:<Empty description="还没有生产任务；不会用假任务填充"/>}</section>;
}
