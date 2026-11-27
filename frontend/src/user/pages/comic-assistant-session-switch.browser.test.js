import test from 'node:test';
import assert from 'node:assert/strict';
import { existsSync, mkdtempSync, writeFileSync } from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { execFileSync } from 'node:child_process';
import { build } from 'esbuild';

const here = path.dirname(fileURLToPath(import.meta.url));
const browser = [process.env.CHROME_PATH, 'C:/Program Files/Google/Chrome/Application/chrome.exe', 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(p => p && existsSync(p));

test('切换项目或会话时保留各自草稿，旧回复只写回发送会话', {skip: !browser}, async () => {
  const contents = `
    import React from 'react';
    import {createRoot} from 'react-dom/client';
    import {useComicAssistantSessions} from './useComicAssistantSessions.js';
    import {setToken,setVerifiedImpersonation} from '../../shared/api/client.js';
    setToken('fixture');setVerifiedImpersonation({actorUsername:'alice',username:'alice',impersonationId:'none'});
    const sessions = ['default','one','two'].map(id=>({id,title:id,mode:'confirm',active:true,revision:1}));
    const tick=()=>new Promise(r=>setTimeout(r,30));
    const wait=async test=>{for(let i=0;i<100;i++){if(test())return;await tick();}throw Error('wait timed out')};
    const check=(ok,why)=>{if(!ok)throw Error(why)};
    const root=createRoot(document.getElementById('root'));
    const render=project=>root.render(<Harness project={project}/>);
    function Harness({project}){const v=useComicAssistantSessions(project);window.view=v;return <span>{v.session.id}</span>}
    let finish, posted;
    const reply=data=>new Response(JSON.stringify(data),{headers:{'Content-Type':'application/json'}});
    window.fetch=async (input,opts={})=>{
      const parts=String(input).split('/');const last=parts.at(-1);
      if(opts.method==='POST'){posted=String(input);return new Promise(resolve=>{finish=()=>resolve(reply({revision:3,messages:[{role:'assistant',content:'only one'}],proposals:[]}))});}
      if(last==='sessions')return reply({sessions});
      if(last==='chat')return reply({state:{revision:1,messages:[],proposals:[]}});
      return reply({session:sessions.find(s=>s.id===last),messages:[],proposals:[]});
    };
    (async()=>{
      render('p');await wait(()=>window.view?.list?.length===3);
      view.select('one');await wait(()=>view.session.id==='one' && !view.current.loading);
      view.setText('first draft');await tick();const running=view.write('chat',{message:'first draft'});
      await wait(()=>finish);check(posted.endsWith('/projects/p/assistant/sessions/one/chat'),'wrong send path');
      view.select('two');await wait(()=>view.session.id==='two'&&!view.current.loading);
      view.setText('second draft');await tick();
      check(!view.current.busy,'new session inherits busy');
      render('q');await wait(()=>view.session.id==='default'&&!view.current.loading);
      view.setText('other project draft');await tick();
      finish();await running;await tick();
      check(view.current.text==='other project draft'&&!view.current.state.messages.length,'late response entered other project');
      render('p');await wait(()=>view.session.id==='two'&&!view.current.loading);
      check(view.current.text==='second draft'&&!view.current.state.messages.length,'late response overwrote second session');
      view.select('one');await wait(()=>view.session.id==='one'&&!view.current.loading);
      check(view.current.state.messages[0]?.content==='only one'&&view.current.text==='','response lost or draft not cleared');
      document.body.dataset.result='passed';
    })().catch(e=>document.body.dataset.error=e.message);
  `;
  const bundle = await build({stdin:{resolveDir:here,loader:'jsx',contents},bundle:true,jsx:'automatic',write:false,format:'iife',platform:'browser',loader:{'.css':'empty'}});
  const dir = mkdtempSync(path.join(os.tmpdir(),'assistant-sessions-'));
  writeFileSync(path.join(dir,'app.js'),bundle.outputFiles[0].text);
  const html=path.join(dir,'index.html');
  writeFileSync(html,'<meta charset="utf-8"><div id="root"></div><script src="app.js"></script>');
  const dom=execFileSync(browser,['--headless=new','--no-first-run','--disable-background-networking','--host-resolver-rules=MAP * ~NOTFOUND','--virtual-time-budget=15000','--user-data-dir='+path.join(dir,'profile'),'--dump-dom',pathToFileURL(html).href],{encoding:'utf8',timeout:60000,windowsHide:true});
  assert.ok(dom.includes('data-result="passed"'),dom.match(/data-error="([^"]*)"/)?.[1]||'browser did not finish');
});


test('新会话面板只在服务器能力开放时允许发送，只读问答仍可发送', {skip: !browser}, async () => {
  const contents = `
    import React from 'react';
    import {createRoot} from 'react-dom/client';
    import {flushSync} from 'react-dom';
    import {ComicAssistantPanel} from './ComicScreenplayAuthoringPanels.jsx';
    window.fetch=()=>{throw Error('unexpected network')};
    window.sent=0;
    const noop=()=>{};
    window.fixture={current:{text:'hello',busy:'',loading:false,error:'',state:{messages:[],proposals:[]}},session:{id:'one',active:true,mode:'readonly',chat_available:false},list:[],refresh:noop,setText:noop,write:()=>{window.sent++},select:noop,manage:noop,create:noop,loadList:noop};
    const root=createRoot(document.getElementById('root'));
    const tick=()=>new Promise(r=>setTimeout(r,30));
    const check=(ok,why)=>{if(!ok)throw Error(why)};
    const button=()=>document.querySelector('.comic-assistant-compose button');
    const render=(key,canGenerate=true)=>flushSync(()=>root.render(<ComicAssistantPanel key={key} projectId="p" readOnly={true} canGenerate={canGenerate}/>));
    (async()=>{
      render('closed');await tick();
      check(button()?.disabled,'closed channel enabled send');
      check(document.body.textContent.includes('新会话暂未开放模型聊天'),'closed notice missing');
      window.fixture.session={...window.fixture.session,chat_available:true};
      render('open');await tick();
      check(!button()?.disabled,'valid server capability blocked readonly chat');
      check(!document.body.textContent.includes('新会话暂未开放模型聊天'),'open channel still says closed');
      button().click();await tick();check(window.sent===1,'open send did not run');
      render('no-grant',false);await tick();check(button()?.disabled,'capability bypassed project grant');
      document.body.dataset.result='passed';
    })().catch(e=>document.body.dataset.error=e.message);
  `;
  const bundle=await build({stdin:{resolveDir:here,loader:'jsx',contents},bundle:true,jsx:'automatic',write:false,format:'iife',platform:'browser',loader:{'.css':'empty'},plugins:[{name:'local-server-response-fixture',setup(b){b.onLoad({filter:/useComicAssistantSessions\.js$/},()=>({contents:'export function useComicAssistantSessions(){return window.fixture}',loader:'js'}));}}]});
  const dir=mkdtempSync(path.join(os.tmpdir(),'assistant-availability-'));
  writeFileSync(path.join(dir,'app.js'),bundle.outputFiles[0].text);
  const html=path.join(dir,'index.html');writeFileSync(html,'<meta charset="utf-8"><div id="root"></div><script src="app.js"></script>');
  const dom=execFileSync(browser,['--headless=new','--no-first-run','--disable-background-networking','--host-resolver-rules=MAP * ~NOTFOUND','--virtual-time-budget=15000','--user-data-dir='+path.join(dir,'profile'),'--dump-dom',pathToFileURL(html).href],{encoding:'utf8',timeout:60000,windowsHide:true,maxBuffer:8*1024*1024});
  assert.ok(dom.includes('data-result="passed"'),dom.match(/data-error="([^"]*)"/)?.[1]||'browser did not finish');
});
