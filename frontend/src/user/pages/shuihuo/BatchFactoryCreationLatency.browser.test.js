import assert from 'node:assert/strict';
import test from 'node:test';
import { execFileSync } from 'node:child_process';
import { existsSync, mkdtempSync, readFileSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { build } from 'esbuild';

const browser = ['C:/Program Files/Google/Chrome/Application/chrome.exe', 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'].find(existsSync);
const here = dirname(fileURLToPath(import.meta.url));

test('the real create dialog submits before an unavailable original service responds', { skip: !browser }, async () => {
  const dir = mkdtempSync(join(tmpdir(), 'batch-create-latency-'));
  try {
    const mocks = { name: 'creation-services', setup(plugin) {
      plugin.onResolve({ filter: /\/api\/batchFactory(?:\.js)?$/ }, () => ({ path: 'batch', namespace: 'creation-test' }));
      plugin.onResolve({ filter: /\/api\/novelFetchWorkshop(?:\.js)?$/ }, () => ({ path: 'platforms', namespace: 'creation-test' }));
      plugin.onResolve({ filter: /BatchFactoryUnifiedSettingsModal\.jsx$/ }, () => ({ path: 'settings', namespace: 'creation-test' }));
      plugin.onResolve({ filter: /TextProductionSettingsForm$/ }, () => ({ path: 'text-settings', namespace: 'creation-test' }));
      if (process.env.QIANTIE_CREATION_BASELINE_SOURCE) plugin.onLoad({ filter: /BatchFactoryCreateModal\.jsx$/ }, () => ({ contents: readFileSync(process.env.QIANTIE_CREATION_BASELINE_SOURCE, 'utf8'), loader: 'jsx', resolveDir: here }));
      plugin.onLoad({ filter: /.*/, namespace: 'creation-test' }, ({ path }) => {
        if (path === 'settings') return { contents: `export const BatchFactoryAiReasoningForm=()=>null,BatchFactoryAutomationPresetManager=()=>null,BatchFactoryComicPublishSettingsForm=()=>null,BatchFactoryEngineSettingsForm=()=>null,BatchFactoryProductionModeControl=()=>null,BatchFactoryPublishSettingsForm=()=>null;export const PRODUCTION_MODE_OPTIONS=[];` };
        if (path === 'text-settings') return { contents: 'export default()=>null;' };
        if (path === 'platforms') return { contents: 'export const getWorkshopPlatforms=async()=>({platforms:[{id:"15",name:"测试书城"}]});' };
        return { contents: `
          export const listAutomationPresets=async()=>({presets:[{id:'comic',name:'漫画测试预设',config:{productionMode:'comic'}},{id:'text',name:'文本测试预设',config:{productionMode:'text'}}]});
          export const fetchDirectOriginals=()=>{window.originalReads=(window.originalReads||0)+1;return new Promise(()=>{});};
          export const classifyBookPublishMetadata=async()=>({}),createNovelFetchIntake=async()=>({}),fetchBookOriginal=async()=>({}),getBatch=async()=>({}),getBatchAutomationStatus=async()=>({}),listBatches=async()=>({batches:[]}),startBatchAutomation=async()=>({}),updateBookMetadata=async()=>({});
        ` };
      });
    } };
    const bundle = await build({ stdin: { resolveDir: here, loader: 'jsx', contents: `
      import React,{useState}from'react';import{createRoot}from'react-dom/client';
      import{BatchFactoryCreateModal}from'./BatchFactoryCreateModal.jsx';
      window.fetch=()=>Promise.reject(Error('Unexpected network request'));
      function App(){const[open,setOpen]=useState(true);return <><BatchFactoryCreateModal open={open} onCancel={()=>setOpen(false)} onCreated={async value=>{window.created=value;setOpen(false);return{id:'created'};}}/>{!open&&<h1 id="workbench">已进入项目</h1>}</>;}
      const wait=async f=>{for(let n=0;n<100;n++){if(f())return;await new Promise(r=>setTimeout(r,30));}throw Error('timeout '+window.step+' reads='+(window.originalReads||0));};
      const button=text=>[...document.querySelectorAll('button')].find(el=>el.textContent.replace(/\s/g,'')===text);
      const fill=(selector,value)=>{const el=document.querySelector(selector);Object.getOwnPropertyDescriptor(Object.getPrototypeOf(el),'value').set.call(el,value);el.dispatchEvent(new Event('input',{bubbles:true}));};
      const check=async(name,value)=>{const el=[...document.querySelectorAll('.ant-checkbox-wrapper')].find(el=>el.textContent.trim()===name).querySelector('input');if(el.checked!==value)el.click();await wait(()=>el.checked===value);await new Promise(r=>setTimeout(r,40));};
      async function choose(label,text){const el=document.querySelector('[aria-label="'+label+'"]');el.closest('.ant-select').querySelector('.ant-select-selector').dispatchEvent(new MouseEvent('mousedown',{bubbles:true}));await wait(()=>[...document.querySelectorAll('.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option')].some(el=>el.textContent===text));[...document.querySelectorAll('.ant-select-dropdown:not(.ant-select-dropdown-hidden) .ant-select-item-option')].find(el=>el.textContent===text).click();await new Promise(r=>setTimeout(r,40));}
      createRoot(document.getElementById('root')).render(<App/>);
      async function run(){await wait(()=>document.getElementById('batch-title'));window.step='input';fill('#batch-title','快速创建测试');fill('#batch-input-text','738579 一本书');await new Promise(r=>setTimeout(r,50));button('添加书城').click();await wait(()=>document.querySelector('.platform-group-tag'));
        await check('漫画生成',true);await check('文本生成',true);await check('视频生成',false);window.step='presets';await choose('漫画生成预设','漫画测试预设');await choose('文本生成预设','文本测试预设');
        button('立即执行').click();await wait(()=>button('创建并立即执行'));window.step='create';button('创建并立即执行').click();await wait(()=>document.getElementById('workbench'));
        if(window.originalReads)throw Error('creation read originals');if(window.created.multiModeRequest.projects.length!==2)throw Error('lost selected projects');document.body.dataset.result='passed';
      }run().catch(error=>document.body.dataset.error=window.step+' '+error.message);
    ` }, plugins: [mocks], bundle: true, write: false, jsx: 'automatic', format: 'iife', loader: { '.css': 'empty' } });
    writeFileSync(join(dir, 'app.js'), bundle.outputFiles[0].text);
    writeFileSync(join(dir, 'index.html'), '<div id="root"></div><script src="app.js"></script>');
    const html = execFileSync(browser, ['--headless=new', '--no-sandbox', '--disable-gpu', '--allow-file-access-from-files', '--virtual-time-budget=14000', '--dump-dom', pathToFileURL(join(dir, 'index.html')).href], { encoding: 'utf8', timeout: 30000, stdio: ['ignore', 'pipe', 'ignore'] });
    assert.equal(/data-result="passed"/.test(html), true, html.match(/data-error="[^"]*/)?.[0] || html.slice(-800));
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
