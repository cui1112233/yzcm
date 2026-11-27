import test from 'node:test';
import assert from 'node:assert/strict';
import * as state from './novel-fetch-process-settings.js';

test('an unavailable saved model remains selected and cannot be silently replaced',()=>{
 const options=[{value:'other',label:'Other'}];
 assert.equal(state.resolveNovelFetchTextModel('saved-missing','account'), 'saved-missing');
 assert.match(state.novelFetchTextModelError('saved-missing',options), /不可用/);
});
test('initial selection uses the explicit account default even if unavailable, never the first option',()=>{
 const options=[{value:'other',label:'Other'}];
 assert.equal(state.resolveNovelFetchTextModel('','account'),'account');
 assert.equal(state.resolveNovelFetchTextModel('',''),'');
 assert.match(state.novelFetchTextModelError('',options),/选择/);
});
test('a valid explicit model is unchanged and can run',()=>{
 assert.equal(state.resolveNovelFetchTextModel('chosen','other'),'chosen');
 assert.equal(state.novelFetchTextModelError('chosen',[{value:'chosen'}]),'');
 assert.match(state.novelFetchTextModelError('chosen',[{value:'chosen',disabled:true}]),/不可用/);
});


test('version profiles keep every supported AI version through a settings round trip', async()=>{
 const {withNovelFetchVersionProfiles,NOVEL_FETCH_PROFILE_VERSIONS}=await import('./novel-fetch-config-defaults.js');
 const profiles=withNovelFetchVersionProfiles({original:'raw',ai1:'p1',ai20:'p20'});
 assert.equal(profiles.ai20,'p20');
 assert.equal(NOVEL_FETCH_PROFILE_VERSIONS.length,21);
 assert.equal(NOVEL_FETCH_PROFILE_VERSIONS.at(-1),'ai20');
 assert.equal(profiles.ai1,'p1');
 assert.equal(profiles.original,'raw');
 assert.equal(profiles.ai21,undefined);
});


test('an explicitly cleared version selection survives save and reload',()=>{
 const values=new Map(); const storage={getItem:key=>values.get(key),setItem:(key,value)=>values.set(key,value)};
 state.writeNovelFetchProcessSettings({selectedVersions:[],textModel:'chosen'},storage);
 const restored=state.readNovelFetchProcessSettings(storage);
 assert.deepEqual(state.resolveNovelFetchSelectedVersions(restored.selectedVersions,['original','ai1','ai20']),[]);
 assert.deepEqual(state.resolveNovelFetchSelectedVersions(undefined,['original','ai1','ai20']),['original','ai1']);
 assert.deepEqual(state.resolveNovelFetchSelectedVersions(['ai20'],['original','ai1','ai20']),['ai20']);
});


test('classification display never treats a placeholder gender as completed',async()=>{
 const {novelFetchClassificationLabel}=await import('./novel-fetch-status.js');
 assert.equal(novelFetchClassificationLabel({gender:'待确认',style:'年代通用',classify_status:'done',classifyError:'需要补填模型Key'}),'需要补填模型Key');
 assert.equal(novelFetchClassificationLabel({gender:'待确认',style:'年代通用'}),'待判断');
 assert.equal(novelFetchClassificationLabel({gender:'女频',style:'年代通用',genderConfidence:0.1}),'已完成');
 assert.equal(novelFetchClassificationLabel({gender:'男频',style:'待判定',classifyStatus:'done'}),'待判断');
});
