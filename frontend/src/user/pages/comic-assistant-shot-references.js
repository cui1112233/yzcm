import {fullBoardEvidence} from '../../shared/api/comicScreenplayAssistantReferences.js';
export {fullBoardEvidence};
export function makeShotRange(board,entries){
 if(!Array.isArray(entries)||!entries.length)throw Error('请至少选择一个镜头');
 if(entries.length>200)throw Error('最多选择200个镜头');
 const rows=[...entries].sort((a,b)=>a.position-b.position),ids=new Set();
 for(const row of rows){if(!row.id||ids.has(row.id)||!Number.isSafeInteger(row.position)||row.position<0)throw Error('镜头目录无效，请重新选择');ids.add(row.id)}
 const evidence=fullBoardEvidence(board),shotRows=rows.map(({id,seq,position})=>({id,seq,position}));
 return {kind:'storyboard',id:evidence.id,name:evidence.name,revision:evidence.revision,digest:evidence.digest,shot_ids:shotRows.map(r=>r.id),board_evidence:evidence,shot_rows:shotRows,range_label:`镜头${shotRows.slice(0,8).map(r=>r.seq).join('、')}${shotRows.length>8?`等${shotRows.length}镜`:''}`};
}
export function replaceBoardReference(selected,next){
 let replaced=false;const out=[];
 for(const row of selected){if(row.kind==='storyboard'&&row.id===next.id){if(!replaced){out.push(next);replaced=true}}else out.push(row)}
 if(!replaced)out.push(next);return out;
}
export function mergeShotPreview(preview,selected){
 return preview.map(snapshot=>{if(snapshot.kind!=='storyboard')return snapshot;const source=selected.find(r=>r.kind==='storyboard'&&r.id===snapshot.id&&r.shot_ids?.length);
  if(!source)return snapshot;
  if(JSON.stringify(source.shot_ids)!==JSON.stringify(snapshot.shot_ids))throw Error('镜头范围预览不一致，请重新选择');
  return {...snapshot,board_evidence:source.board_evidence,shot_rows:source.shot_rows,range_label:source.range_label};
 });
}
