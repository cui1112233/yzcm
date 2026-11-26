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

test('助手逐项显示九个读取工具中文名和未知工具回退', {skip:!browser},async()=>{
 await runCheckCase('assistant-read-labels',`
import React from 'react';import{createRoot}from'react-dom/client';import{ComicAssistantPanel}from './ComicScreenplayAuthoringPanels.jsx';import{setToken,setVerifiedImpersonation}from '../../shared/api/client.js';setToken('fixture');setVerifiedImpersonation({actorUsername:'alice',username:'alice',impersonationId:'none'});
const expected={get_current_screenplay:'读取当前剧本',analyze_screenplay:'分析剧本',get_editor_status:'读取编辑状态',get_shot_numbers:'读取镜头序号',get_shot_detail:'读取镜头详情',read_assets:'读取资产',read_shot_video_prompts:'读取视频提示词',read_shot_image_prompts:'读取图片提示词',get_all_shots_voicetext:'读取镜头台词',storyboard_failures:'分镜失败核对',unknown:'只读工具',toString:'只读工具'};
window.fetch=async()=>new Response(JSON.stringify({state:{revision:0,proposals:[],messages:[{id:'m',role:'assistant',content:'已读取',tools:Object.keys(expected).map(name=>({name,ok:true,summary:'实际结果'}))}]}}),{status:200,headers:{'Content-Type':'application/json'}});
(async()=>{createRoot(document.getElementById('root')).render(<ComicAssistantPanel projectId="p" canGenerate={false}/>);for(let i=0;i<50&&!document.querySelector('.comic-assistant-tools');i++)await new Promise(r=>setTimeout(r,100));const tags=[...document.querySelectorAll('.comic-assistant-tools .ant-tag')];if(tags.length!==12)throw Error('missing tool records');Object.values(expected).forEach((label,i)=>{if(tags[i].textContent!==label+' · 实际结果')throw Error('wrong label '+tags[i].textContent+' expected '+label)});document.body.dataset.result='passed';})().catch(e=>{document.body.dataset.result='failed';document.body.dataset.error=e.message});
`);
});

