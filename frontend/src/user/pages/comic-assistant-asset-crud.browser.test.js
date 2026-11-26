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

test('助手新增删除方案正确显示，确认后只提交一次', {skip:!browser},async()=>{
 await runCheckCase('assistant-asset-crud',`
import React from 'react';import{createRoot}from'react-dom/client';import{ComicAssistantPanel}from './ComicScreenplayAuthoringPanels.jsx';import{setToken,setVerifiedImpersonation}from '../../shared/api/client.js';setToken('fixture');setVerifiedImpersonation({actorUsername:'alice',username:'alice',impersonationId:'none'});
const tick=()=>new Promise(r=>setTimeout(r,120));const assert=(v,m)=>{if(!v)throw Error(m)};
const state={revision:3,messages:[],proposals:[{id:'add',summary:'新增人物甲',status:'pending',actions:[{kind:'asset',asset_operation:'add',target_id:'asset-new',asset:{name:'甲'}}]},{id:'delete',summary:'删除道具信封',status:'pending',actions:[{kind:'asset',asset_operation:'delete',target_id:'asset-old',asset:{name:'信封'}}]}]};
let writes=[];window.fetch=async(url,opts={})=>{if(opts.method==='POST'){writes.push({url:String(url),body:JSON.parse(opts.body)});state.revision++;state.proposals[0].status='applied';state.proposals[1].status='stale';}return new Response(JSON.stringify({state}),{status:200,headers:{'Content-Type':'application/json'}})};
(async()=>{createRoot(document.getElementById('root')).render(<ComicAssistantPanel projectId="p" canGenerate={false}/>);
for(let i=0;i<50&&!document.querySelector('.comic-assistant-proposal');i++)await tick();
const proposals=[...document.querySelectorAll('.comic-assistant-proposal')];assert(proposals.length===2,'missing proposals');assert(proposals[0].textContent.includes('新增资产')&&proposals[0].textContent.includes('甲'),'add content wrong');assert(proposals[1].textContent.includes('删除资产')&&proposals[1].textContent.includes('信封')&&proposals[1].textContent.includes('保留原素材文件'),'delete content wrong');assert(writes.length===0,'wrote before confirmation');
proposals[0].querySelector('button').click();await tick();await tick();assert(writes.length===0,'popconfirm wrote without confirm');
const confirm=[...document.querySelectorAll('.ant-popover button')].find(b=>b.classList.contains('ant-btn-primary'));assert(confirm,'missing confirmation');confirm.click();await tick();await tick();assert(writes.length===1,'wrong apply count');assert(writes[0].url.endsWith('/projects/p/assistant/apply')&&writes[0].body.proposal_id==='add'&&writes[0].body.expected_revision===3&&writes[0].body.confirmed===true,'wrong confirmation payload');assert(document.querySelector('.comic-assistant-proposal button').disabled,'applied proposal can repeat');document.body.dataset.result='passed';})().catch(e=>{document.body.dataset.result='failed';document.body.dataset.error=e.message});
`);
});
