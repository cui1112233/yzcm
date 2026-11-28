import test from 'node:test';
import assert from 'node:assert/strict';
import {applySessionProposal,changeComicSession,createComicSession,listComicSessions,readComicSession,sendSessionMessage} from './comicScreenplayWorkflow.js';

test('默认会话和独立会话使用对应接口，应用前必须确认',async()=>{
  const calls=[];
  const request=async(url,options={})=>{calls.push({url,method:options.method||'GET',body:options.body&&JSON.parse(options.body)});return {};};
  const input={expected_revision:4,message:'hello'};
  await listComicSessions('p',request);
  await createComicSession('p',{title:'第一稿',visibility:'private'},request);
  await readComicSession('p','one',request);
  await sendSessionMessage('p','default',input,request);
  await sendSessionMessage('p','one',input,request);
  await applySessionProposal('p','default',{proposal_id:'q',confirmed:true},request);
  await applySessionProposal('p','one',{proposal_id:'q',confirmed:true},request);
  await changeComicSession('p','one','stop',{expected_revision:5},request);
  assert.deepEqual(calls.map(x=>x.url.split('/projects/p/')[1]),[
    'assistant/sessions','assistant/sessions','assistant/sessions/one','assistant/chat','assistant/sessions/one/chat',
    'assistant/apply','assistant/sessions/one/apply','assistant/sessions/one/stop',
  ]);
  assert.equal(calls[4].body.expected_revision,4);
  assert.throws(()=>applySessionProposal('p','one',{confirmed:false},request));
  assert.throws(()=>readComicSession('p','../default',request));
  assert.throws(()=>changeComicSession('p','one','delete',{},request));
  assert.equal(calls.length,8);
});
