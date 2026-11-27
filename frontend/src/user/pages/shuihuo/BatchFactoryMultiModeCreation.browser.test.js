import assert from 'node:assert/strict';import test from 'node:test';
import {execFileSync}from'node:child_process';import{existsSync,mkdtempSync,writeFileSync,rmSync}from'node:fs';import{tmpdir}from'node:os';import{join}from'node:path';import{fileURLToPath,pathToFileURL}from'node:url';import{build}from'esbuild';
const browser=['C:/Program Files/Google/Chrome/Application/chrome.exe','C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(existsSync);
test('four mode rows choose independent presets and submit one retained request',{skip:!browser},async()=>{
 const dir=mkdtempSync(join(tmpdir(),'batch-modes-'));
 try{
 const bundle=await build({stdin:{resolveDir:fileURLToPath(new URL('.',import.meta.url)),loader:'jsx',contents:`
  import React,{useState}from'react';import{createRoot}from'react-dom/client';
  import Fields from './BatchFactoryMultiModeCreationFields.jsx';
  import{applyMultiModePreset,buildMultiModeCreationRequest}from'./batchFactoryMultiModeCreation.js';
  const options=['video','comic','illustrated_text','text'];
  const presets=options.map((mode,i)=>({id:'p'+i,name:'预设'+i,config:{productionMode:mode==='illustrated_text'?'comic':mode,textFontSize:40+i,...(mode==='comic'?{aiPromptConfig:{comic:{styles:[{presetId:'s1'},{presetId:'s2'},{presetId:'s3'}]}}}:{})}}));
  let submissions=[];
  function App(){const[modes,setModes]=useState(['video']);const[drafts,setDrafts]=useState({});const[active,setActive]=useState('video');window.current={modes,drafts,active};
   return <><Fields modes={modes} onModesChange={setModes} drafts={drafts} activeMode={active} presets={presets}
    onPresetChange={(mode,id)=>setDrafts(current=>applyMultiModePreset(current,mode,presets.find(p=>p.id===id)))}
    onExecutionChange={(mode,value)=>setDrafts(current=>({...current,[mode]:{...current[mode],executionMode:value}}))} onConfigure={setActive}/>
    <button id="submit" onClick={()=>{submissions.push(buildMultiModeCreationRequest({requestId:'retry-same',title:'books',modes,settingsByMode:drafts}));window.submissions=submissions}}>创建</button></>;
  }
  const wait=async f=>{for(let i=0;i<90;i++){if(f())return;await new Promise(r=>setTimeout(r,30))}throw Error('wait timeout '+document.body.dataset.step+' '+JSON.stringify(window.current))};
  createRoot(document.getElementById('root')).render(<App/>);
  async function run(){await wait(()=>document.querySelectorAll('.ant-checkbox-input').length===4);
   for(const index of [1,2,3]){document.querySelectorAll('.ant-checkbox-input')[index].click();await wait(()=>window.current.modes.length===index+1);await new Promise(r=>setTimeout(r,60))}
   for(let i=0;i<4;i++){
    document.body.dataset.step='preset '+i;
    const row=document.querySelectorAll('.batch-factory-multimode-project')[i];row.querySelector('.ant-select-selector').dispatchEvent(new MouseEvent('mousedown',{bubbles:true}));
    const menu=()=>document.getElementById(row.querySelector('[role=combobox]').getAttribute('aria-controls'))?.closest('.ant-select-dropdown');
    await wait(()=>menu() && [...menu().querySelectorAll('.ant-select-item-option-content')].some(e=>e.textContent==='预设'+i));
    [...menu().querySelectorAll('.ant-select-item-option-content')].find(e=>e.textContent==='预设'+i).click();await wait(()=>window.current.drafts[options[i]]?.presetId==='p'+i);
    await new Promise(r=>setTimeout(r,60));
    row.querySelector('button').click();await wait(()=>window.current.active===options[i]);
   }
   document.getElementById('submit').click();await wait(()=>window.submissions?.length===1);
   const result=window.submissions[0];if(result.projects.length!==4)throw Error('missing children');
   if(result.projects[1].settings.aiPromptConfig.comic.styles.length!==3)throw Error('comic style draft lost');
   if(result.projects[3].settings.textFontSize!==43)throw Error('text draft overwritten');
   document.getElementById('submit').click();await wait(()=>window.submissions?.length===2);if(window.submissions[1].requestId!==result.requestId)throw Error('retry identity changed');
   document.body.dataset.result='passed';
  }run().catch(e=>document.body.dataset.error=e.message);
 `},bundle:true,write:false,jsx:'automatic',format:'iife',loader:{'.css':'empty'}});
 writeFileSync(join(dir,'app.js'),bundle.outputFiles[0].text);writeFileSync(join(dir,'index.html'),'<div id="root"></div><script src="app.js"></script>');
 const out=execFileSync(browser,['--headless=new','--no-sandbox','--disable-gpu','--allow-file-access-from-files','--virtual-time-budget=12000','--dump-dom',pathToFileURL(join(dir,'index.html')).href],{encoding:'utf8',timeout:30000,stdio:['ignore','pipe','ignore']});assert.match(out,/data-result="passed"/,out.match(/data-error="[^"]*/)?.[0]||out.slice(-500));
 }finally{rmSync(dir,{recursive:true,force:true})}
});
