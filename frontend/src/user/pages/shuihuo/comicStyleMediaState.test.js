import test from 'node:test';
import assert from 'node:assert/strict';
import { comicStyleMediaKey, isCurrentComicStyleResponse, comicMediaStyleSelection } from './comicStyleMediaState.js';

test('late style response cannot replace a different selection', async () => {
 const a=comicStyleMediaKey('b','k','A'),b=comicStyleMediaKey('b','k','B');
 assert.notEqual(a,b);assert.equal(isCurrentComicStyleResponse(a,b),false);
 let current=a;let view='';let finishA,finishB;
 const pa=new Promise(resolve=>{finishA=resolve}).then(value=>{if(isCurrentComicStyleResponse(a,current))view=value});
 current=b;
 const pb=new Promise(resolve=>{finishB=resolve}).then(value=>{if(isCurrentComicStyleResponse(b,current))view=value});
 finishB('B images');await pb;finishA('A images');await pa;assert.equal(view,'B images');
 assert.equal(comicMediaStyleSelection([{presetId:'B'}],'A'),'B');
});
