import test from 'node:test';
import assert from 'node:assert/strict';
import {existsSync,mkdirSync,mkdtempSync,writeFileSync} from 'node:fs';
import path from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import {execFileSync} from 'node:child_process';
import {build} from 'esbuild';
const here=path.dirname(fileURLToPath(import.meta.url));
const browser=['C:/Program Files/Google/Chrome/Application/chrome.exe','C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(existsSync);
test('138生产等待明确确认才续批，待核对状态不提供重发入口',{skip:!browser},async()=>{
 const out=await build({stdin:{resolveDir:here,loader:'jsx',contents:`
 import React from 'react';import {createRoot} from 'react-dom/client';import Production from './ComicScreenplayProductionPanel.jsx';import {setToken} from '../../shared/api/client.js';import {rememberVerifiedImpersonation} from '../../shared/api/accountImpersonationState.js';setToken('test-token');rememberVerifiedImpersonation({actorUsername:'alice',username:'alice',impersonationId:'self'},'test-token');
 const tick=()=>new Promise(r=>setTimeout(r,140));const assert=(value,message)=>{if(!value)throw Error(message)};
 let productionStatus='blocked';const job=status=>({status:productionStatus,shots:[],batch:{steps:{'5_storyboard':{key:'5_storyboard',status:'blocked',episodes:{1:{episode_no:1,storyboard_generation:{request_key:'batch-138',status,chapter_id:'c1'}}}}}}});
 let state='awaiting_continue';window.writes=[];window.fetch=async(url,options={})=>{if(options.method&&options.method!=='GET'){window.writes.push({url:String(url),body:JSON.parse(options.body)});return new Response(JSON.stringify(job('unknown')),{headers:{'Content-Type':'application/json'}});}return new Response(JSON.stringify(job(state)),{headers:{'Content-Type':'application/json'}});};
 const root=createRoot(document.getElementById('root'));const button=text=>[...document.querySelectorAll('button')].find(b=>b.textContent.replace(/\\s/g,'')===text);
 (async()=>{root.render(<Production projectId="a" canGenerate/>);await tick();await tick();await tick();
 assert(document.body.textContent.includes('等待确认继续分镜'),'missing waiting state');
 assert(button('启动后台生产')?.disabled,'start can bypass wait');
 button('继续下一批分镜').click();await tick();await tick();assert(window.writes.length===0,'submitted before confirmation');
 const ok=button('确认生成下一批');assert(ok,'missing explicit confirmation');ok.click();await tick();await tick();await tick();
 assert(window.writes.length===1,'duplicate or missing continuation: '+JSON.stringify(window.writes));assert(window.writes[0].url.endsWith('/production/storyboard/continue'),'wrong endpoint');assert(JSON.stringify(window.writes[0].body)===JSON.stringify({request_key:'batch-138',confirmed:true}),'wrong confirmation request');
 assert(!button('继续下一批分镜'),'unknown allows replay');assert(button('启动后台生产')?.disabled,'unknown allows start replay');
 assert(document.body.textContent.includes('恢复当前批不重新调用模型；生产未暂停时，完整恢复后继续后续步骤'),'missing no model explanation');const recover=button('恢复已返回回包');assert(recover&&!recover.disabled,'missing explicit recovery');assert(window.writes.length===1,'auto recovery submitted');recover.click();await tick();await tick();assert(window.writes.length===2,'recovery not once');assert(window.writes[1].url.endsWith('/production/storyboard/recover'),'recovery wrong endpoint');assert(JSON.stringify(window.writes[1].body)===JSON.stringify({request_key:'batch-138'}),'recovery sends generation authorization');
 root.render(<Production projectId="b" canGenerate/>);state='awaiting_continue';await tick();await tick();button('继续下一批分镜').click();await tick();await tick();const stale=button('确认生成下一批');root.render(<Production projectId="c" canGenerate/>);await tick();await tick();stale.click();await tick();assert(window.writes.length===2,'stale confirmation submitted');
 productionStatus='paused';state='succeeded';root.render(<Production projectId="d" canGenerate/>);await tick();await tick();assert(!button('恢复已返回回包'),'completed paused recovery still blocks');assert(button('继续/重试任务')&&!button('继续/重试任务').disabled,'completed paused recovery prevents explicit start');assert(window.writes.length===2,'paused recovery auto started');
 productionStatus='blocked';state='failed';root.render(<Production projectId="e" canGenerate/>);await tick();await tick();assert(button('恢复已返回回包')&&!button('恢复已返回回包').disabled,'failed received body has no recovery');assert(!button('继续下一批分镜'),'failed allows paid continuation');assert(button('启动后台生产')?.disabled,'failed bypasses checkpoint');assert(document.body.textContent.includes('未提交或回包不合规时将保留原错误'),'failed claims all errors have a valid receipt');assert(window.writes.length===2,'failed recovery auto submitted');button('恢复已返回回包').click();await tick();await tick();assert(window.writes.length===3&&window.writes[2].url.endsWith('/production/storyboard/recover'),'failed recovery did not use free endpoint');
 document.body.dataset.result='passed';})().catch(e=>{document.body.dataset.result='failed';document.body.dataset.error=e.message});
 `},bundle:true,jsx:'automatic',write:false,format:'iife',platform:'browser',loader:{'.css':'empty'}});
 const base=path.resolve(here,'../../../../_local-archive-20261028/luoshui-screenplay-contract-20261114/production-138-ui');mkdirSync(base,{recursive:true});const dir=mkdtempSync(path.join(base,'run-'));
 writeFileSync(path.join(dir,'test.js'),out.outputFiles[0].text);const html=path.join(dir,'test.html');writeFileSync(html,'<meta charset="utf-8"><div id="root"></div><script src="test.js"></script>');
 const dom=execFileSync(browser,['--headless=new','--disable-gpu','--no-first-run','--disable-background-networking','--host-resolver-rules=MAP * ~NOTFOUND','--virtual-time-budget=12000','--user-data-dir='+path.join(dir,'profile'),'--dump-dom',pathToFileURL(html).href],{encoding:'utf8',timeout:60000,windowsHide:true,stdio:['ignore','pipe','pipe'],maxBuffer:4000000});writeFileSync(path.join(dir,'result.html'),dom);
 assert.ok(/data-result="passed"/.test(dom),dom.match(/data-error="([^"]*)"/)?.[1]||'browser did not finish');
});

