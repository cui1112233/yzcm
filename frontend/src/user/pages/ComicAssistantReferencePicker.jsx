import {useEffect,useRef,useState} from 'react';
import {Alert,Button,Checkbox,Modal,Space,Spin,Tag} from 'antd';
import {listAssistantReferences,resolveAssistantReferences} from '../../shared/api/comicScreenplayAssistantReferences.js';
import ComicAssistantShotReferencePicker from './ComicAssistantShotReferencePicker.jsx';
import {mergeShotPreview,replaceBoardReference} from './comic-assistant-shot-references.js';

const labels={project_source:'作品原文',document:'已保存正文',chapter_source:'章节原文',chapter_script:'章节正文',storyboard:'分镜',asset_text:'资产文字'};
const key=r=>JSON.stringify([r.kind,r.id,r.shot_ids||[]]);
const identity=(a,b)=>a.kind===b.kind&&a.id===b.id;
const rangeLabel=r=>r.range_label||(r.shot_ids?.length?`已选${r.shot_ids.length}镜`:'');
export function AssistantReferenceHistory({references}){
 if(!references?.length)return null;
 return <div className="comic-assistant-reference-history">{references.map((r,i)=><details key={i}><summary>引用：{labels[r.kind]||'作品内容'} · {r.name||r.id}{rangeLabel(r)?` · ${rangeLabel(r)}`:''} · {r.bytes}字节 · {r.digest?.slice(0,8)}</summary><pre style={{whiteSpace:'pre-wrap',maxHeight:240,overflow:'auto'}}>{r.text}</pre><small>发送时的文字快照</small></details>)}</div>;
}
export default function ComicAssistantReferencePicker({projectId,sessionId,references=[],onChange,disabled=false}){
 const [open,setOpen]=useState(false),[rows,setRows]=useState([]),[selected,setSelected]=useState([]),[preview,setPreview]=useState(null),[error,setError]=useState(''),[busy,setBusy]=useState(''),[offset,setOffset]=useState(0),[total,setTotal]=useState(0),[shotTarget,setShotTarget]=useState(null);
 const sequence=useRef(0),activeChild=useRef(null),scope=JSON.stringify([projectId,sessionId]),scopeRef=useRef(scope);scopeRef.current=scope;
 function invalidate(){sequence.current++;setPreview(null);setBusy('')}
 useEffect(()=>{sequence.current++;activeChild.current=null;setOpen(false);setShotTarget(null);setRows([]);setSelected([]);setPreview(null);setBusy('');return()=>{sequence.current++;activeChild.current=null}},[scope]);
 useEffect(()=>{if(disabled){invalidate();activeChild.current=null;setShotTarget(null);setOpen(false)}},[disabled]);
 async function load(next=0){
  const ticket=++sequence.current,capturedScope=scope;setBusy('directory');setError('');
  try{const data=await listAssistantReferences(projectId,sessionId,next);if(ticket!==sequence.current||scopeRef.current!==capturedScope)return;setRows(prev=>next?[...prev,...data.references]:data.references);setOffset(data.next_offset);setTotal(data.total)}
  catch(e){if(ticket===sequence.current&&scopeRef.current===capturedScope)setError(e.message||'引用目录读取失败')}
  finally{if(ticket===sequence.current&&scopeRef.current===capturedScope)setBusy('')}
 }
 function show(){invalidate();activeChild.current=null;setShotTarget(null);setSelected([...references]);setRows([]);setOpen(true);void load()}
 function toggle(row,checked){invalidate();setSelected(prev=>checked?(row.kind==='storyboard'?replaceBoardReference(prev,row):[...prev,row]):prev.filter(r=>!identity(r,row)))}
 async function inspect(){
  const ticket=++sequence.current,capturedScope=scope,selection=[...selected];setBusy('preview');setPreview(null);setError('');
  try{const data=await resolveAssistantReferences(projectId,sessionId,selection);if(ticket===sequence.current&&scopeRef.current===capturedScope)setPreview(mergeShotPreview(data.references,selection))}
  catch(e){if(ticket===sequence.current&&scopeRef.current===capturedScope){setPreview(null);setError(e.message||'引用已变化，请重新选择')}}
  finally{if(ticket===sequence.current&&scopeRef.current===capturedScope)setBusy('')}
 }
 function close(){invalidate();activeChild.current=null;setShotTarget(null);setOpen(false)}
 function openShots(board){
  if(!selected.some(r=>identity(r,board))&&selected.length>=8){setError('最多8项引用，请先移除一项');return}
  invalidate();const ticket=sequence.current;activeChild.current=ticket;
  setShotTarget({board,range:selected.find(r=>identity(r,board)&&r.shot_ids?.length),scope,ticket});
 }
 function closeShots(){invalidate();activeChild.current=null;setShotTarget(null)}
 function chooseShots(range,target){if(scopeRef.current!==target.scope||activeChild.current!==target.ticket)return;invalidate();setSelected(prev=>replaceBoardReference(prev,range));activeChild.current=null;setShotTarget(null)}
 function reselectShots(target){if(scopeRef.current!==target.scope||activeChild.current!==target.ticket)return;closeShots();setSelected(prev=>prev.filter(r=>!identity(r,target.board)));setRows([]);void load()}
 return <div className="comic-assistant-reference-picker"><Space wrap><Button size="small" disabled={disabled} onClick={show}>作品内容 / @资产</Button>{references.map(r=><Tag key={key(r)} closable={!disabled} onClose={e=>{e.preventDefault();invalidate();onChange(references.filter(item=>!identity(item,r)))}}>{labels[r.kind]} · {r.name||r.id}{rangeLabel(r)?` · ${rangeLabel(r)}`:''}</Tag>)}</Space>
 <Modal className="comic-assistant-reference-modal" title="引用当前作品的文字" open={open} onCancel={close} width={720} footer={<Space><Button onClick={close}>取消</Button><Button disabled={!selected.length||!!busy} loading={busy==='preview'} onClick={inspect}>预览所选文字</Button><Button type="primary" disabled={!!busy||preview===null} onClick={()=>{onChange(preview);close()}}>使用这些引用</Button></Space>}>
  <p>最多8项，每项16KiB，合计32KiB。选择的文字会随下一条消息发送，不读取图片。</p>{error&&<Alert type="error" message={error}/>}
  <Spin spinning={busy==='directory'}><div style={{maxHeight:320,overflow:'auto'}}>{rows.map(row=>{
   const chosen=selected.find(r=>identity(r,row)),whole=chosen&&!chosen.shot_ids?.length;
   return <div key={key(row)} style={{padding:'4px 0'}}><Checkbox aria-label={row.kind==='storyboard'?`整份分镜 ${row.id}`:undefined} checked={row.kind==='storyboard'?!!whole:!!chosen} disabled={busy==='directory'||(!chosen&&selected.length>=8)} onChange={e=>toggle(row,e.target.checked)}>{row.kind==='storyboard'?'整份分镜':labels[row.kind]} · {row.name||row.id} · {row.id.slice(-8)} · {row.bytes}字节</Checkbox>{row.kind==='storyboard'&&<Space><Button size="small" disabled={busy==='directory'} onClick={()=>openShots(row)}>选择镜头</Button>{chosen?.shot_ids?.length?<small>{rangeLabel(chosen)}</small>:null}</Space>}</div>
  })}</div>{offset<total&&<Button disabled={busy==='directory'} onClick={()=>load(offset)}>加载更多</Button>}{preview&&<AssistantReferenceHistory references={preview}/>}</Spin>
 </Modal>
 {open&&shotTarget?.scope===scope&&<ComicAssistantShotReferencePicker key={shotTarget.ticket} projectId={projectId} sessionId={sessionId} board={shotTarget.board} initialRange={shotTarget.range} onChoose={range=>chooseShots(range,shotTarget)} onCancel={closeShots} onReselect={()=>reselectShots(shotTarget)}/>}
 </div>;
}
