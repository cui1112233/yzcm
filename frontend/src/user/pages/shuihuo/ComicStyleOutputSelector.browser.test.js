import assert from 'node:assert/strict';
import { execFileSync } from 'node:child_process';
import { existsSync,mkdtempSync,writeFileSync,rmSync } from 'node:fs';
import {tmpdir} from 'node:os';
import {join} from 'node:path';
import {fileURLToPath,pathToFileURL} from 'node:url';
import test from 'node:test';
import {build} from 'esbuild';
const browser=['C:/Program Files/Google/Chrome/Application/chrome.exe','C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(existsSync);

test('one project selects five outputs and the media selector only views',{skip:!browser},async()=>{
 const dir=mkdtempSync(join(tmpdir(),'comic-styles-'));
 try{
  const bundle=await build({stdin:{resolveDir:fileURLToPath(new URL('.',import.meta.url)),loader:'jsx',contents:`
   import React,{useState} from 'react';import{createRoot}from'react-dom/client';
   import {BatchFactoryComicStylePicker} from './BatchFactoryComicStylePicker.jsx';
   import Selector from './ComicStyleOutputSelector.jsx';
   const catalog=Array.from({length:6},(_,i)=>({id:'s'+i,name:'画风'+i,slot:'batch.comic-style',previewUrl:'data:image/gif;base64,R0lGODlhAQABAAAAACw='}));
   function App(){const[value,setValue]=useState({style:{presetId:'s0',presetName:'画风0'}});const[chosen,setChosen]=useState('s0');
    window.state=value;window.selected=chosen;
    return <><BatchFactoryComicStylePicker catalog={catalog} value={value} onChange={setValue}/><Selector styles={value.styles||[value.style]} value={chosen} onChange={setChosen}/></>;
   }
   const wait=async f=>{for(let i=0;i<90;i++){if(f())return;await new Promise(r=>setTimeout(r,30))}throw Error('wait timeout')};
   createRoot(document.getElementById('root')).render(<App/>);
   async function run(){await wait(()=>document.querySelectorAll('.batch-factory-comic-style-card').length===6);
    for(let n=1;n<5;n++){
     document.querySelectorAll('.batch-factory-comic-style-card button')[n].click();
     await wait(()=>document.querySelector('.batch-factory-comic-coverflow-caption b')?.textContent==='画风'+n);
     if((window.state.styles||[window.state.style]).length!==n)throw Error('browsing mutated output list');
     [...document.querySelectorAll('button')].find(e=>e.textContent==='加入输出').click();
     await wait(()=>window.state.styles?.length===n+1);
    }
    document.querySelectorAll('.batch-factory-comic-style-card button')[5].click();await wait(()=>document.querySelector('.batch-factory-comic-coverflow-caption b')?.textContent==='画风5');
    if(![...document.querySelectorAll('button')].find(e=>e.textContent==='加入输出')?.disabled)throw Error('sixth output enabled');
    if(!document.body.textContent.includes('已选 5/5'))throw Error('missing count');
    const select=document.querySelector('[aria-label="查看漫画画风"]').closest('.ant-select');
    select.querySelector('.ant-select-selector').dispatchEvent(new MouseEvent('mousedown',{bubbles:true}));
    await wait(()=>document.querySelectorAll('.ant-select-item-option').length>=5);
    [...document.querySelectorAll('.ant-select-item-option')].find(e=>e.textContent==='画风3').click();
    await wait(()=>window.selected==='s3');
    if(window.state.styles.length!==5)throw Error('view changed production config');
    document.body.dataset.result='passed';
   }run().catch(e=>document.body.dataset.error=e.message);
  `},bundle:true,write:false,format:'iife',jsx:'automatic',loader:{'.css':'empty'}});
  writeFileSync(join(dir,'app.js'),bundle.outputFiles[0].text);
  writeFileSync(join(dir,'index.html'),'<div id="root"></div><script src="app.js"></script>');
  const out=execFileSync(browser,['--headless=new','--no-sandbox','--disable-gpu','--allow-file-access-from-files','--virtual-time-budget=12000','--dump-dom',pathToFileURL(join(dir,'index.html')).href],{encoding:'utf8',timeout:30000,stdio:['ignore','pipe','ignore']});
  assert.match(out,/data-result="passed"/,out.match(/data-error="[^"]*/)?.[0]||out.slice(-700));
 }finally{rmSync(dir,{recursive:true,force:true})}
});
