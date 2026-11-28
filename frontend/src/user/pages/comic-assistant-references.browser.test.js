import test from 'node:test';
import assert from 'node:assert/strict';
import {existsSync,mkdtempSync,writeFileSync,mkdirSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {execFileSync} from 'node:child_process';
import {build} from 'esbuild';
const here=path.dirname(fileURLToPath(import.meta.url));
const browser=['C:/Program Files/Google/Chrome/Application/chrome.exe','C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(existsSync);
async function run(contents){
 const bundle=await build({stdin:{resolveDir:here,loader:'jsx',contents},bundle:true,jsx:'automatic',write:false,format:'iife',platform:'browser',minify:true,loader:{'.css':'empty'}});
 const archive=path.resolve(here,'../../../../_local-archive-20261028/luoshui-parity-20261126/reference-browser');mkdirSync(archive,{recursive:true});const dir=mkdtempSync(path.join(archive,'run-'));
 writeFileSync(path.join(dir,'app.js'),bundle.outputFiles[0].text);const html=path.join(dir,'index.html');writeFileSync(html,'<meta charset="utf-8"><div id="root"></div><script src="app.js"></script>');
 let dom;
 try {dom=execFileSync(browser,['--headless=new','--no-first-run','--disable-background-networking','--host-resolver-rules=MAP * ~NOTFOUND','--virtual-time-budget=8000','--user-data-dir='+path.join(dir,'profile'),'--dump-dom',pathToFileURL(html).href],{encoding:'utf8',timeout:60000,windowsHide:true,maxBuffer:8*1024*1024});}
 catch(error){dom=String(error.stdout||'');writeFileSync(path.join(dir,'failed-dom.html'),dom);throw Error(dom.match(/data-error="([^"]*)"/)?.[1]||`Browser timed out at ${dom.match(/data-stage="([^"]*)"/)?.[1]||'startup'}; evidence ${dir}`);}
 assert.ok(dom.includes('data-result="passed"'),dom.match(/data-error="([^"]*)"/)?.[1]||dom.slice(-1000));
}
const common=`import React from 'react';import {createRoot} from 'react-dom/client';
 import {setToken,setVerifiedImpersonation} from '../../shared/api/client.js';setToken('fixture');setVerifiedImpersonation({actorUsername:'alice',username:'alice',impersonationId:'none'});
 const tick=()=>new Promise(r=>setTimeout(r,30));const wait=async f=>{for(let i=0;i<120;i++){if(f())return;await tick()}throw Error('wait timed out at '+document.body.dataset.stage+' '+document.body.textContent.slice(-1000))};const check=(ok,why)=>{if(!ok)throw Error(why)};const reply=data=>new Response(JSON.stringify(data),{headers:{'Content-Type':'application/json'}});const button=text=>[...document.querySelectorAll('button')].find(b=>b.textContent.replaceAll(' ','').includes(text));
 const ref={kind:'asset_text',id:'a',name:'角色A',revision:2,digest:'d'.repeat(64),bytes:12,text:'真实资产文字'};
 const root=createRoot(document.getElementById('root'));`;
test('真实选择器目录、预览、确认和移除；旧预览不能覆盖新会话', {skip:!browser},()=>run(common+`
 import Picker from './ComicAssistantReferencePicker.jsx';
 let posted,finish,slow=false;window.fetch=async(input,opts={})=>{if(opts.method==='POST'){posted=JSON.parse(opts.body);if(slow)return new Promise(resolve=>finish=()=>resolve(reply({references:[ref]})));return reply({references:[ref]})}return reply({references:[{...ref,text:''}],next_offset:1,total:1})};
 function Harness({session}){const [refs,setRefs]=React.useState([]);window.refs=refs;return <Picker key={session} projectId="p" sessionId={session} references={refs} onChange={setRefs}/>};const render=s=>root.render(<Harness key={s} session={s}/>);
 (async()=>{render('one');await wait(()=>button('作品内容'));button('作品内容').click();document.body.dataset.stage='directory';await wait(()=>document.querySelector('input[type=checkbox]')&&!document.querySelector('input[type=checkbox]').disabled&&!document.querySelector('input[type=checkbox]').checked);document.querySelector('input[type=checkbox]').click();await tick();button('预览所选文字').click();document.body.dataset.stage='preview';await wait(()=>document.body.textContent.includes('真实资产文字'));check(posted.session_id==='one'&&!JSON.stringify(posted).includes('真实资产文字'),'preview did not send strict selector');button('使用这些引用').click();document.body.dataset.stage='apply';await wait(()=>window.refs.length===1);check(refs[0].digest===ref.digest,'snapshot missing');
 document.querySelector('.comic-assistant-reference-picker .ant-tag-close-icon').click();await wait(()=>window.refs.length===0);
 slow=true;button('作品内容').click();document.body.dataset.stage='directory';await wait(()=>document.querySelector('input[type=checkbox]')&&!document.querySelector('input[type=checkbox]').disabled&&!document.querySelector('input[type=checkbox]').checked);document.querySelector('input[type=checkbox]').click();await tick();button('预览所选文字').click();document.body.dataset.stage='delayed-preview';await wait(()=>finish);render('two');await tick();finish();await tick();check(refs.length===0&&!document.body.textContent.includes('真实资产文字'),'late preview leaked across session');document.body.dataset.result='passed';})().catch(e=>document.body.dataset.error=e.message);
`));
test('引用草稿按作品会话保存，发送只传selector，旧回复不清掉新引用', {skip:!browser},()=>run(common+`
 import {useComicAssistantSessions} from './useComicAssistantSessions.js';
 let posted,finish;const sessions=['default','one','two'].map(id=>({id,title:id,active:true,mode:'confirm',revision:1,chat_available:true}));
 window.fetch=async(input,opts={})=>{const last=String(input).split('/').at(-1);if(opts.method==='POST'){posted={path:String(input),body:JSON.parse(opts.body)};return new Promise(resolve=>finish=()=>resolve(reply({revision:3,messages:[{role:'user',content:'sent',references:[ref]}],proposals:[]})))}if(last==='sessions')return reply({sessions});if(last==='chat')return reply({state:{revision:1,messages:[],proposals:[]}});return reply({session:sessions.find(s=>s.id===last),messages:[],proposals:[]})};
 function Harness(){window.v=useComicAssistantSessions('p');return <span>{v.session.id}</span>}root.render(<Harness/>);
 (async()=>{await wait(()=>window.v?.list.length===3);v.select('one');await wait(()=>v.session.id==='one'&&!v.current.loading);v.setReferences([ref]);v.setText('sent');await tick();const pending=v.write('chat',{message:'sent'});await wait(()=>finish);check(posted.path.endsWith('/sessions/one/chat')&&!JSON.stringify(posted.body).includes('真实资产文字'),'wrong captured request');v.setReferences([{...ref,id:'new',name:'下一稿'}]);await tick();v.select('two');await wait(()=>v.session.id==='two'&&!v.current.loading);check(!v.current.references?.length,'other session inherited references');v.setReferences([{...ref,id:'two'}]);await tick();finish();await pending;await tick();check(v.current.references[0].id==='two','late cleared other draft');v.select('one');await wait(()=>v.session.id==='one'&&!v.current.loading);check(v.current.references[0].id==='new','late cleared new references');check(v.current.state.messages[0].references[0].text==='真实资产文字','history snapshot lost');document.body.dataset.result='passed';})().catch(e=>document.body.dataset.error=e.message);
`));
