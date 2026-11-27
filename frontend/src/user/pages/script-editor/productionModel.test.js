import test from 'node:test'; import assert from 'node:assert/strict'; import { PRODUCTION_STAGES } from './productionModel.js';
test('production tabs cover image video voice subtitle cut and export',()=>assert.deepEqual(PRODUCTION_STAGES.map(x=>x.key),['image','video','voice','subtitle','cut','export']));
