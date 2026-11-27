import test from 'node:test';import assert from 'node:assert/strict';
import {modeCreationControls,applyMultiModePreset,buildMultiModeCreationRequest,modeCreationSettings} from './batchFactoryMultiModeCreation.js';
test('four modes retain independent presets/settings and reuse one request identity',()=>{
 const modes=['video','comic','illustrated_text','text'];const settingsByMode={};modes.forEach((mode,i)=>{settingsByMode[mode]={presetId:'p'+i,settings:{textFontSize:42+i,aiPromptConfig:{comic:{style:{presetId:'s1'},styles:[{presetId:'s1'},{presetId:'s2'},{presetId:'s3'}]}}}}});
 const value={requestId:'same-retry',title:'books',books:[{title:'b'}],modes,settingsByMode};const a=buildMultiModeCreationRequest(value);const b=buildMultiModeCreationRequest(value);
 assert.equal(a.requestId,b.requestId);assert.equal(a.projects.length,4);assert.equal(a.projects[1].settings.aiPromptConfig.comic.styles.length,3);
 assert.equal(a.projects[2].settings.productionMode,'comic');assert.equal(a.projects[2].settings.aiPromptConfig.comic.presetId,'batch-comic-page-illustrated-text');
 a.projects[0].settings.textFontSize=100;assert.equal(a.projects[3].settings.textFontSize,45);assert.equal(settingsByMode.video.settings.textFontSize,42);
});
test('wrong mode and six style preset fail without changing drafts',()=>{
 const drafts={text:{settings:{textFont:'Long Cang'}}};const before=JSON.stringify(drafts);
 assert.throws(()=>applyMultiModePreset(drafts,'comic',{id:'video',config:{productionMode:'video'}}));
 assert.throws(()=>applyMultiModePreset(drafts,'comic',{id:'six',config:{productionMode:'comic',aiPromptConfig:{comic:{styles:Array.from({length:6},(_,i)=>({presetId:'s'+i}))}}}}));
 assert.equal(JSON.stringify(drafts),before);assert.equal(modeCreationSettings('text',{textBackgroundColor:'#112233'}).textBackgroundColor,'#112233');
});

test('switching modes restores each modes outer capture controls',()=>{
 const a=modeCreationControls({contentRange:8000,contentRangeLines:12,comicAnalysisCharacters:3000});
 const b=modeCreationControls({contentCaptureCharacters:6000,contentRangeLines:8,comicAnalysisCharacters:1800});
 assert.deepEqual(a,{captureCharacters:8000,contentRangeLines:12,comicAnalysisCharacters:3000});
 assert.equal(b.captureCharacters,6000);assert.equal(modeCreationControls({}).captureCharacters,4000);
});
