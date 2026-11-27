import assert from 'node:assert/strict';
import test from 'node:test';
import * as styles from './batchFactoryComicStyle.js';

test('output styles preserve legacy selection and enforce distinct maximum five',()=>{
 assert.equal(typeof styles.normalizeComicOutputStyles,'function');
 assert.equal(styles.normalizeComicOutputStyles({style:{presetId:'old'},globalReferenceImage:{url:'old-ref'}})[0].presetId,'old');
 const five=Array.from({length:5},(_,i)=>({presetId:`s${i}`}));
 assert.equal(styles.normalizeComicOutputStyles({styles:five}).length,5);
 assert.throws(()=>styles.normalizeComicOutputStyles({styles:[...five,{presetId:'six'}]}));
 assert.throws(()=>styles.normalizeComicOutputStyles({styles:[five[0],five[0]]}));
 assert.throws(()=>styles.normalizeComicOutputStyles({styles:[{}]}));
});

test('selecting style keeps independent references, shared prompt and original value',()=>{
 assert.equal(typeof styles.selectComicOutputStyle,'function');
 const original={presetId:'pages',style:{presetId:'old'},globalReferenceImage:{assetId:'upload-old',url:'old-ref'}};
 const first=styles.selectComicOutputStyle(original,{id:'new',name:'New',previewUrl:'new-preview'});
 assert.deepEqual(original,{presetId:'pages',style:{presetId:'old'},globalReferenceImage:{assetId:'upload-old',url:'old-ref'}});
 assert.equal(first.presetId,'pages');assert.equal(first.styles.length,2);
 assert.equal(first.styles[0].globalReferenceImage.url,'old-ref');
 assert.notEqual(first.styles[1].globalReferenceImage.url,'old-ref');
 const removed=styles.selectComicOutputStyle(first,{id:'old'});
 assert.equal(removed.styles.length,1);assert.equal(removed.style.presetId,'new');
 assert.throws(()=>styles.selectComicOutputStyle(removed,{id:'new'}),/至少/);
});
