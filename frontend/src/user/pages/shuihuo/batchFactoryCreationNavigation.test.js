import assert from 'node:assert/strict';
import test from 'node:test';
import { readFileSync } from 'node:fs';
const source=readFileSync(new URL('../ShuihuoProductionPage.jsx',import.meta.url),'utf8');
const handler=source.slice(source.indexOf('  async function handleCreateBatch(input) {'),source.indexOf('\n  useEffect(() => {',source.indexOf('  async function handleCreateBatch(input) {')));
function fixture(automatic){
  const events=[];let release;
  const delayed=new Promise(resolve=>{release=resolve;});
  const result={groupId:'group',batches:[{id:'comic'},{id:'text'}],projects:[{batchId:'comic'},{batchId:'text'}]};
  const deps={createMultiModeBatches:async()=>result,localStorage:{setItem(){}},BATCH_FACTORY_ACTIVE_BATCH_STORAGE_KEY:'active',setActiveBatchProject:()=>events.push('open'),setView:()=>{},refreshProjects:()=>new Promise(()=>{}),message:{warning:()=>events.push('warning'),success(){}},startBatchAutomation:()=>{events.push('start');return delayed;},resumeMultiModeSources:()=>{events.push('sources');return delayed;},mountedRef:{current:true}};
  const create=Function(...Object.keys(deps),`${handler};return handleCreateBatch;`)(...Object.values(deps));
  return {events,release:()=>release(result),run:()=>create({multiModeRequest:{},automationEnabled:automatic})};
}
test('automatic projects open before project refresh or workers finish',async()=>{
  const f=fixture(true);const pending=f.run();await new Promise(resolve=>setImmediate(resolve));
  assert.deepEqual(f.events,['open','start','start']);f.release();await pending;
});
test('manual multi-mode projects open first and still prepare their shared originals',async()=>{
  const f=fixture(false);const pending=f.run();await new Promise(resolve=>setImmediate(resolve));
  assert.deepEqual(f.events,['open','sources']);f.release();await pending;
});
