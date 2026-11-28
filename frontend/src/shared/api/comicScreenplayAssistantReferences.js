import {apiRequest,LONG_RUNNING_API_TIMEOUT_MS} from './client.js';
function id(value){if(typeof value!=='string'||!/^[A-Za-z0-9][A-Za-z0-9_-]{0,95}$/.test(value))throw Error('作品或会话无效');return value;}
const base=project=>`/api/comic-screenplays/projects/${id(project)}/assistant`;
export function referenceSelector(ref){return {kind:ref.kind,id:ref.id,expected_revision:ref.expected_revision??ref.revision??0,expected_digest:ref.expected_digest??ref.digest,...(ref.shot_ids?.length?{shot_ids:[...ref.shot_ids]}:{})};}
export function listAssistantReferences(project,session,offset=0,request=apiRequest){return request(`${base(project)}/references?session_id=${id(session)}&offset=${offset}`);}
export function fullBoardEvidence(ref){
 const source=ref?.shot_ids?.length?ref.board_evidence:(ref?.board_evidence||ref);
 if(!source||!Number.isSafeInteger(source.revision)||source.revision<0||!/^[a-f0-9]{64}$/.test(source.digest||''))throw Error('缺少整份分镜版本凭据，请重新选择');
 return {id:id(source.id),name:source.name||ref.name||'',revision:source.revision,digest:source.digest};
}
export function listAssistantReferenceShots(project,session,board,page={},request=apiRequest){
 const evidence=fullBoardEvidence(board),offset=page.offset??0,limit=page.limit??100;
 if(!Number.isSafeInteger(offset)||offset<0||!Number.isSafeInteger(limit)||limit<0||limit>100)throw Error('镜头分页范围无效');
 return request(`${base(project)}/references/storyboard-shots`,{method:'POST',body:JSON.stringify({session_id:id(session),board_id:evidence.id,expected_revision:evidence.revision,expected_digest:evidence.digest,offset,limit})});
}
export function resolveAssistantReferences(project,session,references,request=apiRequest){return request(`${base(project)}/references/resolve`,{method:'POST',body:JSON.stringify({session_id:id(session),references:references.map(referenceSelector)})});}
export function sendReferenceMessage(project,session,input,request=apiRequest){return request(`${base(project)}/${session==='default'?'chat':`sessions/${id(session)}/chat`}`,{method:'POST',body:JSON.stringify({message:input.message,expected_revision:input.expected_revision,references:input.references.map(referenceSelector)}),timeoutMs:LONG_RUNNING_API_TIMEOUT_MS});}
