import test from 'node:test';
import assert from 'node:assert/strict';
import {listAssistantReferences,resolveAssistantReferences,sendReferenceMessage,referenceSelector} from './comicScreenplayAssistantReferences.js';
test('引用API绑定明确作品会话，发送只投影selector',async()=>{
 const calls=[];const request=async(...args)=>{calls.push(args);return {}};
 const ref={kind:'asset_text',id:'a',revision:2,digest:'d'.repeat(64),text:'server snapshot',url:'DO_NOT_SEND',owner:'other'};
 await listAssistantReferences('p','s',0,request);
 await resolveAssistantReferences('p','s',[ref],request);
 await sendReferenceMessage('p','default',{message:'q',expected_revision:3,references:[ref]},request);
 assert.match(calls[0][0],/projects\/p\/assistant\/references\?session_id=s/);
 assert.equal(JSON.parse(calls[1][1].body).session_id,'s');
 assert.equal(calls[2][0],'/api/comic-screenplays/projects/p/assistant/chat');
 assert.deepEqual(JSON.parse(calls[2][1].body).references,[referenceSelector(ref)]);
 assert.ok(!calls[2][1].body.includes('DO_NOT_SEND')&&!calls[2][1].body.includes('server snapshot'));
 await sendReferenceMessage('p','s',{message:'q',references:[ref]},request);
 assert.match(calls[3][0],/sessions\/s\/chat$/);
 assert.throws(()=>listAssistantReferences('../other','s',0,request));
});
