import test from 'node:test';
import assert from 'node:assert/strict';
import {fullBoardEvidence,makeShotRange,mergeShotPreview,replaceBoardReference} from './comic-assistant-shot-references.js';
test('范围必须非空，以board身份互斥，按真实顺序传ID并保留两种摘要',()=>{
 const board={kind:'storyboard',id:'b',name:'第1集',revision:2,digest:'a'.repeat(64)};
 assert.throws(()=>makeShotRange(board,[]),/选择/);
 const range=makeShotRange(board,[{id:'actual-101',seq:101,position:100},{id:'actual-1',seq:1,position:0}]);
 assert.deepEqual(range.shot_ids,['actual-1','actual-101']);
 const selected=replaceBoardReference([board,{kind:'asset_text',id:'a'}],range);assert.equal(selected.length,2);assert.equal(selected.filter(r=>r.id==='b').length,1);
 const previews=mergeShotPreview([{...range,digest:'c'.repeat(64),board_evidence:undefined,shot_rows:undefined}],selected);
 assert.equal(previews[0].digest,'c'.repeat(64));assert.equal(fullBoardEvidence(previews[0]).digest,board.digest);
 assert.equal(replaceBoardReference(previews,board)[0].shot_ids,undefined);
 assert.throws(()=>makeShotRange(board,Array.from({length:201},(_,i)=>({id:'s'+i,seq:i,position:i}))),/200/);
 const sameIDAsset={kind:'asset_text',id:'b',digest:'d'.repeat(64)};
 assert.deepEqual(mergeShotPreview([sameIDAsset],[range]),[sameIDAsset]);
});
