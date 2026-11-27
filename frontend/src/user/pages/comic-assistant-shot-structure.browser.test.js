import test from 'node:test';
import assert from 'node:assert/strict';
import { existsSync,mkdirSync,mkdtempSync,writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath,pathToFileURL } from 'node:url';
import { execFileSync } from 'node:child_process';
import { build } from 'esbuild';
const here=path.dirname(fileURLToPath(import.meta.url));
const browser=[process.env.CHROME_PATH,'C:/Program Files/Google/Chrome/Application/chrome.exe','C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(file=>file&&existsSync(file));
const archive=path.resolve(here,'../../../../_local-archive-20261028/luoshui-screenplay-contract-20261114/ui-reconstruction');

async function runCheckCase(name,contents,timeout=60000){
  const bundle=await build({stdin:{resolveDir:here,loader:'jsx',contents},bundle:true,jsx:'automatic',write:false,format:'iife',platform:'browser',loader:{'.css':'empty'}});
  mkdirSync(archive,{recursive:true});
  const dir=mkdtempSync(path.join(archive,name+'-'));
  writeFileSync(path.join(dir,'test.js'),bundle.outputFiles[0].text);
  const html=path.join(dir,'test.html');
  writeFileSync(html,'<meta charset="utf-8"><div id="root"></div><script src="test.js"></script>');
  const output=execFileSync(browser,['--headless=new','--disable-gpu','--no-first-run','--disable-background-networking','--host-resolver-rules=MAP * ~NOTFOUND','--window-size=900,900','--virtual-time-budget=16000','--user-data-dir='+path.join(dir,'profile'),'--dump-dom',pathToFileURL(html).href],{encoding:'utf8',timeout,windowsHide:true,stdio:['ignore','pipe','pipe'],maxBuffer:4000000});
  writeFileSync(path.join(dir,'result.html'),output);
  assert.ok(/data-result="passed"/.test(output),output.match(/data-error="([^"]*)"/)?.[1]||'browser did not finish');
}

test('助手新增删除镜头明确显示目标与保存差异，确认前零写入', {skip:!browser},async()=>{
 await runCheckCase('assistant-shot-structure',`
import React from 'react';import{createRoot}from'react-dom/client';import{ComicAssistantPanel}from './ComicScreenplayAuthoringPanels.jsx';import{setToken,setVerifiedImpersonation}from '../../shared/api/client.js';setToken('fixture');setVerifiedImpersonation({actorUsername:'alice',username:'alice',impersonationId:'none'});
const tick=()=>new Promise(r=>setTimeout(r,120));const assert=(v,m)=>{if(!v)throw Error(m)};
const state={revision:3,messages:[],proposals:[{id:'shot',summary:'删除镜头',status:'pending',actions:[{kind:'creative_shot_structure',target_id:'real-board',creative_structure:{board_id:'real-board',episode_no:2,engine:'prompt_138',operation:'delete',before:[{id:'real-shot',seq:7,line:'删除前台词'},{id:'keep',seq:8}],after:[{id:'keep',seq:1}],targets:[{id:'real-shot',seq:7,line:'删除前台词'}]}}]},{id:'add',summary:'新增镜头',status:'pending',actions:[{kind:'creative_shot_structure',creative_structure:{episode_no:2,engine:'prompt_138',operation:'add',before:[{id:'keep',seq:1}],after:[{id:'keep',seq:1},{id:'new',seq:2,line:'新增台词'}],targets:[{id:'new',seq:2,line:'新增台词'}]}}]}]};
let writes=[];window.fetch=async(url,opts={})=>{if(opts.method==='POST'){writes.push({url:String(url),body:JSON.parse(opts.body)});state.revision++;state.proposals[0].status='applied';}return new Response(JSON.stringify({state}),{status:200,headers:{'Content-Type':'application/json'}})};
(async()=>{createRoot(document.getElementById('root')).render(<ComicAssistantPanel projectId="p" canGenerate={false}/>);
for(let i=0;i<50&&!document.querySelector('.comic-assistant-proposal');i++)await tick();
const proposal=document.querySelector('.comic-assistant-proposal');assert(proposal,'missing proposal');for(const text of ['第2集','镜头7','prompt_138','real-shot','删除前台词','保留原素材文件','修改前','2个镜头','修改后','1个镜头'])assert(proposal.textContent.includes(text),'missing '+text);const add=document.querySelectorAll('.comic-assistant-proposal')[1];for(const text of ['新增镜头','新增台词','new','来源未核验'])assert(add.textContent.includes(text),'missing add '+text);assert(writes.length===0,'wrote before confirmation');
proposal.querySelector('button').click();await tick();await tick();assert(writes.length===0,'popconfirm wrote without confirm');
const confirm=[...document.querySelectorAll('.ant-popover button')].find(b=>b.classList.contains('ant-btn-primary'));assert(confirm,'missing confirmation');confirm.click();await tick();await tick();assert(writes.length===1,'wrong apply count');assert(writes[0].url.endsWith('/projects/p/assistant/apply')&&writes[0].body.proposal_id==='shot'&&writes[0].body.expected_revision===3&&writes[0].body.confirmed===true,'wrong confirmation payload');assert(document.querySelector('.comic-assistant-proposal button').disabled,'applied proposal can repeat');document.body.dataset.result='passed';})().catch(e=>{document.body.dataset.result='failed';document.body.dataset.error=e.message});
`);
});
