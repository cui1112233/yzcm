import test from 'node:test';
import assert from 'node:assert/strict';
import {listAssistantReferenceShots} from './comicScreenplayAssistantReferences.js';
test('镜头目录仅提交整份证据与分页，不把局部digest或摘录当凭据',async()=>{
 const calls=[];const request=async(...args)=>{calls.push(args);return {}};
 const full={id:'b',revision:7,digest:'a'.repeat(64),name:'board'};
 await listAssistantReferenceShots('p','s',full,{offset:100,limit:100},request);
 const body=JSON.parse(calls[0][1].body);assert.equal(calls[0][0],'/api/comic-screenplays/projects/p/assistant/references/storyboard-shots');
 assert.deepEqual(body,{session_id:'s',board_id:'b',expected_revision:7,expected_digest:full.digest,offset:100,limit:100});
 await listAssistantReferenceShots('p','s',{...full,digest:'b'.repeat(64),shot_ids:['shot'],board_evidence:full,text:'DO_NOT_SEND',shot_rows:[{excerpt:'DO_NOT_SEND'}]},{offset:0},request);
 assert.equal(JSON.parse(calls[1][1].body).expected_digest,full.digest);assert.ok(!calls[1][1].body.includes('DO_NOT_SEND'));
 assert.throws(()=>listAssistantReferenceShots('p','s',{...full,shot_ids:['shot']},{offset:0},request),/整份/);
 assert.throws(()=>listAssistantReferenceShots('p','s',full,{offset:-1},request));
 assert.throws(()=>listAssistantReferenceShots('p','s',full,{limit:101},request));
});
