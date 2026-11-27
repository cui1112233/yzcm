import { normalizeComicOutputStyles } from './batchFactoryComicStyle.js';
export const CREATION_MODE_OPTIONS=[{value:'video',label:'视频生成'},{value:'comic',label:'漫画生成'},{value:'illustrated_text',label:'文字插画'},{value:'text',label:'文本生成'}];
const clone=value=>JSON.parse(JSON.stringify(value||{}));
export const creationExecutionMode=mode=>mode==='illustrated_text'?'comic':mode;
export function modeCreationSettings(mode,settings={}){
 const next=clone(settings);next.productionMode=creationExecutionMode(mode);next.outputKind=mode;
 if(mode==='illustrated_text')next.aiPromptConfig={...(next.aiPromptConfig||{}),comic:{...(next.aiPromptConfig?.comic||{}),enabled:true,presetId:'batch-comic-page-illustrated-text'}};
 if(next.aiPromptConfig?.comic)normalizeComicOutputStyles(next.aiPromptConfig.comic);
 return next;
}
export function applyMultiModePreset(drafts,mode,preset){
 const config=preset?.config;if(!config||typeof config!=='object')throw Error('预设内容无效');
 if(config.productionMode&&config.productionMode!==creationExecutionMode(mode))throw Error('该预设不适用于当前生产模式');
 const settings=modeCreationSettings(mode,config);
 return {...drafts,[mode]:{...(drafts[mode]||{}),settings,presetId:String(preset.id||''),executionMode:drafts[mode]?.executionMode||'full_submit'}};
}
export function buildMultiModeCreationRequest({requestId,title,books=[],manualInput,intakeId,modes,settingsByMode={}}){
 if(!requestId||!title?.trim()||!Array.isArray(modes)||modes.length<1||modes.length>4||new Set(modes).size!==modes.length)throw Error('请选择1～4种不同模式');
 const projects=modes.map(mode=>{
  if(!CREATION_MODE_OPTIONS.some(item=>item.value===mode))throw Error('生产模式无效');
  const value=settingsByMode[mode]||{};
  return {mode,presetId:value.presetId||'',executionMode:value.executionMode||'full_submit',settings:modeCreationSettings(mode,value.settings||{})};
 });
 return {requestId,title:title.trim(),books:clone(books),projects,...(manualInput?{manualInput:clone(manualInput)}:{}),...(intakeId?{intakeId}:{})};
}

export function modeCreationControls(settings={}) {
 return {captureCharacters:Number(settings.contentCaptureCharacters ?? settings.contentRange) || 4000,contentRangeLines:Number(settings.contentRangeLines)||5,comicAnalysisCharacters:Number(settings.comicAnalysisCharacters)||2000};
}
