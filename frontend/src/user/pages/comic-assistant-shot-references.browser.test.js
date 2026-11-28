import test from 'node:test';
import assert from 'node:assert/strict';
import {existsSync,mkdtempSync,writeFileSync,mkdirSync,rmSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {spawn} from 'node:child_process';
import {build} from 'esbuild';
const here=path.dirname(fileURLToPath(import.meta.url));
const browser=['C:/Program Files/Google/Chrome/Application/chrome.exe','C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(existsSync);
const delay=ms=>new Promise(resolve=>setTimeout(resolve,ms));
async function realChromeDOM(html,dir){
 const profile=path.resolve(dir,'profile');
 assert.equal(path.dirname(profile),path.resolve(dir)); // only this run's owned profile may be removed
 const chromeProcess=spawn(browser,['--headless=new','--no-first-run','--disable-background-networking','--host-resolver-rules=MAP * ~NOTFOUND','--remote-debugging-port=0','--user-data-dir='+profile,'about:blank'],{windowsHide:true,stdio:['ignore','ignore','pipe']});
 const exited=new Promise(resolve=>chromeProcess.once('exit',resolve));
 let socket,send,session,dom='',nextID=0;const pending=new Map(),events=new Map();
 try {
  const endpoint=await new Promise((resolve,reject)=>{let output='';const timer=setTimeout(()=>reject(Error('Chrome startup timeout')),15000);chromeProcess.once('error',e=>{clearTimeout(timer);reject(e)});chromeProcess.once('exit',()=>{clearTimeout(timer);reject(Error('Own Chrome exited during startup'))});chromeProcess.stderr.on('data',chunk=>{output+=chunk.toString();const match=output.match(/DevTools listening on (ws:\/\/[^\s]+)/);if(match){clearTimeout(timer);resolve(match[1])}})});
  socket=new WebSocket(endpoint);
  await new Promise((resolve,reject)=>{const timer=setTimeout(()=>reject(Error('Own Chrome CDP connection timeout')),15000);socket.addEventListener('open',()=>{clearTimeout(timer);resolve()},{once:true});socket.addEventListener('error',()=>{clearTimeout(timer);reject(Error('Own Chrome CDP connection failed'))},{once:true})});
  socket.addEventListener('message',event=>{const data=JSON.parse(event.data);if(data.id){const call=pending.get(data.id);if(call){pending.delete(data.id);clearTimeout(call.timer);data.error?call.reject(Error(data.error.message)):call.resolve(data.result)}}else{const key=(data.sessionId||'')+':'+data.method;const waiting=events.get(key);if(waiting){events.delete(key);clearTimeout(waiting.timer);waiting.resolve()}}});
  send=(method,params={},sessionId)=>new Promise((resolve,reject)=>{const id=++nextID;const timer=setTimeout(()=>{pending.delete(id);reject(Error('CDP timed out: '+method))},25000);pending.set(id,{resolve,reject,timer});socket.send(JSON.stringify({id,method,params,...(sessionId?{sessionId}:{})}))});
  const target=await send('Target.createTarget',{url:'about:blank'});session=(await send('Target.attachToTarget',{targetId:target.targetId,flatten:true})).sessionId;
  await send('Page.enable',{},session);await send('Runtime.enable',{},session);
  const loaded=new Promise((resolve,reject)=>{const timer=setTimeout(()=>reject(Error('QA HTML load timeout')),15000);events.set(session+':Page.loadEventFired',{timer,resolve})});
  await send('Page.navigate',{url:pathToFileURL(html).href},session);await loaded;
  await send('Runtime.evaluate',{expression:`new Promise(resolve=>{let n=0;const poll=()=>{if(document.body?.dataset.result||document.body?.dataset.error||++n>800){resolve(document.body?.dataset.result||document.body?.dataset.error||'timeout');return}setTimeout(poll,25)};poll()})`,awaitPromise:true,returnByValue:true},session);
  dom=(await send('Runtime.evaluate',{expression:'document.documentElement.outerHTML',returnByValue:true},session)).result.value;
  return dom;
 }catch(error){
  if(send&&session){try{dom=(await send('Runtime.evaluate',{expression:'document.documentElement.outerHTML',returnByValue:true},session)).result.value}catch{}}
  writeFileSync(path.join(dir,'failed-dom.html'),dom);throw Error(dom.match(/data-error="([^"]*)"/)?.[1]||error.message);
 }finally{
  if(socket?.readyState===1&&send){try{await send('Browser.close')}catch{}}
  socket?.close();for(const call of pending.values()){clearTimeout(call.timer);call.reject(Error('Own Chrome closed'))}pending.clear();for(const event of events.values())clearTimeout(event.timer);events.clear();
  if(!await Promise.race([exited.then(()=>true),delay(2000).then(()=>false)])){chromeProcess.kill();await Promise.race([exited,delay(2000)])}
  rmSync(profile,{recursive:true,force:true,maxRetries:10,retryDelay:100});
 }
}
async function run(scenario){
 const contents=common+scenario+`})().then(()=>document.body.dataset.result='passed').catch(e=>document.body.dataset.error=e.message+' at '+document.body.dataset.stage);`;
 const bundle=await build({stdin:{resolveDir:here,loader:'jsx',contents},bundle:true,jsx:'automatic',write:false,format:'iife',platform:'browser',minify:true,loader:{'.css':'empty'}});
 const archive=path.resolve(here,'../../../../_local-archive-20261028/luoshui-parity-20261126/shot-reference-browser');mkdirSync(archive,{recursive:true});const dir=mkdtempSync(path.join(archive,'run-'));writeFileSync(path.join(dir,'app.js'),bundle.outputFiles[0].text);const html=path.join(dir,'index.html');writeFileSync(html,'<meta charset="utf-8"><div id="root"></div><script src="app.js"></script>');
 const dom=await realChromeDOM(html,dir);
 writeFileSync(path.join(dir,'dom.html'),dom);assert.ok(dom.includes('data-result="passed"'),dom.match(/data-error="([^"]*)"/)?.[1]||dom.slice(-800));
}
const common=`import React from 'react';import {createRoot} from 'react-dom/client';import Picker from './ComicAssistantReferencePicker.jsx';import {setToken,setVerifiedImpersonation} from '../../shared/api/client.js';
 setToken('fixture');setVerifiedImpersonation({actorUsername:'alice',username:'alice',impersonationId:'none'});
 const tick=()=>new Promise(r=>setTimeout(r,30));const wait=async(f,stage)=>{document.body.dataset.stage=stage;for(let i=0;i<120;i++){if(f())return;await tick()}throw Error('wait '+stage+' '+document.body.textContent.slice(-500))};const check=(ok,msg)=>{if(!ok)throw Error(msg)};
 const modal=cls=>[...document.querySelectorAll('.'+cls)].find(e=>getComputedStyle(e.closest('.ant-modal-wrap')).display!=='none'&&getComputedStyle(e).visibility!=='hidden'&&!e.classList.contains('ant-zoom-enter-prepare')&&!e.classList.contains('ant-zoom-leave'));const btn=(text,cls)=>[...(cls?modal(cls)||document.createElement('div'):document).querySelectorAll('button')].find(b=>b.textContent.replaceAll(' ','').includes(text));
 const click=(text,cls)=>{const b=btn(text,cls);if(!b)throw Error('missing button '+text+' at '+document.body.dataset.stage+' class='+cls+' modal='+!!modal(cls)+' buttons='+[...document.querySelectorAll('button')].map(b=>b.textContent).join('|'));b.click()};const parent='comic-assistant-reference-modal',child='comic-assistant-shot-modal';const box=n=>modal(child)?.querySelector('input[aria-label="镜头'+n+'"]');
 const reply=(data,status=200)=>new Response(JSON.stringify(data),{status,headers:{'Content-Type':'application/json'}});
 let board={kind:'storyboard',id:'board',name:'第2集 · seedance',revision:7,digest:'a'.repeat(64),bytes:450000};let directoryCalls=0,pageCalls=[],resolveCalls=[],holdPage=false,holdResolve=false,finishPage,finishResolve;
 window.fetch=async(input,opts={})=>{const url=String(input);if(url.endsWith('/api/client-errors'))return reply({});if(opts.method!=='POST'){directoryCalls++;return reply({references:[board],total:1,next_offset:1})};const body=JSON.parse(opts.body);
 if(url.endsWith('/storyboard-shots')){pageCalls.push(body);if(body.expected_digest!==board.digest||body.expected_revision!==board.revision)return reply({error:'分镜已变化，请重新选择'},409);const start=body.offset||0,end=Math.min(start+100,205);const data={board:{id:board.id,name:board.name,revision:board.revision,digest:board.digest},offset:start,next_offset:end,total:205,shots:Array.from({length:end-start},(_,i)=>({id:'real-'+(start+i),seq:start+i+1,position:start+i,excerpt:'摘录'+(start+i),excerpt_truncated:false}))};if(holdPage)return new Promise(r=>finishPage=()=>r(reply(data)));return reply(data)}
 resolveCalls.push(body);const ref=body.references[0];if(!ref?.shot_ids?.length)return reply({error:'整份超过16KiB'},400);const snapshot={kind:'storyboard',id:'board',name:board.name,revision:board.revision,digest:'b'.repeat(64),shot_ids:ref.shot_ids,text:'FULLTEXT '+ref.shot_ids.join(','),bytes:50};if(holdResolve)return new Promise(r=>finishResolve=()=>r(reply({references:[snapshot]})));return reply({references:[snapshot]});};
 function Harness({session='one'}){React.useEffect(()=>{window.effectReady=true},[]);const [drafts,setDrafts]=React.useState({});window.refs=drafts[session]||[];window.setRefs=refs=>setDrafts(old=>({...old,[session]:refs}));return <Picker projectId="p" sessionId={session} references={window.refs} onChange={window.setRefs}/>};const root=createRoot(document.getElementById('root'));const render=session=>root.render(<Harness session={session}/>);
 const open=async()=>{await wait(()=>[...document.querySelectorAll('.'+parent)].every(e=>getComputedStyle(e.closest('.ant-modal-wrap')).display==='none'),'previous parent closed');const before=directoryCalls;click('作品内容');await wait(()=>directoryCalls>before&&modal(parent)&&btn('选择镜头',parent)&&!btn('选择镜头',parent).disabled,'parent directory')};const openShots=async()=>{document.body.dataset.stage='open shots';click('选择镜头',parent);await wait(()=>box(1)&&!box(1).disabled,'shot first page');};
 (async()=>{render('one');await wait(()=>btn('作品内容')&&window.effectReady,'initial');`;

test('镜头跨页真实ID、空选取消、预览应用、重开全量证据及整份互斥',{skip:!browser},()=>run(`
 await open();await openShots();click('下一页',child);await wait(()=>box(101)&&!box(101).disabled,'page 2');click('下一页',child);await wait(()=>box(201)&&!box(201).disabled,'page 3');box(201).click();await tick();click('上一页',child);await wait(()=>box(101)&&!box(101).disabled,'back page 2');click('上一页',child);await wait(()=>box(1)&&!box(1).disabled,'back page 1');box(1).click();await tick();click('使用所选镜头',child);await wait(()=>!modal(child),'range selected');check(btn('使用这些引用',parent).disabled,'applied before preview');click('预览所选文字',parent);await wait(()=>!btn('使用这些引用',parent).disabled,'range preview');check(JSON.stringify(resolveCalls.at(-1).references[0].shot_ids)===JSON.stringify(['real-0','real-200']),'IDs or board order incorrect');click('使用这些引用',parent);await wait(()=>refs.length===1,'apply');check(refs[0].digest==='b'.repeat(64)&&refs[0].board_evidence.digest==='a'.repeat(64),'range/full evidence mixed');check(document.body.textContent.includes('镜头1、201'),'range chip missing');
 await open();await openShots();check(pageCalls.at(-1).expected_digest==='a'.repeat(64),'range digest used on reopen');click('清空选中',child);await tick();check(btn('使用所选镜头',child).disabled,'empty range becomes whole board');click('取消',child);await wait(()=>!modal(child),'cancel empty');check(btn('使用这些引用',parent).disabled,'cancel retained old preview');click('预览所选文字',parent);await wait(()=>!btn('使用这些引用',parent).disabled,'preview retained range');check(resolveCalls.at(-1).references[0].shot_ids.length===2,'empty cancel silently selected whole board');
 const whole=modal(parent).querySelector('input[aria-label="整份分镜 board"]');whole.click();await tick();check(btn('使用这些引用',parent).disabled,'whole toggle kept range preview');click('预览所选文字',parent);await wait(()=>document.body.textContent.includes('整份超过16KiB'),'whole preview result');check(resolveCalls.at(-1).references.length===1&&!resolveCalls.at(-1).references[0].shot_ids,'whole/range not exclusive');
`));

test('范围修改作废未决父预览，stale明确重选，切会话作废旧分页',{skip:!browser},()=>run(`
 await open();await openShots();box(1).click();await tick();click('使用所选镜头',child);await wait(()=>!modal(child),'one chosen');holdResolve=true;click('预览所选文字',parent);await wait(()=>finishResolve,'pending parent preview');await openShots();box(1).click();box(2).click();await tick();click('使用所选镜头',child);await wait(()=>!modal(child),'modified');finishResolve();await tick();check(btn('使用这些引用',parent).disabled,'late old preview restored apply');holdResolve=false;click('预览所选文字',parent);await wait(()=>!btn('使用这些引用',parent).disabled,'new preview');check(JSON.stringify(resolveCalls.at(-1).references[0].shot_ids)==='["real-1"]','modified IDs lost');click('使用这些引用',parent);await wait(()=>refs.length===1,'applied modified');
 board={...board,revision:8,digest:'c'.repeat(64)};await open();click('选择镜头',parent);await wait(()=>modal(child)?.textContent.includes('分镜已变化'),'stale old full token');check(pageCalls.at(-1).expected_digest==='a'.repeat(64),'old selection silently re-signed');click('返回并重新选择',child);await wait(()=>!modal(child)&&btn('选择镜头',parent),'explicit reset');await openShots();check(btn('使用所选镜头',child).disabled,'stale IDs retained after explicit reset');check(pageCalls.at(-1).expected_digest==='c'.repeat(64),'fresh version not explicitly selected');
 click('取消',child);await wait(()=>!modal(child),'close before pending page');holdPage=true;click('选择镜头',parent);await wait(()=>finishPage,'pending page');render('two');await tick();finishPage();await tick();check(!modal(child)&&!modal(parent)&&refs.length===0,'old page entered another session');
`));

test('缺整份证据显式重选，关闭父预览和取消分页后重开拒绝旧回包',{skip:!browser},()=>run(`
 setRefs([{...board,digest:'b'.repeat(64),shot_ids:['real-0'],text:'old range'}]);await wait(()=>refs.length===1,'old range fixture');await open();const pagesBefore=pageCalls.length;click('选择镜头',parent);await wait(()=>modal(child)?.textContent.includes('缺少整份'),'missing full evidence');check(pageCalls.length===pagesBefore,'range digest was sent as directory evidence');click('返回并重新选择',child);await wait(()=>!modal(child)&&btn('选择镜头',parent)&&!btn('选择镜头',parent).disabled,'reset missing evidence');await openShots();box(1).click();await tick();click('使用所选镜头',child);await wait(()=>!modal(child),'range ready');
 holdResolve=true;click('预览所选文字',parent);await wait(()=>finishResolve,'pending preview to close');click('取消',parent);await wait(()=>!modal(parent),'closed parent');holdResolve=false;await open();check(btn('使用这些引用',parent).disabled,'reopen reused pending preview');finishResolve();await tick();check(btn('使用这些引用',parent).disabled,'closed preview late result restored apply');
 click('选择镜头',parent);await wait(()=>modal(child)?.textContent.includes('缺少整份'),'still requires explicit selection');click('返回并重新选择',child);await wait(()=>!modal(child)&&btn('选择镜头',parent)&&!btn('选择镜头',parent).disabled,'reset again');await openShots();box(1).click();await tick();click('使用所选镜头',child);await wait(()=>!modal(child),'fresh chosen');await openShots();holdPage=true;click('下一页',child);await wait(()=>finishPage,'pending page to cancel');click('取消',child);await wait(()=>!modal(child),'cancel page');holdPage=false;await openShots();check(box(1).checked,'cancel lost current range');finishPage();await tick();check(box(1)&&!box(101),'old page replaced reopened page');box(1).click();box(2).click();await tick();click('使用所选镜头',child);await wait(()=>!modal(child),'final range');click('预览所选文字',parent);await wait(()=>!btn('使用这些引用',parent).disabled,'final preview');click('使用这些引用',parent);await wait(()=>refs[0]?.shot_ids?.[0]==='real-1','new range applied');
`));
