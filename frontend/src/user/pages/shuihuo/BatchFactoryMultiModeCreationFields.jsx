import {Button,Checkbox,Select,Tag} from 'antd';
import {CREATION_MODE_OPTIONS,creationExecutionMode} from './batchFactoryMultiModeCreation.js';
import {normalizeComicOutputStyles} from './batchFactoryComicStyle.js';
import './batchFactoryMultiModeCreation.css';

export default function BatchFactoryMultiModeCreationFields({modes,onModesChange,drafts={},activeMode,presets=[],onPresetChange,onExecutionChange,onConfigure,disabled=false}){
 return <section className="batch-factory-multimode-create">
  <Checkbox.Group aria-label="生产模式" value={modes} onChange={value=>{if(value.length)onModesChange(value)}} options={CREATION_MODE_OPTIONS} disabled={disabled}/>
  {modes.length>1||modes[0]==='illustrated_text'?<div className="batch-factory-multimode-projects">{CREATION_MODE_OPTIONS.filter(item=>modes.includes(item.value)).map(item=>{
   const draft=drafts[item.value]||{};const settings=draft.settings||{};let count=0;try{count=normalizeComicOutputStyles(settings.aiPromptConfig?.comic||{}).length}catch{}
   return <div className={`batch-factory-multimode-project${activeMode===item.value?' is-active':''}`} key={item.value}>
    <strong>{item.label}</strong><Select aria-label={`${item.label}预设`} value={draft.presetId||undefined} placeholder="选择预设" disabled={disabled} options={presets.filter(p=>!p.config?.productionMode||p.config.productionMode===creationExecutionMode(item.value)).map(p=>({value:p.id,label:p.name}))} onChange={value=>onPresetChange(item.value,value)}/>
    <Select aria-label={`${item.label}执行模式`} value={draft.executionMode||'full_submit'} disabled={disabled} options={[{value:'full_submit',label:'生成并上传'},{value:'video_no_submit',label:'只生成'},{value:'storyboard_only',label:'只分析'}]} onChange={value=>onExecutionChange(item.value,value)}/>
    <Button size="small" disabled={disabled} onClick={()=>onConfigure(item.value)}>统一配置</Button>{item.value==='comic'&&count?<Tag>{count} 种画风</Tag>:null}
   </div>;
  })}</div>:null}
 </section>;
}
