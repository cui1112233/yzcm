import assert from 'node:assert/strict';
import test from 'node:test';
import { comicProductionView,comicScheduleRequest,comicProductionShotKey, buildComicTimeline, safeComicMediaUrl, comicSubtitleSRT } from './comic-screenplay-workflow.js';
test('镜头生成结束与可交付成片明确区分',()=>{
  assert.equal(comicProductionView({status:'clips_ready'}).deliveryComplete,false);
  assert.equal(comicProductionView({status:'clips_ready'}).label,'镜头生成完成，待编辑与合成');
  assert.equal(comicProductionView({status:'running'}).active,true);
});
test('媒体地址拒绝脚本协议',()=>{
  assert.equal(safeComicMediaUrl('javascript:alert(1)'), '');
  assert.equal(safeComicMediaUrl('/api/ai-cut/projects/p/assets/a/content'),'/api/ai-cut/projects/p/assets/a/content');
});
test('时间线按顺序编排视频，音频保留自己的起始时间',()=>{
  const items=[{id:'v',kind:'video'},{id:'a',kind:'audio'}];
  assert.deepEqual(buildComicTimeline([{itemId:'a',durationMs:3000,startMs:2000,order:1},{itemId:'v',durationMs:4000,order:0}],items).map(item=>[item.itemId,item.startMs]),[['v',0],['a',2000]]);
  assert.throws(()=>buildComicTimeline([{itemId:'x',durationMs:1000}],items),/素材/);
  assert.throws(()=>buildComicTimeline([{itemId:'v',durationMs:0}],items),/时长/);
});
test('字幕导出为 SRT，毫秒与时间线一致且跳过空行',()=>{
  assert.equal(comicSubtitleSRT([{text:'开场',startMs:0,durationMs:1500},{text:'  ',startMs:2000,durationMs:1000},{text:'第二句',startMs:3723004,durationMs:2500}]),'1\n00:00:00,000 --> 00:00:01,500\n开场\n\n2\n01:02:03,004 --> 01:02:05,504\n第二句\n');
  assert.equal(comicSubtitleSRT([]),'');
});
test('成片状态必须有第八步真实交付地址',()=>{
  assert.equal(comicProductionView({status:'completed'}).deliveryComplete,false);
  assert.equal(comicProductionView({status:'completed',batch:{steps:{'8_finalize':{status:'done',artifacts:['project://p/completed']}}}}).deliveryComplete,false);
  const complete=comicProductionView({status:'completed',batch:{steps:{'8_finalize':{status:'done',artifacts:['/api/ai-cut/export-jobs/export-1/output']}}}});
  assert.equal(complete.deliveryComplete,true);assert.deepEqual(complete.deliverables,['/api/ai-cut/export-jobs/export-1/output']);
  assert.equal(comicProductionView({status:'clips_ready',batch:{steps:{'8_finalize':{status:'done',artifacts:['https://media.example/clip.mp4']}}}}).deliveryComplete,false);
});
test('任务历史按请求版本区分同一镜头',()=>{
  const shot={project_id:'p',storyboard_id:'b',episode:1,seq:2,shot_id:'s'};
  assert.notEqual(comicProductionShotKey({...shot,request_hash:'old'}),comicProductionShotKey({...shot,request_hash:'new'}));
});
test('定时制作显示等待时间，未来时间使用本地输入转成明确时区',()=>{
 const now=Date.parse('2026-11-14T00:00:00Z');assert.throws(()=>comicScheduleRequest('2026-11-13T23:00:00Z',now),/未来/);assert.throws(()=>comicScheduleRequest('',now),/未来/);
 assert.deepEqual(comicScheduleRequest('2026-11-14T01:00:00Z',now),{run_at:'2026-11-14T01:00:00.000Z'});
 const view=comicProductionView({status:'scheduled',run_at:'2026-11-14T01:00:00Z'});assert.equal(view.scheduled,true);assert.equal(view.active,false);assert.equal(view.label,'等待定时启动');
});

test('138分镜等待与质检阻断分开显示，仅明确待续批状态可继续',()=>{
 const data=status=>({status:'blocked',batch:{steps:{'5_storyboard':{episodes:{1:{episode_no:1,storyboard_generation:{request_key:'batch-138',chapter_id:'c1',status}}}}}}});
 const waiting=comicProductionView(data('awaiting_continue'));
 assert.equal(waiting.label,'等待确认继续分镜');assert.equal(waiting.active,false);
 assert.equal(waiting.storyboardWaits[0].canContinue,true);assert.equal(waiting.storyboardWaits[0].requestKey,'batch-138');
 for(const status of ['calling','unknown','cancelled'])assert.equal(comicProductionView(data(status)).storyboardWaits[0].canContinue,false);
 assert.equal(comicProductionView({status:'blocked'}).label,'等待处理质检问题');
 assert.equal(comicProductionView(data('succeeded')).storyboardWaits.length,0);
 assert.equal(comicProductionView(data('unknown')).label,'分镜结果待核对');
 assert.equal(comicProductionView({...data('awaiting_continue'),status:'running'}).storyboardWaits[0].canContinue,false);
});

test('138生产未知、调用中或取消状态可手动恢复已返回回包',()=>{
 for(const status of ['awaiting_continue','unknown','calling','cancelled']){
 const data={status:'blocked',batch:{steps:{'5_storyboard':{episodes:{1:{episode_no:1,storyboard_generation:{request_key:'batch-138',status}}}}}}};
 assert.equal(comicProductionView(data).storyboardWaits[0].canRecover,['unknown','calling','cancelled'].includes(status));
 }
});

test('暂停保留138等待时仍能明确继续或免费恢复',()=>{
 const data=status=>({status:'paused',batch:{steps:{'5_storyboard':{episodes:{1:{episode_no:1,storyboard_generation:{request_key:'batch-138',status}}}}}}});
 assert.equal(comicProductionView(data('awaiting_continue')).storyboardWaits[0].canContinue,true);
 for(const status of ['unknown','calling','cancelled']){
  const wait=comicProductionView(data(status)).storyboardWaits[0];assert.equal(wait.canContinue,false);assert.equal(wait.canRecover,true);
 }
 assert.deepEqual(comicProductionView({status:'paused'}).storyboardWaits,[]);
});

test('138失败保留核验回包入口，不能把failed当成明确续批授权',()=>{
 for(const jobStatus of ['blocked','paused']){
  const data={status:jobStatus,batch:{steps:{'5_storyboard':{episodes:{1:{episode_no:1,storyboard_generation:{request_key:'received-save-failed',status:'failed'}}}}}}};
  const view=comicProductionView(data);assert.equal(view.storyboardWaits.length,1);assert.equal(view.storyboardWaits[0].canRecover,true);assert.equal(view.storyboardWaits[0].canContinue,false);
 }
 const noKey=comicProductionView({status:'blocked',batch:{steps:{'5_storyboard':{episodes:{1:{episode_no:1,storyboard_generation:{status:'failed'}}}}}}});
 assert.equal(noKey.storyboardWaits[0].canRecover,false);
});
