import {useEffect,useRef,useState} from 'react';
import {Alert,Button,Checkbox,Modal,Space,Spin,Tag} from 'antd';
import {listAssistantReferences,resolveAssistantReferences} from '../../shared/api/comicScreenplayAssistantReferences.js';
const labels={project_source:'作品原文',document:'已保存正文',chapter_source:'章节原文',chapter_script:'章节正文',storyboard:'分镜',asset_text:'资产文字'};
const key=r=>JSON.stringify([r.kind,r.id,r.shot_ids||[]]);
export function AssistantReferenceHistory({references}){
 if(!references?.length)return null;
 return <div className="comic-assistant-reference-history">{references.map((r,i)=><details key={i}><summary>引用：{labels[r.kind]||'作品内容'} · {r.name||r.id} · {r.bytes}字节 · {r.digest?.slice(0,8)}</summary><pre style={{whiteSpace:'pre-wrap',maxHeight:240,overflow:'auto'}}>{r.text}</pre><small>发送时的文字快照</small></details>)}</div>;
}
export default function ComicAssistantReferencePicker({projectId,sessionId,references=[],onChange,disabled=false}){
 const [open,setOpen]=useState(false),[rows,setRows]=useState([]),[selected,setSelected]=useState([]),[preview,setPreview]=useState(null),[error,setError]=useState(''),[busy,setBusy]=useState(false),[offset,setOffset]=useState(0),[total,setTotal]=useState(0);
 const sequence=useRef(0);
 useEffect(()=>()=>{sequence.current++},[]);
 async function load(next=0){const ticket=++sequence.current;setBusy(true);setError('');try{const data=await listAssistantReferences(projectId,sessionId,next);if(ticket!==sequence.current)return;setRows(prev=>next?[...prev,...data.references]:data.references);setOffset(data.next_offset);setTotal(data.total);}catch(e){if(ticket===sequence.current)setError(e.message||'引用目录读取失败');}finally{if(ticket===sequence.current)setBusy(false)}}
 function show(){setSelected(references);setPreview(null);setOpen(true);void load();}
 function toggle(row,checked){setPreview(null);setSelected(prev=>checked?[...prev,row]:prev.filter(r=>key(r)!==key(row)));}
 async function inspect(){const ticket=++sequence.current;setBusy(true);setError('');try{const data=await resolveAssistantReferences(projectId,sessionId,selected);if(ticket===sequence.current)setPreview(data.references);}catch(e){if(ticket===sequence.current){setPreview(null);setError(e.message||'引用已变化，请重新选择');}}finally{if(ticket===sequence.current)setBusy(false)}}
 function close(){sequence.current++;setOpen(false);setBusy(false);}
 return <div className="comic-assistant-reference-picker"><Space wrap><Button size="small" disabled={disabled} onClick={show}>作品内容 / @资产</Button>{references.map(r=><Tag key={key(r)} closable={!disabled} onClose={e=>{e.preventDefault();onChange(references.filter(item=>key(item)!==key(r)))}}>{labels[r.kind]} · {r.name||r.id}</Tag>)}</Space>
 <Modal title="引用当前作品的文字" open={open} onCancel={close} width={720} footer={<Space><Button onClick={close}>取消</Button><Button disabled={!selected.length||busy} loading={busy} onClick={inspect}>预览所选文字</Button><Button type="primary" disabled={busy||preview===null} onClick={()=>{onChange(preview);close()}}>使用这些引用</Button></Space>}>
 <p>最多8项，每项16KiB，合计32KiB。选择的文字会随下一条消息发送，不读取图片。</p>{error&&<Alert type="error" message={error}/>}<Spin spinning={busy}><div style={{maxHeight:320,overflow:'auto'}}>{rows.map(row=><div key={key(row)}><Checkbox checked={selected.some(r=>key(r)===key(row))} disabled={busy||(!selected.some(r=>key(r)===key(row))&&selected.length>=8)} onChange={e=>toggle(row,e.target.checked)}>{labels[row.kind]} · {row.name||row.id} · {row.id.slice(-8)} · {row.bytes}字节</Checkbox></div>)}</div>{offset<total&&<Button onClick={()=>load(offset)}>加载更多</Button>}{preview&&<AssistantReferenceHistory references={preview}/>}</Spin></Modal></div>;
}
