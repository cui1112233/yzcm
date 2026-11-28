import {useEffect,useRef,useState} from 'react';
import {Alert,Button,Checkbox,Modal,Space,Spin} from 'antd';
import {listAssistantReferenceShots} from '../../shared/api/comicScreenplayAssistantReferences.js';
import {fullBoardEvidence,makeShotRange} from './comic-assistant-shot-references.js';

function initialState(board,range){
 try {
  const evidence=fullBoardEvidence(range||board),rows=range?.shot_rows||[];
  if(range?.shot_ids?.length&&(!range.board_evidence||range.shot_ids.some(id=>!rows.some(row=>row.id===id))))throw Error('范围缺少整份目录证据，请重新选择');
  return {evidence,selected:new Map(rows.map(row=>[row.id,row])),error:''};
 }catch(error){return {evidence:null,selected:new Map(),error:error.message}}
}
export default function ComicAssistantShotReferencePicker({projectId,sessionId,board,initialRange,onChoose,onCancel,onReselect}){
 const [initial]=useState(()=>initialState(board,initialRange));
 const [selected,setSelected]=useState(initial.selected),[rows,setRows]=useState([]),[error,setError]=useState(initial.error),[busy,setBusy]=useState(false),[page,setPage]=useState({offset:0,next_offset:0,total:0});
 const sequence=useRef(0),live=useRef(true);
 async function load(offset){
  if(!initial.evidence)return;
  const ticket=++sequence.current;setBusy(true);setError('');
  try {
   const result=await listAssistantReferenceShots(projectId,sessionId,initial.evidence,{offset,limit:100});
   if(!live.current||ticket!==sequence.current)return;
   const evidence=initial.evidence;
   if(result.board?.id!==evidence.id||result.board.revision!==evidence.revision||result.board.digest!==evidence.digest||result.shots.length>100||result.shots.some((row,i)=>row.position!==offset+i))throw Error('目录版本不一致，请重新选择');
   setRows(result.shots);setPage({offset:result.offset,next_offset:result.next_offset,total:result.total});
  }catch(err){if(live.current&&ticket===sequence.current)setError(err.message||'镜头目录读取失败')}
  finally{if(live.current&&ticket===sequence.current)setBusy(false)}
 }
 useEffect(()=>{live.current=true;void load(0);return()=>{live.current=false;sequence.current++;}},[]);
 function toggle(row,checked){setSelected(old=>{const next=new Map(old);if(checked)next.set(row.id,{id:row.id,seq:row.seq,position:row.position});else next.delete(row.id);return next})}
 function choose(){try{onChoose(makeShotRange(initial.evidence,[...selected.values()]))}catch(err){setError(err.message)}}
 function cancel(){sequence.current++;onCancel()}
 return <Modal className="comic-assistant-shot-modal" title="选择分镜镜头" open onCancel={cancel} width={740} footer={<Space><Button onClick={cancel}>取消</Button><Button type="primary" disabled={busy||!!error||!selected.size} onClick={choose}>使用所选镜头</Button></Space>}>
  <p>{board.name} · 已选{selected.size}/200镜。仅加入范围，返回后须预览文字再使用引用。</p>
  {error&&<Alert type="error" message={error} action={<Button size="small" onClick={()=>{sequence.current++;onReselect()}}>返回并重新选择</Button>}/>}
  <Space><Button disabled={busy||!selected.size} onClick={()=>setSelected(new Map())}>清空选中</Button><small>第{Math.floor(page.offset/100)+1}页 · 共{page.total}镜</small></Space>
  <Spin spinning={busy}><div className="comic-assistant-shot-picker" style={{maxHeight:380,overflow:'auto'}}>{rows.map(row=><div className="comic-assistant-shot-row" key={row.id} style={{padding:'8px 0'}}><Checkbox aria-label={`镜头${row.seq}`} checked={selected.has(row.id)} disabled={busy||!!error||(!selected.has(row.id)&&selected.size>=200)} onChange={e=>toggle(row,e.target.checked)}>镜头{row.seq} · {row.id}</Checkbox><p style={{whiteSpace:'pre-wrap',margin:'4px 0'}}>{row.excerpt}{row.excerpt_truncated?'（摘录已截短）':''}</p></div>)}</div></Spin>
  <Space><Button disabled={busy||!!error||page.offset===0} onClick={()=>load(Math.max(0,page.offset-100))}>上一页</Button><Button disabled={busy||!!error||page.next_offset>=page.total} onClick={()=>load(page.next_offset)}>下一页</Button></Space>
 </Modal>;
}
