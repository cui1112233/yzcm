import test from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, mkdirSync, mkdtempSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { execFileSync } from 'node:child_process';
import { build } from 'esbuild';

const here=path.dirname(fileURLToPath(import.meta.url));
const browser=['C:/Program Files/Google/Chrome/Application/chrome.exe','C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(existsSync);
test('真实章节组件刷新保留分批状态，显式继续只发一次，重新开始需确认，未知结果不自动重发',{skip:!browser},async()=>{
 const fixture=String.raw`
 import React from 'react';import {createRoot} from 'react-dom/client';import {ComicChaptersPanel} from './ComicScreenplayAuthoringPanels.jsx';
 const tick=()=>new Promise(r=>setTimeout(r,80));const assert=(ok,message)=>{if(!ok)throw Error(message)};const wait=async(fn)=>{for(let i=0;i<100;i++){if(fn())return;await tick()}throw Error('wait timed out: '+window.step)};
 const text=el=>el?.textContent.replace(/\s+/g,'');const button=label=>[...document.querySelectorAll('button')].find(el=>text(el)===label);const click=el=>{assert(el&&!el.disabled,'missing enabled button '+window.step);el.click()};
 let state={revision:1,chapters:[{id:'c',title:'章',source_text:'第一句。第二句。',script_text:'旧成功剧本',raw_result:'旧回包'}]},calls=[];let phase=0;window.calls=calls;
 const response=value=>new Response(JSON.stringify(value),{status:200,headers:{'Content-Type':'application/json'}});
 window.fetch=async(url,options={})=>{const method=options.method||'GET';const body=options.body?JSON.parse(options.body):{};calls.push({url:String(url),method,body});
 if(String(url).endsWith('/chapters'))return response({state,access:{read:true,edit:true,generate:true}});
 if(String(url).endsWith('/chapters/c/script')){assert(body.expected_revision===state.revision,'wrong revision');if(body.action==='publish'){assert(phase===2&&state.chapters[0].script_generation.status==='completed','unexpected free publication');assert(!state.chapters[0].script_generation.publication_error&&!state.chapters[0].script_generation.published_document_id,'fixture should represent lost publication marker');state={...state,revision:6,chapters:[{...state.chapters[0],script_generation:{...state.chapters[0].script_generation,published_document_id:'saved-doc'}}]};return response({state,access:{read:true,edit:true,generate:true}});}if(body.action==='recover'){assert(phase===3,'unexpected recovery');assert(!Object.hasOwn(body,'raw_result')&&!Object.hasOwn(body,'receipt_path'),'browser supplied untrusted receipt');state={...state,revision:8,chapters:[{...state.chapters[0],script_generation:{...state.chapters[0].script_generation,status:'awaiting_continue'}}]};return response({state,access:{read:true,edit:true,generate:true}});}phase++;
  if(phase===1){assert(body.action==='start','not start');state={...state,revision:3,chapters:[{...state.chapters[0],script_generation:{id:'run1',status:'awaiting_continue',instruction:'',batches:[{raw_result:'batch1'}]}}]};}
  else if(phase===2){assert(body.action==='continue','not explicit continue');state={...state,revision:5,chapters:[{...state.chapters[0],script_text:'完整新剧本',script_generation:{...state.chapters[0].script_generation,status:'completed',batches:[{},{}]}}]};}
  else {assert(body.action==='restart'&&body.confirmed===true,'restart not confirmed');state={...state,revision:7,chapters:[{...state.chapters[0],script_generation:{id:'run2',status:'unknown',batches:[{}]}}]};}
  return response({state,access:{read:true,edit:true,generate:true}});
 }
 throw Error('unexpected request '+url);};
 const root=createRoot(document.getElementById('root'));let key=0;const mount=async()=>{root.render(<ComicChaptersPanel key={++key} projectId="p"/>);await wait(()=>button('生成本章剧本')||button('继续生成剩余剧本'));await tick()};
 (async()=>{
 window.step='first batch';await mount();const start=button('生成本章剧本');start.click();start.click();await wait(()=>button('继续生成剩余剧本'));assert(phase===1,'double click paid twice');assert([...document.querySelectorAll('textarea')].some(el=>el.value==='旧成功剧本'),'partial overwrote old script');assert(document.body.textContent.includes('本批已保存，待继续'),'missing saved batch state');
 window.step='refresh';root.render(<div/>);await tick();await mount();await wait(()=>button('继续生成剩余剧本'));assert(phase===1,'refresh called model');
 window.step='continue';click(button('继续生成剩余剧本'));await wait(()=>document.body.textContent.includes('本章剧本已全部生成'));assert(phase===2,'continue did not call exactly once');assert(button('生成本章剧本').disabled,'completed can repeat paid start');assert([...document.querySelectorAll('textarea')].some(el=>el.value==='完整新剧本'),'completed script missing');
 window.step='completed publication marker missing after refresh';root.render(<div/>);await tick();await mount();const freeSave=button('保存到剧本合集（不调用模型）');assert(freeSave,'completed missing publication marker has no free save action');click(freeSave);await wait(()=>!button('保存到剧本合集（不调用模型）'));assert(phase===2,'completed publication recovery called model');
 window.step='restart cancel';click(button('重新开始'));await wait(()=>button('取消'));click(button('取消'));await tick();assert(phase===2,'cancel restart called model');
 window.step='restart confirm';click(button('重新开始'));await wait(()=>button('确认重新开始'));click(button('确认重新开始'));await wait(()=>document.body.textContent.includes('结果不确定'));assert(phase===3,'restart not single call');await tick();await tick();assert(phase===3&&button('生成本章剧本').disabled,'unknown automatically retried');assert(!button('继续生成剩余剧本'),'unknown offered continuation');window.step='recover saved receipt';click(button('恢复已返回批次（不调用模型）'));await wait(()=>button('继续生成剩余剧本'));assert(phase===3,'receipt recovery called model');document.body.dataset.result='passed';
 })().catch(error=>{document.body.dataset.result='failed';document.body.dataset.error=error.message;document.body.dataset.step=window.step});
 `;
 const bundle=await build({stdin:{resolveDir:here,contents:fixture,loader:'jsx'},bundle:true,jsx:'automatic',platform:'browser',format:'iife',write:false,loader:{'.css':'empty'}});
 const archive=path.resolve(here,'../../../../_local-archive-20261028/script-batch-293');mkdirSync(archive,{recursive:true});const dir=mkdtempSync(path.join(archive,'ui-'));writeFileSync(path.join(dir,'test.js'),bundle.outputFiles[0].text);const html=path.join(dir,'test.html');writeFileSync(html,'<meta charset="utf-8"><div id="root"></div><script src="test.js"></script>');
 const output=execFileSync(browser,['--headless=new','--disable-gpu','--no-first-run','--disable-background-networking','--host-resolver-rules=MAP * ~NOTFOUND','--virtual-time-budget=20000','--user-data-dir='+path.join(dir,'profile'),'--dump-dom',pathToFileURL(html).href],{encoding:'utf8',timeout:60000,windowsHide:true,stdio:['ignore','pipe','pipe'],maxBuffer:4000000});writeFileSync(path.join(dir,'result.html'),output);assert.ok(/data-result="passed"/.test(output),output.match(/data-error="([^"]*)"/)?.[1]||'browser did not finish');
});
